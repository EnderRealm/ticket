package ticket

import (
	"fmt"
	"strings"
)

// ResolveParent enforces the one-level hierarchy — a ticket's parent, when set,
// must resolve to an epic in the same store, and an epic itself has no parent —
// and rewrites t.Parent to the epic it resolved to. Epic membership is then a
// single field read instead of a walk up a chain of unknown depth, and "an
// epic's children" has one definition.
//
// Every write path reaches it through FileStore.Create/Update (MultiStore
// delegates to a per-project FileStore), so CLI, MCP, and TUI are covered
// structurally. Reads are untouched — a store written before the rule still
// lists and renders — but editing any field of an already-violating ticket now
// goes through here, so the errors say how to clear the violation.
func ResolveParent(store Store, t *Ticket) error {
	if t.Parent == "" {
		return nil
	}
	if t.Type == TypeEpic {
		return fmt.Errorf("epic %s cannot have parent %s: epics are top level. "+
			"Clear the parent, or change this ticket's type", t.ID, t.Parent)
	}
	if isCrossProjectParent(store, t) {
		return fmt.Errorf("ticket %s: parent %s is in another project: an epic and its children must live in the same project. "+
			"Repoint the parent at an epic in this project, or clear it", t.ID, t.Parent)
	}
	// readStored, not Get: only the parent's type and resolved ID matter here,
	// and deriving the status of an epic — which every legitimate parent is —
	// would read the whole store on every child write.
	parent, err := readStored(store, t.Parent)
	if err != nil {
		return fmt.Errorf("ticket %s: parent %s does not resolve: %w. "+
			"Repoint the parent at an existing epic, or clear it", t.ID, t.Parent, err)
	}
	if parent.Type != TypeEpic {
		return fmt.Errorf("ticket %s: parent %s is type %s, not an epic: only epics hold children. "+
			"Repoint the parent at an epic, or clear it", t.ID, parent.ID, parent.Type)
	}
	// Store what the parent resolved to, not what the caller typed. Resolution
	// matches partially, so `--parent abcd` names epic-abcd, while every reader
	// matches a parent by ID — leaving the typed form would store a parent the
	// write path called an epic and no view can place. The namespace is kept as
	// given: dropping it would leave a bare ID to resolve across projects.
	if proj, _ := ParseNamespacedID(t.Parent); proj != "" {
		t.Parent = FormatNamespacedID(proj, parent.ID)
	} else {
		t.Parent = parent.ID
	}
	return nil
}

// isCrossProjectParent reports whether t's parent names a project other than
// the one its own store owns. A per-project store cannot resolve another
// project's tickets, so this is named as its own reason rather than surfacing
// as a bare "not found": an epic and its children are meant to live together.
// Shared by ResolveParent and the audit so both apply one rule.
func isCrossProjectParent(store Store, t *Ticket) bool {
	p, ok := store.(projectStore)
	if !ok {
		return false
	}
	proj, _ := ParseNamespacedID(t.Parent)
	return proj != "" && proj != p.projectName()
}

// parentLookup resolves the ticket a child names as its parent, against a set
// the caller has already read. A loop over a store would otherwise resolve one
// parent per ticket through Store.Get, which reads the whole store again for
// every epic — and a parent is always meant to be an epic. Anything the set
// cannot answer falls through to read, which is where a parent field written
// before the one-level rule lands: a partial ID only the store's own matching
// resolves.
func parentLookup(store Store, tickets []*Ticket, read func(string) (*Ticket, error)) func(*Ticket) (*Ticket, error) {
	index := ticketsByID(tickets, storeProject(store))
	return func(t *Ticket) (*Ticket, error) {
		if parent, ok := index(qualifiedParent(t)); ok {
			return parent, nil
		}
		return read(t.Parent)
	}
}

// qualifiedParent returns the parent ID a ticket names, in the ticket's own
// namespace. An epic and its children live in the same project, so a parent
// recorded bare — as everything written before the namespacing rollout was —
// names one in the child's project; matching it against a bare key across a
// central store would find a same-named ticket in another project.
func qualifiedParent(t *Ticket) string {
	proj, _ := ParseNamespacedID(t.ID)
	parentProj, bare := ParseNamespacedID(t.Parent)
	if proj == "" || parentProj != "" {
		return t.Parent
	}
	return FormatNamespacedID(proj, bare)
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
// is reported against the ticket that carries it.
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
