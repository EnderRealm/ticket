package ticket

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const sampleTicketYAML = `---
id: t-abc1
status: ready
deps: [t-dep1, t-dep2]
links: []
created: 2026-02-22T00:57:39Z
type: feature
priority: 1
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
	if tk.Status != StatusReady {
		t.Errorf("Status = %q, want %q", tk.Status, StatusReady)
	}
	if tk.Type != TypeFeature {
		t.Errorf("Type = %q, want %q", tk.Type, TypeFeature)
	}
	if tk.Priority != 1 {
		t.Errorf("Priority = %d, want 1", tk.Priority)
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
status: ready
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: feature
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

// TestParse_MistypedFieldCostsOnlyThatField covers the values a ticket file can
// carry that tk would never have written — the central store is a git repo other
// machines push into, and every typed field is a way for one bad scalar to have
// cost the whole ticket. A ticket absent from a listing is absent from its
// epic's derivation, so leniency here is what keeps an epic honest.
func TestParse_MistypedFieldCostsOnlyThatField(t *testing.T) {
	const frontmatter = `---
id: t-abc1
status: open
abandoned: %s
deps: [t-dep1]
links: []
created: %s
type: feature
priority: %s
parent: t-0f08
---
# Sample ticket
`
	cases := []struct {
		name      string
		abandoned string
		created   string
		priority  string
		want      Ticket
	}{
		{
			name: "every field well typed", abandoned: "true", created: "2026-02-22T00:57:39Z", priority: "1",
			want: Ticket{Abandoned: true, Created: time.Date(2026, 2, 22, 0, 57, 39, 0, time.UTC), Priority: 1},
		},
		{
			name: "bad bool", abandoned: "maybe", created: "2026-02-22T00:57:39Z", priority: "1",
			want: Ticket{Created: time.Date(2026, 2, 22, 0, 57, 39, 0, time.UTC), Priority: 1},
		},
		{
			name: "bad int", abandoned: "true", created: "2026-02-22T00:57:39Z", priority: "high",
			want: Ticket{Abandoned: true, Created: time.Date(2026, 2, 22, 0, 57, 39, 0, time.UTC)},
		},
		{
			name: "bad date", abandoned: "true", created: "soon", priority: "1",
			want: Ticket{Abandoned: true, Priority: 1},
		},
		{
			name: "all three at once", abandoned: "maybe", created: "soon", priority: "high",
			want: Ticket{},
		},
	}
	for _, c := range cases {
		tk, err := Parse(strings.NewReader(fmt.Sprintf(frontmatter, c.abandoned, c.created, c.priority)))
		if err != nil {
			t.Errorf("%s: Parse: %v", c.name, err)
			continue
		}
		if tk.Abandoned != c.want.Abandoned || tk.Priority != c.want.Priority || !tk.Created.Equal(c.want.Created) {
			t.Errorf("%s: abandoned=%v priority=%d created=%v, want %v/%d/%v",
				c.name, tk.Abandoned, tk.Priority, tk.Created, c.want.Abandoned, c.want.Priority, c.want.Created)
		}
		// The fields around the mistyped one are what the ticket is: an epic
		// derives from a child's status, and every view keys on the ID.
		if tk.ID != "t-abc1" || tk.Status != StatusOpen || tk.Type != TypeFeature || tk.Parent != "t-0f08" {
			t.Errorf("%s: a mistyped field cost an unrelated one: %+v", c.name, tk)
		}
		if tk.Title != "Sample ticket" || len(tk.Deps) != 1 || tk.Deps[0] != "t-dep1" {
			t.Errorf("%s: title = %q, deps = %v", c.name, tk.Title, tk.Deps)
		}
	}
}

func TestParse_UnusableFrontmatterStillFails(t *testing.T) {
	// Leniency stops where the decode produced no ticket. A duplicate key is the
	// case that has to be named: yaml.v3 reports it as a TypeError like a
	// per-field mismatch, but finds it in the uniqueKeys scan before decoding
	// anything, so tolerating it would yield a blank ticket rather than an error
	// — no skip recorded, no warning, and nothing to tell an epic its children
	// were read short. A hand-resolved merge conflict in the synced store is
	// exactly how a doubled key arrives.
	cases := []struct {
		name  string
		input string
	}{
		{"broken syntax", "---\nid: t-abc1\n  status: open\n---\n# Broken\n"},
		{"unclosed frontmatter", "---\nid: t-abc1\nstatus: open\n# Never closed\n"},
		{"duplicate status", "---\nid: t-abc1\nstatus: open\ntype: feature\npriority: 2\nstatus: done\n---\n# Doubled\n"},
		{"duplicate id", "---\nid: t-abc1\nid: t-abc2\nstatus: open\ntype: feature\n---\n# Doubled\n"},
		{"mistyped id", "---\nid: [t-abc1]\nstatus: open\ntype: feature\n---\n# No usable ID\n"},
		// A relational field that failed to decode is other tickets' business:
		// the parent silently vanishes from its epic's children, and the deps
		// silently stop blocking. Neither shows on the ticket, so neither is
		// tolerable the way a lost priority is.
		{"parent as a sequence", "---\nid: t-abc1\nstatus: open\ntype: feature\nparent: [epic-1111]\n---\n# Lost parent\n"},
		{"parent as a mapping", "---\nid: t-abc1\nstatus: open\ntype: feature\nparent: {a: b}\n---\n# Lost parent\n"},
		{"deps as a scalar", "---\nid: t-abc1\nstatus: open\ntype: feature\ndeps: notalist\n---\n# Lost deps\n"},
		// A typeless epic is passed over by the derivation and renders the stale
		// status its file stores, which is the demotion bypassed rather than
		// applied.
		{"type as a sequence", "---\nid: t-abc1\nstatus: done\ntype: [epic]\n---\n# Lost type\n"},
		// A complex top-level key: the struct pass tolerates it and carries on,
		// while the raw pass errors — so the load-bearing check cannot run. An
		// unvetted tolerance falls back to strict, or the parent below would be
		// dropped silently.
		{"complex key hiding a lost parent", "---\nid: t-abc1\nstatus: open\ntype: feature\n? {a: b}\n: c\nparent: [epic-1111]\n---\n# Unvettable\n"},
	}
	for _, c := range cases {
		tk, err := Parse(strings.NewReader(c.input))
		if err == nil {
			t.Errorf("%s: expected an error, got a ticket %+v", c.name, tk)
		}
	}
}

func TestParse_EmptyLoadBearingFieldsStillParse(t *testing.T) {
	// The load-bearing check must not catch a writer saying "none". Each of these
	// is a legitimately empty parent, dep list or type, paired with a mistyped
	// local field so the tolerated-TypeError path is the one under test. An empty
	// type is Validate's business on the way in, not the parser's.
	cases := []struct {
		name  string
		input string
	}{
		{"parent empty string", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\nparent: ''\ndeps: []\n---\n# Fine\n"},
		{"parent null", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\nparent:\ndeps: []\n---\n# Fine\n"},
		{"parent absent", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\ndeps: []\n---\n# Fine\n"},
		{"deps empty", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\nparent: epic-1111\ndeps: []\n---\n# Fine\n"},
		{"deps absent", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\nparent: epic-1111\n---\n# Fine\n"},
		{"type empty string", "---\nid: t-abc1\nstatus: open\ntype: ''\nabandoned: maybe\ndeps: []\n---\n# Fine\n"},
		{"type absent", "---\nid: t-abc1\nstatus: open\nabandoned: maybe\ndeps: []\n---\n# Fine\n"},
		{"all populated", "---\nid: t-abc1\nstatus: open\ntype: feature\nabandoned: maybe\nparent: epic-1111\ndeps: [d-1]\n---\n# Fine\n"},
	}
	for _, c := range cases {
		tk, err := Parse(strings.NewReader(c.input))
		if err != nil {
			t.Errorf("%s: Parse: %v", c.name, err)
			continue
		}
		if tk.ID != "t-abc1" || tk.Status != StatusOpen {
			t.Errorf("%s: parsed %+v, want the ticket intact", c.name, tk)
		}
		if tk.Abandoned {
			t.Errorf("%s: the mistyped local field was not dropped", c.name)
		}
	}
}

func TestSerialize_RoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "t-abc1",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 1,
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
	if parsed.Status != tk.Status {
		t.Errorf("Status = %q, want %q", parsed.Status, tk.Status)
	}
	if len(parsed.Deps) != 1 || parsed.Deps[0] != "t-dep1" {
		t.Errorf("Deps = %v, want [t-dep1]", parsed.Deps)
	}
}

func TestSerialize_EmptyArrays(t *testing.T) {
	tk := &Ticket{
		ID:       "t-test",
		Status:   StatusReady,
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
		Status:   StatusReady,
		Type:     TypeFeature,
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

func TestSerialize_StatusFields(t *testing.T) {
	tk := &Ticket{
		ID:       "t-status",
		Status:   StatusOpen,
		Type:     TypeFeature,
		Priority: 1,
		Deps:     []string{},
		Links:    []string{},
		Tags:     []string{},
		Created:  time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:    "Status test",
		Body:     "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "status: open") {
		t.Error("missing status field")
	}
}

func TestSerialize_ZeroUpdatedOmitted(t *testing.T) {
	// Legacy/unstamped tickets have a zero Updated; it must not serialize as
	// "updated: 0001-01-01...". (completed is already conditional.)
	tk := &Ticket{
		ID:       "t-zero",
		Status:   StatusBacklog,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:    "Zero updated",
		Body:     "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "updated:") {
		t.Errorf("zero Updated should be omitted, got:\n%s", s)
	}
	if strings.Contains(s, "0001-01-01") {
		t.Errorf("output should not contain a zero-time timestamp, got:\n%s", s)
	}

	// A zero Updated must round-trip back to zero, not a parsed 0001 time.
	parsed, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if !parsed.Updated.IsZero() {
		t.Errorf("round-tripped Updated = %v, want zero", parsed.Updated)
	}
}

func TestSerialize_NonZeroUpdatedPreserved(t *testing.T) {
	updated := time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)
	tk := &Ticket{
		ID:       "t-upd",
		Status:   StatusOpen,
		Type:     TypeFeature,
		Priority: 1,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Updated:  updated,
		Title:    "Has updated",
		Body:     "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if !strings.Contains(string(data), "updated: 2026-03-01T12:30:00Z") {
		t.Errorf("non-zero Updated should serialize in UTC RFC3339, got:\n%s", string(data))
	}

	parsed, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if !parsed.Updated.Equal(updated) {
		t.Errorf("round-tripped Updated = %v, want %v", parsed.Updated, updated)
	}
}

func TestSerialize_StatusRoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "t-rt",
		Status:   StatusOpen,
		Type:     TypeBug,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Title:    "Round trip",
		Body:     "\nBug description.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	parsed, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}

	if parsed.Status != StatusOpen {
		t.Errorf("Status = %s, want open", parsed.Status)
	}
}

func TestParse_BackwardCompat_StatusOnly(t *testing.T) {
	// Legacy ticket with only old status field — should auto-migrate.
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
	if tk.Status != StatusOpen {
		t.Errorf("Status = %q, want open", tk.Status)
	}
}

func TestParse_BackwardCompat_LegacyStage(t *testing.T) {
	// Legacy ticket with stage field — should auto-migrate to status.
	input := `---
