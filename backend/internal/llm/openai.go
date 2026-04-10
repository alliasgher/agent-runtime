package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAIProvider struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string {
	return fmt.Sprintf("openai-compatible (%s)", p.model)
}

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	// Build request body
	reqBody := map[string]any{
		"model":    p.model,
		"messages": p.convertMessages(messages),
	}

	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return p.parseResponse(respBody)
}

func (p *OpenAIProvider) convertMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		m := map[string]any{
			"role": string(msg.Role),
		}

		if msg.Content != "" {
			m["content"] = msg.Content
		}

		if len(msg.ToolCalls) > 0 {
			calls := make([]map[string]any, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				calls[i] = map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				}
			}
			m["tool_calls"] = calls
		}

		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}

		if msg.Name != "" {
			m["name"] = msg.Name
		}

		out = append(out, m)
	}

	return out
}

func (p *OpenAIProvider) parseResponse(body []byte) (*Response, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if raw.Error != nil {
		return nil, fmt.Errorf("API error: %s", raw.Error.Message)
	}

	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := raw.Choices[0]
	resp := &Response{}

	if choice.Message.Content != nil {
		resp.Content = *choice.Message.Content
	}

	for _, tc := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return resp, nil
}
