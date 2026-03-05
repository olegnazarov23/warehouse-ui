package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"warehouse-ui/internal/logger"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

func init() {
	Register("openai", func(apiKey, endpoint string) Provider {
		return &OpenAIProvider{apiKey: apiKey, endpoint: endpoint}
	})
}

// OpenAIProvider implements Provider for OpenAI-compatible APIs.
type OpenAIProvider struct {
	apiKey   string
	endpoint string // custom endpoint (e.g. Azure, local proxy)
}

func (p *OpenAIProvider) Name() string         { return "openai" }
func (p *OpenAIProvider) DefaultModel() string { return "gpt-4o" }
func (p *OpenAIProvider) MinModel() string     { return "gpt-4o, o3, gpt-5.2" }
func (p *OpenAIProvider) IsConfigured() bool   { return p.apiKey != "" }

func (p *OpenAIProvider) client() *openai.Client {
	cfg := openai.DefaultConfig(p.apiKey)
	if p.endpoint != "" {
		cfg.BaseURL = p.endpoint
	}
	return openai.NewClientWithConfig(cfg)
}

// buildOpenAITools converts CodeTools + DataTools into OpenAI tool definitions.
func buildOpenAITools(hasCodePaths bool, hasQueryFunc bool) []openai.Tool {
	var allTools []CodeTool
	if hasCodePaths {
		allTools = append(allTools, CodeTools()...)
	}
	if hasQueryFunc {
		allTools = append(allTools, DataTools()...)
	}

	var tools []openai.Tool
	for _, ct := range allTools {
		props := map[string]jsonschema.Definition{}
		var required []string

		if params, ok := ct.Parameters["properties"].(map[string]any); ok {
			for pName, pDef := range params {
				if pm, ok := pDef.(map[string]any); ok {
					def := jsonschema.Definition{
						Type:        jsonschema.DataType(pm["type"].(string)),
						Description: pm["description"].(string),
					}
					props[pName] = def
				}
			}
		}
		if req, ok := ct.Parameters["required"].([]string); ok {
			required = req
		}

		tools = append(tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        ct.Name,
				Description: ct.Description,
				Parameters: jsonschema.Definition{
					Type:       jsonschema.Object,
					Properties: props,
					Required:   required,
				},
			},
		})
	}
	return tools
}