id: t-legacy2
stage: implement
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: task
priority: 2
---
# Legacy stage ticket

Description.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tk.Status != StatusOpen {
		t.Errorf("Status = %q, want open (auto-migrated from stage implement)", tk.Status)
	}
}

func TestParse_BackwardCompat_LegacyReviewLogStripped(t *testing.T) {
	// Legacy ticket with a Review Log section: v7 retired the review system, so
	// the section is stripped from the body and never parsed — but the read
	// records how much it dropped, which is what the write that removes it from
	// the file reports and what `tk audit` lists until then.
	input := `---
id: t-revlog
status: open
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

## Notes

**2026-02-25T14:00:00Z**

Decision: use JWT for auth.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Review Log should be stripped from body.
	if strings.Contains(tk.Body, "Review Log") {
		t.Errorf("body should not contain Review Log section:\n%s", tk.Body)
	}

	// The section runs from its heading to the notes that follow it.
	section := "\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED — All file paths verified.\n"
	if tk.droppedReviewLog != len(section) {
		t.Errorf("droppedReviewLog = %d, want %d — the strip has to be recorded, not silent", tk.droppedReviewLog, len(section))
	}

	// Notes should still work.
	if len(tk.Notes) != 1 {
		t.Fatalf("len(Notes) = %d, want 1", len(tk.Notes))
	}
	if !strings.Contains(tk.Notes[0].Text, "JWT") {
		t.Errorf("note text = %q, should contain JWT", tk.Notes[0].Text)
	}
}

func TestSerialize_BodyNoBlankLineAccumulation(t *testing.T) {
	// Start with a leading newline in Body — the form that triggered the bug.
	tk := &Ticket{
		ID:       "t-accum",
		Status:   StatusOpen,
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

	// Simulate 10 round-trips (parse -> serialize -> parse -> ...).
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

func TestExtraFields_RoundTrip(t *testing.T) {
	input := `---
id: t-extra1
status: ready
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: feature
priority: 2
env: production
team: backend
---
# Extra fields ticket

Description.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(tk.Extra) != 2 {
		t.Fatalf("len(Extra) = %d, want 2", len(tk.Extra))
	}
	if tk.Extra["env"] != "production" {
		t.Errorf("Extra[env] = %q, want production", tk.Extra["env"])
	}
	if tk.Extra["team"] != "backend" {
		t.Errorf("Extra[team] = %q, want backend", tk.Extra["team"])
	}

	// Round-trip: serialize and re-parse.
	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	tk2, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if tk2.Extra["env"] != "production" {
		t.Errorf("after round-trip Extra[env] = %q", tk2.Extra["env"])
	}
	if tk2.Extra["team"] != "backend" {
		t.Errorf("after round-trip Extra[team] = %q", tk2.Extra["team"])
	}
}

