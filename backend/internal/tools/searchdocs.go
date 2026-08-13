package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/ali-asghar/agent-runtime/internal/docs"
)

// maxSearchResults keeps a tool result well inside the agent's 10,000-character
// truncation limit — five ~1,200-character chunks leaves room to spare.
const maxSearchResults = 5

// NewSearchDocumentsTool exposes BM25 retrieval over the documents attached to
// the current conversation. Scope comes from the session ID the agent stamps
// onto the context, so one chat can never read another's uploads.
func NewSearchDocumentsTool(store *docs.Store) *Tool {
	return &Tool{
		Name: "search_documents",
		Description: "Search the documents the user uploaded to this conversation and return the most relevant passages. " +
			"Use this whenever the question mentions an uploaded file, attachment, document, PDF, or report, or asks about " +
			"content you would not otherwise know. Search with the distinctive keywords from the question rather than a full " +
			"sentence. Call it more than once with different keywords if the first passages do not answer the question.",
		Parameters: map[string]ParameterDef{
			"query": {
				Type:        "string",
				Description: "Keywords to look for in the uploaded documents",
				Required:    true,
			},
		},
		Execute: func(ctx context.Context, args map[string]any) (string, error) {
			return searchDocuments(ctx, store, args)
		},
	}
}

func searchDocuments(ctx context.Context, store *docs.Store, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	sessionID := docs.SessionIDFrom(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("no conversation context available for document search")
	}

	attached := store.List(sessionID)
	if len(attached) == 0 {
		return "No documents have been uploaded to this conversation. Tell the user they can attach one with the paperclip button next to the message box, then ask again.", nil
	}

	names := make([]string, len(attached))
	for i, doc := range attached {
		names[i] = doc.Name
	}

	results := store.Search(sessionID, query, maxSearchResults)
	if len(results) == 0 {
		return fmt.Sprintf(
			"No passages matched %q. The uploaded documents are: %s. Try again with different or broader keywords.",
			query, strings.Join(names, ", "),
		), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d relevant passage(s) for %q across %d uploaded document(s): %s\n",
		len(results), query, len(attached), strings.Join(names, ", "))
	sb.WriteString("Answer from these passages and cite the document name. If they don't contain the answer, say so.\n")

	for i, res := range results {
		fmt.Fprintf(&sb, "\n--- [%d] %s (passage %d of %d, relevance %.2f) ---\n%s\n",
			i+1, res.DocName, res.Ordinal, res.Total, res.Score, res.Text)
	}

	return sb.String(), nil
}
