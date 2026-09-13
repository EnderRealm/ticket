package ticket

import "strings"

// ResolveParent enforces the one-level hierarchy — a ticket's parent, when set,
// must resolve to an epic, in the same project until the catalog activates
// cross-project parents, and an epic itself has no parent — and rewrites
// t.Parent to the epic it resolved to. Epic membership is then a single field
// read instead of a walk up a chain of unknown depth, and "an epic's children"
// has one definition.
//
// Every write path applies this inside the store lock (op.resolveParent); this
// is the same check against a snapshot read without the exclusive lock, for a
// caller asking what a write would say without making one.
func ResolveParent(store Store, t *Ticket) error {
	snap, err := snapshotOf(store)
	if err != nil {
		return err
	}
	ns := storeProject(store)
	id := t.ID
	if proj, bare := ParseNamespacedID(t.ID); proj != "" {
		ns, id = proj, bare
	}
	prior, _ := snap.Get(FormatNamespacedID(ns, id))
	return (&op{snap: snap}).resolveParent(ns, prior, t)
}

// ParentViolationKind classifies how a ticket breaks the one-level hierarchy.
type ParentViolationKind string

const (
	ViolationEpicHasParent      ParentViolationKind = "epic-has-parent"
	ViolationParentMissing      ParentViolationKind = "parent-missing"
	ViolationParentCycle        ParentViolationKind = "parent-cycle"
	ViolationParentNotEpic      ParentViolationKind = "parent-not-epic"
	ViolationParentCrossProject ParentViolationKind = "parent-cross-project"
)

// ParentViolation is one ticket whose parent the one-level hierarchy rejects.
type ParentViolation struct {
	ID     string              `json:"id"`
	Parent string              `json:"parent"`
	Kind   ParentViolationKind `json:"kind"`
	Detail string              `json:"detail"`
}

// ContentIssueKind classifies what an audit found wrong with a ticket's stored
// free text.
type ContentIssueKind string

const (
	ContentEnvelopeFragment ContentIssueKind = "envelope-fragment"
	ContentEmptyAcceptance  ContentIssueKind = "empty-acceptance"
	ContentBareAcceptance   ContentIssueKind = "bare-acceptance"
	ContentLegacyReviewLog  ContentIssueKind = "legacy-review-log"
)

// ContentIssue is one ticket whose stored body says something is missing —
// a section that absorbed part of the tool call that wrote it, a description
// with no acceptance criteria beside it, or criteria that state what done means
// with nothing that can decide it — or holds content no reader sees: a legacy
// `## Review Log` the parser strips and the ticket's next write drops for good.
type ContentIssue struct {
	ID     string           `json:"id"`
	Kind   ContentIssueKind `json:"kind"`
	Field  string           `json:"field,omitempty"`  // which body section, for envelope-fragment
	Detail string           `json:"detail,omitempty"` // the offending tail, for envelope-fragment
	Bytes  int              `json:"bytes,omitempty"`  // size of the stripped section, for legacy-review-log
	Bare   int              `json:"bare,omitempty"`   // how many criteria carry no check, for bare-acceptance
}

// parentCycle walks t's parent chain and returns the chain as text if it
// revisits a ticket, or "" if it terminates. Only a store written before the
// one-level rule can hold such a chain; an unresolvable link ends the walk and
// is reported against the ticket that carries it. parentOf resolves a stored
// parent the snapshot's way: bare means the child's own namespace.
func parentCycle(parentOf func(*Ticket) (*Ticket, error), t *Ticket) string {
	chain := []string{t.ID}
	seen := map[string]bool{t.ID: true}
	cur := t
	for cur.Parent != "" {
		next, err := parentOf(cur)
		if err != nil {
			return ""
		}
		chain = append(chain, next.ID)
		if seen[next.ID] {
			return strings.Join(chain, " -> ")
		}
		seen[next.ID] = true
		cur = next
	}
	return ""
}