func TestSerialize_ExtraFieldOrdering(t *testing.T) {
	tk := &Ticket{
		ID:       "t-order",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Ordering test",
		Body:     "\n",
		Extra:    map[string]string{"zebra": "z", "alpha": "a", "mid": "m"},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	// Extra fields should appear after known fields, sorted alphabetically.
	alphaIdx := strings.Index(s, "alpha: a")
	midIdx := strings.Index(s, "mid: m")
	zebraIdx := strings.Index(s, "zebra: z")
	closingIdx := strings.Index(s, "---\n# ")

	if alphaIdx < 0 || midIdx < 0 || zebraIdx < 0 {
		t.Fatalf("extra fields missing from output:\n%s", s)
	}
	if alphaIdx >= midIdx || midIdx >= zebraIdx {
		t.Errorf("extra fields not sorted: alpha@%d mid@%d zebra@%d", alphaIdx, midIdx, zebraIdx)
	}

	// Extra fields should come after priority (last guaranteed known field) and before closing ---.
	priorityIdx := strings.Index(s, "priority: 2")
	if alphaIdx <= priorityIdx {
		t.Errorf("extra fields should come after known fields")
	}
	if zebraIdx >= closingIdx {
		t.Errorf("extra fields should come before closing ---")
	}
}

func TestSerialize_NoExtraFieldsUnchanged(t *testing.T) {
	tk := &Ticket{
		ID:       "t-noextra",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "No extra",
		Body:     "\nDescription.\n",
		Extra:    map[string]string{},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	// Output should end frontmatter with priority then --- (no extra lines).
	if !strings.Contains(s, "priority: 2\n---\n") {
		t.Errorf("unexpected frontmatter ending:\n%s", s)
	}
}

func TestAbandoned_RoundTrip(t *testing.T) {
	// The abandon intent is stored, not derived, so it has to survive the file:
	// a writer that read a ticket carries the flag back and must write the same
	// value it was given.
	tk := &Ticket{
		ID:        "t-aband",
		Status:    StatusClosed,
		Abandoned: true,
		Type:      TypeEpic,
		Priority:  2,
		Deps:      []string{},
		Links:     []string{},
		Created:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:     "Abandoned epic",
		Body:      "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if !strings.Contains(string(data), "abandoned: true\n") {
		t.Errorf("frontmatter does not carry the flag:\n%s", data)
	}

	parsed, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !parsed.Abandoned {
		t.Error("Abandoned = false after a round trip, want true")
	}
	if len(parsed.Extra) != 0 {
		t.Errorf("Extra = %v, want empty — abandoned is a known field, not an unknown one", parsed.Extra)
	}

	// An epic that was never abandoned writes no key at all, so the field costs
	// nothing on the tickets that do not use it.
	tk.Abandoned = false
	data, err = Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if strings.Contains(string(data), "abandoned") {
		t.Errorf("frontmatter carries an abandoned key it does not need:\n%s", data)
	}
}

func TestOutputs_RoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "t-out1",
		Status:   StatusDone,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Outputs round trip",
		Body:     "\nDescription.\n",
		Outputs: map[string]string{
			"zebra":         "last",
			OutputKeyBranch: "feature-x",
			// All digits: the hand-rolled serializer writes it unquoted, so it
			// comes back as a YAML int unless the field decodes as a string.
			OutputKeyCommit: "1234567",
		},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	// Nested block with two-space indent, keys sorted alphabetically.
	if !strings.Contains(s, "outputs:\n  branch: feature-x\n  commit: 1234567\n  zebra: last\n") {
		t.Errorf("unexpected outputs block:\n%s", s)
	}

	parsed, err := parseBytes(data)
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if len(parsed.Outputs) != len(tk.Outputs) {
		t.Fatalf("Outputs = %v, want %v", parsed.Outputs, tk.Outputs)
	}
	for k, v := range tk.Outputs {
		if parsed.Outputs[k] != v {
			t.Errorf("Outputs[%q] = %q, want %q", k, parsed.Outputs[k], v)
		}
	}
	// The outputs block must not leak into extra fields.
	if len(parsed.Extra) != 0 {
		t.Errorf("Extra = %v, want empty", parsed.Extra)
	}
}

func TestParse_OutputsBlock(t *testing.T) {
	input := `---
id: t-out2
status: done
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: feature
priority: 2
outputs:
  branch: add-outputs-1234
  commit: 0f08abc
  artifact: dist/tk
---
# Landed ticket

Description.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{"branch": "add-outputs-1234", "commit": "0f08abc", "artifact": "dist/tk"}
	if len(tk.Outputs) != len(want) {
		t.Fatalf("Outputs = %v, want %v", tk.Outputs, want)
	}
	for k, v := range want {
		if tk.Outputs[k] != v {
			t.Errorf("Outputs[%q] = %q, want %q", k, tk.Outputs[k], v)
		}
	}
}

func TestSerialize_NoOutputsUnchanged(t *testing.T) {
	// A ticket predating outputs must serialize exactly as before.
	tk := &Ticket{
		ID:       "t-nooutputs",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "No outputs",
		Body:     "\nDescription.\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	want := "---\nid: t-nooutputs\nstatus: ready\ndeps: []\nlinks: []\n" +
		"created: 2026-01-01T00:00:00Z\ntype: feature\npriority: 2\n---\n" +
		"# No outputs\n\n\nDescription.\n"
	if string(data) != want {
		t.Errorf("Serialize =\n%q\nwant\n%q", string(data), want)
	}

	parsed, err := parseBytes(data)
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if len(parsed.Outputs) != 0 {
		t.Errorf("Outputs = %v, want empty", parsed.Outputs)
	}
}

func TestPopulateDoneOutputs(t *testing.T) {
	// Nil map is allocated on demand; empty values are skipped.
	tk := &Ticket{}
	PopulateDoneOutputs(tk, "abc1234", "")
	if tk.Outputs[OutputKeyCommit] != "abc1234" {
		t.Errorf("Outputs[commit] = %q, want abc1234", tk.Outputs[OutputKeyCommit])
	}
	if _, ok := tk.Outputs[OutputKeyBranch]; ok {
		t.Errorf("Outputs[branch] set from empty value: %v", tk.Outputs)
	}

	// Existing values win; absent ones get filled.
	PopulateDoneOutputs(tk, "def5678", "feature-x")
	if tk.Outputs[OutputKeyCommit] != "abc1234" {
		t.Errorf("Outputs[commit] = %q, want abc1234 (must not overwrite)", tk.Outputs[OutputKeyCommit])
	}
	if tk.Outputs[OutputKeyBranch] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", tk.Outputs[OutputKeyBranch])
	}

	// No values, no map.
	empty := &Ticket{}
	PopulateDoneOutputs(empty, "", "")
	if empty.Outputs != nil {
		t.Errorf("Outputs = %v, want nil", empty.Outputs)
	}
}

func TestOutputs_KeyAndValueValidation(t *testing.T) {
	// Reserved frontmatter names are valid outputs keys.
	for _, key := range []string{"branch", "commit", "status", "artifact_2"} {
		if err := ValidateOutputKey(key); err != nil {
			t.Errorf("ValidateOutputKey(%q) = %v", key, err)
		}
	}
	for _, key := range []string{"", "has space", "has:colon", "has.dot"} {
		if err := ValidateOutputKey(key); err == nil {
			t.Errorf("ValidateOutputKey(%q) should return error", key)
		}
	}
	for _, val := range []string{"feature-x", "abc1234", "dist/tk"} {
		if err := ValidateOutputValue(val); err != nil {
			t.Errorf("ValidateOutputValue(%q) = %v", val, err)
		}
	}
	// Unquoted values must round-trip exactly, so surrounding whitespace and
	// characters that break the scalar are rejected.
	for _, val := range []string{"has:colon", "has\nnewline", "has#hash", "%wip", " padded", "padded "} {
		if err := ValidateOutputValue(val); err == nil {
			t.Errorf("ValidateOutputValue(%q) should return error", val)
		}
	}
}

func TestPopulateDoneOutputs_DropsUnserializableValues(t *testing.T) {
	// A legal git branch name that would break the unquoted YAML scalar is
	// dropped rather than written — best-effort metadata must not corrupt the
	// ticket file.
	tk := &Ticket{}
	PopulateDoneOutputs(tk, "abc1234", "%wip")
	if _, ok := tk.Outputs[OutputKeyBranch]; ok {
		t.Errorf("Outputs[branch] = %q, want absent", tk.Outputs[OutputKeyBranch])
	}
	if tk.Outputs[OutputKeyCommit] != "abc1234" {
		t.Errorf("Outputs[commit] = %q, want abc1234", tk.Outputs[OutputKeyCommit])
	}
}

func TestDepCargo_RoundTrip(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("the ingest event schema plus the fixtures it needs ", 5))
	tk := &Ticket{
		ID:       "t-cargo1",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{"zeta-0001", "alpha-0002"},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Cargo round trip",
		// Already in canonical body form, so serialize→parse→serialize compares
		// the frontmatter rather than body normalization.
		Body: "Description.\n",
		DepCargo: map[string]string{
			// Prose the unquoted writeField path could not serialize.
			"zeta-0001":  "event schema: the ingest table's #1 contract",
			"alpha-0002": long,
		},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "dep-cargo:\n  alpha-0002: ") {
		t.Errorf("unexpected dep-cargo block (keys should be sorted):\n%s", s)
	}

	parsed, err := parseBytes(data)
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if len(parsed.DepCargo) != len(tk.DepCargo) {
		t.Fatalf("DepCargo = %v, want %v", parsed.DepCargo, tk.DepCargo)
	}
	for k, v := range tk.DepCargo {
		if parsed.DepCargo[k] != v {
			t.Errorf("DepCargo[%q] = %q, want %q", k, parsed.DepCargo[k], v)
		}
	}
	// The block must not leak into extra fields.
	if len(parsed.Extra) != 0 {
		t.Errorf("Extra = %v, want empty", parsed.Extra)
	}

	again, err := Serialize(parsed)
	if err != nil {
		t.Fatalf("Serialize after Parse: %v", err)
	}
	if string(again) != s {
		t.Errorf("serialize is not stable:\n%q\nwant\n%q", string(again), s)
	}
}

func TestVerdicts_RoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "t-verd1",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Verdict round trip",
		// Already in canonical body form, so serialize→parse→serialize compares
		// the frontmatter rather than body normalization.
		Body: "Description.\n",
		Verdicts: []VerdictRow{{
			Ticket: "t-verd1",
			SHA:    shaA,
			Class:  VerdictTestVerified,
			Role:   VerdictRoleVerifier,
			// Punctuation the unquoted writeField path could not serialize.
			Evidence: `see: "run #12" at https://ci/x`,
			By:       "verifier-1",
			At:       "2026-08-31T00:00:00Z",
		}},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(data)

	parsed, err := parseBytes(data)
	if err != nil {
		t.Fatalf("Parse after Serialize: %v", err)
	}
	if len(parsed.Verdicts) != 1 || parsed.Verdicts[0] != tk.Verdicts[0] {
		t.Fatalf("Verdicts = %+v, want %+v", parsed.Verdicts, tk.Verdicts)
	}
	// The block must not leak into extra fields.
	if len(parsed.Extra) != 0 {
		t.Errorf("Extra = %v, want empty", parsed.Extra)
	}

	again, err := Serialize(parsed)
	if err != nil {
		t.Fatalf("Serialize after Parse: %v", err)
	}
	if string(again) != s {
		t.Errorf("serialize is not stable:\n%q\nwant\n%q", string(again), s)
	}
}

