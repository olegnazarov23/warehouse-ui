package ai

import (
	"context"
	"fmt"
	"io"
	"strings"

	openai "github.com/sashabaranov/go-openai"
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

func (p *OpenAIProvider) StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error {
	if !p.IsConfigured() {
		return fmt.Errorf("OpenAI API key not configured")
	}
	if model == "" {
		model = p.DefaultModel()
	}

	c := p.client()
	oaiMsgs := buildOpenAIMessages(messages, schema)

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
// Newer models (gpt-5.x, o-series) have fixed temperature/top_p and use reasoning_effort instead.
// Classic models (gpt-4o, gpt-4, gpt-3.5) support temperature tuning.
func applyModelParams(req *openai.ChatCompletionRequest) {
	m := strings.ToLower(req.Model)
	switch {
	case strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		// Reasoning models: use reasoning_effort, temperature/top_p/n locked at 1
		req.ReasoningEffort = "high"
		req.MaxCompletionTokens = 16384
	case strings.HasPrefix(m, "gpt-5"):
		// GPT-5.x: temperature/top_p/n fixed at 1, presence/frequency_penalty fixed at 0
		req.MaxCompletionTokens = 16384
	default:
		// Classic models (gpt-4o, gpt-4, gpt-3.5, etc.)
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
	// Find ```sql ... ``` blocks
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