func (p *OpenAIProvider) StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error {
	if !p.IsConfigured() {
		return fmt.Errorf("OpenAI API key not configured")
	}
	if model == "" {
		model = p.DefaultModel()
	}

	c := p.client()
	oaiMsgs := buildOpenAIMessages(messages, schema)

	// Add tools if code paths are linked or query functions are available
	var tools []openai.Tool
	hasCodePaths := schema != nil && len(schema.CodePaths) > 0
	hasQueryFunc := schema != nil && schema.RunQuery != nil
	hasTools := hasCodePaths || hasQueryFunc
	if hasTools {
		tools = buildOpenAITools(hasCodePaths, hasQueryFunc)
	}

	const maxToolRounds = 5

	for round := 0; round < maxToolRounds; round++ {
		// Non-streaming call for tool rounds
		req := openai.ChatCompletionRequest{
			Model:    model,
			Messages: oaiMsgs,
			Tools:    tools,
		}
		applyModelParams(&req)

		resp, err := c.CreateChatCompletion(ctx, req)
		if err != nil {
			return fmt.Errorf("openai complete: %w", err)
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("openai: empty response")
		}

		choice := resp.Choices[0]

		// If the model wants to call tools, execute them and loop
		if choice.FinishReason == openai.FinishReasonToolCalls && len(choice.Message.ToolCalls) > 0 {
			// Append the assistant message with tool calls
			oaiMsgs = append(oaiMsgs, choice.Message)

			for _, tc := range choice.Message.ToolCalls {
				logger.Info("openai tool call: %s args=%s", tc.Function.Name, tc.Function.Arguments)

				var input map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
					input = map[string]any{}
				}

				onChunk(ToolActivityMsg(tc.Function.Name, input))

				var result string
				var execErr error
				if IsDataTool(tc.Function.Name) {
					result, execErr = ExecuteDataTool(ctx, tc.Function.Name, input, schema)
				} else {
					result, execErr = ExecuteTool(tc.Function.Name, input, schema.CodePaths)
				}
				if execErr != nil {
					result = fmt.Sprintf("Error: %v", execErr)
				}

				// Cap tool result size
				if len(result) > 50000 {
					result = result[:50000] + "\n... (truncated)"
				}

				oaiMsgs = append(oaiMsgs, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					Name:       tc.Function.Name,
					ToolCallID: tc.ID,
				})
			}
			continue // loop back to LLM
		}

		// Final text response — stream it for better UX
		// Re-request with streaming for the final response
		req.Stream = true
		req.Tools = nil // no tools for final streaming
		stream, err := c.CreateChatCompletionStream(ctx, req)
		if err != nil {
			// Fallback: just output the non-streamed response
			if choice.Message.Content != "" {
				onChunk(choice.Message.Content)
			}
			return nil
		}
		defer stream.Close()

		for {
			sResp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("openai recv: %w", err)
			}
			if len(sResp.Choices) > 0 && sResp.Choices[0].Delta.Content != "" {
				onChunk(sResp.Choices[0].Delta.Content)
			}
		}
		return nil
	}

	// Max tool rounds exceeded — do a final streaming call without tools
	onChunk("\n\n")
	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: oaiMsgs,
		Stream:   true,
	}
	applyModelParams(&req)
	stream, err := c.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return fmt.Errorf("openai stream: %w", err)
	}
	defer stream.Close()

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("openai recv: %w", err)
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Delta.Content != "" {
			onChunk(resp.Choices[0].Delta.Content)
		}
	}
	return nil
}

func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message, schema *SchemaContext, model string) (string, error) {
	if !p.IsConfigured() {
		return "", fmt.Errorf("OpenAI API key not configured")
	}
	if model == "" {
		model = p.DefaultModel()
	}

	c := p.client()
	oaiMsgs := buildOpenAIMessages(messages, schema)

	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: oaiMsgs,
	}
	applyModelParams(&req)
	resp, err := c.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

// applyModelParams sets optimal generation parameters based on model family.
func applyModelParams(req *openai.ChatCompletionRequest) {
	m := strings.ToLower(req.Model)
	switch {
	case strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		req.ReasoningEffort = "high"
		req.MaxCompletionTokens = 16384
	case strings.HasPrefix(m, "gpt-5"):
		req.MaxCompletionTokens = 16384
	default:
		req.Temperature = 0.2
		req.TopP = 0.95
		req.MaxTokens = 4096
	}
}

func buildOpenAIMessages(messages []Message, schema *SchemaContext) []openai.ChatCompletionMessage {
	systemPrompt := BuildSystemPrompt(schema)
	oaiMsgs := []openai.ChatCompletionMessage{
		{Role: "system", Content: systemPrompt},
	}
	for _, m := range messages {
		role := m.Role
		if role == "user" || role == "assistant" {
			oaiMsgs = append(oaiMsgs, openai.ChatCompletionMessage{
				Role:    role,
				Content: m.Content,
			})
		}
	}
	return oaiMsgs
}

// ExtractSQL pulls SQL from the AI response (looks for ```sql blocks).
func ExtractSQL(response string) string {
	start := strings.Index(response, "```sql")
	if start == -1 {
		start = strings.Index(response, "```SQL")
	}
	if start == -1 {
		return ""
	}
	nlPos := strings.Index(response[start:], "\n")
	if nlPos == -1 {
		return ""
	}
	remaining := response[start+nlPos+1:]
	end := strings.Index(remaining, "```")
	if end == -1 {
		return strings.TrimSpace(remaining)
	}
	return strings.TrimSpace(remaining[:end])
}
