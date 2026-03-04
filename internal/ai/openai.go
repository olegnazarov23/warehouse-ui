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

	stream, err := c.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    oaiMsgs,
		Temperature: 0.2,
		Stream:      true,
	})
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

	resp, err := c.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    oaiMsgs,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("openai complete: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return resp.Choices[0].Message.Content, nil
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
	start = strings.Index(response[start:], "\n")
	if start == -1 {
		return ""
	}
	remaining := response[start+1:]
	end := strings.Index(remaining, "```")
	if end == -1 {
		return strings.TrimSpace(remaining)
	}
	return strings.TrimSpace(remaining[:end])
}
