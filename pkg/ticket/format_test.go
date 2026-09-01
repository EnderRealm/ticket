package ticket

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Legacy ticket with Review Log section — should be silently stripped.
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
