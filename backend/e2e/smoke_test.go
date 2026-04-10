// Package e2e runs smoke tests against a live agent-runtime backend.
//
// Usage (against prod):
//
//	E2E=1 go test ./e2e/... -v -timeout 5m
//
// Usage (against local):
//
//	E2E=1 BASE_URL=http://localhost:8080 go test ./e2e/... -v -timeout 5m
//
// The test is skipped automatically unless E2E=1 is set, so it never runs
// in the normal CI pipeline (which has no real API key).
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func baseURL() string {
	if v := os.Getenv("BASE_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://agent-runtime-production.up.railway.app"
}

func wsURL(base, sessionID string) string {
	u, _ := url.Parse(base)
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/ws/" + sessionID
	return u.String()
}

func createSession(t *testing.T) string {
	t.Helper()
	resp, err := http.Post(baseURL()+"/api/sessions", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.ID == "" {
		t.Fatalf("parse session response: %v — body: %s", err, body)
	}
	return data.ID
}

type agentEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Step    int    `json:"step"`
}

// ask sends a message and collects all events until the channel closes.
// Returns the final response content and all events received.
func ask(t *testing.T, sessionID, message string, timeout time.Duration) (string, []agentEvent) {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(baseURL(), sessionID), nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	// Send message
	payload, _ := json.Marshal(map[string]string{"type": "message", "content": message})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("send message: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var events []agentEvent
	var streamedContent strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var ev agentEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				continue
			}
			events = append(events, ev)
			switch ev.Type {
			case "token":
				streamedContent.WriteString(ev.Content)
			case "response", "error":
				return
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Errorf("timeout waiting for response to: %q", message)
	}

	// Final content: streamed tokens take priority (same logic as frontend)
	content := streamedContent.String()
	if content == "" {
		for _, ev := range events {
			if ev.Type == "response" {
				content = ev.Content
				break
			}
		}
	}
	return content, events
}

// ── failure pattern checks ────────────────────────────────────────────────────

var leakPatterns = []*regexp.Regexp{
	regexp.MustCompile(`<\|python_tag\|>`),           // Llama native format leaked
	regexp.MustCompile(`<function[=\\/\\ :\w]`),      // <function=...> leaked
	regexp.MustCompile(`</function>`),                 // closing tag leaked
	regexp.MustCompile(`\{"type"\s*:\s*"function"\}`), // raw JSON tool call leaked
}

func checkResponse(t *testing.T, question, content string, events []agentEvent) {
	t.Helper()

	// 1. Must not be blank
	if strings.TrimSpace(content) == "" {
		t.Errorf("BLANK RESPONSE for: %q\n  events: %s", question, summariseEvents(events))
		return
	}

	// 2. Must not contain leaked tool syntax
	for _, re := range leakPatterns {
		if re.MatchString(content) {
			t.Errorf("LEAKED TOOL SYNTAX (%s) for: %q\n  content snippet: %.200s",
				re.String(), question, content)
		}
	}

	// 3. If a tool was used, a tool_result must appear before response
	hasToolCall := false
	hasToolResult := false
	for _, ev := range events {
		if ev.Type == "tool_call" {
			hasToolCall = true
		}
		if ev.Type == "tool_result" {
			hasToolResult = true
		}
	}
	if hasToolCall && !hasToolResult {
		t.Errorf("TOOL CALLED BUT NO RESULT for: %q\n  events: %s", question, summariseEvents(events))
	}
}

func summariseEvents(events []agentEvent) string {
	types := make([]string, len(events))
	for i, ev := range events {
		types[i] = ev.Type
	}
	return "[" + strings.Join(types, ", ") + "]"
}

// ── test cases ────────────────────────────────────────────────────────────────

// Each case has a category so failures are easy to group.
var cases = []struct {
	category string
	question string
}{
	// Pure text — no tools needed
	{"text", "What is 2 + 2?"},
	{"text", "Say hello in three languages."},
	{"text", "What is the capital of France?"},
	{"text", "List three benefits of exercise."},
	{"text", "Explain what a REST API is in one paragraph."},

	// Python execution
	{"python", "Write and run a Python script that prints the first 10 fibonacci numbers."},
	{"python", "Write a Python script to generate a multiplication table for numbers 1 to 5."},
	{"python", "Run Python code to calculate the factorial of 10."},
	{"python", "Write a Python script that sorts a list of names alphabetically and prints them."},

	// Web search
	{"web_search", "Search for the top programming languages in 2025 and compare them."},
	{"web_search", "What is the latest version of Go?"},
	{"web_search", "Search for recent news about artificial intelligence."},

	// Wikipedia
	{"wikipedia", "What does the name Ali mean?"},
	{"wikipedia", "Look up the history of the Python programming language."},
	{"wikipedia", "What is quantum computing according to Wikipedia?"},

	// URL reading
	{"read_url", "Read https://go.dev and summarise what Go is."},

	// Multi-step / tool chaining
	{"multi", "Search for SpaceX latest launch and summarise the findings."},
	{"multi", "Write a Python script to check if a number is prime, run it with the number 97, and explain the output."},
	{"multi", "Look up the Go programming language on Wikipedia and compare it to Python in terms of use cases."},

	// Edge cases that have caused bugs before
	{"edge", "go and rust?"},
	{"edge", "explain it"},
	{"edge", "Search for top AI tools and then write a Python script that prints their names."},

	// Math / calculation
	{"math", "What is the square root of 144?"},
	{"math", "Calculate the compound interest on $1000 at 5% for 3 years using Python."},

	// Code explanation
	{"code", "Explain what a goroutine is in Go."},
	{"code", "What is the difference between a slice and an array in Go?"},
	{"code", "Write a Python function that reverses a string and run it."},

	// Mixed tool usage
	{"mixed", "Search for the meaning of the name Ali and also look it up on Wikipedia."},
	{"mixed", "Find the current population of Japan and write a Python script that calculates what 0.1% of that number is."},
}

// ── main test ─────────────────────────────────────────────────────────────────

func TestSmoke(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("set E2E=1 to run end-to-end smoke tests")
	}

	// Each question gets its own session to avoid cross-contamination.
	// Run sequentially to avoid hammering the API.
	results := make([]struct {
		question string
		passed   bool
		detail   string
	}, len(cases))

	passed := 0
	failed := 0

	// Reuse one session per category to minimise session-creation rate limit hits.
	categorySession := make(map[string]string)

	for i, tc := range cases {
		tc := tc
		i := i
		t.Run(fmt.Sprintf("%s/%d", tc.category, i), func(t *testing.T) {
			sid, ok := categorySession[tc.category]
			if !ok {
				sid = createSession(t)
				categorySession[tc.category] = sid
			}

			// Small pause so we don't hammer Groq rate limits
			time.Sleep(2 * time.Second)

			content, events := ask(t, sid, tc.question, 90*time.Second)
			checkResponse(t, tc.question, content, events)

			if t.Failed() {
				failed++
				results[i] = struct {
					question string
					passed   bool
					detail   string
				}{tc.question, false, content}
			} else {
				passed++
				results[i] = struct {
					question string
					passed   bool
					detail   string
				}{tc.question, true, ""}
			}
		})
	}

	t.Logf("\n\n=== SMOKE TEST SUMMARY ===\nPassed: %d / %d\nFailed: %d / %d\n",
		passed, len(cases), failed, len(cases))
}
