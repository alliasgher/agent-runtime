package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func NewWebSearchTool() *Tool {
	return &Tool{
		Name:        "web_search",
		Description: "Search the web for current information. Returns a list of results with titles, URLs, and snippets.",
		Parameters: map[string]ParameterDef{
			"query": {Type: "string", Description: "The search query", Required: true},
		},
		Execute: executeWebSearch,
	}
}

func executeWebSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return parseDDGResults(string(body)), nil
}

func parseDDGResults(html string) string {
	// Extract result blocks
	resultRe := regexp.MustCompile(`(?s)<a rel="nofollow" class="result__a" href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetRe := regexp.MustCompile(`(?s)<a class="result__snippet"[^>]*>(.*?)</a>`)

	links := resultRe.FindAllStringSubmatch(html, 10)
	snippets := snippetRe.FindAllStringSubmatch(html, 10)

	if len(links) == 0 {
		return "No search results found."
	}

	var sb strings.Builder
	for i, link := range links {
		if i >= 8 {
			break
		}
		title := stripHTML(link[2])
		href := link[1]

		// DuckDuckGo wraps URLs in a redirect
		if u, err := url.QueryUnescape(href); err == nil {
			if parsed, err := url.Parse(u); err == nil {
				if uddg := parsed.Query().Get("uddg"); uddg != "" {
					href = uddg
				}
			}
		}

		snippet := ""
		if i < len(snippets) {
			snippet = stripHTML(snippets[i][1])
		}

		sb.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n   %s\n\n", i+1, title, href, snippet))
	}

	return sb.String()
}

func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.TrimSpace(s)
	return s
}
