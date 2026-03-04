package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("ollama", func(apiKey, endpoint string) Provider {
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		return &OllamaProvider{endpoint: endpoint}
	})
}

// OllamaProvider implements Provider for local Ollama models.
type OllamaProvider struct {
	endpoint string
}

func (p *OllamaProvider) Name() string         { return "ollama" }
func (p *OllamaProvider) DefaultModel() string { return "llama3" }

func (p *OllamaProvider) IsConfigured() bool {
	// Check if Ollama is running
	resp, err := http.Get(p.endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  ollamaOptions   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaStreamResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func (p *OllamaProvider) StreamChat(ctx context.Context, messages []Message, schema *SchemaContext, model string, onChunk func(string)) error {
	if model == "" {
		model = p.DefaultModel()
	}

	systemPrompt := BuildSystemPrompt(schema)
	ollamaMsgs := []ollamaMessage{{Role: "system", Content: systemPrompt}}
	for _, m := range messages {
		ollamaMsgs = append(ollamaMsgs, ollamaMessage{Role: m.Role, Content: m.Content})
	}

	body, _ := json.Marshal(ollamaChatRequest{
		Model:    model,
		Messages: ollamaMsgs,
		Stream:   true,
		Options:  ollamaOptions{Temperature: 0.2},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("ollama: status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ollamaStreamResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			onChunk(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	return scanner.Err()
}

func (p *OllamaProvider) Complete(ctx context.Context, messages []Message, schema *SchemaContext, model string) (string, error) {
	var sb strings.Builder
	err := p.StreamChat(ctx, messages, schema, model, func(chunk string) {
		sb.WriteString(chunk)
	})
	return sb.String(), err
}
