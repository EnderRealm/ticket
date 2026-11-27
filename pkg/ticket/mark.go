package ticket

import "fmt"

// Marks: what a person said of a generic ticket they finished by hand. No
// commit or run attests a step done by hand, so the person's word is recorded
// on the ticket with who gave it, where and when. Written through ApplyAction
// alone.

// MarkResult is what a person said of a generic ticket.
type MarkResult string

const (
	MarkDone      MarkResult = "done"
	MarkFailed    MarkResult = "failed"
	MarkNotNeeded MarkResult = "not_needed"
)

// MarkRow is one recorded mark. The rows are a stack, oldest first: mark pushes
// one and unmark removes the last.
//
// By is the declared source, as on ActionReceipt; Host is the machine that
// recorded the mark, as os.Hostname named it at the write. Prior is the status
// the ticket held before the mark, which unmark restores. At is a string for
// the reason VerdictRow.At is.
type MarkRow struct {
	Result MarkResult `yaml:"result" json:"result"`
	Note   string     `yaml:"note" json:"note"`
	At     string     `yaml:"at" json:"at"`
	By     string     `yaml:"by" json:"by"`
	Host   string     `yaml:"host" json:"host"`
	Prior  Status     `yaml:"prior" json:"prior"`
}

// MarshalYAML writes the row with its free text quoted.
func (r MarkRow) MarshalYAML() (any, error) {
	return struct {
		Result MarkResult `yaml:"result"`
		Note   quoted     `yaml:"note"`
		At     string     `yaml:"at"`
		By     quoted     `yaml:"by"`
		Host   quoted     `yaml:"host"`
		Prior  Status     `yaml:"prior"`
	}{r.Result, quoted(r.Note), r.At, quoted(r.By), quoted(r.Host), r.Prior}, nil
}

// validMarkResult reports whether r is a result tk records.
func validMarkResult(r MarkResult) bool {
	return r == MarkDone || r == MarkFailed || r == MarkNotNeeded
}

// status is what the ticket holds once the row is recorded: a failed step stays
// where it was, with the person.
func (r MarkRow) status() Status {
	switch r.Result {
	case MarkDone:
		return StatusDone
	case MarkNotNeeded:
		return StatusClosed
	}
	return r.Prior
}

// LastMark is the last mark standing on the ticket, or nil when none does. The
// pointer is into t's own rows.
func LastMark(t *Ticket) *MarkRow {
	if len(t.Marks) == 0 {
		return nil
	}
	return &t.Marks[len(t.Marks)-1]
}

// markRefusal is why mark or unmark does not apply to t, or nil. A coding
// ticket finishes by landing, and an epic's status is derived from its
// children. Unmark undoes a mark only while the ticket still holds the status
// that mark left.
func markRefusal(t *Ticket, tr Transition) error {
	if t.Type == TypeEpic {
		return fmt.Errorf("%w: %s on epic %s — an epic's status is derived from its children; mark the child", ErrTransitionNotPermitted, tr, t.ID)
	}
	if kind := WorkflowKindOf(t); kind != WorkflowGeneric {
		return fmt.Errorf("%w: %s on %s, which is %s and not generic — a coding ticket finishes by landing", ErrTransitionNotPermitted, tr, t.ID, kind)
	}
	if tr != TransitionUnmark {
		return nil
	}
	last := LastMark(t)
	switch {
	case last == nil:
		return fmt.Errorf("%w: unmark on %s, which has no mark to undo", ErrTransitionNotPermitted, t.ID)
	case t.Status != last.status():
		return fmt.Errorf("%w: unmark on %s, which is %s and no longer holds the status its last mark (%s) left", ErrTransitionNotPermitted, t.ID, t.Status, last.Result)
	}
	return nil
}
