package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

func NewReadURLTool() *Tool {
	return &Tool{
		Name:        "read_url",
		Description: "Fetch and read the text content of a webpage. Use this to get detailed information from a specific URL.",
		Parameters: map[string]ParameterDef{
			"url": {Type: "string", Description: "The URL to read", Required: true},
		},
		Execute: executeReadURL,
	}
}

func executeReadURL(ctx context.Context, args map[string]any) (string, error) {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	text := extractText(string(body))

	// Truncate to reasonable length for LLM context
	if len(text) > 8000 {
		text = text[:8000] + "\n\n[Content truncated...]"
	}

	return text, nil
}

func extractText(html string) string {
	// Remove script and style blocks
	scriptRe := regexp.MustCompile(`(?si)<(script|style|noscript)[^>]*>.*?</\1>`)
	html = scriptRe.ReplaceAllString(html, "")

	// Remove HTML comments
	commentRe := regexp.MustCompile(`(?s)<!--.*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// Replace block elements with newlines
	blockRe := regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr|br|hr)[^>]*>`)
	html = blockRe.ReplaceAllString(html, "\n")

	// Strip remaining tags
	tagRe := regexp.MustCompile(`<[^>]*>`)
	text := tagRe.ReplaceAllString(html, "")

	// Decode entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&#x27;", "'")
	text = strings.ReplaceAll(text, "&#39;", "'")

	// Collapse whitespace
	spaceRe := regexp.MustCompile(`[ \t]+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// Collapse multiple newlines
	nlRe := regexp.MustCompile(`\n{3,}`)
	text = nlRe.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}
