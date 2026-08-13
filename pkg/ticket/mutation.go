package ticket

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/state"
)

// MutationOp names the kind of change one mutation log line records.
type MutationOp string

const (
	MutationCreate  MutationOp = "create"
	MutationEdit    MutationOp = "edit"
	MutationAddNote MutationOp = "add-note"
	MutationDep     MutationOp = "dep"
	MutationLink    MutationOp = "link"
	MutationDelete  MutationOp = "delete"
	MutationMove    MutationOp = "move"
)

// MutationEntry is one line of a project's mutations.jsonl: the audit trail of
// who changed which ticket, when, and in what respect. It is independent of git
// history — a ticket is changed far more often than the store is committed, and
// a commit records the store's state rather than the writers that produced it.
//
// TicketID is the bare ID, without the project namespace: the log is already
// keyed by project through its path, and the namespace a write carried depends
// on which store the caller went through rather than on which ticket it changed.
//
// Source is declared by the writer and unauthenticated: the trail records which
// cooperating tool wrote a change, not proof of who did, and the log file offers
// no integrity guarantee against a process running as the user.
type MutationEntry struct {
	Timestamp     time.Time  `json:"timestamp"`
	TicketID      string     `json:"ticket_id"`
	Operation     MutationOp `json:"operation"`
	Source        string     `json:"source"`
	FieldsChanged []string   `json:"fields_changed,omitempty"`
}

// SourceEnv attributes every write made by the process that sets it, whatever
// surface the write came through. It wins over the source a store carries, so a
// harness or an agent wrapper can name itself once rather than per call.
const SourceEnv = "TK_SOURCE"

// SourceHuman is what a write with no other attribution records: a person at
// the CLI or in the TUI, which is the only writer that reaches the store
// without announcing itself.
const SourceHuman = "human"

// WithSource returns a store whose writes are attributed to source, leaving the
// store it was given untouched: `tk serve` shares one store across every tool
// call, and setting the field on it would attribute one caller's write to
// whichever client called in last. A store type this package does not own
// carries no attribution and comes back unchanged.
func WithSource(s Store, source string) Store {
	switch st := s.(type) {
	case *FileStore:
		copied := *st
		copied.Source = source
		return &copied
	case *MultiStore:
		copied := *st
		copied.Source = source
		return &copied
	}
	return s
}

// logMutation appends one entry to the project's mutation log. Every write path
// reaches it through the store, so no call site is instrumented.
func (s *FileStore) logMutation(id string, op MutationOp, fields []string) {
	// A store with no project has no path to key the log on: the log lives at
	// ~/.ticket/state/<project>/, and this shape (NewFileStore) is the legacy
	// unit-test one that no store resolution produces. Nothing is recorded for
	// it rather than inventing a project name for the entry to land under.
	if s.Project == "" {
		return
	}
	path, err := state.MutationLogPath(s.Project)
	if err == nil {
		err = state.AppendJSONL(path, MutationEntry{
			Timestamp:     time.Now().UTC(),
			TicketID:      id,
			Operation:     op,
			Source:        s.mutationSource(),
			FieldsChanged: fields,
		})
	}
	if err != nil {
		// The log records the write; it does not gate it. The mutation is
		// already on disk by the time this runs, and failing it here would
		// report an error for a change that happened — so a broken audit trail
		// is a warning on stderr and nothing more.
		fmt.Fprintf(os.Stderr, "warning: mutation log for project %s: %v\n", s.Project, err)
	}
}

// mutationSource decides who a write is attributed to. TK_SOURCE wins wherever
// it is set, then the source the caller wrapped the store with (an MCP client
// name, the journal watcher), then the human at the terminal.
func (s *FileStore) mutationSource() string {
	if env := strings.TrimSpace(os.Getenv(SourceEnv)); env != "" {
		return env
	}
	if s.Source != "" {
		return s.Source
	}
	return SourceHuman
}

