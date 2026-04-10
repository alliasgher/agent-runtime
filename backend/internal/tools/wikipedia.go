package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func NewWikipediaTool() *Tool {
	return &Tool{
		Name:        "wikipedia",
		Description: "Search Wikipedia and read article summaries. Use this for factual information, history, definitions, and background knowledge.",
		Parameters: map[string]ParameterDef{
			"query": {Type: "string", Description: "The topic to search for on Wikipedia", Required: true},
		},
		Execute: executeWikipedia,
	}
}

func executeWikipedia(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	// First, search for the page
	searchURL := fmt.Sprintf(
		"https://en.wikipedia.org/w/api.php?action=opensearch&search=%s&limit=3&format=json",
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse opensearch results: [query, [titles], [descriptions], [urls]]
	var results []json.RawMessage
	if err := json.Unmarshal(body, &results); err != nil {
		return "", fmt.Errorf("failed to parse results: %w", err)
	}

	if len(results) < 2 {
		return "No Wikipedia articles found.", nil
	}

	var titles []string
	if err := json.Unmarshal(results[1], &titles); err != nil || len(titles) == 0 {
		return "No Wikipedia articles found.", nil
	}

	// Get the summary of the first result
	return getWikipediaSummary(ctx, titles[0])
}

func getWikipediaSummary(ctx context.Context, title string) (string, error) {
	summaryURL := fmt.Sprintf(
		"https://en.wikipedia.org/api/rest_v1/page/summary/%s",
		url.PathEscape(title),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", summaryURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "AgentRuntime/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch summary: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read summary: %w", err)
	}

	var summary struct {
		Title       string `json:"title"`
		Extract     string `json:"extract"`
		Description string `json:"description"`
		ContentURLs struct {
			Desktop struct {
				Page string `json:"page"`
			} `json:"desktop"`
		} `json:"content_urls"`
	}

	if err := json.Unmarshal(body, &summary); err != nil {
		return "", fmt.Errorf("failed to parse summary: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n", summary.Title))
	if summary.Description != "" {
		sb.WriteString(fmt.Sprintf("_%s_\n\n", summary.Description))
	}
	sb.WriteString(summary.Extract)
	if summary.ContentURLs.Desktop.Page != "" {
		sb.WriteString(fmt.Sprintf("\n\nSource: %s", summary.ContentURLs.Desktop.Page))
	}

	return sb.String(), nil
}
