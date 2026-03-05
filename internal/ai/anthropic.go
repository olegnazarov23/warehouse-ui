package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"warehouse-ui/internal/logger"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func init() {
	Register("anthropic", func(apiKey, endpoint string) Provider {
		return &AnthropicProvider{apiKey: apiKey, endpoint: endpoint}
	})
}

// AnthropicProvider implements Provider for Claude.
type AnthropicProvider struct {
	apiKey   string
	endpoint string
}

func (p *AnthropicProvider) Name() string         { return "anthropic" }
func (p *AnthropicProvider) DefaultModel() string { return "claude-sonnet-4-20250514" }
func (p *AnthropicProvider) MinModel() string     { return "claude-sonnet-4, claude-opus-4" }
func (p *AnthropicProvider) IsConfigured() bool   { return p.apiKey != "" }

func (p *AnthropicProvider) client() anthropic.Client {
	opts := []option.RequestOption{
		option.WithAPIKey(p.apiKey),
	}
	if p.endpoint != "" {
		opts = append(opts, option.WithBaseURL(p.endpoint))
	}
	return anthropic.NewClient(opts...)
}

// buildAnthropicTools converts CodeTools + DataTools into Anthropic tool definitions.
func buildAnthropicTools(hasCodePaths bool, hasQueryFunc bool) []anthropic.ToolUnionParam {
	var allTools []CodeTool
	if hasCodePaths {
		allTools = append(allTools, CodeTools()...)
	}
	if hasQueryFunc {
		allTools = append(allTools, DataTools()...)
	}

	var tools []anthropic.ToolUnionParam
	for _, ct := range allTools {
		props := map[string]any{}
		var required []string

		if params, ok := ct.Parameters["properties"].(map[string]any); ok {
			for pName, pDef := range params {
				if pm, ok := pDef.(map[string]any); ok {
					props[pName] = map[string]any{
						"type":        pm["type"],
						"description": pm["description"],
					}
				}
			}
		}
		if req, ok := ct.Parameters["required"].([]string); ok {
			required = req
		}

		tools = append(tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        ct.Name,
				Description: anthropic.String(ct.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: props,
					Required:   required,
				},
			},
		})
	}
	return tools
}

func (p *AnthropicProvider) StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error {
	if !p.IsConfigured() {
		return fmt.Errorf("Anthropic API key not configured")
	}
	if model == "" {
		model = p.DefaultModel()
	}

	c := p.client()
	systemPrompt := BuildSystemPrompt(schema)

	// Build initial message list
	var anthropicMsgs []anthropic.MessageParam
	for _, m := range messages {
		switch m.Role {
		case "user":
			anthropicMsgs = append(anthropicMsgs, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case "assistant":
			anthropicMsgs = append(anthropicMsgs, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	// Add tools if code paths are linked or query functions are available
	var tools []anthropic.ToolUnionParam
	hasCodePaths := schema != nil && len(schema.CodePaths) > 0
	hasQueryFunc := schema != nil && schema.RunQuery != nil
	hasTools := hasCodePaths || hasQueryFunc
	if hasTools {
		tools = buildAnthropicTools(hasCodePaths, hasQueryFunc)
	}

	const maxToolRounds = 5

	for round := 0; round < maxToolRounds; round++ {
		params := anthropic.MessageNewParams{
			Model: anthropic.Model(model),
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages: anthropicMsgs,
			Tools:    tools,
		}
		applyAnthropicParams(&params, model)

		// Non-streaming call for tool rounds
		resp, err := c.Messages.New(ctx, params)
		if err != nil {
			return fmt.Errorf("anthropic complete: %w", err)
		}

		// Check if the model wants to call tools
		hasToolUse := false
		var assistantContent []anthropic.ContentBlockParamUnion
		var toolResults []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				assistantContent = append(assistantContent, anthropic.NewTextBlock(v.Text))
			case anthropic.ToolUseBlock:
				hasToolUse = true
				assistantContent = append(assistantContent, anthropic.NewToolUseBlock(v.ID, json.RawMessage(v.Input), v.Name))

				logger.Info("anthropic tool call: %s input=%s", v.Name, string(v.Input))

				var input map[string]any
				if err := json.Unmarshal(v.Input, &input); err != nil {
					input = map[string]any{}
				}

				onChunk(ToolActivityMsg(v.Name, input))

				var result string
				var execErr error
				if IsDataTool(v.Name) {
					result, execErr = ExecuteDataTool(ctx, v.Name, input, schema)
				} else {
					result, execErr = ExecuteTool(v.Name, input, schema.CodePaths)
				}
				if execErr != nil {
					result = fmt.Sprintf("Error: %v", execErr)
				}

				// Cap tool result size
				if len(result) > 50000 {
					result = result[:50000] + "\n... (truncated)"
				}

				toolResults = append(toolResults, anthropic.NewToolResultBlock(v.ID, result, execErr != nil))
			}
		}

		if hasToolUse {

			// Append assistant message with tool calls
			anthropicMsgs = append(anthropicMsgs, anthropic.NewAssistantMessage(assistantContent...))
			// Append user message with tool results
			anthropicMsgs = append(anthropicMsgs, anthropic.NewUserMessage(toolResults...))
			continue // loop back to LLM
		}

		// Final text response — stream it
		params.Tools = nil // no tools for final streaming
		stream := c.Messages.NewStreaming(ctx, params)
		defer stream.Close()

		for stream.Next() {
			event := stream.Current()
			switch variant := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				switch delta := variant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if delta.Text != "" {
						onChunk(delta.Text)
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			return fmt.Errorf("anthropic stream: %w", err)
		}
		return nil
	}

	// Max tool rounds exceeded — do a final streaming call without tools
	onChunk("\n\n")
	params := anthropic.MessageNewParams{
		Model: anthropic.Model(model),
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: anthropicMsgs,
	}
	applyAnthropicParams(&params, model)
	stream := c.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	for stream.Next() {
		event := stream.Current()
		switch variant := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			switch delta := variant.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if delta.Text != "" {
					onChunk(delta.Text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("anthropic stream: %w", err)
	}
	return nil
}

func (p *AnthropicProvider) Complete(ctx context.Context, messages []Message, schema *SchemaContext, model string) (string, error) {
	if !p.IsConfigured() {
		return "", fmt.Errorf("Anthropic API key not configured")
	}
	if model == "" {
		model = p.DefaultModel()
	}

	c := p.client()
	systemPrompt := BuildSystemPrompt(schema)

	var anthropicMsgs []anthropic.MessageParam
	for _, m := range messages {
		switch m.Role {
		case "user":
			anthropicMsgs = append(anthropicMsgs, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case "assistant":
			anthropicMsgs = append(anthropicMsgs, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	params := anthropic.MessageNewParams{
		Model: anthropic.Model(model),
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: anthropicMsgs,
	}
	applyAnthropicParams(&params, model)
	resp, err := c.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic complete: %w", err)
	}

	var sb strings.Builder
	for _, block := range resp.Content {
		if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(textBlock.Text)
		}
	}
	return sb.String(), nil
}

// applyAnthropicParams sets optimal generation parameters based on model family.
func applyAnthropicParams(params *anthropic.MessageNewParams, model string) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus-4"), strings.Contains(m, "sonnet-4"):
		params.MaxTokens = int64(8192)
		params.Temperature = anthropic.Float(0.2)
		params.TopP = anthropic.Float(0.95)
	default:
		params.MaxTokens = int64(4096)
		params.Temperature = anthropic.Float(0.2)
		params.TopP = anthropic.Float(0.95)
	}
}
