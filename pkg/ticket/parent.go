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
	parent, err := store.Get(t.Parent)
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
	fs, ok := store.(*FileStore)
	if !ok {
		return false
	}
	proj, _ := ParseNamespacedID(t.Parent)
	return proj != "" && proj != fs.Project
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

// ProjectSkip is a project the audit could not read, and why.
type ProjectSkip struct {
	Project string `json:"project"`
	Error   string `json:"error"`
}

// ParentAudit is what AuditParents found: the violations, and the projects it
// could not read. Skipped projects are part of the result rather than swallowed
// — a report that silently covered less than the whole store would call a store
// clean that a write can still trip on, which is the wrong way to fail.
type ParentAudit struct {
	Violations []ParentViolation `json:"violations"`
	Skipped    []ProjectSkip     `json:"skipped,omitempty"`
}

// AuditParents reports every ticket whose parent breaks the one-level
// hierarchy, so a store predating the rule can be cleaned before a write trips
// on it. Strictly read-only: nothing is repaired or rewritten.
//
// A MultiStore is audited project by project, against the same per-project
// FileStore the write path validates against. Resolving through MultiStore.Get
// instead would disagree with enforcement three ways — it accepts another
// project's prefix, resolves a bare ID into another project, and turns a bare
// ID that matches in two projects into a false parent-missing — so the audit
// would clear tickets that no write can touch and flag tickets writes accept.
func AuditParents(store Store) (ParentAudit, error) {
	m, ok := store.(*MultiStore)
	if !ok {
		violations, err := auditStoreParents(store)
		return ParentAudit{Violations: violations}, err
	}

	projects, err := m.projects()
	if err != nil {
		return ParentAudit{}, err
	}
	var audit ParentAudit
	for _, proj := range projects {
		projStore, err := m.storeFor(proj)
		if err != nil {
			audit.Skipped = append(audit.Skipped, ProjectSkip{Project: proj, Error: err.Error()})
			continue
		}
		violations, err := auditStoreParents(projStore)
		if err != nil {
			audit.Skipped = append(audit.Skipped, ProjectSkip{Project: proj, Error: err.Error()})
			continue
		}
		for _, v := range violations {
			v.ID = FormatNamespacedID(proj, v.ID)
			audit.Violations = append(audit.Violations, v)
		}
	}
	return audit, nil
}

// auditStoreParents runs the checks ResolveParent runs over one store's
// tickets, plus the cycle class only a pre-rule store can hold. Parents resolve
// through store.Get, which accepts the full, partial, and namespaced forms
// stored parent fields actually carry; unlike the write path it only reads them
// — nothing here rewrites a parent to what it resolved to. One violation per
// ticket, most specific first — a cycle is reported as a cycle rather than as
// the non-epic parent each of its members also has.
func auditStoreParents(store Store) ([]ParentViolation, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var violations []ParentViolation
	report := func(t *Ticket, kind ParentViolationKind, detail string) {
		violations = append(violations, ParentViolation{ID: t.ID, Parent: t.Parent, Kind: kind, Detail: detail})
	}
	for _, t := range tickets {
		if t.Parent == "" {
			continue
		}
		if t.Type == TypeEpic {
			report(t, ViolationEpicHasParent, "epics are top level and cannot have a parent")
			continue
		}
		if isCrossProjectParent(store, t) {
			report(t, ViolationParentCrossProject, "an epic and its children must live in the same project")
			continue
		}
		parent, err := store.Get(t.Parent)
		if err != nil {
			report(t, ViolationParentMissing, err.Error())
			continue
		}
		if chain := parentCycle(store, t); chain != "" {
			report(t, ViolationParentCycle, chain)
			continue
		}
		if parent.Type != TypeEpic {
			report(t, ViolationParentNotEpic, fmt.Sprintf("parent %s is type %s", parent.ID, parent.Type))
		}
	}
	return violations, nil
}

// parentCycle walks t's parent chain and returns the chain as text if it
// revisits a ticket, or "" if it terminates. Only a store written before the
// one-level rule can hold such a chain; an unresolvable link ends the walk and
// is reported against the ticket that carries it.
func parentCycle(store Store, t *Ticket) string {
	chain := []string{t.ID}
	seen := map[string]bool{t.ID: true}
	cur := t
	for cur.Parent != "" {
		next, err := store.Get(cur.Parent)
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
