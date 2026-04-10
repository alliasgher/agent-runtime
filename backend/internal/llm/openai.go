package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type OpenAIProvider struct {
	baseURL      string
	apiKey       string
	model        string
	client       *http.Client // 60 s — non-streaming requests
	streamClient *http.Client // 5 min — streaming (body read takes longer)
}

func NewOpenAIProvider(baseURL, apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL:      baseURL,
		apiKey:       apiKey,
		model:        model,
		client:       &http.Client{Timeout: 60 * time.Second},
		streamClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *OpenAIProvider) Name() string {
	return fmt.Sprintf("openai-compatible (%s)", p.model)
}

// ChatCompletion sends a non-streaming request and returns the full response.
func (p *OpenAIProvider) ChatCompletion(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	firstParam := buildFirstParamLookup(tools)

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

	var respBody []byte
	for attempt := 0; attempt < 3; attempt++ {
		var doErr error
		resp, doErr := p.client.Do(req)
		if doErr != nil {
			return nil, fmt.Errorf("request failed: %w", doErr)
		}
		respBody, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfterDuration(resp)
			slog.Warn("rate limited, waiting", "seconds", wait.Seconds(), "attempt", attempt+1)
			time.Sleep(wait)
			// Recreate the request body for the next attempt
			req, _ = http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(bodyJSON))
			req.Header.Set("Content-Type", "application/json")
			if p.apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+p.apiKey)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusBadRequest {
				if recovered := recoverFromFailedGeneration(respBody, firstParam); recovered != nil {
					return recovered, nil
				}
			}
			return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
		}
		return p.parseResponse(respBody, firstParam)
	}
	return nil, fmt.Errorf("API error after retries: %s", string(respBody))
}

