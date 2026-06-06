package ticket

import (
	"sort"
	"strings"
)

// SearchResult pairs a ticket with its relevance score for a query.
type SearchResult struct {
	Ticket *Ticket
	Score  float64
}

// Field weights for relevance scoring. Title hits are the strongest signal for
// duplicate detection, body (description/design/acceptance) is the bulk of the
// content, and notes are incidental discussion so they count least.
const (
	titleWeight = 3.0
	bodyWeight  = 1.0
	notesWeight = 0.5
)

// Search ranks tickets by token-based relevance to query. The query is
// lowercased and tokenized on non-alphanumeric boundaries; each term contributes
// per field (title/body/notes) by occurrence count times the field weight, so
// partial substring matches still score (fuzzy) and term frequency matters.
// Only tickets with a positive score are returned, sorted by score descending
// then ID ascending. An empty query (no terms) returns nil.
func Search(tickets []*Ticket, query string) []SearchResult {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	var results []SearchResult
	for _, t := range tickets {
		title := strings.ToLower(t.Title)
		body := strings.ToLower(t.Body)
		var notes strings.Builder
		for _, n := range t.Notes {
			notes.WriteString(n.Text)
			notes.WriteByte('\n')
		}
		noteText := strings.ToLower(notes.String())

		var score float64
		for _, term := range terms {
			score += float64(strings.Count(title, term)) * titleWeight
			score += float64(strings.Count(body, term)) * bodyWeight
			score += float64(strings.Count(noteText, term)) * notesWeight
		}

		if score > 0 {
			results = append(results, SearchResult{Ticket: t, Score: score})
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

// tokenize lowercases s and splits it into terms on non-alphanumeric runes.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}