// logUpdate records a write that replaced an existing ticket, deriving the
// operation and the changed fields from the bytes it replaced. The prior state
// is parsed with the parser readFile uses, so both sides of the diff are the
// stored form.
func (s *FileStore) logUpdate(prior []byte, next *Ticket) {
	if s.Project == "" {
		return
	}
	old, err := Parse(bytes.NewReader(prior))
	if err != nil {
		// A prior that does not parse leaves nothing to diff against. The write
		// still happened, so it is recorded as an edit naming no fields rather
		// than dropped from the trail.
		s.logMutation(next.ID, MutationEdit, nil)
		return
	}
	fields := changedFields(old, next)
	s.logMutation(next.ID, classifyUpdate(fields, old, next), fields)
}

// Field names shared by the diff and the operation it is classified into.
const (
	fieldNotes    = "notes"
	fieldDeps     = "deps"
	fieldDepCargo = "dep_cargo"
	fieldLinks    = "links"
)

// changedFields names the stored fields that differ between two states of a
// ticket, in a fixed order. The stamped timestamps are left out: updated moves
// on every write and completed follows the status, so neither says anything
// about what the writer changed.
func changedFields(prior, next *Ticket) []string {
	var fields []string
	add := func(name string, changed bool) {
		if changed {
			fields = append(fields, name)
		}
	}
	add("title", prior.Title != next.Title)
	// An epic's stored status is advisory: the derivation reads its children and
	// the abandon flag, never the status field, so Get hands back a status the
	// file need not hold and every read-modify-write carries that one back. A
	// delta on an epic therefore says nothing about what the writer changed, and
	// counting it would classify an accumulating write as an edit.
	add("status", prior.Status != next.Status && prior.Type != TypeEpic && next.Type != TypeEpic)
	add("abandoned", prior.Abandoned != next.Abandoned)
	add("type", prior.Type != next.Type)
	add("priority", prior.Priority != next.Priority)
	add("parent", prior.Parent != next.Parent)
	add("tags", !slices.Equal(prior.Tags, next.Tags))
	add(fieldDeps, !slices.Equal(prior.Deps, next.Deps))
	add(fieldLinks, !slices.Equal(prior.Links, next.Links))
	add(fieldDepCargo, !maps.Equal(prior.DepCargo, next.DepCargo))
	add("outputs", !maps.Equal(prior.Outputs, next.Outputs))
	add("extra", !maps.Equal(prior.Extra, next.Extra))
	add("branch", prior.Branch != next.Branch)
	add("external_ref", prior.ExternalRef != next.ExternalRef)
	// Trimmed on both sides: the parser normalizes a body's surrounding
	// whitespace and a caller that built one by hand has not.
	add("body", strings.TrimSpace(prior.Body) != strings.TrimSpace(next.Body))
	add(fieldNotes, !notesEqual(prior.Notes, next.Notes))
	return fields
}

// classifyUpdate names the operation an update performed. The accumulating
// writes are recognised by what they touch and nothing else — a note appended, a
// dep edge with its cargo, a link — so the trail distinguishes them from an edit
// of the ticket's own fields. Anything else, including a write that changed
// nothing, is an edit.
func classifyUpdate(fields []string, prior, next *Ticket) MutationOp {
	switch {
	case onlyFields(fields, fieldNotes) && len(next.Notes) > len(prior.Notes):
		return MutationAddNote
	case onlyFields(fields, fieldDeps, fieldDepCargo):
		return MutationDep
	case onlyFields(fields, fieldLinks):
		return MutationLink
	}
	return MutationEdit
}

// onlyFields reports whether fields is non-empty and holds nothing outside the
// given set.
func onlyFields(fields []string, allowed ...string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if !slices.Contains(allowed, f) {
			return false
		}
	}
	return true
}

func notesEqual(a, b []Note) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Text != b[i].Text || !a[i].Timestamp.Equal(b[i].Timestamp) {
			return false
		}
	}
	return true
}
