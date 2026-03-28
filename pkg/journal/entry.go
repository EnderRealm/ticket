package journal

import "fmt"

// Entry represents a single commit-to-ticket link in the journal JSONL file.
type Entry struct {
	SHA          string   `json:"sha"`
	Ticket       string   `json:"ticket"`
	Repo         string   `json:"repo"`
	TS           string   `json:"ts"`
	Msg          string   `json:"msg"`
	Author       string   `json:"author"`
	Action       string   `json:"action"`
	FilesChanged []string `json:"files_changed,omitempty"`
	LinesAdded   int      `json:"lines_added,omitempty"`
	LinesRemoved int      `json:"lines_removed,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	WorkStarted  string   `json:"work_started,omitempty"`
	WorkEnded    string   `json:"work_ended,omitempty"`
	DurationSecs int      `json:"duration_seconds,omitempty"`
}

// EffortSummary holds aggregated diff stats for a set of journal entries.
type EffortSummary struct {
	LinesAdded   int
	LinesRemoved int
	FilesChanged int
	Commits      int
}

// String returns a human-readable effort summary, or empty string if no data.
func (e EffortSummary) String() string {
	if e.LinesAdded == 0 && e.LinesRemoved == 0 && e.FilesChanged == 0 {
		return ""
	}
	return fmt.Sprintf("+%d -%d, %d file(s)", e.LinesAdded, e.LinesRemoved, e.FilesChanged)
}

// Effort computes aggregated diff stats from a slice of journal entries.
func Effort(entries []Entry) EffortSummary {
	fileSet := map[string]struct{}{}
	var added, removed int
	for _, e := range entries {
		added += e.LinesAdded
		removed += e.LinesRemoved
		for _, f := range e.FilesChanged {
			fileSet[f] = struct{}{}
		}
	}
	return EffortSummary{
		LinesAdded:   added,
		LinesRemoved: removed,
		FilesChanged: len(fileSet),
		Commits:      len(entries),
	}
}

// GroupByTicket groups entries by ticket ID into a map.
func GroupByTicket(entries []Entry) map[string][]Entry {
	out := make(map[string][]Entry)
	for _, e := range entries {
		out[e.Ticket] = append(out[e.Ticket], e)
	}
	return out
}

// FirstLine returns the first line of s, truncating at the first newline.
func FirstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
