// Package docs implements retrieval-augmented generation over user-uploaded
// files: text extraction, chunking, and BM25 ranking, all in-process.
//
// Retrieval is lexical rather than embedding-based on purpose. Groq — the
// default LLM provider — serves no embeddings endpoint, so vectors would mean
// a second provider, a second API key, and a per-token cost for what is
// mostly keyword lookup over a handful of documents.
package docs

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// MaxFileSize caps a single upload. The free tier has 512MB of RAM and holds
// every document's text in memory, so this is deliberately conservative.
const MaxFileSize = 10 << 20 // 10 MB

// textExtensions are read as-is. Anything not listed here still falls back to
// plain text if the bytes look like UTF-8, which covers most source files.
var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
	".log": true, ".rst": true, ".org": true, ".tex": true,
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".java": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rb": true, ".rs": true, ".php": true, ".swift": true, ".kt": true, ".scala": true,
	".sh": true, ".bash": true, ".zsh": true, ".sql": true, ".html": true, ".css": true,
}

// SupportedFormats is the human-readable list shown in upload errors and the UI.
const SupportedFormats = "PDF, DOCX, TXT, MD, CSV, JSON, and source code files"

// Extract pulls plain text out of an uploaded file, choosing a parser by
// extension. It returns an error the user can act on — the message surfaces
// directly in the upload UI.
func Extract(filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("file is empty")
	}
	if len(data) > MaxFileSize {
		return "", fmt.Errorf("file is larger than %dMB", MaxFileSize>>20)
	}

	ext := strings.ToLower(filepath.Ext(filename))

	var (
		text string
		err  error
	)
	switch ext {
	case ".pdf":
		text, err = extractPDF(data)
	case ".docx":
		text, err = extractDOCX(data)
	case ".doc":
		return "", fmt.Errorf("legacy .doc files aren't supported — save it as .docx or PDF first")
	default:
		text, err = extractPlainText(ext, data)
	}
	if err != nil {
		return "", err
	}

	text = normalizeWhitespace(text)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no readable text found in %s (a scanned or image-only document needs OCR, which isn't supported)", filename)
	}
	return text, nil
}

func extractPlainText(ext string, data []byte) (string, error) {
	if !textExtensions[ext] && !looksLikeText(data) {
		return "", fmt.Errorf("unsupported file type %q — supported formats: %s", ext, SupportedFormats)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file isn't valid UTF-8 text")
	}
	return string(data), nil
}

// looksLikeText decides whether an unknown extension holds text, using the
// same heuristic as git: a NUL byte in the first 8KB means binary.
func looksLikeText(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	return utf8.Valid(head)
}

// extractPDF reads a text-based PDF page by page. Pages that fail to parse are
// skipped rather than failing the whole upload — a single malformed page in an
// otherwise readable report shouldn't cost the user the document.
func extractPDF(data []byte) (text string, err error) {
	// The parser indexes untrusted binary structures and panics on some
	// malformed files, so contain it here.
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("could not read PDF — the file may be corrupt or password-protected")
		}
	}()

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read PDF — the file may be corrupt or password-protected")
	}

	var sb strings.Builder
	pages := reader.NumPage()
	for i := 1; i <= pages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		pageText, pageErr := page.GetPlainText(nil)
		if pageErr != nil || strings.TrimSpace(pageText) == "" {
			continue
		}
		// Page markers give the model something to cite and keep chunk
		// boundaries from silently merging unrelated sections.
		fmt.Fprintf(&sb, "\n\n[Page %d]\n%s", i, pageText)
	}

	return sb.String(), nil
}

// extractDOCX reads word/document.xml out of the OOXML zip. Only the text runs
// matter, so this walks tokens instead of unmarshalling the full schema.
func extractDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read DOCX — the file may be corrupt")
	}

	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("not a valid DOCX file (no word/document.xml inside)")
	}

	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("could not open DOCX contents: %w", err)
	}
	defer rc.Close()

	var sb strings.Builder
	dec := xml.NewDecoder(rc)
	inText := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("could not parse DOCX contents: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				sb.WriteString("\t")
			case "br", "cr":
				sb.WriteString("\n")
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				// Each w:p is a paragraph; the blank line is what the chunker
				// splits on downstream.
				sb.WriteString("\n\n")
			case "tr":
				sb.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}

	return sb.String(), nil
}

// normalizeWhitespace collapses the ragged spacing that PDF and DOCX
// extraction produce, so chunk boundaries land on real paragraph breaks.
func normalizeWhitespace(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	blanks := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blanks++
			// Keep at most one blank line so paragraph detection stays stable.
			if blanks > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blanks = 0
		out = append(out, line)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}