// StreamChatCompletion sends a streaming request. Text tokens are delivered to
// onToken as they arrive; the full Response is returned when the stream ends.
func (p *OpenAIProvider) StreamChatCompletion(ctx context.Context, messages []Message, tools []ToolDef, onToken func(string)) (*Response, error) {
	firstParam := buildFirstParamLookup(tools)

	reqBody := map[string]any{
		"model":    p.model,
		"messages": p.convertMessages(messages),
		"stream":   true,
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
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		var doErr error
		resp, doErr = p.streamClient.Do(req)
		if doErr != nil {
			return nil, fmt.Errorf("request failed: %w", doErr)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfterDuration(resp)
			resp.Body.Close()
			slog.Warn("stream rate limited, waiting", "seconds", wait.Seconds(), "attempt", attempt+1)
			time.Sleep(wait)
			req, _ = http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(bodyJSON))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			if p.apiKey != "" {
				req.Header.Set("Authorization", "Bearer "+p.apiKey)
			}
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusBadRequest {
			if recovered := recoverFromFailedGeneration(body, firstParam); recovered != nil {
				return recovered, nil
			}
		}
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Accumulated state across SSE chunks
	type tcAccum struct {
		id        string
		name      string
		arguments strings.Builder
	}
	var (
		content   strings.Builder
		toolCalls = make(map[int]*tcAccum)
	)

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large tool-call argument chunks
	scanner.Buffer(make([]byte, 64*1024), 512*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   *string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		// Text token
		if delta.Content != nil && *delta.Content != "" {
			tok := *delta.Content
			content.WriteString(tok)
			if onToken != nil {
				onToken(tok)
			}
		}

		// Tool call argument fragments
		for _, tc := range delta.ToolCalls {
			if _, ok := toolCalls[tc.Index]; !ok {
				toolCalls[tc.Index] = &tcAccum{}
			}
			if tc.ID != "" {
				toolCalls[tc.Index].id = tc.ID
			}
			if tc.Function.Name != "" {
				toolCalls[tc.Index].name = tc.Function.Name
			}
			toolCalls[tc.Index].arguments.WriteString(tc.Function.Arguments)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read error: %w", err)
	}

	result := &Response{Content: content.String()}

	// Collect tool calls in index order
	indices := make([]int, 0, len(toolCalls))
	for idx := range toolCalls {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		tc := toolCalls[idx]
		argsStr := tc.arguments.String()
		// Apply same normalization as the non-streaming path
		argsStr = strings.ReplaceAll(argsStr, "'", "\"")
		if !json.Valid([]byte(argsStr)) {
			key := firstParam(tc.name)
			b, _ := json.Marshal(map[string]string{key: argsStr})
			argsStr = string(b)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:        tc.id,
			Name:      tc.name,
			Arguments: argsStr,
		})
	}

	// Text fallback: models that narrate tool calls instead of using structured format
	if len(result.ToolCalls) == 0 && result.Content != "" {
		if calls := extractTextToolCalls(result.Content, firstParam); len(calls) > 0 {
			result.ToolCalls = calls
			result.Content = ""
		}
	}

	return result, nil
}

// buildFirstParamLookup returns a function that maps a tool name to its first
// required parameter name. Used to wrap plain-text tool call arguments with the
// correct key when the model doesn't emit valid JSON.
func buildFirstParamLookup(tools []ToolDef) func(string) string {
	lookup := make(map[string]string, len(tools))
	for _, def := range tools {
		fn, ok := def["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := fn["name"].(string)
		params, ok := fn["parameters"].(map[string]any)
		if !ok {
			continue
		}
		required, _ := params["required"].([]string)
		if len(required) > 0 {
			lookup[name] = required[0]
		}
	}
	return func(toolName string) string {
		if key, ok := lookup[toolName]; ok {
			return key
		}
		return "input"
	}
}

func (p *OpenAIProvider) convertMessages(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))

	for _, msg := range messages {
		m := map[string]any{"role": string(msg.Role)}

		switch {
		case msg.Content != "":
			m["content"] = msg.Content
		case msg.Role == RoleAssistant:
			// OpenAI spec: assistant messages with tool_calls use explicit null.
			m["content"] = nil
		case msg.Role == RoleTool:
			// Tool messages must always have a content field (empty string is fine,
			// but missing causes a 400 from Groq/OpenAI).
			m["content"] = ""
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

func (p *OpenAIProvider) parseResponse(body []byte, firstParam func(string) string) (*Response, error) {
	var raw struct {
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
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

	if len(resp.ToolCalls) == 0 && resp.Content != "" {
		if calls := extractTextToolCalls(resp.Content, firstParam); len(calls) > 0 {
			resp.ToolCalls = calls
			resp.Content = ""
		}
	}

	return resp, nil
}

func recoverFromFailedGeneration(body []byte, firstParam func(string) string) *Response {
	var errResp struct {
		Error struct {
			FailedGeneration string `json:"failed_generation"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		return nil
	}
	fg := errResp.Error.FailedGeneration
	if fg == "" {
		return nil
	}
	calls := extractTextToolCalls(fg, firstParam)
	if len(calls) == 0 {
		return nil
	}
	return &Response{ToolCalls: calls}
}

// retryAfterDuration reads the Retry-After header from a 429 response and
// returns how long to wait. Falls back to 60 seconds if the header is absent.
func retryAfterDuration(resp *http.Response) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 60 * time.Second
}

// textToolCallRe matches <function=name>...</function> and variants including
// <function(name)>, <function\name>, <function:name>, <function name>, <function=name>.
var textToolCallRe = regexp.MustCompile(`(?s)<?function[\W(](\w+)[)>]?\s*(.*?)</function>`)

// pythonTagRe matches Llama's native <|python_tag|>toolname{...} format.
var pythonTagRe = regexp.MustCompile(`<\|python_tag\|>(\w+)(\{.*?\})`)

func extractTextToolCalls(content string, firstParam func(string) string) []ToolCall {
	type rawMatch struct {
		name string
		args string
	}
	var raws []rawMatch

	for _, m := range textToolCallRe.FindAllStringSubmatch(content, -1) {
		raws = append(raws, rawMatch{name: m[1], args: strings.TrimSpace(m[2])})
	}
	for _, m := range pythonTagRe.FindAllStringSubmatch(content, -1) {
		raws = append(raws, rawMatch{name: m[1], args: strings.TrimSpace(m[2])})
	}

	if len(raws) == 0 {
		return nil
	}

	calls := make([]ToolCall, 0, len(raws))
	for i, r := range raws {
		raw := strings.TrimRight(r.args, ">") // strip trailing > captured before </function>
		raw = strings.ReplaceAll(raw, "'", "\"")
		if !json.Valid([]byte(raw)) {
			key := "input"
			if firstParam != nil {
				key = firstParam(r.name)
			}
			b, _ := json.Marshal(map[string]string{key: raw})
			raw = string(b)
		}
		calls = append(calls, ToolCall{
			ID:        fmt.Sprintf("text-call-%d", i),
			Name:      r.name,
			Arguments: raw,
		})
	}
	return calls
}
