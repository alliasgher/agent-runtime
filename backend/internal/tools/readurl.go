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
	var b strings.Builder
	b.Grow(len(html) / 2)

	inTag := false
	inScript := false
	i := 0
	lower := strings.ToLower(html)

	for i < len(html) {
		// Skip script/style/noscript blocks entirely
		if !inTag && !inScript {
			for _, tag := range []string{"<script", "<style", "<noscript"} {
				if strings.HasPrefix(lower[i:], tag) {
					// find closing tag
					closeTag := "</" + tag[1:] + ">"
					end := strings.Index(lower[i:], closeTag)
					if end >= 0 {
						i += end + len(closeTag)
					} else {
						i = len(html)
					}
					goto next
				}
			}
		}

		if html[i] == '<' {
			inTag = true
			// Add newline for block-level closing tags
			if i+2 < len(html) && html[i+1] == '/' {
				tag := strings.ToLower(html[i:])
				for _, bt := range []string{"</p", "</div", "</h1", "</h2", "</h3", "</h4", "</h5", "</h6", "</li", "</tr", "</br"} {
					if strings.HasPrefix(tag, bt) {
						b.WriteByte('\n')
						break
					}
				}
			}
		} else if html[i] == '>' {
			inTag = false
		} else if !inTag {
			b.WriteByte(html[i])
		}
		i++
	next:
	}

	text := b.String()

	// Decode common entities
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