func TestDepCargo_BareDepsUnchanged(t *testing.T) {
	// A ticket predating dep cargo must parse and serialize without the block.
	input := `---
id: t-cargo2
status: ready
deps: [a-0001, b-0002]
links: []
created: 2026-01-01T00:00:00Z
type: feature
priority: 2
---
# Bare deps

Description.
`
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(tk.DepCargo) != 0 {
		t.Errorf("DepCargo = %v, want empty", tk.DepCargo)
	}
	if CargoFor(tk, "a-0001") != "" {
		t.Errorf("CargoFor = %q, want empty", CargoFor(tk, "a-0001"))
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if strings.Contains(string(data), "dep-cargo") {
		t.Errorf("empty DepCargo emitted a block:\n%s", string(data))
	}
}

func TestCargo_KeyAndValueValidation(t *testing.T) {
	for _, key := range []string{"foo-abcd", "project/foo-abcd"} {
		if err := ValidateCargoKey(key); err != nil {
			t.Errorf("ValidateCargoKey(%q) = %v", key, err)
		}
	}
	for _, key := range []string{"", "has space", "has\nnewline", "has\ttab"} {
		if err := ValidateCargoKey(key); err == nil {
			t.Errorf("ValidateCargoKey(%q) should return error", key)
		}
	}
	// Cargo is prose: punctuation the extra/outputs rules reject is fine here.
	for _, val := range []string{"event schema: the ingest table", "branch #1", "don't break it", "café menü"} {
		if err := ValidateCargoValue(val); err != nil {
			t.Errorf("ValidateCargoValue(%q) = %v", val, err)
		}
	}
	for _, val := range []string{"", "   ", "two\nlines", "carriage\rreturn", "bell\x07", " padded", "padded "} {
		if err := ValidateCargoValue(val); err == nil {
			t.Errorf("ValidateCargoValue(%q) should return error", val)
		}
	}
}

func TestExtra_ReservedKeyRejected(t *testing.T) {
	reserved := []string{"id", "status", "type", "priority", "deps", "created", "external-ref"}
	for _, key := range reserved {
		if err := ValidateExtraKey(key); err == nil {
			t.Errorf("ValidateExtraKey(%q) should return error", key)
		}
	}
}

func TestExtra_InvalidCharsRejected(t *testing.T) {
	// Keys: only letters, digits, hyphens, underscores allowed.
	badKeys := []string{
		"has space", "has:colon", "has#hash", "has[bracket",
		"has\nnewline", "has\ttab", "has%percent", "has.dot",
		"has!bang", "has@at", "has'quote",
	}
	for _, key := range badKeys {
		if err := ValidateExtraKey(key); err == nil {
			t.Errorf("ValidateExtraKey(%q) should return error", key)
		}
	}

	// Values: YAML indicator characters and control chars rejected.
	badValues := []string{
		"has:colon", "has#hash", "has[bracket", "has{brace",
		"has\nnewline", "has\rreturn", "has\ttab",
		"has%percent", "has!bang", "has&amp", "has*star",
		"has@at", "has`tick", "has|pipe", "has>angle",
		"has'quote", "has\"dquote", "has]close", "has}close",
	}
	for _, val := range badValues {
		if err := ValidateExtraValue(val); err == nil {
			t.Errorf("ValidateExtraValue(%q) should return error", val)
		}
	}

	// Valid keys: letters, digits, hyphens, underscores.
	goodKeys := []string{"valid-key", "another_key", "Key123", "ABC"}
	for _, key := range goodKeys {
		if err := ValidateExtraKey(key); err != nil {
			t.Errorf("ValidateExtraKey(%q) = %v", key, err)
		}
	}

	// Valid values: printable ASCII minus YAML indicators.
	goodValues := []string{"simple value", "abc123", "hello-world_v2", "path/to/file", "a,b,c", "1+2=3", "v2.0 (beta)"}
	for _, val := range goodValues {
		if err := ValidateExtraValue(val); err != nil {
			t.Errorf("ValidateExtraValue(%q) = %v", val, err)
		}
	}
}

func TestExtra_LeadingIndicatorRejected(t *testing.T) {
	// writeField emits `key: value` unquoted, and yaml refuses a plain scalar
	// that opens a sequence entry, a mapping key or a flow separator.
	badValues := []string{"- foo", "-", "? foo", "?", ", foo", ",foo", " - foo", "- "}
	for _, val := range badValues {
		if err := ValidateExtraValue(val); err == nil {
			t.Errorf("ValidateExtraValue(%q) should return error", val)
		}
		if err := ValidateOutputValue(val); err == nil {
			t.Errorf("ValidateOutputValue(%q) should return error", val)
		}
	}

	// Only the opening indicator shape is a problem; attached or mid-string
	// occurrences serialize fine.
	for _, val := range leadingIndicatorGoodValues {
		if err := ValidateExtraValue(val); err != nil {
			t.Errorf("ValidateExtraValue(%q) = %v", val, err)
		}
		if err := ValidateOutputValue(val); err != nil {
			t.Errorf("ValidateOutputValue(%q) = %v", val, err)
		}
	}
}

// Shapes near the rejected ones that must keep round-tripping.
var leadingIndicatorGoodValues = []string{"-foo", "?foo", "-- foo", "a, b", "x - y", "end?"}

func TestExtra_LeadingIndicatorGoodValuesRoundTrip(t *testing.T) {
	for _, val := range leadingIndicatorGoodValues {
		tk := &Ticket{
			ID:       "t-indicator",
			Status:   StatusReady,
			Type:     TypeFeature,
			Priority: 2,
			Deps:     []string{},
			Links:    []string{},
			Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Title:    "Indicator round trip",
			Body:     "\nDescription.\n",
			Extra:    map[string]string{"question": val},
			Outputs:  map[string]string{"artifact": val},
		}

		data, err := Serialize(tk)
		if err != nil {
			t.Fatalf("Serialize(%q): %v", val, err)
		}
		tk2, err := Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("Parse after Serialize(%q): %v\n%s", val, err, data)
		}
		if tk2.Extra["question"] != val {
			t.Errorf("Extra[question] = %q, want %q", tk2.Extra["question"], val)
		}
		if tk2.Outputs["artifact"] != val {
			t.Errorf("Outputs[artifact] = %q, want %q", tk2.Outputs["artifact"], val)
		}
	}
}

// A value the API would refuse can still arrive in a file — a repository's
// legacy .tickets/ is parsed and reserialized on migration — and written
// plain, a quoted multiline extra that parsed as one value comes back as that
// value and a top-level `parent` the boundary never saw. The serializer has
// to write it back as the string it was.
func TestSerialize_QuotesValuesThatWouldNotReadBackAsThemselves(t *testing.T) {
	input := "---\nid: t-quote\nstatus: ready\ntype: feature\npriority: 2\ncreated: 2026-01-01T00:00:00Z\nmemo: \"text\\nparent: proj/leaf\"\n---\n# Quoted\n"
	tk, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tk.Parent != "" || tk.Extra["memo"] != "text\nparent: proj/leaf" {
		t.Fatalf("Parent = %q, Extra[memo] = %q; want no parent and the two-line memo", tk.Parent, tk.Extra["memo"])
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if !strings.Contains(string(data), "\nmemo: \"text\\nparent: proj/leaf\"\n") || strings.Contains(string(data), "\nparent: ") {
		t.Fatalf("Serialize wrote the memo plain:\n%s", data)
	}
	again, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("Parse after Serialize: %v\n%s", err, data)
	}
	if again.Parent != "" {
		t.Errorf("Parent = %q after the round trip, want none", again.Parent)
	}
	if again.Extra["memo"] != tk.Extra["memo"] {
		t.Errorf("Extra[memo] = %q after the round trip, want %q", again.Extra["memo"], tk.Extra["memo"])
	}

	// Every shape yaml would read as something other than the string is
	// quoted; the plain shapes the API accepts stay plain.
	for _, val := range []string{"a #comment", "a: b", "- item", " lead", "trail ", "1.0", "0x1f", "null", "", "[a, b]", "'single'", "|", "@at", "2026-01-01T00:00:00Z"} {
		tk.Extra["memo"] = val
		tk.Parent = val
		data, err := Serialize(tk)
		if err != nil {
			t.Fatalf("Serialize(%q): %v", val, err)
		}
		again, err := Parse(strings.NewReader(string(data)))
		if err != nil {
			t.Fatalf("Parse after Serialize(%q): %v\n%s", val, err, data)
		}
		if again.Extra["memo"] != val || again.Parent != val {
			t.Errorf("round trip of %q: Extra[memo] = %q, Parent = %q\n%s", val, again.Extra["memo"], again.Parent, data)
		}
	}
	for _, val := range append(leadingIndicatorGoodValues, "simple value", "path/to/file", "v2.0 (beta)", "1+2=3", "http://x/y", "warp/epic-0001", "2", "1.5", "true") {
		tk.Extra["memo"] = val
		data, err := Serialize(tk)
		if err != nil {
			t.Fatalf("Serialize(%q): %v", val, err)
		}
		if !strings.Contains(string(data), "\nmemo: "+val+"\n") {
			t.Errorf("plain-safe %q was not written plain:\n%s", val, data)
		}
	}
}

func TestUpdateSection_ReplacesExisting(t *testing.T) {
	body := "\nOriginal description.\n\n## Acceptance Criteria\n\nOld criteria\n"
	updated := UpdateSection(body, "Acceptance Criteria", "New criteria")
	count := strings.Count(updated, "## Acceptance Criteria")
	if count != 1 {
		t.Errorf("expected 1 Acceptance Criteria section, got %d.\nBody:\n%s", count, updated)
	}
	if !strings.Contains(updated, "New criteria") {
		t.Errorf("expected new content in body:\n%s", updated)
	}
	if strings.Contains(updated, "Old criteria") {
		t.Errorf("expected old content to be replaced:\n%s", updated)
	}
}

