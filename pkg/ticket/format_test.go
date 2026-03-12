package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleTicketYAML = `---
id: t-abc1
stage: triage
deps: [t-dep1, t-dep2]
links: []
created: 2026-02-22T00:57:39Z
type: task
priority: 1
assignee: Steve Macbeth
parent: t-0f08
tags: [phase-1]
---
# Sample ticket

This is the description.

## Design

Some design notes here.

## Acceptance Criteria

Things must work.
`

func TestParse_BasicFields(t *testing.T) {
	tk, err := Parse(strings.NewReader(sampleTicketYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if tk.ID != "t-abc1" {
		t.Errorf("ID = %q, want %q", tk.ID, "t-abc1")
	}
	if tk.Stage != StageTriage {
		t.Errorf("Stage = %q, want %q", tk.Stage, StageTriage)
	}
	if tk.Type != TypeTask {
		t.Errorf("Type = %q, want %q", tk.Type, TypeTask)
	}
	if tk.Priority != 1 {
		t.Errorf("Priority = %d, want 1", tk.Priority)
	}
	if tk.Assignee != "Steve Macbeth" {
		t.Errorf("Assignee = %q, want %q", tk.Assignee, "Steve Macbeth")
	}
	if tk.Parent != "t-0f08" {
		t.Errorf("Parent = %q, want %q", tk.Parent, "t-0f08")
	}
	if len(tk.Deps) != 2 || tk.Deps[0] != "t-dep1" || tk.Deps[1] != "t-dep2" {
		t.Errorf("Deps = %v, want [t-dep1, t-dep2]", tk.Deps)
	}
	if len(tk.Links) != 0 {
		t.Errorf("Links = %v, want []", tk.Links)
	}
	if len(tk.Tags) != 1 || tk.Tags[0] != "phase-1" {
		t.Errorf("Tags = %v, want [phase-1]", tk.Tags)
	}
	if tk.Title != "Sample ticket" {
		t.Errorf("Title = %q, want %q", tk.Title, "Sample ticket")
	}
}

func TestParse_Notes(t *testing.T) {
	input := `---
id: t-note1
stage: triage
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: task
priority: 2
---
# Ticket with notes

Description here.

## Notes

**2026-02-22T01:15:50Z**

First note text.

**2026-02-22T02:30:00Z**

Second note text.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(tk.Notes) != 2 {
		t.Fatalf("len(Notes) = %d, want 2", len(tk.Notes))
	}
	if tk.Notes[0].Text != "First note text." {
		t.Errorf("Notes[0].Text = %q, want %q", tk.Notes[0].Text, "First note text.")
	}
	if tk.Notes[1].Text != "Second note text." {
		t.Errorf("Notes[1].Text = %q, want %q", tk.Notes[1].Text, "Second note text.")
	}
	expected := time.Date(2026, 2, 22, 1, 15, 50, 0, time.UTC)
	if !tk.Notes[0].Timestamp.Equal(expected) {
		t.Errorf("Notes[0].Timestamp = %v, want %v", tk.Notes[0].Timestamp, expected)
	}
}

func TestParse_MissingFrontmatter(t *testing.T) {
	_, err := Parse(strings.NewReader("# Just a heading\nSome text\n"))
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestSerialize_RoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "t-abc1",
		Stage:    StageTriage,
		Type:     TypeTask,
		Priority: 1,
		Assignee: "Steve Macbeth",
		Parent:   "t-0f08",
		Deps:     []string{"t-dep1"},
		Links:    []string{},
		Tags:     []string{"phase-1"},
		Created:  time.Date(2026, 2, 22, 0, 57, 39, 0, time.UTC),
		Title:    "Sample ticket",
		Body:     "\nThis is the description.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	// Parse it back.
	parsed, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}

	if parsed.ID != tk.ID {
		t.Errorf("ID = %q, want %q", parsed.ID, tk.ID)
	}
	if parsed.Title != tk.Title {
		t.Errorf("Title = %q, want %q", parsed.Title, tk.Title)
	}
	if parsed.Stage != tk.Stage {
		t.Errorf("Stage = %q, want %q", parsed.Stage, tk.Stage)
	}
	if len(parsed.Deps) != 1 || parsed.Deps[0] != "t-dep1" {
		t.Errorf("Deps = %v, want [t-dep1]", parsed.Deps)
	}
}

func TestSerialize_EmptyArrays(t *testing.T) {
	tk := &Ticket{
		ID:       "t-test",
		Stage:    StageTriage,
		Type:     TypeBug,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Test",
		Body:     "\n",
	}
	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "deps: []") {
		t.Error("empty deps should serialize as []")
	}
	if !strings.Contains(s, "links: []") {
		t.Error("empty links should serialize as []")
	}
}

func TestSerialize_WithNotes(t *testing.T) {
	tk := &Ticket{
		ID:       "t-test",
		Stage:    StageTriage,
		Type:     TypeTask,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Test",
		Body:     "\nDescription.\n",
		Notes: []Note{
			{Timestamp: time.Date(2026, 2, 22, 1, 0, 0, 0, time.UTC), Text: "A note."},
		},
	}
	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "## Notes") {
		t.Error("serialized output should contain ## Notes")
	}
	if !strings.Contains(s, "**2026-02-22T01:00:00Z**") {
		t.Error("serialized output should contain note timestamp")
	}
	if !strings.Contains(s, "A note.") {
		t.Error("serialized output should contain note text")
	}
}

func TestParse_RealTicketFiles(t *testing.T) {
	// Parse actual ticket files from the repo to verify compatibility.
	dir := filepath.Join("..", "..", ".tickets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no .tickets directory: %v", err)
	}

	var parsed int
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Errorf("open %s: %v", e.Name(), err)
			continue
		}
		tk, err := Parse(f)
		f.Close()
		if err != nil {
			t.Errorf("parse %s: %v", e.Name(), err)
			continue
		}
		if tk.ID == "" {
			t.Errorf("%s: empty ID", e.Name())
		}
		if tk.Title == "" {
			t.Errorf("%s: empty Title", e.Name())
		}
		parsed++
	}
	if parsed == 0 {
		t.Error("no ticket files were parsed")
	}
	t.Logf("successfully parsed %d ticket files", parsed)
}

func TestSerialize_StageFields(t *testing.T) {
	tk := &Ticket{
		ID:            "t-stage",
		Stage:         StageDesign,
		Review:        ReviewPending,
		Risk:          RiskHigh,
		Type:          TypeFeature,
		Priority:      1,
		Deps:          []string{},
		Links:         []string{},
		Tags:          []string{},
		Skipped:       []Stage{StageSpec},
		Conversations: []string{"sess-abc123"},
		Created:       time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:         "Stage test",
		Body:          "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "stage: design") {
		t.Error("missing stage field")
	}
	if !strings.Contains(s, "review: pending") {
		t.Error("missing review field")
	}
	if !strings.Contains(s, "risk: high") {
		t.Error("missing risk field")
	}
	if !strings.Contains(s, "skipped: [spec]") {
		t.Error("missing skipped field")
	}
	if !strings.Contains(s, "conversations: [sess-abc123]") {
		t.Error("missing conversations field")
	}
}

func TestSerialize_StageRoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:            "t-rt",
		Stage:         StageImplement,
		Review:        ReviewApproved,
		Risk:          RiskNormal,
		Type:          TypeBug,
		Priority:      2,
		Deps:          []string{},
		Links:         []string{},
		Skipped:       []Stage{},
		Conversations: []string{},
		Created:       time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:         "Round trip",
		Body:          "\nBug description.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	parsed, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}

	if parsed.Stage != StageImplement {
		t.Errorf("Stage = %s, want implement", parsed.Stage)
	}
	if parsed.Review != ReviewApproved {
		t.Errorf("Review = %s, want approved", parsed.Review)
	}
	if parsed.Risk != RiskNormal {
		t.Errorf("Risk = %s, want normal", parsed.Risk)
	}
}

func TestSerialize_ReviewLog(t *testing.T) {
	tk := &Ticket{
		ID:       "t-revlog",
		Stage:    StageDesign,
		Type:     TypeFeature,
		Priority: 1,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:    "With reviews",
		Body:     "\nDescription.\n",
		Reviews: []ReviewRecord{
			{
				Timestamp: time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC),
				Reviewer:  "agent:design-reviewer",
				Verdict:   "approved",
				Comment:   "All file paths verified.",
			},
		},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "## Review Log") {
		t.Error("missing Review Log section")
	}
	if !strings.Contains(s, "[agent:design-reviewer]") {
		t.Error("missing reviewer in review log")
	}
	if !strings.Contains(s, "APPROVED") {
		t.Error("missing verdict in review log")
	}
}

func TestParse_ReviewLog(t *testing.T) {
	input := `---
id: t-revlog
stage: design
deps: []
links: []
created: 2026-02-25T10:00:00Z
type: feature
priority: 1
---
# With reviews

Description.

## Review Log

**2026-02-25T12:00:00Z [agent:design-reviewer]**
APPROVED — All file paths verified.

**2026-02-25T14:00:00Z [human:steve]**
APPROVED — Proceed to implementation.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(tk.Reviews) != 2 {
		t.Fatalf("len(Reviews) = %d, want 2", len(tk.Reviews))
	}
	if tk.Reviews[0].Reviewer != "agent:design-reviewer" {
		t.Errorf("Reviews[0].Reviewer = %q, want agent:design-reviewer", tk.Reviews[0].Reviewer)
	}
	if tk.Reviews[0].Verdict != "approved" {
		t.Errorf("Reviews[0].Verdict = %q, want approved", tk.Reviews[0].Verdict)
	}
	if tk.Reviews[0].Comment != "All file paths verified." {
		t.Errorf("Reviews[0].Comment = %q", tk.Reviews[0].Comment)
	}
	if tk.Reviews[1].Reviewer != "human:steve" {
		t.Errorf("Reviews[1].Reviewer = %q, want human:steve", tk.Reviews[1].Reviewer)
	}
}

func TestParse_BackwardCompat_StatusOnly(t *testing.T) {
	// Legacy ticket with only status field — should auto-migrate to stage.
	input := `---
id: t-legacy
status: open
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: task
priority: 2
---
# Legacy ticket

Description.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tk.Stage != StageTriage {
		t.Errorf("Stage = %q, want triage (auto-migrated from status open)", tk.Stage)
	}
}

func TestParse_ReviewLogAndNotes(t *testing.T) {
	input := `---
id: t-both
stage: implement
deps: []
links: []
created: 2026-02-25T10:00:00Z
type: feature
priority: 1
---
# Both sections

Description.

## Review Log

**2026-02-25T12:00:00Z [agent:code-review]**
APPROVED — Code looks good.

## Notes

**2026-02-25T14:00:00Z**

Decision: use JWT for auth.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(tk.Reviews) != 1 {
		t.Fatalf("len(Reviews) = %d, want 1", len(tk.Reviews))
	}
	if len(tk.Notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(tk.Notes))
	}
	if tk.Reviews[0].Reviewer != "agent:code-review" {
		t.Errorf("review reviewer = %q", tk.Reviews[0].Reviewer)
	}
	if !strings.Contains(tk.Notes[0].Text, "JWT") {
		t.Errorf("note text = %q, should contain JWT", tk.Notes[0].Text)
	}
}

func TestSerialize_BodyNoBlankLineAccumulation(t *testing.T) {
	// Start with a leading newline in Body — the form that triggered the bug.
	tk := &Ticket{
		ID:       "t-accum",
		Stage:    StageImplement,
		Type:     TypeBug,
		Priority: 0,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		Title:    "Body should not accumulate blank lines",
		Body:     "\nDescription text here.\n",
		Notes: []Note{{
			Timestamp: time.Date(2026, 2, 28, 1, 0, 0, 0, time.UTC),
			Text:      "A note.",
		}},
	}

	// Simulate 10 round-trips (parse → serialize → parse → ...).
	for i := 0; i < 10; i++ {
		data, err := Serialize(tk)
		if err != nil {
			t.Fatalf("round %d Serialize: %v", i, err)
		}
		tk, err = Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("round %d Parse: %v", i, err)
		}
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("final Serialize: %v", err)
	}
	s := string(data)

	// Positive check: title followed by exactly one blank line then description.
	want := "# Body should not accumulate blank lines\n\nDescription text here.\n"
	if !strings.Contains(s, want) {
		t.Errorf("expected title + one blank line + description, got:\n%s", s)
	}
	// Negative check: no triple newline anywhere.
	if strings.Contains(s, "\n\n\n") {
		t.Errorf("found triple newline after 10 round-trips:\n%s", s)
	}
	// Notes should still be present.
	if !strings.Contains(s, "## Notes") {
		t.Error("notes section missing after round-trips")
	}
	if !strings.Contains(s, "A note.") {
		t.Error("note text missing after round-trips")
	}
}
