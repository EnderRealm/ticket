package ticket

import (
	"fmt"
	"strings"
)

// DepNode represents one entry in a dependency tree.
type DepNode struct {
	ID     string
	Title  string
	Status Status
	Depth  int
	// Cargo is what flows across the edge from the parent node to this one.
	// Empty for the root (no incoming edge) and for unannotated edges.
	Cargo string
}

// DepTree walks the dependency graph for the given ticket ID.
// With full=false (default), each node appears only once (dedup).
// With full=true, shows the full tree with repeated subtrees.
func DepTree(store Store, id string, full bool) ([]DepNode, error) {
	root, err := store.Get(id)
	if err != nil {
		return nil, err
	}

	var nodes []DepNode
	seen := map[string]bool{}
	var walk func(t *Ticket, depth int, cargo string)
	walk = func(t *Ticket, depth int, cargo string) {
		nodes = append(nodes, DepNode{
			ID:     t.ID,
			Title:  t.Title,
			Status: t.Status,
			Depth:  depth,
			Cargo:  cargo,
		})
		for _, depID := range t.Deps {
			if !full && seen[depID] {
				continue
			}
			seen[depID] = true
			dep, err := store.Get(depID)
			if err != nil {
				// Dep references a missing ticket — include a stub.
				nodes = append(nodes, DepNode{
					ID:    depID,
					Title: "(not found)",
					Depth: depth + 1,
					Cargo: CargoFor(t, depID),
				})
				continue
			}
			walk(dep, depth+1, CargoFor(t, depID))
		}
	}
	walk(root, 0, "")
	return nodes, nil
}

// Cycle represents a dependency cycle as an ordered list of ticket IDs.
type Cycle struct {
	IDs []string
}

func (c Cycle) String() string {
	s := ""
	for i, id := range c.IDs {
		if i > 0 {
			s += " -> "
		}
		s += id
	}
	return s + " -> " + c.IDs[0]
}

// FindCycles detects dependency cycles among open (non-done/closed) tickets.
// Uses DFS with white(0)/gray(1)/black(2) coloring.
func FindCycles(store Store) ([]Cycle, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	// Index only non-terminal tickets.
	byID := map[string]*Ticket{}
	for _, t := range tickets {
		if t.Status != StatusDone && t.Status != StatusClosed {
			byID[t.ID] = t
		}
	}

	color := map[string]int{} // 0=white, 1=gray, 2=black
	path := []string{}
	seen := map[string]bool{} // normalized cycle dedup
	var cycles []Cycle

	var dfs func(id string)
	dfs = func(id string) {
		t, ok := byID[id]
		if !ok || color[id] == 2 {
			return
		}
		if color[id] == 1 {
			// Found cycle — extract from path.
			var cycle []string
			for i := len(path) - 1; i >= 0; i-- {
				cycle = append([]string{path[i]}, cycle...)
				if path[i] == id {
					break
				}
			}
			// Normalize: rotate so smallest ID is first.
			key := normalizeCycle(cycle)
			if !seen[key] {
				seen[key] = true
				cycles = append(cycles, Cycle{IDs: cycle})
			}
			return
		}

		color[id] = 1
		path = append(path, id)

		for _, depID := range t.Deps {
			dfs(depID)
		}

		path = path[:len(path)-1]
		color[id] = 2
	}

	for id := range byID {
		if color[id] == 0 {
			dfs(id)
		}
	}

	return cycles, nil
}

// normalizeCycle produces a canonical string key for a cycle by rotating
// so the lexicographically smallest ID comes first.
func normalizeCycle(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	minIdx := 0
	for i, id := range ids {
		if id < ids[minIdx] {
			minIdx = i
		}
	}
	key := ""
	for i := 0; i < len(ids); i++ {
		if i > 0 {
			key += ","
		}
		key += ids[(minIdx+i)%len(ids)]
	}
	return key
}

// isTerminal returns true if a ticket is in a terminal state.
func isTerminal(t *Ticket) bool {
	return t.Status == StatusDone || t.Status == StatusClosed
}

// IsBlocked returns true if any of the ticket's dependencies are not done/closed.
func IsBlocked(store Store, t *Ticket) bool {
	if len(t.Deps) == 0 {
		return false
	}
	for _, depID := range t.Deps {
		dep, err := store.Get(depID)
		if err != nil {
			// Missing dep is treated as blocking.
			return true
		}
		if !isTerminal(dep) {
			return true
		}
	}
	return false
}

// BlockingDeps returns the IDs of dependencies that are not done/closed.
func BlockingDeps(store Store, t *Ticket) []string {
	var blocking []string
	for _, depID := range t.Deps {
		dep, err := store.Get(depID)
		if err != nil {
			blocking = append(blocking, depID)
			continue
		}
		if !isTerminal(dep) {
			blocking = append(blocking, depID)
		}
	}
	return blocking
}

