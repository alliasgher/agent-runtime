package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/tools"
)

const (
	MaxSteps     = 15
	SystemPrompt = `You are a helpful AI assistant with access to tools.

Rules:
- Use tools via the function calling mechanism ONLY — never write tool calls as text.
- ALWAYS supply every required parameter when calling a tool (e.g. "query" for web_search and wikipedia, "code" for run_python, "expression" for calculate, "url" for read_url). Never call a tool with an empty or missing required parameter.
- Do not narrate what you are about to do; call the tool immediately.
- When asked to write or produce code: run it with run_python, then in your final response ALWAYS reproduce the COMPLETE script inside a fenced code block (` + "```python" + ` ... ` + "```" + `). Never omit the code — the user must be able to copy and run it themselves.
- After receiving tool results, synthesize a clear, helpful response that includes all relevant output and code.`
)

// Event types streamed to the client
type EventType string

const (
	EventThinking   EventType = "thinking"
	EventToken      EventType = "token"      // streaming text token
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventResponse   EventType = "response"
	EventError      EventType = "error"
)

type Event struct {
	Type      EventType `json:"type"`
	Content   string    `json:"content,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	ToolInput string    `json:"tool_input,omitempty"`
	ToolID    string    `json:"tool_id,omitempty"`
	Step      int       `json:"step"`
	Timestamp int64     `json:"timestamp"`
}

type Agent struct {
	provider llm.Provider
	registry *tools.Registry
}

func New(provider llm.Provider, registry *tools.Registry) *Agent {
	return &Agent{provider: provider, registry: registry}
}

func (a *Agent) Run(ctx context.Context, session *Session, input string, events chan<- Event) {
	defer close(events)

	session.AddMessage(llm.Message{Role: llm.RoleUser, Content: input})

	messages := make([]llm.Message, 0, len(session.Messages)+1)
	messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: SystemPrompt})
	messages = append(messages, session.Messages...)

	toolDefs := a.registry.OpenAIDefs()

	for step := 1; step <= MaxSteps; step++ {
		select {
		case <-ctx.Done():
			events <- newEvent(EventError, step, "Request cancelled")
			return
		default:
		}

		events <- Event{
			Type:      EventThinking,
			Content:   "Thinking...",
			Step:      step,
			Timestamp: time.Now().UnixMilli(),
		}

		// onToken streams individual text tokens to the client while the LLM
		// is generating. If the step ends up calling tools the frontend will
		// clear this buffer; if it ends with text it becomes the final response.
		onToken := func(token string) {
			select {
			case events <- Event{
				Type:      EventToken,
				Content:   token,
				Step:      step,
				Timestamp: time.Now().UnixMilli(),
			}:
			case <-ctx.Done():
			}
		}

		resp, err := a.provider.StreamChatCompletion(ctx, messages, toolDefs, onToken)
		if err != nil {
			events <- newEvent(EventError, step, fmt.Sprintf("LLM error: %v", err))
			return
		}

		slog.Debug("llm response", "step", step, "content_len", len(resp.Content), "tool_calls", len(resp.ToolCalls))

		// Empty response — no content, no tool calls. Retry once with non-streaming.
		if len(resp.ToolCalls) == 0 && resp.Content == "" {
			slog.Warn("empty streaming response, retrying with non-streaming", "step", step)
			resp, err = a.provider.ChatCompletion(ctx, messages, toolDefs)
			if err != nil {
				events <- newEvent(EventError, step, fmt.Sprintf("LLM error: %v", err))
				return
			}
			slog.Info("non-streaming retry", "step", step, "content_len", len(resp.Content), "tool_calls", len(resp.ToolCalls))
		}

		// Final text response — no tool calls
		if len(resp.ToolCalls) == 0 {
			session.AddMessage(llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
			events <- Event{
				Type:      EventResponse,
				Content:   resp.Content,
				Step:      step,
				Timestamp: time.Now().UnixMilli(),
			}
			return
		}

		// Assistant message that includes tool calls
		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		session.AddMessage(assistantMsg)
		messages = append(messages, assistantMsg)

		// Execute tools in parallel
		type toolResult struct {
			callID string
			name   string
			input  string
			output string
			err    error
			dur    time.Duration
		}

		results := make([]toolResult, len(resp.ToolCalls))
		var wg sync.WaitGroup

		for i, tc := range resp.ToolCalls {
			wg.Add(1)
			go func(idx int, call llm.ToolCall) {
				defer wg.Done()
				events <- Event{
					Type:      EventToolCall,
					ToolName:  call.Name,
					ToolInput: call.Arguments,
					ToolID:    call.ID,
					Step:      step,
					Timestamp: time.Now().UnixMilli(),
				}
				start := time.Now()
				output, err := a.registry.Execute(ctx, call.Name, call.Arguments)
				results[idx] = toolResult{
					callID: call.ID,
					name:   call.Name,
					input:  call.Arguments,
					output: output,
					err:    err,
					dur:    time.Since(start),
				}
			}(i, tc)
		}

		wg.Wait()

		for _, res := range results {
			output := res.output
			if res.err != nil {
				output = fmt.Sprintf("Error: %v", res.err)
				slog.Error("tool execution failed", "tool", res.name, "error", res.err)
			}
			if len(output) > 10000 {
				output = output[:10000] + "\n\n[Output truncated...]"
			}

			toolMsg := llm.Message{
				Role:       llm.RoleTool,
				Content:    output,
				ToolCallID: res.callID,
				Name:       res.name,
			}
			session.AddMessage(toolMsg)
			messages = append(messages, toolMsg)

			events <- Event{
				Type:      EventToolResult,
				Content:   output,
				ToolName:  res.name,
				ToolID:    res.callID,
				Step:      step,
				Timestamp: time.Now().UnixMilli(),
			}
		}
	}

	events <- newEvent(EventError, MaxSteps, "Maximum steps reached")
}

func newEvent(t EventType, step int, content string) Event {
	return Event{
		Type:      t,
		Content:   content,
		Step:      step,
		Timestamp: time.Now().UnixMilli(),
	}
}
