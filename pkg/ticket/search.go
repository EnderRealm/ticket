package ticket

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// SearchResult pairs a ticket with its relevance score for a query.
type SearchResult struct {
	Ticket  *Ticket
	Score   float64
	Field   string // "title", "body", or "notes" — where the best match was found
	Snippet string // one-line context excerpt around the earliest matched term in Field
}

// Field weights for relevance scoring. Title hits are the strongest signal for
// duplicate detection, body (description/design/acceptance) is the bulk of the
// content, and notes are incidental discussion so they count least.
const (
	titleWeight = 3.0
	bodyWeight  = 1.0
	notesWeight = 0.5
)

// snippetWidth is the maximum byte length of a context excerpt, before ellipses.
const snippetWidth = 80

// Search ranks tickets by token-based relevance to query. The query is
// lowercased and tokenized on non-alphanumeric boundaries; each term contributes
// per field (title/body/notes) by occurrence count times the field weight, so
// partial substring matches still score (fuzzy) and term frequency matters.
// Only tickets with a positive score are returned, sorted by score descending
// then ID ascending. An empty query (no terms) returns nil.
func Search(tickets []*Ticket, query string) []SearchResult {
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	var results []SearchResult
	for _, t := range tickets {
		title := strings.ToLower(t.Title)
		body := strings.ToLower(t.Body)
		noteText := strings.ToLower(notesText(t))

		var score float64
		for _, term := range terms {
			score += float64(strings.Count(title, term)) * titleWeight
			score += float64(strings.Count(body, term)) * bodyWeight
			score += float64(strings.Count(noteText, term)) * notesWeight
		}

		if score > 0 {
			field, snippet := bestSnippet(t, terms)
			results = append(results, SearchResult{Ticket: t, Score: score, Field: field, Snippet: snippet})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Ticket.ID < results[j].Ticket.ID
	})

	return results
}

// notesText concatenates a ticket's note bodies into a single newline-joined
// string. Defined once so scoring and snippet selection see the same text.
func notesText(t *Ticket) string {
	var b strings.Builder
	for _, n := range t.Notes {
		b.WriteString(n.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// bestSnippet picks the highest-weighted field that yields a context excerpt for
// any term, returning the field name and the excerpt.
func bestSnippet(t *Ticket, terms []string) (string, string) {
	fields := []struct {
		name string
		text string
	}{
		{"title", t.Title},
		{"body", t.Body},
		{"notes", notesText(t)},
	}
	for _, f := range fields {
		if s, ok := excerpt(f.text, terms, snippetWidth); ok {
			return f.name, s
		}
	}
	return "", ""
}

// excerpt collapses whitespace in text and returns a one-line context window of
// at most max bytes centered on the earliest matched term, prefixed/suffixed with
// an ellipsis when truncated. Window edges snap to UTF-8 rune boundaries so multi-
// byte runes are never split. Returns ok=false when no term occurs.
func excerpt(text string, terms []string, max int) (string, bool) {
	clean := strings.Join(strings.Fields(text), " ")
	lower := ToLowerASCII(clean)

	idx := -1
	matchLen := 0
	for _, term := range terms {
		if term == "" {
			continue
		}
		if i := strings.Index(lower, term); i >= 0 && (idx < 0 || i < idx) {
			idx = i
			matchLen = len(term)
		}
	}
	if idx < 0 {
		return "", false
	}

	if len(clean) <= max {
		return clean, true
	}

	// Center the window on the match.
	center := idx + matchLen/2
	start := center - max/2
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > len(clean) {
		end = len(clean)
		start = end - max
		if start < 0 {
			start = 0
		}
	}

	// Snap edges to rune boundaries.
	for start > 0 && !utf8.RuneStart(clean[start]) {
		start++
	}
	for end < len(clean) && !utf8.RuneStart(clean[end]) {
		end++
	}

	out := clean[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(clean) {
		out = out + "…"
	}
	return out, true
}

// ToLowerASCII lowercases ASCII letters only, leaving every other byte
// (including UTF-8 multibyte sequences) untouched. Unlike strings.ToLower it
// is byte-length-preserving, so byte offsets computed against the result map
// back onto the original string — required for offset-stable substring matching.
func ToLowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// Tokenize lowercases s and splits it into terms on non-alphanumeric runes.
func Tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}
