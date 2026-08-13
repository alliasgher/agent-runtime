package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ali-asghar/agent-runtime/internal/agent"
	"github.com/ali-asghar/agent-runtime/internal/docs"
	"github.com/ali-asghar/agent-runtime/internal/llm"
	"github.com/ali-asghar/agent-runtime/internal/server"
	"github.com/ali-asghar/agent-runtime/internal/tools"
)

const handbookText = `# Company Handbook

Expense reimbursement must be filed within thirty days of travel.

Parking is validated in garage B for visitors only.
`

// newDocServer wires the upload endpoints and the search_documents tool to the
// same store, exactly as main.go does — the whole point of the test is that
// those two halves see the same documents.
func newDocServer(t *testing.T, provider llm.Provider) *httptest.Server {
	t.Helper()
	docStore := docs.NewStore(nil)
	reg := tools.NewRegistry()
	reg.Register(tools.NewSearchDocumentsTool(docStore))
	srv := server.New(provider, reg, nil, docStore)
	return httptest.NewServer(srv.Handler())
}

func uploadDoc(t *testing.T, srv *httptest.Server, sessionID, filename string, content []byte) (int, []byte) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/sessions/"+sessionID+"/documents", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func listDocs(t *testing.T, srv *httptest.Server, sessionID string) []docs.Document {
	t.Helper()

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/documents")
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	defer resp.Body.Close()

	var out []docs.Document
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode documents: %v", err)
	}
	return out
}

func TestDocumentUploadListDelete(t *testing.T) {
	srv := newDocServer(t, &mockLLM{})
	defer srv.Close()

	sessionID := createSession(t, srv)

	status, body := uploadDoc(t, srv, sessionID, "handbook.md", []byte(handbookText))
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", status, body)
	}

	var uploaded docs.Document
	if err := json.Unmarshal(body, &uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.ID == "" || uploaded.Name != "handbook.md" || uploaded.NumChunks == 0 {
		t.Fatalf("incomplete upload response: %+v", uploaded)
	}

	listed := listDocs(t, srv, sessionID)
	if len(listed) != 1 || listed[0].ID != uploaded.ID {
		t.Fatalf("expected the uploaded document in the list, got %+v", listed)
	}

	req, _ := http.NewRequest(http.MethodDelete,
		srv.URL+"/api/sessions/"+sessionID+"/documents/"+uploaded.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from delete, got %d", resp.StatusCode)
	}

	if got := listDocs(t, srv, sessionID); len(got) != 0 {
		t.Errorf("document survived deletion: %+v", got)
	}
}

func TestDocumentUploadRejectsUnreadableFile(t *testing.T) {
	srv := newDocServer(t, &mockLLM{})
	defer srv.Close()

	sessionID := createSession(t, srv)

	status, body := uploadDoc(t, srv, sessionID, "image.png", []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01})
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", status, body)
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == "" {
		t.Fatalf("expected an actionable error message, got %s", body)
	}
}

func TestDocumentUploadUnknownSession(t *testing.T) {
	srv := newDocServer(t, &mockLLM{})
	defer srv.Close()

	status, _ := uploadDoc(t, srv, "does-not-exist", "notes.txt", []byte("hello"))
	if status != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown session, got %d", status)
	}
}

// TestAgentRetrievesFromUploadedDocument drives the full path: upload over
// HTTP, then a conversation in which the model calls search_documents and the
// passage comes back through the WebSocket.
func TestAgentRetrievesFromUploadedDocument(t *testing.T) {
	provider := &mockLLM{responses: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID:        "call-1",
			Name:      "search_documents",
			Arguments: `{"query": "expense reimbursement deadline"}`,
		}}},
		{Content: "Expenses must be filed within thirty days of travel (handbook.md)."},
	}}

	srv := newDocServer(t, provider)
	defer srv.Close()

	sessionID := createSession(t, srv)
	if status, body := uploadDoc(t, srv, sessionID, "handbook.md", []byte(handbookText)); status != http.StatusCreated {
		t.Fatalf("upload failed with %d: %s", status, body)
	}

	conn := connectWS(t, srv, sessionID)
	defer conn.Close()

	sendMessage(t, conn, "What is the expense reimbursement deadline?")
	events := readEvents(t, conn, 10*time.Second)

	if !hasType(events, agent.EventToolCall) {
		t.Fatalf("expected a tool call, got events: %v", eventTypes(events))
	}

	var result string
	for _, e := range events {
		if e.Type == agent.EventToolResult && e.ToolName == "search_documents" {
			result = e.Content
		}
	}
	if result == "" {
		t.Fatalf("no search_documents result event; got %v", eventTypes(events))
	}
	if !strings.Contains(result, "thirty days") {
		t.Errorf("retrieved passage missing the answer:\n%s", result)
	}
	if !strings.Contains(result, "handbook.md") {
		t.Errorf("retrieved passage not attributed to its document:\n%s", result)
	}
}