// TestUpdateSection_MergedAcceptanceSection covers the write side of the merged
// acceptance section: the read joins every `## Acceptance*` block, so the edit
// replaces the whole run under one heading rather than dropping the later block
// or leaving it as stale text.
func TestUpdateSection_MergedAcceptanceSection(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		survives string // substring the edit must leave intact, in order
		gone     string // text of a replaced block, absent afterwards
	}{
		{
			name: "later acceptance block replaced",
			body: "\nDescription.\n\n## Acceptance Criteria\n\n- old one\n\n## Acceptance Notes\n\n- old two\n",
			gone: "old two",
		},
		{
			name:     "test results and notes survive",
			body:     "\nDescription.\n\n## Acceptance Criteria\n\n- old one\n\n## Acceptance Notes\n\n- old two\n\n## Test Results\n\nrun record\n\n## Notes\n\nkeep me\n",
			survives: "## Test Results\n\nrun record\n\n## Notes\n\nkeep me\n",
			gone:     "old two",
		},
		{
			name:     "unrelated heading closes the section",
			body:     "\nDescription.\n\n## Acceptance Criteria\n\n- old one\n\n## Something\n\n- not a criterion\n",
			survives: "## Something\n\n- not a criterion\n",
		},
		{
			name: "second literal acceptance criteria block replaced",
			body: "\nDescription.\n\n## Acceptance Criteria\n\n- old one\n\n## Acceptance Criteria\n\n- old two\n",
			gone: "old two",
		},
		{
			name:     "acceptance blocks separated by design",
			body:     "\nDescription.\n\n## Acceptance Criteria\n\n- old one\n\n## Design\n\nA design.\n\n## Acceptance Notes\n\n- old two\n\n## Test Results\n\nrun record\n",
			survives: "## Acceptance Criteria\n\n- new one\n\n## Design\n\nA design.\n\n## Test Results\n\nrun record\n",
			gone:     "old two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := UpdateSection(tt.body, "Acceptance Criteria", "- new one")

			if count := strings.Count(updated, "## Acceptance"); count != 1 {
				t.Errorf("expected 1 Acceptance heading, got %d.\nBody:\n%s", count, updated)
			}

			criteria := ParseCriteria(AcceptanceCriteria(updated))
			if len(criteria) != 1 || criteria[0].Text != "new one" {
				t.Fatalf("criteria = %+v, want only the replacement.\nBody:\n%s", criteria, updated)
			}
			if tt.gone != "" && strings.Contains(updated, tt.gone) {
				t.Errorf("expected %q replaced:\n%s", tt.gone, updated)
			}
			if tt.survives != "" && !strings.Contains(updated, tt.survives) {
				t.Errorf("expected %q to survive the edit:\n%s", tt.survives, updated)
			}
		})
	}
}

// TestUpdateSection_DescriptionStopsAtAcceptanceNotes guards the description
// edit against a body whose first structural heading is `## Acceptance Notes`:
// the marker list matches the acceptance prefix, so the edit bounds itself
// there instead of replacing the whole body.
func TestUpdateSection_DescriptionStopsAtAcceptanceNotes(t *testing.T) {
	body := "\nOld description.\n\n## Acceptance Notes\n\n- a criterion\n"
	updated := UpdateSection(body, "", "New description.")

	if !strings.Contains(updated, "## Acceptance Notes") {
		t.Errorf("expected the acceptance block to survive:\n%s", updated)
	}
	criteria := ParseCriteria(AcceptanceCriteria(updated))
	if len(criteria) != 1 || criteria[0].Text != "a criterion" {
		t.Errorf("expected the criterion to survive, got %+v.\nBody:\n%s", criteria, updated)
	}
	if strings.Contains(updated, "Old description.") {
		t.Errorf("expected the description replaced:\n%s", updated)
	}
}

func TestUpdateSection_RoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "test-roundtrip-1234",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Round trip test",
		Body:     "\nDescription here.\n",
	}

	// First edit: add acceptance criteria
	tk.Body = UpdateSection(tk.Body, "Acceptance Criteria", "First AC")

	// Serialize and parse (simulates write to disk + read back)
	data, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	tk2, err := parseBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	// Second edit: update acceptance criteria
	tk2.Body = UpdateSection(tk2.Body, "Acceptance Criteria", "Updated AC")

	// Serialize again and parse
	data2, err := Serialize(tk2)
	if err != nil {
		t.Fatal(err)
	}
	tk3, err := parseBytes(data2)
	if err != nil {
		t.Fatal(err)
	}

	count := strings.Count(tk3.Body, "## Acceptance Criteria")
	if count != 1 {
		t.Errorf("expected 1 Acceptance Criteria section after round-trip, got %d.\nBody:\n%s", count, tk3.Body)
	}
	if !strings.Contains(tk3.Body, "Updated AC") {
		t.Errorf("expected updated content:\n%s", tk3.Body)
	}
	if strings.Contains(tk3.Body, "First AC") {
		t.Errorf("expected old content replaced:\n%s", tk3.Body)
	}
}

// TestSerialize_TwoAcceptanceHeadingsRoundTrip guards the storage side of the
// merged acceptance section: BodySections joins the blocks for its callers, but
// the stored body still carries both headings verbatim.
func TestSerialize_TwoAcceptanceHeadingsRoundTrip(t *testing.T) {
	tk := &Ticket{
		ID:       "test-two-acceptance-1234",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Two acceptance headings",
		Body:     "Description.\n\n## Acceptance Criteria\n\n- a\n  verify: true\n\n## Acceptance Notes\n\n- b\n  verify: false\n",
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	tk2, err := parseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	data2, err := Serialize(tk2)
	if err != nil {
		t.Fatal(err)
	}

	if count := strings.Count(tk2.Body, "## Acceptance"); count != 2 {
		t.Errorf("expected 2 Acceptance headings after round-trip, got %d.\nBody:\n%s", count, tk2.Body)
	}
	if string(data2) != string(data) {
		t.Errorf("re-serialization differs:\nfirst:\n%s\nsecond:\n%s", data, data2)
	}
}

func parseBytes(data []byte) (*Ticket, error) {
	r := strings.NewReader(string(data))
	return Parse(r)
}

func TestSerialize_NotesDuplication(t *testing.T) {
	tk := &Ticket{
		ID:       "test-notes-dup",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Notes dup test",
		Body:     "\nDescription.\n",
	}

	// Add first note
	tk.Notes = append(tk.Notes, Note{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Text:      "First note",
	})

	// Round trip 1: Serialize -> Parse
	data1, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	tk2, err := parseBytes(data1)
	if err != nil {
		t.Fatal(err)
	}

	// Add second note (simulates MCP ticket_add_note without body stripping)
	tk2.Notes = append(tk2.Notes, Note{
		Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Text:      "Second note",
	})

	// Round trip 2: Serialize -> Parse
	data2, err := Serialize(tk2)
	if err != nil {
		t.Fatal(err)
	}

	count := strings.Count(string(data2), "First note")
	if count != 1 {
		t.Errorf("expected 'First note' to appear once, got %d times.\nSerialized:\n%s", count, string(data2))
	}

	tk3, err := parseBytes(data2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tk3.Notes) != 2 {
		t.Errorf("expected 2 notes, got %d", len(tk3.Notes))
	}
}

// TestUpdateSection_HeadingNamedInProse pins the anchoring: a description that
// names a structural heading inline — the shape every ticket about the ticket
// format has — is prose, not a heading, so an edit spans the real section and
// leaves the rest of the body byte-identical.
func TestUpdateSection_HeadingNamedInProse(t *testing.T) {
	const prose = "The structural headings are `## Design`, `## Acceptance Criteria`,\n" +
		"`## Test Results` and `## Notes`."
	body := func(desc, design, acceptance, results string) string {
		return "\n" + desc + "\n\n## Design\n\n" + design +
			"\n\n## Acceptance Criteria\n\n" + acceptance +
			"\n\n## Test Results\n\n" + results + "\n"
	}
	original := body(prose, "A real design.", "- a criterion", "A real run record.")

	tests := []struct {
		name    string
		heading string
		content string
		want    string
	}{
		{
			name:    "design edit",
			heading: "Design",
			content: "A new design.",
			want:    body(prose, "A new design.", "- a criterion", "A real run record."),
		},
		{
			name:    "test results edit",
			heading: "Test Results",
			content: "A new run record.",
			want:    body(prose, "A real design.", "- a criterion", "A new run record."),
		},
		{
			name:    "description edit",
			heading: "",
			content: "A new description.",
			want:    body("A new description.", "A real design.", "- a criterion", "A real run record."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if updated := UpdateSection(original, tt.heading, tt.content); updated != tt.want {
				t.Errorf("body after the edit =\n%q\nwant\n%q", updated, tt.want)
			}
		})
	}
}

// TestUpdateSection_DescriptionKeepsLeadingHeading covers a body with no
// description at all: the first heading opens the body, and parseBody trims the
// newline that would have preceded it, so the description edit has to recognise
// a heading sitting at offset 0.
func TestUpdateSection_DescriptionKeepsLeadingHeading(t *testing.T) {
	body := "## Design\n\nA real design.\n"
	updated := UpdateSection(body, "", "A new description.")

	desc, design, _, _ := BodySections(updated)
	if desc != "A new description." || design != "A real design." {
		t.Errorf("desc = %q, design = %q, want the design section to survive.\nBody:\n%s", desc, design, updated)
	}
}

func TestNextStructuralSection(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string // the heading line the offset lands on, "" for no match
	}{
		{
			name: "prose naming a later structural heading",
			body: "Description mentioning `## Test Results` inline.\n\n## Design\n\nA design.\n",
			want: "## Design",
		},
		{
			name: "heading at the start of the body",
			body: "## Design\n\nA design.\n",
			want: "## Design",
		},
		{
			name: "a notes line is text, as BodySections reads it",
			body: "Intro.\n\n## Notes\n\nfoo\n\n## Test Results\n\nrun\n",
			want: "## Test Results",
		},
		{
			name: "no structural heading",
			body: "Description only, naming `## Notes` in prose.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := nextStructuralSection(tt.body)
			if tt.want == "" {
				if idx >= 0 {
					t.Errorf("nextStructuralSection = %d, want -1.\nRest:\n%s", idx, tt.body[idx:])
				}
				return
			}
			if idx < 0 {
				t.Fatalf("nextStructuralSection = -1, want the %q line", tt.want)
			}
			if !strings.HasPrefix(tt.body[idx:], tt.want) {
				t.Errorf("nextStructuralSection landed on %q, want the %q line", tt.body[idx:], tt.want)
			}
		})
	}
}

