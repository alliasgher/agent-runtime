package docs

import (
	"strings"
	"unicode/utf8"
)

const (
	// ChunkTargetChars is the size a chunk grows to before a new one starts.
	// Large enough to hold a coherent passage, small enough that five of them
	// fit in a tool result without crowding out the conversation.
	ChunkTargetChars = 1200

	// ChunkOverlapChars is how much of a chunk's tail seeds the next one, so a
	// passage that straddles a boundary is still matchable as a whole.
	ChunkOverlapChars = 200
)

// SplitIntoChunks splits extracted text into overlapping passages, preferring
// paragraph boundaries, then sentence boundaries, and only splitting
// mid-sentence when a single sentence is longer than a whole chunk.
func SplitIntoChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	units := splitIntoUnits(text)

	var (
		chunks  []string
		current []string
		size    int
		fresh   int // units added since the last flush; overlap doesn't count
	)

	flush := func() {
		if fresh == 0 {
			return
		}
		chunk := strings.TrimSpace(strings.Join(current, "\n\n"))
		current, size, fresh = nil, 0, 0
		if chunk == "" {
			return
		}
		chunks = append(chunks, chunk)
		if tail := overlapTail(chunk); tail != "" {
			current = append(current, tail)
			size = len(tail) + 2
		}
	}

	for _, unit := range units {
		if size > 0 && size+len(unit) > ChunkTargetChars {
			flush()
		}
		current = append(current, unit)
		size += len(unit) + 2
		fresh++
	}
	flush()

	return chunks
}

// splitIntoUnits breaks text into pieces that each fit inside a chunk.
func splitIntoUnits(text string) []string {
	var units []string
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if len(para) <= ChunkTargetChars {
			units = append(units, para)
			continue
		}
		for _, sentence := range splitSentences(para) {
			if len(sentence) <= ChunkTargetChars {
				units = append(units, sentence)
				continue
			}
			units = append(units, splitLongText(sentence)...)
		}
	}
	return units
}

// splitSentences breaks on terminal punctuation followed by whitespace. It
// splits abbreviations like "e.g." too, which is harmless here: this only runs
// on paragraphs already too long to keep whole.
func splitSentences(text string) []string {
	var out []string
	start := 0

	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '.', '!', '?', '\n':
		default:
			continue
		}

		// Absorb trailing punctuation and closing marks: `...)" ` ends one sentence.
		end := i + 1
		for end < len(text) && strings.IndexByte(`.!?"')]`, text[end]) >= 0 {
			end++
		}
		// Require whitespace after, so decimals like 3.14 stay intact.
		if end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '\t' {
			continue
		}
		if s := strings.TrimSpace(text[start:end]); s != "" {
			out = append(out, s)
		}
		start = end
		i = end - 1
	}

	if s := strings.TrimSpace(text[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// splitLongText hard-splits a run of text with no usable sentence break,
// backing up to a word boundary and never cutting mid-rune.
func splitLongText(text string) []string {
	var out []string

	for len(text) > ChunkTargetChars {
		cut := ChunkTargetChars
		if idx := strings.LastIndexByte(text[:cut], ' '); idx > ChunkTargetChars/2 {
			cut = idx
		}
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if cut == 0 {
			// A single rune longer than the backoff window — advance instead of
			// looping forever.
			cut = ChunkTargetChars
			for cut < len(text) && !utf8.RuneStart(text[cut]) {
				cut++
			}
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}

	if text != "" {
		out = append(out, text)
	}
	return out
}

// overlapTail returns the trailing slice of a chunk, snapped forward to a word
// boundary. Returns "" when the chunk is too short for overlap to add anything.
func overlapTail(chunk string) string {
	if len(chunk) <= ChunkOverlapChars {
		return ""
	}
	tail := chunk[len(chunk)-ChunkOverlapChars:]
	if idx := strings.IndexByte(tail, ' '); idx >= 0 {
		tail = tail[idx+1:]
	} else {
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}
	return strings.TrimSpace(tail)
}
