package ticket

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// idCounter provides per-process monotonic noise so that multiple IDs
// generated within the same nanosecond still differ.
var idCounter atomic.Uint64

// stopWords are common filler words stripped from titles when generating slugs.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "shall": true, "should": true,
	"may": true, "might": true, "must": true, "can": true, "could": true,
	"of": true, "in": true, "to": true, "for": true, "on": true,
	"at": true, "by": true, "with": true, "from": true, "as": true,
	"into": true, "through": true, "about": true, "between": true,
	"after": true, "before": true, "above": true, "below": true,
	"up": true, "down": true, "out": true, "off": true, "over": true,
	"under": true, "and": true, "but": true, "or": true, "nor": true,
	"not": true, "no": true, "so": true, "yet": true, "both": true,
	"either": true, "neither": true, "each": true, "every": true,
	"all": true, "any": true, "few": true, "more": true, "most": true,
	"some": true, "such": true, "than": true, "too": true, "very": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "what": true, "which": true, "who": true, "whom": true,
	"how": true, "when": true, "where": true, "why": true,
}

// GenerateID creates a ticket ID from the title and a hash.
// Format: slug-hash where slug is up to 3 meaningful words from the title,
// and hash is 4 hex chars derived from PID + timestamp.
func GenerateID(title string) string {
	return GenerateIDFrom(title, time.Now())
}

// GenerateIDFrom creates a ticket ID from explicit inputs (testable).
func GenerateIDFrom(title string, t time.Time) string {
	slug := slugifyTitle(title)
	hash := idHash(t)
	return slug + "-" + hash
}

// slugifyTitle extracts up to 3 meaningful words from the title,
// stripping stop words and non-alphanumeric characters.
func slugifyTitle(title string) string {
	// Lowercase and replace non-alphanumeric with spaces.
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}

	words := strings.Fields(b.String())

	var slug []string
	for _, w := range words {
		if stopWords[w] {
			continue
		}
		slug = append(slug, w)
		if len(slug) >= 3 {
			break
		}
	}

	if len(slug) == 0 {
		return "ticket"
	}
	return strings.Join(slug, "-")
}

// ParseNamespacedID splits a namespaced ticket ID ("project/ticket-id")
// into its project and ticket ID components. If the ID contains no slash,
// the project is empty and the full string is the ticket ID.
func ParseNamespacedID(id string) (project, ticketID string) {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

// FormatNamespacedID combines a project name and ticket ID into a
// namespaced ID ("project/ticket-id"). If project is empty, returns
// the bare ticket ID.
func FormatNamespacedID(project, ticketID string) string {
	if project == "" {
		return ticketID
	}
	return project + "/" + ticketID
}

// idHash returns 4 hex chars from sha256 of nanosecond timestamp +
// monotonic counter. The counter prevents collisions when multiple IDs are
// generated within the same nanosecond.
func idHash(t time.Time) string {
	seq := idCounter.Add(1)
	data := fmt.Sprintf("%d%d", t.UnixNano(), seq)
	sum := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", sum[:2]) // 2 bytes = 4 hex chars
}