// TestSerialize_NotesHeadingNamedInProse guards the notes round trip against a
// description that names the notes heading inline: the heading has to be
// written because the parse side only reads notes under a real one, and without
// it the notes come back as body text under whatever section ends the body.
func TestSerialize_NotesHeadingNamedInProse(t *testing.T) {
	tk := &Ticket{
		ID:       "test-notes-prose",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Notes heading named in prose",
		Body:     "The headings are `## Notes` and the rest.\n\n## Acceptance Criteria\n\n- a criterion\n",
		Notes: []Note{{
			Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Text:      "A note that must stay a note.",
		}},
	}

	data, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	tk2, err := parseBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(tk2.Notes) != 1 || tk2.Notes[0].Text != "A note that must stay a note." {
		t.Errorf("notes = %+v, want the note to survive.\nSerialized:\n%s", tk2.Notes, data)
	}
	if tk2.Body != tk.Body {
		t.Errorf("body = %q, want %q", tk2.Body, tk.Body)
	}
}

// roundTrip writes tk and reads it back, the path every store write and read
// takes.
func roundTrip(t *testing.T, tk *Ticket) *Ticket {
	t.Helper()
	data, err := Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	got, err := parseBytes(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	return got
}

func bodyTicket(body string, notes ...Note) *Ticket {
	return &Ticket{
		ID:      "t-body",
		Status:  StatusReady,
		Type:    TypeFeature,
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:   "Body text",
		Body:    body,
		Notes:   notes,
	}
}

var (
	firstNote  = Note{Timestamp: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Text: "First note."}
	secondNote = Note{Timestamp: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), Text: "Second note."}
)

// A `## Notes` line in a description is description text, with or without notes
// written after it, and it stays put however many times the ticket is rewritten:
// no heading is added and no text crosses between the body and the notes.
func TestNotesLineInDescriptionSurvivesRoundTrips(t *testing.T) {
	for descName, desc := range map[string]string{
		"text below the line":    "Intro\n\n## Notes\n\nfoo",
		"nothing below the line": "Intro\n\n## Notes",
	} {
		for name, notes := range map[string][]Note{
			"no notes":   nil,
			"with notes": {firstNote, secondNote},
		} {
			t.Run(descName+"/"+name, func(t *testing.T) {
				got := bodyTicket(desc, notes...)
				first, err := Serialize(roundTrip(t, got))
				if err != nil {
					t.Fatal(err)
				}
				for range 3 {
					got = roundTrip(t, got)
					if gotDesc, _, _, _ := BodySections(got.Body); gotDesc != desc {
						t.Errorf("description = %q, want %q", gotDesc, desc)
					}
					if len(got.Notes) != len(notes) {
						t.Fatalf("notes = %+v, want %+v", got.Notes, notes)
					}
					for i, n := range notes {
						if got.Notes[i].Text != n.Text {
							t.Errorf("notes[%d] = %q, want %q", i, got.Notes[i].Text, n.Text)
						}
					}
					data, err := Serialize(got)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(data, first) {
						t.Errorf("a rewrite changed the file:\nfirst:\n%s\nnow:\n%s", first, data)
					}
				}
			})
		}
	}
}

// An empty `## Notes` heading in a stored file is text, before and after a note
// is added: the note reads back as a note, and no later write moves the file or
// adds a heading.
func TestEmptyNotesHeadingInStoredFile(t *testing.T) {
	const file = "---\nid: t-empty\nstatus: ready\ncreated: 2026-01-01T00:00:00Z\ntype: feature\n---\n# Title\n\nIntro.\n\n## Notes\n"
	got, err := Parse(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Intro.\n\n## Notes\n" || len(got.Notes) != 0 {
		t.Fatalf("body = %q, notes = %+v", got.Body, got.Notes)
	}
	for range 3 {
		got = roundTrip(t, got)
		if got.Body != "Intro.\n\n## Notes\n" || len(got.Notes) != 0 {
			t.Fatalf("rewrite moved text: body %q, notes %+v", got.Body, got.Notes)
		}
	}

	got.Notes = append(got.Notes, firstNote)
	first, err := Serialize(got)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(first), notesHeading+"\n"); count != 2 {
		t.Errorf("%d notes headings, want the text line and the section:\n%s", count, first)
	}
	for range 3 {
		got = roundTrip(t, got)
		if got.Body != "Intro.\n\n## Notes\n" {
			t.Errorf("body = %q", got.Body)
		}
		if len(got.Notes) != 1 || got.Notes[0].Text != firstNote.Text {
			t.Fatalf("notes = %+v, want the one note", got.Notes)
		}
		data, err := Serialize(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, first) {
			t.Errorf("a rewrite changed the file:\nfirst:\n%s\nnow:\n%s", first, data)
		}
	}
}

// The sections below a `## Notes` line in a description used to go with it.
func TestNotesLineInDescriptionKeepsLaterSections(t *testing.T) {
	const body = "Intro\n\n## Notes\n\nfoo\n\n## Design\n\nThe design.\n\n## Acceptance Criteria\n\n- It works\n"
	got := roundTrip(t, bodyTicket(body, firstNote))

	desc, design, acceptance, _ := BodySections(got.Body)
	if desc != "Intro\n\n## Notes\n\nfoo" || design != "The design." || acceptance != "- It works" {
		t.Errorf("desc = %q, design = %q, acceptance = %q", desc, design, acceptance)
	}
	if len(got.Notes) != 1 {
		t.Errorf("notes = %+v, want the one note", got.Notes)
	}
}

// parseBody and Serialize share one rule for where the notes open; each row is
// a stored file's body, with the fields it has to read as.
func TestNotesSectionOpening(t *testing.T) {
	const stamp = "**2026-03-01T00:00:00Z**"
	tests := []struct {
		name  string
		rest  string
		body  string
		notes []string
	}{
		{
			name:  "timestamp on the next non-blank line",
			rest:  "Intro.\n\n## Notes\n\n" + stamp + "\n\nA note.\n",
			body:  "Intro.\n",
			notes: []string{"A note."},
		},
		{
			name:  "whitespace-only line ahead of the timestamp",
			rest:  "Intro.\n\n## Notes\n   \n" + stamp + "\n\nA note.\n",
			body:  "Intro.\n",
			notes: []string{"A note."},
		},
		{
			name:  "section opens the body",
			rest:  "## Notes\n\n" + stamp + "\n\nA note.\n",
			body:  "\n",
			notes: []string{"A note."},
		},
		{
			name: "nothing below the heading is text",
			rest: "Intro.\n\n## Notes\n\n\n",
			body: "Intro.\n\n## Notes\n",
		},
		{
			name: "text below the heading",
			rest: "Intro.\n\n## Notes\n\nfoo\n",
			body: "Intro.\n\n## Notes\n\nfoo\n",
		},
		{
			name: "indented timestamp is not one parseNotes accepts",
			rest: "Intro.\n\n## Notes\n\n  " + stamp + "\n\nfoo\n",
			body: "Intro.\n\n## Notes\n\n  " + stamp + "\n\nfoo\n",
		},
		{
			name: "stamp carrying more than a timestamp",
			rest: "Intro.\n\n## Notes\n\n**2026-03-01T00:00:00Z [agent]**\n\nfoo\n",
			body: "Intro.\n\n## Notes\n\n**2026-03-01T00:00:00Z [agent]**\n\nfoo\n",
		},
		{
			name: "text ahead of the first timestamp stays with what it precedes",
			rest: "## Notes\n\nfoo\n\n" + stamp + "\n\nbar\n",
			body: "## Notes\n\nfoo\n\n" + stamp + "\n\nbar\n",
		},
		{
			name:  "a later heading is the section",
			rest:  "Intro.\n\n## Notes\n\nfoo\n\n## Notes\n\n" + stamp + "\n\nA note.\n",
			body:  "Intro.\n\n## Notes\n\nfoo\n",
			notes: []string{"A note."},
		},
		{
			name:  "a notes line inside a note is note text",
			rest:  "## Notes\n\n" + stamp + "\n\nfoo\n\n## Notes\n\nbar\n",
			body:  "\n",
			notes: []string{"foo\n\n## Notes\n\nbar"},
		},
		{
			name: "a longer heading is not the notes heading",
			rest: "Intro.\n\n## Notes on rollout\n\n" + stamp + "\n\nfoo\n",
			body: "Intro.\n\n## Notes on rollout\n\n" + stamp + "\n\nfoo\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader("---\nid: t-open\nstatus: ready\ncreated: 2026-01-01T00:00:00Z\ntype: feature\n---\n# Title\n\n" + tt.rest))
			if err != nil {
				t.Fatal(err)
			}
			if got.Body != tt.body {
				t.Errorf("body = %q, want %q", got.Body, tt.body)
			}
			var notes []string
			for _, n := range got.Notes {
				notes = append(notes, n.Text)
			}
			if !reflect.DeepEqual(notes, tt.notes) {
				t.Errorf("notes = %q, want %q", notes, tt.notes)
			}
			if again := roundTrip(t, got); again.Body != got.Body || len(again.Notes) != len(got.Notes) {
				t.Errorf("rewrite moved text: body %q notes %+v", again.Body, again.Notes)
			}
		})
	}
}