// IsReady returns true if the ticket is actionable: not terminal,
// not backlog, all deps done, and parent chain is active.
func IsReady(store Store, t *Ticket) bool {
	if isTerminal(t) || t.Status == StatusBacklog {
		return false
	}
	if IsBlocked(store, t) {
		return false
	}
	return parentChainActive(store, t.ID, map[string]bool{})
}

// IsReadyOpen is like IsReady but bypasses parent gating.
// Shows all unblocked non-terminal tickets regardless of epic status.
func IsReadyOpen(store Store, t *Ticket) bool {
	if isTerminal(t) || t.Status == StatusBacklog {
		return false
	}
	return !IsBlocked(store, t)
}

// parentChainActive checks that every ancestor (via parent field) is
// not terminal. If a parent is not found in the store, it's treated as active.
func parentChainActive(store Store, id string, visited map[string]bool) bool {
	if visited[id] {
		return true // avoid infinite loops
	}
	visited[id] = true

	t, err := store.Get(id)
	if err != nil {
		return true
	}
	if t.Parent == "" {
		return true
	}
	parent, err := store.Get(t.Parent)
	if err != nil {
		return true // parent not in store — treat as active
	}
	if isTerminal(parent) {
		return false
	}
	return parentChainActive(store, parent.ID, visited)
}

// ReadyTickets returns all tickets that pass the IsReady check.
func ReadyTickets(store Store) ([]*Ticket, error) {
	return readyTicketsImpl(store, false)
}

// ReadyTicketsOpen returns all unblocked tickets, bypassing parent gating.
func ReadyTicketsOpen(store Store) ([]*Ticket, error) {
	return readyTicketsImpl(store, true)
}

func readyTicketsImpl(store Store, openMode bool) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var ready []*Ticket
	for _, t := range tickets {
		var r bool
		if openMode {
			r = IsReadyOpen(store, t)
		} else {
			r = IsReady(store, t)
		}
		if r {
			ready = append(ready, t)
		}
	}
	return ready, nil
}

// FrontierTickets returns the schedulable set: tickets with status ready
// whose dependencies are all terminal (done/closed).
func FrontierTickets(store Store) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var frontier []*Ticket
	for _, t := range tickets {
		if t.Status == StatusReady && !IsBlocked(store, t) {
			frontier = append(frontier, t)
		}
	}
	return frontier, nil
}

// BlockedTickets returns all non-terminal, non-backlog tickets with unresolved deps.
func BlockedTickets(store Store) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var blocked []*Ticket
	for _, t := range tickets {
		if isTerminal(t) || t.Status == StatusBacklog {
			continue
		}
		if IsBlocked(store, t) {
			blocked = append(blocked, t)
		}
	}
	return blocked, nil
}

// ValidateStateTransition rejects saving an epic as done while any of its
// children are still non-terminal (not done, not closed). Only checks when
// t.Type == TypeEpic and t.Status == StatusDone; otherwise it is a no-op.
//
// The returned error names the offending children and suggests remediation,
// so CLI and MCP callers (and LLM agents) can self-correct.
func ValidateStateTransition(store Store, t *Ticket) error {
	if t.Type != TypeEpic || t.Status != StatusDone {
		return nil
	}
	tickets, err := store.List()
	if err != nil {
		return err
	}
	var open []string
	for _, child := range tickets {
		if child.ID == t.ID {
			continue
		}
		if child.Parent != t.ID {
			continue
		}
		if !isTerminal(child) {
			open = append(open, fmt.Sprintf("%s [%s]", child.ID, child.Status))
		}
	}
	if len(open) == 0 {
		return nil
	}
	return fmt.Errorf(
		"cannot mark epic %s as done: %d child ticket(s) are not done or closed: %s. "+
			"Close or mark these children done first, or mark the epic closed to abandon it",
		t.ID, len(open), strings.Join(open, ", "),
	)
}

// PropagateStatusUp bumps a parent epic's status in response to a child's
// status change. Rules:
//
//   - child → ready: parent epic backlog → ready (no-op if parent already
//     ready, open, done, or closed).
//   - child → open: parent epic backlog or ready → open (no-op if parent
//     already open, done, or closed).
//   - child → done: if every child of the parent is now terminal (done or
//     closed) and the parent is not already terminal, parent → done.
//
// Changes cascade up nested epic chains via store.Update, which calls this
// function again on each write.
func PropagateStatusUp(store Store, child *Ticket) error {
	if child.Parent == "" {
		return nil
	}
	parent, err := store.Get(child.Parent)
	if err != nil {
		// Parent is missing or ambiguous — nothing to propagate.
		return nil
	}
	if parent.Type != TypeEpic {
		return nil
	}
	newStatus := computePropagatedStatus(store, parent, child)
	if newStatus == parent.Status {
		return nil
	}
	parent.Status = newStatus
	return store.Update(parent)
}

