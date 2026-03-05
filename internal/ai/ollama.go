package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"warehouse-ui/internal/logger"
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
func (p *OllamaProvider) MinModel() string     { return "llama3, qwen2.5, deepseek-r1" }

func (p *OllamaProvider) IsConfigured() bool {
	resp, err := http.Get(p.endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

type ollamaChatRequest struct {
	Model    string             `json:"model"`
	Messages []ollamaMessage    `json:"messages"`
	Stream   bool               `json:"stream"`
	Options  ollamaOptions      `json:"options,omitempty"`
	Tools    []ollamaToolDef    `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolDef struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"top_p,omitempty"`
	TopK        int     `json:"top_k,omitempty"`
	NumCtx      int     `json:"num_ctx,omitempty"`
}

type ollamaStreamResponse struct {
	Message struct {
		Content   string           `json:"content"`
		Role      string           `json:"role"`
		ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done bool `json:"done"`
}

// buildOllamaTools converts CodeTools + DataTools into Ollama tool definitions.
func buildOllamaTools(hasCodePaths bool, hasQueryFunc bool) []ollamaToolDef {
	var allTools []CodeTool
	if hasCodePaths {
		allTools = append(allTools, CodeTools()...)
	}
	if hasQueryFunc {
		allTools = append(allTools, DataTools()...)
	}

	var tools []ollamaToolDef
	for _, ct := range allTools {
		tools = append(tools, ollamaToolDef{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        ct.Name,
				Description: ct.Description,
				Parameters:  ct.Parameters,
			},
		})
	}
	return tools
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

	// Add tools if code paths are linked or query functions are available
	var tools []ollamaToolDef
	hasCodePaths := schema != nil && len(schema.CodePaths) > 0
	hasQueryFunc := schema != nil && schema.RunQuery != nil
	hasTools := hasCodePaths || hasQueryFunc
	if hasTools {
		tools = buildOllamaTools(hasCodePaths, hasQueryFunc)
	}

	opts := ollamaOptions{
		Temperature: 0.2,
		TopP:        0.95,
		TopK:        40,
		NumCtx:      8192,
	}

	const maxToolRounds = 5

	for round := 0; round < maxToolRounds; round++ {
		// Non-streaming call for tool rounds when tools are available
		if hasTools {
			body, _ := json.Marshal(ollamaChatRequest{
				Model:    model,
				Messages: ollamaMsgs,
				Stream:   false,
				Options:  opts,
				Tools:    tools,
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

			if resp.StatusCode != 200 {
				resp.Body.Close()
				return fmt.Errorf("ollama: status %d", resp.StatusCode)
			}

			respBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return fmt.Errorf("ollama read: %w", err)
			}

			var chatResp ollamaStreamResponse
			if err := json.Unmarshal(respBody, &chatResp); err != nil {
				return fmt.Errorf("ollama parse: %w", err)
			}

			// Check for tool calls
			if len(chatResp.Message.ToolCalls) > 0 {
				// Append assistant message with tool calls
				ollamaMsgs = append(ollamaMsgs, ollamaMessage{
					Role:      "assistant",
					Content:   chatResp.Message.Content,
					ToolCalls: chatResp.Message.ToolCalls,
				})

				// Execute each tool call and append results
				for _, tc := range chatResp.Message.ToolCalls {
					logger.Info("ollama tool call: %s args=%v", tc.Function.Name, tc.Function.Arguments)

					onChunk(ToolActivityMsg(tc.Function.Name, tc.Function.Arguments))

					var result string
					var execErr error
					if IsDataTool(tc.Function.Name) {
						result, execErr = ExecuteDataTool(ctx, tc.Function.Name, tc.Function.Arguments, schema)
					} else {
						result, execErr = ExecuteTool(tc.Function.Name, tc.Function.Arguments, schema.CodePaths)
					}
					if execErr != nil {
						result = fmt.Sprintf("Error: %v", execErr)
					}

					if len(result) > 50000 {
						result = result[:50000] + "\n... (truncated)"
					}

					ollamaMsgs = append(ollamaMsgs, ollamaMessage{
						Role:    "tool",
						Content: result,
					})
				}
				continue // loop back to LLM
			}

			// No tool calls — this is the final response, emit it
			if chatResp.Message.Content != "" {
				onChunk(chatResp.Message.Content)
			}
			return nil
		}

		// No tools — fall through to streaming
		break
	}

	// Streaming response (no tools, or after tool rounds completed)
	body, _ := json.Marshal(ollamaChatRequest{
		Model:    model,
		Messages: ollamaMsgs,
		Stream:   true,
		Options:  opts,
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
