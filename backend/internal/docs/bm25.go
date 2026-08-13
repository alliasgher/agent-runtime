package docs

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 tuning. These are the standard Robertson/Sparck-Jones defaults: k1
// controls how fast term frequency saturates, b how strongly long chunks are
// penalised.
const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

// Chunk is one retrievable passage, tagged with where it came from so the
// model can cite the source document.
type Chunk struct {
	DocID   string
	DocName string
	Ordinal int // 1-based position within its document
	Total   int // total chunks in that document
	Text    string
}

// Result is a chunk with its relevance score for a given query.
type Result struct {
	Chunk
	Score float64
}

// index is an immutable BM25 index over every chunk in one session. It is
// rebuilt whenever a document is added or removed — corpus statistics depend
// on the full set, and sessions hold a handful of documents at most.
type index struct {
	chunks    []Chunk
	termFreqs []map[string]int
	lengths   []int
	docFreq   map[string]int
	avgLen    float64
}

func buildIndex(chunks []Chunk) *index {
	ix := &index{
		chunks:    chunks,
		termFreqs: make([]map[string]int, len(chunks)),
		lengths:   make([]int, len(chunks)),
		docFreq:   make(map[string]int),
	}

	total := 0
	for i, c := range chunks {
		// Chunks are indexed with stopwords intact. They cost almost nothing —
		// IDF drives a term appearing in every chunk to near zero — and keeping
		// them is what lets a query like "what is this about" fall back to
		// matching anything at all instead of returning empty.
		terms := tokenizeRaw(c.Text)
		tf := make(map[string]int, len(terms))
		for _, t := range terms {
			tf[t]++
		}
		ix.termFreqs[i] = tf
		ix.lengths[i] = len(terms)
		total += len(terms)
		for t := range tf {
			ix.docFreq[t]++
		}
	}

	if len(chunks) > 0 {
		ix.avgLen = float64(total) / float64(len(chunks))
	}
	return ix
}

// search ranks chunks against the query and returns the best `limit`, dropping
// anything that matched no query term at all.
func (ix *index) search(query string, limit int) []Result {
	if ix == nil || len(ix.chunks) == 0 {
		return nil
	}

	terms := tokenize(query)
	if len(terms) == 0 {
		// The query was nothing but stopwords ("what is this about?"). Fall
		// back to the raw tokens: they carry near-zero IDF, so every chunk
		// scores about the same and the tiebreak surfaces each document's
		// opening passages — which is the useful answer to that question.
		terms = tokenizeRaw(query)
	}
	if len(terms) == 0 {
		return nil
	}

	n := float64(len(ix.chunks))
	scores := make([]Result, 0, len(ix.chunks))

	for i, c := range ix.chunks {
		var score float64
		for _, term := range terms {
			tf, ok := ix.termFreqs[i][term]
			if !ok {
				continue
			}
			df := float64(ix.docFreq[term])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))

			norm := 1 - bm25B
			if ix.avgLen > 0 {
				norm += bm25B * float64(ix.lengths[i]) / ix.avgLen
			}
			score += idf * (float64(tf) * (bm25K1 + 1)) / (float64(tf) + bm25K1*norm)
		}
		if score > 0 {
			scores = append(scores, Result{Chunk: c, Score: score})
		}
	}

	sort.SliceStable(scores, func(a, b int) bool {
		if scores[a].Score != scores[b].Score {
			return scores[a].Score > scores[b].Score
		}
		// Stable tiebreak so identical scores return in document order.
		if scores[a].DocName != scores[b].DocName {
			return scores[a].DocName < scores[b].DocName
		}
		return scores[a].Ordinal < scores[b].Ordinal
	})

	if len(scores) > limit {
		scores = scores[:limit]
	}
	return scores
}

// tokenize lowercases, splits on non-alphanumerics, drops stopwords and
// single characters, and normalises plurals. Used for queries.
func tokenize(text string) []string {
	return tokenizeWith(text, true)
}

// tokenizeRaw keeps stopwords. Used for indexing, and for queries that consist
// of nothing but stopwords.
func tokenizeRaw(text string) []string {
	return tokenizeWith(text, false)
}

func tokenizeWith(text string, dropStopwords bool) []string {
	var (
		out []string
		buf strings.Builder
	)

	emit := func() {
		if buf.Len() == 0 {
			return
		}
		tok := buf.String()
		buf.Reset()
		if len(tok) < 2 {
			return
		}
		if dropStopwords && stopwords[tok] {
			return
		}
		out = append(out, normalizeTerm(tok))
	}

	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
			continue
		}
		emit()
	}
	emit()

	return out
}

// normalizeTerm folds common English plurals so "invoices" matches "invoice".
// Deliberately conservative — over-stemming costs precision, and the same
// function runs over both the query and the documents, so it only has to be
// consistent.
func normalizeTerm(tok string) string {
	if len(tok) <= 3 {
		return tok
	}
	switch {
	case len(tok) > 4 && strings.HasSuffix(tok, "ies"):
		return tok[:len(tok)-3] + "y" // companies -> company
	case len(tok) > 4 && (strings.HasSuffix(tok, "ses") ||
		strings.HasSuffix(tok, "xes") ||
		strings.HasSuffix(tok, "zes") ||
		strings.HasSuffix(tok, "ches") ||
		strings.HasSuffix(tok, "shes")):
		return tok[:len(tok)-2] // boxes -> box, batches -> batch
	case strings.HasSuffix(tok, "ss"), strings.HasSuffix(tok, "us"), strings.HasSuffix(tok, "is"):
		return tok // class, status, analysis
	case strings.HasSuffix(tok, "s"):
		return tok[:len(tok)-1] // notes -> note
	}
	return tok
}

// stopwords carry almost no discriminating power, so they're stripped from
// queries. Chunks keep them — see buildIndex.
var stopwords = map[string]bool{
	"the": true, "be": true, "to": true, "of": true, "and": true, "in": true,
	"that": true, "have": true, "it": true, "for": true, "not": true, "on": true,
	"with": true, "as": true, "do": true, "at": true, "this": true, "but": true,
	"by": true, "from": true, "they": true, "we": true, "say": true, "her": true,
	"she": true, "or": true, "an": true, "will": true, "my": true, "one": true,
	"all": true, "would": true, "there": true, "their": true, "what": true,
	"so": true, "up": true, "out": true, "if": true, "about": true, "who": true,
	"get": true, "which": true, "go": true, "me": true, "when": true, "make": true,
	"can": true, "like": true, "no": true, "just": true, "him": true, "know": true,
	"take": true, "into": true, "your": true, "some": true, "could": true,
	"them": true, "see": true, "other": true, "than": true, "then": true,
	"now": true, "look": true, "only": true, "come": true, "its": true, "over": true,
	"also": true, "back": true, "after": true, "use": true, "two": true, "how": true,
	"our": true, "well": true, "way": true, "even": true, "want": true, "any": true,
	"these": true, "us": true, "is": true, "are": true, "was": true, "were": true,
	"been": true, "has": true, "had": true, "does": true, "did": true, "am": true,
	"you": true, "he": true, "his": true, "hers": true, "theirs": true, "ours": true,
	"where": true, "why": true, "whom": true, "should": true, "may": true,
	"might": true, "must": true, "shall": true, "very": true, "much": true,
	"more": true, "most": true, "such": true, "own": true, "same": true, "too": true,
}
