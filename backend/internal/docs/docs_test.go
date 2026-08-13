package docs

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSplitIntoChunksShortTextStaysWhole(t *testing.T) {
	chunks := SplitIntoChunks("A short note about invoices.\n\nAnd a second paragraph.")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %q", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0], "second paragraph") {
		t.Errorf("chunk lost content: %q", chunks[0])
	}
}

func TestSplitIntoChunksEmptyText(t *testing.T) {
	if got := SplitIntoChunks("   \n\n  "); got != nil {
		t.Errorf("expected nil for blank text, got %q", got)
	}
}

func TestSplitIntoChunksSplitsLongTextWithOverlap(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "Paragraph %d covers quarterly revenue and operating costs in some detail.\n\n", i)
	}

	chunks := SplitIntoChunks(sb.String())
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		// Overlap seeds a chunk before the size check runs, so a chunk can
		// exceed the target by roughly one unit plus the overlap.
		if limit := ChunkTargetChars + ChunkOverlapChars + 200; len(c) > limit {
			t.Errorf("chunk %d is %d chars, over the %d limit", i, len(c), limit)
		}
	}

	// Consecutive chunks should share text, or a passage spanning a boundary
	// would be unmatchable.
	prevTail := chunks[0][len(chunks[0])-ChunkOverlapChars:]
	if !strings.Contains(chunks[1], strings.TrimSpace(prevTail[strings.IndexByte(prevTail, ' ')+1:])) {
		t.Errorf("chunk 1 does not overlap chunk 0")
	}
}

func TestSplitIntoChunksHandlesUnbrokenText(t *testing.T) {
	// No spaces, no punctuation, longer than a chunk: must terminate and
	// preserve every byte.
	text := strings.Repeat("abcdefghij", 400) // 4000 chars
	chunks := SplitIntoChunks(text)
	if len(chunks) < 3 {
		t.Fatalf("expected the long run to split, got %d chunks", len(chunks))
	}
	if joined := strings.Join(chunks, ""); len(joined) < len(text) {
		t.Errorf("content lost: %d chars in, %d out", len(text), len(joined))
	}
}

func TestSplitIntoChunksPreservesMultibyteRunes(t *testing.T) {
	text := strings.Repeat("日本語のテキストです。", 400)
	for _, c := range SplitIntoChunks(text) {
		if strings.ContainsRune(c, '�') {
			t.Fatalf("chunk contains a replacement rune — a split landed mid-rune")
		}
	}
}