func computePropagatedStatus(store Store, parent, child *Ticket) Status {
	switch child.Status {
	case StatusReady:
		if parent.Status == StatusBacklog {
			return StatusReady
		}
	case StatusOpen:
		if parent.Status == StatusBacklog || parent.Status == StatusReady {
			return StatusOpen
		}
	case StatusDone:
		if parent.Status == StatusDone || parent.Status == StatusClosed {
			return parent.Status
		}
		if allChildrenTerminal(store, parent.ID) {
			return StatusDone
		}
	}
	return parent.Status
}

// allChildrenTerminal returns true if every ticket whose parent is parentID
// is in a terminal state (done or closed). Returns true when there are no
// children — a childless epic is trivially "all terminal."
func allChildrenTerminal(store Store, parentID string) bool {
	tickets, err := store.List()
	if err != nil {
		return false
	}
	hadChild := false
	for _, t := range tickets {
		if t.Parent != parentID {
			continue
		}
		hadChild = true
		if !isTerminal(t) {
			return false
		}
	}
	_ = hadChild
	return true
}

// sameTicketID returns true if two ticket IDs refer to the same ticket,
// tolerating namespace-prefix mismatches ("project/foo-abcd" matches
// "foo-abcd"). This matters because deps and links may have been stored
// in bare form before the namespacing rollout while Get()-resolved IDs
// come back namespaced; exact-string compare would miss those.
func sameTicketID(a, b string) bool {
	if a == b {
		return true
	}
	_, ab := ParseNamespacedID(a)
	_, bb := ParseNamespacedID(b)
	return ab == bb
}

// AddDep adds depID to the ticket's deps list. Returns error if it would
// create a self-dependency.
func AddDep(t *Ticket, depID string) error {
	if sameTicketID(t.ID, depID) {
		return fmt.Errorf("cannot depend on self")
	}
	for _, d := range t.Deps {
		if sameTicketID(d, depID) {
			return nil // already present (maybe in the other ID form)
		}
	}
	t.Deps = append(t.Deps, depID)
	return nil
}

// SetDepCargo records what concretely flows across the edge from t to depID.
// An empty cargo removes the annotation. Matching tolerates namespace-prefix
// mismatches, so annotating an already-stored dep overwrites its entry instead
// of adding a second one under the other ID form.
func SetDepCargo(t *Ticket, depID, cargo string) error {
	if err := ValidateCargoKey(depID); err != nil {
		return err
	}
	key := depID
	for k := range t.DepCargo {
		if sameTicketID(k, depID) {
			key = k
			break
		}
	}
	cargo = strings.TrimSpace(cargo)
	if cargo == "" {
		delete(t.DepCargo, key)
		return nil
	}
	if err := ValidateCargoValue(cargo); err != nil {
		return err
	}
	if t.DepCargo == nil {
		t.DepCargo = map[string]string{}
	}
	t.DepCargo[key] = cargo
	return nil
}

// CargoFor returns what flows across the edge from t to depID, or "" if the
// edge carries no annotation.
func CargoFor(t *Ticket, depID string) string {
	for k, v := range t.DepCargo {
		if sameTicketID(k, depID) {
			return v
		}
	}
	return ""
}

// RemoveDep removes depID from the ticket's deps list, along with any cargo
// recorded for that edge. Matching is tolerant of namespace-prefix mismatches
// between the stored dep ID and the caller's argument.
func RemoveDep(t *Ticket, depID string) {
	filtered := t.Deps[:0]
	for _, d := range t.Deps {
		if !sameTicketID(d, depID) {
			filtered = append(filtered, d)
		}
	}
	t.Deps = filtered
	for k := range t.DepCargo {
		if sameTicketID(k, depID) {
			delete(t.DepCargo, k)
		}
	}
}

// AddLink adds a symmetric link between two tickets.
func AddLink(a, b *Ticket) {
	if !containsID(a.Links, b.ID) {
		a.Links = append(a.Links, b.ID)
	}
	if !containsID(b.Links, a.ID) {
		b.Links = append(b.Links, a.ID)
	}
}

// RemoveLink removes a symmetric link between two tickets. Matching is
// tolerant of namespace-prefix mismatches.
func RemoveLink(a, b *Ticket) {
	a.Links = removeID(a.Links, b.ID)
	b.Links = removeID(b.Links, a.ID)
}

func containsID(ss []string, s string) bool {
	for _, v := range ss {
		if sameTicketID(v, s) {
			return true
		}
	}
	return false
}

func removeID(ss []string, s string) []string {
	filtered := ss[:0]
	for _, v := range ss {
		if !sameTicketID(v, s) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