// Serialize writes the heading only when the body does not already open the
// section, or a second heading would land in the first note's text. A body ending
// in the line has not opened it, so that line stays text above Serialize's own.
func TestSerializeWritesOneNotesSectionHeading(t *testing.T) {
	for name, tt := range map[string]struct {
		body     string
		headings int
		want     string
	}{
		"body ends in the heading": {"Intro.\n\n## Notes\n", 2, "Intro.\n\n## Notes\n"},
		"body opens the section":   {"Intro.\n\n## Notes\n\n**2026-02-01T00:00:00Z**\n\nEarlier.\n", 1, "Intro.\n"},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := Serialize(bodyTicket(tt.body, firstNote))
			if err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(string(data), "## Notes"); count != tt.headings {
				t.Errorf("%d notes headings, want %d:\n%s", count, tt.headings, data)
			}
			got := roundTrip(t, bodyTicket(tt.body, firstNote))
			if got.Body != tt.want {
				t.Errorf("body = %q, want %q", got.Body, tt.want)
			}
			if last := got.Notes[len(got.Notes)-1]; last.Text != firstNote.Text {
				t.Errorf("notes = %+v", got.Notes)
			}
		})
	}
}

// A Review Log line inside a note is note text, whichever note it is in.
func TestReviewLogLineInNoteIsNoteText(t *testing.T) {
	const text = "Before.\n\n## Review Log\n\nAfter."
	got := roundTrip(t, bodyTicket("Intro.\n", Note{Timestamp: firstNote.Timestamp, Text: text}, secondNote))

	if len(got.Notes) != 2 || got.Notes[0].Text != text || got.Notes[1].Text != secondNote.Text {
		t.Fatalf("notes = %+v, want the whole note and the later one", got.Notes)
	}
	if got.Body != "Intro.\n" {
		t.Errorf("body = %q", got.Body)
	}
	if got.droppedReviewLog != 0 {
		t.Errorf("droppedReviewLog = %d, want 0: a note is not the legacy section", got.droppedReviewLog)
	}
}

// Every writer tk has shipped put the Review Log ahead of the notes, so what
// follows a note is that note's text: kept and readable rather than stripped.
func TestReviewLogBelowNotesIsKept(t *testing.T) {
	got, err := Parse(strings.NewReader("---\nid: t-after\nstatus: ready\ncreated: 2026-01-01T00:00:00Z\ntype: feature\n---\n# Title\n\nIntro.\n\n## Notes\n\n**2026-03-01T00:00:00Z**\n\nA note.\n\n## Review Log\n\n**2026-03-02T00:00:00Z [agent:reviewer]**\nAPPROVED\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0].Text, "APPROVED") {
		t.Errorf("notes = %+v, want the section's text kept in the note", got.Notes)
	}
	if got.droppedReviewLog != 0 {
		t.Errorf("droppedReviewLog = %d, want 0", got.droppedReviewLog)
	}
}

// A legacy section sitting under an empty notes heading is still the legacy
// section, stripped and counted, and the heading above it is text like any other
// non-opening line. A real notes section below it survives.
func TestReviewLogUnderEmptyNotesHeading(t *testing.T) {
	const section = "\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED\n"
	for name, tt := range map[string]struct {
		tail  string
		notes int
	}{
		"nothing below":     {tail: ""},
		"real notes follow": {tail: "\n## Notes\n\n**2026-03-01T00:00:00Z**\n\nA note.\n", notes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(strings.NewReader("---\nid: t-empty\nstatus: ready\ncreated: 2026-01-01T00:00:00Z\ntype: feature\n---\n# Title\n\nIntro.\n\n## Notes\n" + section + tt.tail))
			if err != nil {
				t.Fatal(err)
			}
			if got.Body != "Intro.\n\n## Notes\n" {
				t.Errorf("body = %q", got.Body)
			}
			if got.droppedReviewLog != len(section) {
				t.Errorf("droppedReviewLog = %d, want %d", got.droppedReviewLog, len(section))
			}
			if len(got.Notes) != tt.notes {
				t.Errorf("notes = %+v, want %d", got.Notes, tt.notes)
			}
		})
	}
}

// The legacy section ends at the next exact `## Notes` line, opening or not, so a
// non-opening one and what is under it stay body text. Each row is a stored file's
// body; the file has to settle after the first write, the only one with a section
// to drop, whether or not notes sit below what the cut kept.
func TestReviewLogCutKeepsNotesLineWithText(t *testing.T) {
	const (
		section = "\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED\n"
		stamp   = "**2026-03-01T00:00:00Z**"
	)
	for name, tt := range map[string]struct {
		rest    string
		want    string
		dropped int
		notes   int
	}{
		"text under the heading": {
			rest:    "Intro.\n\n## Notes\n\nfoo\n" + section,
			want:    "Intro.\n\n## Notes\n\nfoo\n",
			dropped: len(section),
		},
		"whitespace under the heading": {
			rest:    "Intro.\n\n## Notes\n   \n" + section,
			want:    "Intro.\n\n## Notes\n",
			dropped: len(section),
		},
		"earlier heading is text": {
			rest:    "Intro.\n\n## Notes\n\nfoo\n\n## Notes\n" + section,
			want:    "Intro.\n\n## Notes\n\nfoo\n\n## Notes\n",
			dropped: len(section),
		},
		"text below the section": {
			rest:    "Intro.\n" + section + "\n## Notes\n\nfoo\n",
			want:    "Intro.\n\n## Notes\n\nfoo\n",
			dropped: len(section),
		},
		"text and a timestamp below the section": {
			rest:    "Intro.\n" + section + "\n## Notes\n\nfoo\n\n" + stamp + "\n\nreal note\n",
			want:    "Intro.\n\n## Notes\n\nfoo\n\n" + stamp + "\n\nreal note\n",
			dropped: len(section),
		},
		"heading with nothing under it below the section": {
			rest:    "Intro.\n" + section + "\n## Notes\n",
			want:    "Intro.\n\n## Notes\n",
			dropped: len(section),
		},
		"no blank line around the section": {
			rest:    "Intro.\n## Review Log\nAPPROVED\n## Notes\nfoo\n",
			want:    "Intro.\n## Notes\nfoo\n",
			dropped: len("\n## Review Log\nAPPROVED"),
		},
		"section opens the body": {
			rest:    section[1:] + "\n## Notes\n\nfoo\n",
			want:    "## Notes\n\nfoo\n",
			dropped: len(section),
		},
		"real notes below the kept text": {
			rest:    "Intro.\n" + section + "\n## Notes\n\nfoo\n\n## Notes\n\n" + stamp + "\n\nreal note\n",
			want:    "Intro.\n\n## Notes\n\nfoo\n",
			dropped: len(section),
			notes:   1,
		},
		"a second section below the kept text": {
			rest:    "Intro.\n" + section + "\n## Notes\n\nfoo\n" + section,
			want:    "Intro.\n\n## Notes\n\nfoo\n",
			dropped: 2 * len(section),
		},
		"a second section ahead of real notes": {
			rest:    "Intro.\n" + section + "\n## Notes\n\nfoo\n" + section + "\n## Notes\n\n" + stamp + "\n\nreal note\n",
			want:    "Intro.\n\n## Notes\n\nfoo\n",
			dropped: 2 * len(section),
			notes:   1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(strings.NewReader("---\nid: t-cut\nstatus: ready\ncreated: 2026-01-01T00:00:00Z\ntype: feature\n---\n# Title\n\n" + tt.rest))
			if err != nil {
				t.Fatal(err)
			}
			if got.Body != tt.want {
				t.Errorf("body = %q, want %q", got.Body, tt.want)
			}
			if len(got.Notes) != tt.notes {
				t.Errorf("notes = %+v, want %d", got.Notes, tt.notes)
			}
			if got.droppedReviewLog != tt.dropped {
				t.Errorf("droppedReviewLog = %d, want %d", got.droppedReviewLog, tt.dropped)
			}

			written, err := Serialize(got)
			if err != nil {
				t.Fatal(err)
			}
			again, err := parseBytes(written)
			if err != nil {
				t.Fatal(err)
			}
			if again.droppedReviewLog != 0 {
				t.Errorf("droppedReviewLog after a write = %d, want 0", again.droppedReviewLog)
			}
			rewritten, err := Serialize(again)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(rewritten, written) {
				t.Errorf("second write changed the file:\n%s\n---\n%s", written, rewritten)
			}

			again.Notes = append(again.Notes, secondNote)
			noted := roundTrip(t, again)
			if noted.Body != tt.want {
				t.Errorf("body after a note = %q, want %q", noted.Body, tt.want)
			}
			if len(noted.Notes) != tt.notes+1 || noted.Notes[tt.notes].Text != secondNote.Text {
				t.Errorf("notes after a note = %+v", noted.Notes)
			}
		})
	}
}