func TestNormalizeTermFoldsPlurals(t *testing.T) {
	cases := map[string]string{
		"invoices":  "invoice",
		"notes":     "note",
		"companies": "company",
		"boxes":     "box",
		"batches":   "batch",
		"class":     "class",
		"status":    "status",
		"analysis":  "analysis",
		"uses":      "use",
		"data":      "data",
	}
	for in, want := range cases {
		if got := normalizeTerm(in); got != want {
			t.Errorf("normalizeTerm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchRanksRelevantChunkFirst(t *testing.T) {
	chunks := []Chunk{
		{DocID: "d1", DocName: "a.md", Ordinal: 1, Total: 3, Text: "The office cat is named Mochi and sleeps on the printer."},
		{DocID: "d1", DocName: "a.md", Ordinal: 2, Total: 3, Text: "Quarterly revenue reached 4.2 million dollars, up 18 percent."},
		{DocID: "d1", DocName: "a.md", Ordinal: 3, Total: 3, Text: "The parking garage closes at eleven on weekdays."},
	}
	ix := buildIndex(chunks)

	results := ix.search("quarterly revenue", 3)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Ordinal != 2 {
		t.Errorf("expected the revenue chunk first, got ordinal %d (%q)", results[0].Ordinal, results[0].Text)
	}
}

func TestSearchMatchesAcrossPluralForms(t *testing.T) {
	ix := buildIndex([]Chunk{
		{DocID: "d1", DocName: "a.md", Ordinal: 1, Total: 1, Text: "Each invoice is archived after ninety days."},
	})
	if got := ix.search("invoices", 3); len(got) == 0 {
		t.Error("plural query did not match singular document text")
	}
}

func TestSearchReturnsNothingForUnrelatedQuery(t *testing.T) {
	ix := buildIndex([]Chunk{
		{DocID: "d1", DocName: "a.md", Ordinal: 1, Total: 1, Text: "The parking garage closes at eleven."},
	})
	if got := ix.search("photosynthesis chlorophyll", 3); len(got) != 0 {
		t.Errorf("expected no matches, got %d", len(got))
	}
}

func TestSearchStopwordOnlyQueryStillReturnsSomething(t *testing.T) {
	ix := buildIndex([]Chunk{
		{DocID: "d1", DocName: "a.md", Ordinal: 1, Total: 1, Text: "What is this about? It is about the thing."},
	})
	if got := ix.search("what is it about", 3); len(got) == 0 {
		t.Error("expected the stopword fallback to return a match")
	}
}

func TestSearchEmptyIndex(t *testing.T) {
	if got := buildIndex(nil).search("anything", 3); got != nil {
		t.Errorf("expected nil from an empty index, got %v", got)
	}
}

func TestExtractPlainText(t *testing.T) {
	got, err := Extract("notes.md", []byte("# Title\n\nSome body text."))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Some body text.") {
		t.Errorf("extraction lost content: %q", got)
	}
}

func TestExtractRejectsBinaryAndEmpty(t *testing.T) {
	if _, err := Extract("thing.bin", []byte{0x00, 0x01, 0x02, 0xff}); err == nil {
		t.Error("expected binary content to be rejected")
	}
	if _, err := Extract("empty.txt", nil); err == nil {
		t.Error("expected empty file to be rejected")
	}
	if _, err := Extract("old.doc", []byte("anything")); err == nil {
		t.Error("expected legacy .doc to be rejected with guidance")
	}
}

func TestExtractRejectsOversizedFile(t *testing.T) {
	if _, err := Extract("big.txt", bytes.Repeat([]byte("a"), MaxFileSize+1)); err == nil {
		t.Error("expected oversized file to be rejected")
	}
}

func TestExtractDOCX(t *testing.T) {
	data := buildDOCX(t, `<w:p><w:r><w:t>Annual report</w:t></w:r></w:p>`+
		`<w:p><w:r><w:t>Revenue grew by</w:t><w:tab/><w:t>18 percent.</w:t></w:r></w:p>`)

	got, err := Extract("report.docx", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "Annual report") || !strings.Contains(got, "18 percent.") {
		t.Errorf("DOCX extraction lost content: %q", got)
	}
	// Paragraphs must stay separated so the chunker can split on them.
	if !strings.Contains(got, "Annual report\n\nRevenue grew by") {
		t.Errorf("paragraph break not preserved: %q", got)
	}
}

func TestExtractRejectsInvalidDOCX(t *testing.T) {
	if _, err := Extract("broken.docx", []byte("not a zip file at all")); err == nil {
		t.Error("expected a corrupt DOCX to be rejected")
	}
}

func TestExtractRejectsCorruptPDF(t *testing.T) {
	// Must return an error rather than panicking out of the parser.
	if _, err := Extract("broken.pdf", []byte("%PDF-1.4\nthis is not a real pdf")); err == nil {
		t.Error("expected a corrupt PDF to be rejected")
	}
}

func TestNormalizeWhitespaceCollapsesBlankRuns(t *testing.T) {
	got := normalizeWhitespace("a\r\n\r\n\r\n\r\nb   \n\t\nc")
	if got != "a\n\nb\n\nc" {
		t.Errorf("got %q, want %q", got, "a\n\nb\n\nc")
	}
}

func TestStoreAddListSearchDelete(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil)

	doc, err := s.Add(ctx, "sess-1", "handbook.md", []byte(
		"# Handbook\n\nThe expense reimbursement deadline is thirty days after travel.\n\nParking is validated in garage B."))
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if doc.NumChunks == 0 || doc.Chars == 0 {
		t.Errorf("document metadata not populated: %+v", doc)
	}

	if got := s.List("sess-1"); len(got) != 1 {
		t.Fatalf("expected 1 document, got %d", len(got))
	}

	results := s.Search("sess-1", "reimbursement deadline", 5)
	if len(results) == 0 {
		t.Fatal("expected a search hit")
	}
	if !strings.Contains(results[0].Text, "thirty days") {
		t.Errorf("wrong passage returned: %q", results[0].Text)
	}
	if results[0].DocName != "handbook.md" {
		t.Errorf("result not attributed to its document: %q", results[0].DocName)
	}

	if !s.Delete(ctx, "sess-1", doc.ID) {
		t.Error("Delete reported the document as missing")
	}
	if got := s.List("sess-1"); len(got) != 0 {
		t.Errorf("document survived deletion: %d left", len(got))
	}
	if got := s.Search("sess-1", "reimbursement", 5); len(got) != 0 {
		t.Error("index still returns hits after deletion")
	}
}

func TestStoreIsolatesSessions(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil)

	if _, err := s.Add(ctx, "sess-1", "secret.txt", []byte("The passphrase is hummingbird.")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if got := s.Search("sess-2", "passphrase hummingbird", 5); len(got) != 0 {
		t.Error("one session's documents leaked into another")
	}
	if got := s.List("sess-2"); len(got) != 0 {
		t.Error("List leaked another session's documents")
	}
}

func TestStoreReplacesSameFilename(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil)

	if _, err := s.Add(ctx, "sess-1", "notes.txt", []byte("Original content about penguins.")); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}
	if _, err := s.Add(ctx, "sess-1", "notes.txt", []byte("Replacement content about walruses.")); err != nil {
		t.Fatalf("second Add failed: %v", err)
	}

	if got := s.List("sess-1"); len(got) != 1 {
		t.Fatalf("expected the re-upload to replace, got %d documents", len(got))
	}
	if got := s.Search("sess-1", "penguins", 5); len(got) != 0 {
		t.Error("replaced document is still searchable")
	}
	if got := s.Search("sess-1", "walruses", 5); len(got) == 0 {
		t.Error("replacement document is not searchable")
	}
}