// TestDocumentsAreScopedToSession is the security-relevant case: one
// conversation must never retrieve another's uploads.
func TestDocumentsAreScopedToSession(t *testing.T) {
	provider := &mockLLM{responses: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID:        "call-1",
			Name:      "search_documents",
			Arguments: `{"query": "expense reimbursement deadline"}`,
		}}},
		{Content: "I don't have that document."},
	}}

	srv := newDocServer(t, provider)
	defer srv.Close()

	owner := createSession(t, srv)
	if status, body := uploadDoc(t, srv, owner, "handbook.md", []byte(handbookText)); status != http.StatusCreated {
		t.Fatalf("upload failed with %d: %s", status, body)
	}

	other := createSession(t, srv)
	conn := connectWS(t, srv, other)
	defer conn.Close()

	sendMessage(t, conn, "What is the expense reimbursement deadline?")
	events := readEvents(t, conn, 10*time.Second)

	for _, e := range events {
		if e.Type != agent.EventToolResult {
			continue
		}
		if strings.Contains(e.Content, "thirty days") || strings.Contains(e.Content, "handbook.md") {
			t.Fatalf("another session's document leaked into this conversation:\n%s", e.Content)
		}
		if !strings.Contains(e.Content, "No documents have been uploaded") {
			t.Errorf("expected the empty-corpus message, got:\n%s", e.Content)
		}
	}
}

// TestGetSessionSkipsToolCallPreamble reproduces a duplication seen in
// production: a model that emits text alongside its tool call had that
// preamble stored as an assistant message, so reloading the conversation
// rendered the answer twice — once as the preamble, once as the real reply.
// The live UI never showed it, because it discards streamed text when the
// tool_call event arrives, so the bug only appeared after a reload.
func TestGetSessionSkipsToolCallPreamble(t *testing.T) {
	const answer = "The 12th Fibonacci number is 144."

	provider := &mockLLM{responses: []*llm.Response{
		{
			// Content *and* tool calls in the same response — the case that duplicated.
			Content:   answer,
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"msg": "144"}`}},
		},
		{Content: answer},
	}}

	echo := &tools.Tool{
		Name:        "echo",
		Description: "echoes input",
		Parameters:  map[string]tools.ParameterDef{"msg": {Type: "string", Required: true}},
		Execute: func(_ context.Context, args map[string]any) (string, error) {
			return args["msg"].(string), nil
		},
	}

	reg := tools.NewRegistry()
	reg.Register(echo)
	srv := httptest.NewServer(server.New(provider, reg, nil, docs.NewStore(nil)).Handler())
	defer srv.Close()

	sessionID := createSession(t, srv)
	conn := connectWS(t, srv, sessionID)
	sendMessage(t, conn, "What is the 12th Fibonacci number?")
	readEvents(t, conn, 10*time.Second)
	conn.Close()

	resp, err := http.Get(srv.URL + "/api/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	var assistant int
	for _, m := range out.Messages {
		if m.Role == "assistant" {
			assistant++
		}
	}
	if assistant != 1 {
		t.Errorf("expected exactly 1 assistant message on reload, got %d: %+v", assistant, out.Messages)
	}
}

func TestDeletingSessionRemovesItsDocuments(t *testing.T) {
	srv := newDocServer(t, &mockLLM{})
	defer srv.Close()

	sessionID := createSession(t, srv)
	if status, _ := uploadDoc(t, srv, sessionID, "handbook.md", []byte(handbookText)); status != http.StatusCreated {
		t.Fatal("upload failed")
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/sessions/"+sessionID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}
	resp.Body.Close()

	// The session is gone, so the documents endpoint should 404 rather than
	// still serving its documents.
	listResp, err := http.Get(srv.URL + "/api/sessions/" + sessionID + "/documents")
	if err != nil {
		t.Fatalf("list documents: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after session deletion, got %d", listResp.StatusCode)
	}
}