func TestBodySectionsJoinsRepeatedBlocks(t *testing.T) {
	tests := []struct {
		name               string
		body               string
		design, testResult string
	}{
		{
			name:   "design blocks",
			body:   "## Design\n\nA\n\n## Design Notes\n\nB",
			design: "A\n\nB",
		},
		{
			name:       "test results blocks",
			body:       "## Test Results\n\nA\n\n## Test Results Summary\n\nB",
			testResult: "A\n\nB",
		},
		{
			name:   "blocks apart keep body order",
			body:   "## Design\n\nA\n\n## Acceptance Criteria\n\n- x\n\n## Design Notes\n\nB\n",
			design: "A\n\nB",
		},
		{
			name:   "empty later block adds nothing",
			body:   "## Design\n\nA\n\n## Design Notes\n",
			design: "A",
		},
		{
			name:   "empty earlier block adds nothing",
			body:   "## Design\n\n## Design Notes\n\nB\n",
			design: "B",
		},
		{
			name:   "folded heading stays in its block",
			body:   "## Design\n\nA\n\n## Rollout\n\nStaged.\n\n## Design Notes\n\nB\n",
			design: "A\n\n## Rollout\n\nStaged.\n\nB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, design, _, testResults := BodySections(tt.body)
			if design != tt.design {
				t.Errorf("design = %q, want %q", design, tt.design)
			}
			if testResults != tt.testResult {
				t.Errorf("test results = %q, want %q", testResults, tt.testResult)
			}
		})
	}
}

// The write is the inverse of the join: one edit replaces every block the read
// merged, under one heading where the first block was, and leaves what sat
// between them in place.
func TestUpdateSection_MergedDesignAndTestResults(t *testing.T) {
	tests := []struct {
		name    string
		heading string
		body    string
		want    string
	}{
		{
			name:    "adjacent design blocks",
			heading: "Design",
			body:    "\nIntro.\n\n## Design\n\nA\n\n## Design Notes\n\nB\n",
			want:    "\nIntro.\n\n## Design\n\nnew\n",
		},
		{
			name:    "design blocks around acceptance",
			heading: "Design",
			body:    "\nIntro.\n\n## Design\n\nA\n\n## Acceptance Criteria\n\n- x\n\n## Design Notes\n\nB\n\n## Test Results\n\nrun\n",
			want:    "\nIntro.\n\n## Design\n\nnew\n\n## Acceptance Criteria\n\n- x\n\n## Test Results\n\nrun\n",
		},
		{
			name:    "a folded heading is design text",
			heading: "Design",
			body:    "\nIntro.\n\n## Design\n\nA\n\n## Rollout\n\nStaged.\n\n## Acceptance Criteria\n\n- x\n",
			want:    "\nIntro.\n\n## Design\n\nnew\n\n## Acceptance Criteria\n\n- x\n",
		},
		{
			name:    "a hand-written heading is replaced by the canonical one",
			heading: "Design",
			body:    "\n## Design Notes\n\nA\n",
			want:    "\n## Design\n\nnew\n",
		},
		{
			name:    "test results blocks",
			heading: "Test Results",
			body:    "\nIntro.\n\n## Test Results\n\nA\n\n## Design\n\nd\n\n## Test Results Summary\n\nB\n",
			want:    "\nIntro.\n\n## Test Results\n\nnew\n\n## Design\n\nd\n",
		},
		{
			name:    "an acceptance block still closes at an unnamed heading",
			heading: "Acceptance Criteria",
			body:    "\n## Acceptance Criteria\n\n- x\n\n## Rollout\n\nStaged.\n",
			want:    "\n## Acceptance Criteria\n\nnew\n\n## Rollout\n\nStaged.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpdateSection(tt.body, tt.heading, "new"); got != tt.want {
				t.Errorf("body =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// A client writing back the value it read leaves the read unchanged, for every
// section, across a body holding each shape the read folds or merges.
func TestUpdateSection_WriteBackIsStable(t *testing.T) {
	for name, body := range map[string]string{
		"notes line in the description": "Intro\n\n## Notes\n\nfoo\n\n## Design\n\nd\n",
		"notes line alone in the body":  "Intro\n\n## Notes\n\nfoo\n",
		"longer notes heading":          "Intro\n\n## Notes on rollout\n\nfoo\n\n## Acceptance Criteria\n\n- x\n",
		"notes line in design":          "Intro\n\n## Design\n\nd\n\n## Notes\n\nfoo\n\n## Test Results\n\nrun\n",
		"repeated design":               "Intro\n\n## Design\n\nA\n\n## Design Notes\n\nB\n",
		"repeated test results":         "Intro\n\n## Test Results\n\nA\n\n## Acceptance Criteria\n\n- x\n\n## Test Results Summary\n\nB\n",
		"repeated acceptance":           "Intro\n\n## Acceptance Criteria\n\n- x\n\n## Acceptance Notes\n\n- y\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := roundTrip(t, bodyTicket(body, firstNote))
			desc, design, acceptance, results := BodySections(got.Body)
			read := []string{desc, design, acceptance, results}

			for i, heading := range []string{"", "Design", "Acceptance Criteria", "Test Results"} {
				if read[i] == "" {
					continue
				}
				got.Body = UpdateSection(got.Body, heading, read[i])
				got = roundTrip(t, got)
				d, g, a, r := BodySections(got.Body)
				if now := []string{d, g, a, r}; !reflect.DeepEqual(now, read) {
					t.Errorf("writing back %q moved the sections:\nread %q\nnow  %q", heading, read, now)
				}
			}
			if len(got.Notes) != 1 {
				t.Errorf("notes = %+v, want the one note", got.Notes)
			}
		})
	}
}

// A description edit replaces the whole description the read returned, the
// `## Notes` line and what follows it included, and a new section goes after
// the description rather than into it.
func TestUpdateSection_NotesLineIsDescriptionText(t *testing.T) {
	const body = "\nIntro\n\n## Notes\n\nfoo\n"

	if got := UpdateSection(body, "", "New."); got != "\nNew.\n" {
		t.Errorf("description edit = %q, want the old tail gone", got)
	}
	got := UpdateSection(body, "Design", "d")
	desc, design, _, _ := BodySections(got)
	if desc != "Intro\n\n## Notes\n\nfoo" || design != "d" {
		t.Errorf("desc = %q, design = %q.\nBody:\n%s", desc, design, got)
	}
}

// A body can hold a real notes section before it is first written: an edit
// stores what it was given, and two edits in one call run on that body. The
// section is not a place an edit writes, and new sections stay above it, where
// the next read sees them as sections.
func TestUpdateSection_LeavesNotesSectionAlone(t *testing.T) {
	const notes = "## Notes\n\n**2026-03-01T00:00:00Z**\n\n## Design\n\nA note.\n"
	tests := []struct {
		name    string
		heading string
		body    string
		want    string
	}{
		{"description", "", "\nOld.\n\n" + notes, "\nnew\n\n" + notes},
		{"inserted section", "Design", "\nIntro.\n\n" + notes, "\nIntro.\n\n## Design\n\nnew\n\n" + notes},
		{"replaced section", "Design", "\nIntro.\n\n## Design\n\nold\n\n" + notes, "\nIntro.\n\n## Design\n\nnew\n\n" + notes},
		{"acceptance", "Acceptance Criteria", "\n## Acceptance Criteria\n\n- x\n\n" + notes, "\n## Acceptance Criteria\n\nnew\n\n" + notes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpdateSection(tt.body, tt.heading, "new"); got != tt.want {
				t.Errorf("body =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// The three reported losses through a real store: what goes in comes back after
// a write of the ticket read from disk, which is the write that used to make a
// parse-time loss permanent.
func TestStoreKeepsBodyTextAcrossWrites(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("keep-0001")
	tk.Body = "Intro\n\n## Notes\n\nfoo\n\n## Design\n\nA\n\n## Design Notes\n\nB\n"
	tk.Notes = []Note{{Timestamp: firstNote.Timestamp, Text: "Before.\n\n## Review Log\n\nAfter."}, secondNote}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for range 2 {
		got, err := store.Get("keep-0001")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		desc, design, _, _ := BodySections(got.Body)
		if desc != "Intro\n\n## Notes\n\nfoo" || design != "A\n\nB" {
			t.Errorf("desc = %q, design = %q", desc, design)
		}
		if len(got.Notes) != 2 || got.Notes[0].Text != tk.Notes[0].Text || got.Notes[1].Text != secondNote.Text {
			t.Errorf("notes = %+v", got.Notes)
		}
		if err := store.Update(got); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}
}
