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
	MaxSteps      = 15
	SystemPrompt  = `You are a helpful AI assistant with access to tools. When you need current information, calculations, or code execution, you MUST call the tool directly using the function calling mechanism — never write tool calls as text in your response. Do not narrate or describe what you are about to do; just call the tool. After getting tool results, synthesize the information into a clear, helpful response.`
)

// Event types streamed to the client
type EventType string

const (
	EventThinking   EventType = "thinking"
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
	return &Agent{
		provider: provider,
		registry: registry,
	}
}

func (a *Agent) Run(ctx context.Context, session *Session, input string, events chan<- Event) {
	defer close(events)

	// Add user message
	session.AddMessage(llm.Message{
		Role:    llm.RoleUser,
		Content: input,
	})

	// Build messages with system prompt
	messages := make([]llm.Message, 0, len(session.Messages)+1)
	messages = append(messages, llm.Message{
		Role:    llm.RoleSystem,
		Content: SystemPrompt,
	})
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

		resp, err := a.provider.ChatCompletion(ctx, messages, toolDefs)
		if err != nil {
			events <- newEvent(EventError, step, fmt.Sprintf("LLM error: %v", err))
			return
		}

		// If no tool calls, this is the final response
		if len(resp.ToolCalls) == 0 {
			session.AddMessage(llm.Message{
				Role:    llm.RoleAssistant,
				Content: resp.Content,
			})
			events <- Event{
				Type:      EventResponse,
				Content:   resp.Content,
				Step:      step,
				Timestamp: time.Now().UnixMilli(),
			}
			return
		}

		// The assistant message with tool calls (may also have content)
		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		session.AddMessage(assistantMsg)
		messages = append(messages, assistantMsg)

		if resp.Content != "" {
			events <- Event{
				Type:      EventThinking,
				Content:   resp.Content,
				Step:      step,
				Timestamp: time.Now().UnixMilli(),
			}
		}

		// Execute tools in parallel
		type toolResult struct {
			callID string
			name   string
			input  string
			output string
			err    error
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

				output, err := a.registry.Execute(ctx, call.Name, call.Arguments)
				results[idx] = toolResult{
					callID: call.ID,
					name:   call.Name,
					input:  call.Arguments,
					output: output,
					err:    err,
				}
			}(i, tc)
		}

		wg.Wait()

		// Add tool results to messages
		for _, res := range results {
			output := res.output
			if res.err != nil {
				output = fmt.Sprintf("Error: %v", res.err)
				slog.Error("tool execution failed", "tool", res.name, "error", res.err)
			}

			// Truncate very long results
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
