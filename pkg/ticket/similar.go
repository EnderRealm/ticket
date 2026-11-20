package ticket

import (
	"fmt"
	"sort"
	"strings"
)

// similarThreshold is the Jaccard overlap of two token sets at which they
// count as the same subject; maxSimilar caps how many candidates come back.
const (
	similarThreshold = 0.5
	maxSimilar       = 5
	minSimilarShared = 3
)

// Similar is an existing ticket that resembles a ticket about to be created.
type Similar struct {
	ID     string
	Title  string
	Status Status
	Score  float64
}

// SimilarTickets returns the non-done, non-closed tickets in existing that
// closely match t, best first. A match is a title overlapping t's title, or a
// body overlapping t's body, by at least similarThreshold (Jaccard over word
// sets, three or more shared words). Tickets of another project than t are
// skipped, and so is t itself. Advisory: callers report it and never refuse.
func SimilarTickets(existing []*Ticket, t *Ticket) []Similar {
	project, _ := ParseNamespacedID(t.ID)
	title := tokenSet(t.Title)
	body := tokenSet(t.Body)
	var out []Similar
	for _, e := range existing {
		if e.ID == t.ID || e.Status == StatusDone || e.Status == StatusClosed {
			continue
		}
		if p, _ := ParseNamespacedID(e.ID); p != project {
			continue
		}
		score := jaccard(title, tokenSet(e.Title))
		if len(body) > 0 {
			if s := jaccard(body, tokenSet(e.Body)); s > score {
				score = s
			}
		}
		if score >= similarThreshold {
			out = append(out, Similar{ID: e.ID, Title: e.Title, Status: e.Status, Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > maxSimilar {
		out = out[:maxSimilar]
	}
	return out
}

// SimilarTicketsWarning is the one phrasing for candidates SimilarTickets
// found, shared by the CLI and the MCP tool. Empty when there are none.
func SimilarTicketsWarning(id string, similar []Similar) string {
	if len(similar) == 0 {
		return ""
	}
	names := make([]string, len(similar))
	for i, s := range similar {
		names[i] = fmt.Sprintf("%s (%s)", s.ID, s.Title)
	}
	return fmt.Sprintf("ticket %s was created, but resembles open tickets: %s. If it restates one of them, add your context to that ticket (ticket_add_note) and close %s rather than working it twice.",
		id, strings.Join(names, "; "), id)
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range Tokenize(s) {
		if len(w) >= 3 {
			set[w] = true
		}
	}
	return set
}

func jaccard(a, b map[string]bool) float64 {
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	if shared < minSimilarShared {
		return 0
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}
