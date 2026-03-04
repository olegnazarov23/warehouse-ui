package ai

import (
	"context"
	"fmt"
	"strings"

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

func (p *AnthropicProvider) StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error {
	if !p.IsConfigured() {
		return fmt.Errorf("Anthropic API key not configured")
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
// Claude 4.x (opus/sonnet) supports extended thinking; older models use temperature tuning.
func applyAnthropicParams(params *anthropic.MessageNewParams, model string) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus-4"), strings.Contains(m, "sonnet-4"):
		// Claude 4.x: higher token limit, low temperature for precision
		params.MaxTokens = int64(8192)
		params.Temperature = anthropic.Float(0.2)
		params.TopP = anthropic.Float(0.95)
	default:
		// Older models (claude-3.x, etc.)
		params.MaxTokens = int64(4096)
		params.Temperature = anthropic.Float(0.2)
		params.TopP = anthropic.Float(0.95)
	}
}