func TestStoreEnforcesDocumentLimit(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil)

	for i := 0; i < MaxDocumentsPerSession; i++ {
		name := fmt.Sprintf("doc-%d.txt", i)
		if _, err := s.Add(ctx, "sess-1", name, []byte("some indexable content here")); err != nil {
			t.Fatalf("Add %d failed: %v", i, err)
		}
	}
	if _, err := s.Add(ctx, "sess-1", "one-too-many.txt", []byte("content")); err == nil {
		t.Error("expected the document limit to be enforced")
	}
}

func TestStoreDeleteSessionDropsDocuments(t *testing.T) {
	ctx := context.Background()
	s := NewStore(nil)

	if _, err := s.Add(ctx, "sess-1", "notes.txt", []byte("Content about migratory birds.")); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	s.DeleteSession("sess-1")

	if got := s.List("sess-1"); len(got) != 0 {
		t.Error("documents survived session deletion")
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	// The agent holds a *Store that tests and embedders may leave nil.
	var s *Store
	if got := s.List("sess-1"); got != nil {
		t.Error("expected nil List from a nil store")
	}
	if got := s.Search("sess-1", "anything", 5); got != nil {
		t.Error("expected nil Search from a nil store")
	}
	s.DeleteSession("sess-1")
}

func TestSessionIDContextRoundTrip(t *testing.T) {
	ctx := WithSessionID(context.Background(), "sess-abc")
	if got := SessionIDFrom(ctx); got != "sess-abc" {
		t.Errorf("got %q, want %q", got, "sess-abc")
	}
	if got := SessionIDFrom(context.Background()); got != "" {
		t.Errorf("expected empty string for a bare context, got %q", got)
	}
}

// buildDOCX assembles a minimal OOXML package around the given body XML.
func buildDOCX(t *testing.T, bodyXML string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	doc := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + bodyXML + `</w:body></w:document>`
	if _, err := w.Write([]byte(doc)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return buf.Bytes()
}
