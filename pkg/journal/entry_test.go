package journal

import (
	"encoding/json"
	"testing"
)

func TestEntryJSON(t *testing.T) {
	e := Entry{
		SHA:          "abc123",
		Ticket:       "fix-bug-1234",
		Repo:         "/tmp/repo",
		TS:           "2026-03-28T10:00:00Z",
		Msg:          "[fix-bug-1234] Fix the thing",
		Author:       "steve",
		Action:       "ref",
		FilesChanged: []string{"main.go", "util.go"},
		LinesAdded:   42,
		LinesRemoved: 7,
		Branch:       "master",
		WorkStarted:  "2026-03-28T09:55:00Z",
		WorkEnded:    "2026-03-28T10:00:00Z",
		DurationSecs: 300,
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SHA != e.SHA {
		t.Errorf("SHA = %q, want %q", decoded.SHA, e.SHA)
	}
	if decoded.Ticket != e.Ticket {
		t.Errorf("Ticket = %q, want %q", decoded.Ticket, e.Ticket)
	}
	if decoded.Action != e.Action {
		t.Errorf("Action = %q, want %q", decoded.Action, e.Action)
	}
	if decoded.LinesAdded != e.LinesAdded {
		t.Errorf("LinesAdded = %d, want %d", decoded.LinesAdded, e.LinesAdded)
	}
	if decoded.LinesRemoved != e.LinesRemoved {
		t.Errorf("LinesRemoved = %d, want %d", decoded.LinesRemoved, e.LinesRemoved)
	}
	if decoded.DurationSecs != e.DurationSecs {
		t.Errorf("DurationSecs = %d, want %d", decoded.DurationSecs, e.DurationSecs)
	}
	if len(decoded.FilesChanged) != 2 {
		t.Errorf("FilesChanged len = %d, want 2", len(decoded.FilesChanged))
	}

	// Verify JSON field names match tkt convention
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"sha", "ticket", "repo", "ts", "msg", "author", "action",
		"files_changed", "lines_added", "lines_removed", "branch",
		"work_started", "work_ended", "duration_seconds"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

func TestEntryJSON_OmitEmpty(t *testing.T) {
	e := Entry{SHA: "abc", Ticket: "t-1", Action: "ref"}
	data, _ := json.Marshal(e)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	for _, key := range []string{"files_changed", "lines_added", "lines_removed", "branch",
		"work_started", "work_ended", "duration_seconds"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected key %q to be omitted for zero value", key)
		}
	}
}

func TestEffort(t *testing.T) {
	entries := []Entry{
		{LinesAdded: 10, LinesRemoved: 2, FilesChanged: []string{"a.go", "b.go"}},
		{LinesAdded: 5, LinesRemoved: 3, FilesChanged: []string{"b.go", "c.go"}},
	}
	s := Effort(entries)
	if s.LinesAdded != 15 {
		t.Errorf("LinesAdded = %d, want 15", s.LinesAdded)
	}
	if s.LinesRemoved != 5 {
		t.Errorf("LinesRemoved = %d, want 5", s.LinesRemoved)
	}
	if s.FilesChanged != 3 {
		t.Errorf("FilesChanged = %d, want 3 (deduplicated)", s.FilesChanged)
	}
	if s.Commits != 2 {
		t.Errorf("Commits = %d, want 2", s.Commits)
	}
	if s.String() == "" {
		t.Error("String() should not be empty for non-zero summary")
	}
}

func TestEffort_Empty(t *testing.T) {
	s := Effort(nil)
	if s.String() != "" {
		t.Errorf("String() = %q, want empty for zero summary", s.String())
	}
}

func TestGroupByTicket(t *testing.T) {
	entries := []Entry{
		{Ticket: "a", SHA: "1"},
		{Ticket: "b", SHA: "2"},
		{Ticket: "a", SHA: "3"},
	}
	grouped := GroupByTicket(entries)
	if len(grouped["a"]) != 2 {
		t.Errorf("ticket a: got %d entries, want 2", len(grouped["a"]))
	}
	if len(grouped["b"]) != 1 {
		t.Errorf("ticket b: got %d entries, want 1", len(grouped["b"]))
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"hello\nworld", "hello"},
		{"", ""},
		{"\n", ""},
	}
	for _, tt := range tests {
		if got := FirstLine(tt.input); got != tt.want {
			t.Errorf("FirstLine(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
