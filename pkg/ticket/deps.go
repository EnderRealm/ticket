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
	// The walk resolves every dep it reaches, and one naming an epic resolves
	// through Store.Get, which reads the whole store to derive it. One listing
	// answers them all; the store stays the fallback for the partial forms a
	// stored dep can carry.
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}
	depOf := depLookup(store, tickets)

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
			dep, err := depOf(depID)
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
	return isBlocked(t, store.Get)
}

// isBlocked is IsBlocked with the dep lookup supplied, so a caller looping over
// a list it already read can answer the deps from that list.
func isBlocked(t *Ticket, depOf func(string) (*Ticket, error)) bool {
	if len(t.Deps) == 0 {
		return false
	}
	for _, depID := range t.Deps {
		dep, err := depOf(depID)
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

// BlockedFunc answers IsBlocked against a set the caller has already read, for
// a loop that asks it per ticket. Resolving a dep that names an epic through
// the store derives that epic, which reads the whole store.
func BlockedFunc(store Store, tickets []*Ticket) func(*Ticket) bool {
	depOf := depLookup(store, tickets)
	return func(t *Ticket) bool {
		return isBlocked(t, depOf)
	}
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

// depLookup resolves a dep ID against a set the caller has already read. Store
// resolution is the fallback, not the path: a dep naming an epic resolves
// through Store.Get, which derives — reading the whole store — so a loop over
// the store would re-read it once per epic dep. Whether an epic dep is terminal
// genuinely depends on that derivation, so the answer has to come from a
// derived set rather than a stored read; the index is the derived set the
// caller is already holding.
func depLookup(store Store, tickets []*Ticket) func(string) (*Ticket, error) {
	index := ticketsByID(tickets, storeProject(store))
	return func(id string) (*Ticket, error) {
		if t, ok := index(id); ok {
			return t, nil
		}
		return store.Get(id)
	}
}

// ticketsByID indexes a set of tickets for lookup by an ID that may carry a
// namespace the set does not, or lack one the set has — the mismatch
// SameTicketID tolerates and a map key cannot. project is the namespace the
// set's own IDs live under when they do not carry one themselves, which is a
// per-project FileStore listing bare IDs against a store that records deps and
// parents namespaced.
//
// Matching follows FileStore.Resolve: a prefix naming this set's project is
// stripped, one naming another project matches nothing. A bare ID against a
// namespaced set matches only where one project holds it, the way MultiStore
// resolves a bare ID; anything else misses and is left to the caller's
// fallback, which is the store's own resolution.
//
// An ID two listed tickets both claim resolves to neither, the same way a bare
// half two of them share does.
func ticketsByID(tickets []*Ticket, project string) func(string) (*Ticket, bool) {
	byID := make(map[string]*Ticket, len(tickets))
	byBare := make(map[string]*Ticket, len(tickets))
	// One guard per key. A store holds whatever files arrived over its git
	// remote, and a stored prefix naming the project reads as its bare
	// remainder (readFile), so two files can claim one ID: `x.md` holding
	// `id: x` beside `zz.md` holding `id: proj/x`. Answered with whichever was
	// listed last, a reference to x would resolve by directory order — and an
	// impostor reading done beside a live ticket would clear a blocker without
	// a word. Both keys report not-found instead and leave the reference to the
	// caller's fallback, which resolves against the store itself.
	claimed := map[string]bool{}
	ambiguous := map[string]bool{}
	for _, t := range tickets {
		if _, seen := byID[t.ID]; seen {
			claimed[t.ID] = true
		}
		byID[t.ID] = t
		_, bare := ParseNamespacedID(t.ID)
		if _, seen := byBare[bare]; seen {
			ambiguous[bare] = true
			continue
		}
		byBare[bare] = t
	}
	return func(id string) (*Ticket, bool) {
		if claimed[id] {
			return nil, false
		}
		if t, ok := byID[id]; ok {
			return t, true
		}
		proj, bare := ParseNamespacedID(id)
		if ambiguous[bare] {
			return nil, false
		}
		t, ok := byBare[bare]
		if !ok {
			return nil, false
		}
		if proj == "" {
			return t, true
		}
		listed, _ := ParseNamespacedID(t.ID)
		if listed == "" {
			listed = project
		}
		if proj != listed {
			return nil, false
		}
		return t, true
	}
}

// IndexByID returns a lookup over the given listing. It matches an exact ID
// first, then the bare half of a namespaced one, and reports not-found for an
// ID two listed tickets both claim, for a bare half two of them share, and for
// a reference whose namespace names a project other than the store's — so an
// ambiguous or foreign reference stays unresolved rather than being answered
// with a guess. Callers outside this
// package need those rules stated here, since the function carrying them is
// unexported.
//
// It exists because a display site matching a ticket's deps and links by exact
// string misses every reference stored in the other ID form, and both forms are
// on disk. The store supplies the namespace its listed IDs live under, so no
// caller re-derives it.
func IndexByID(store Store, tickets []*Ticket) func(string) (*Ticket, bool) {
	return ticketsByID(tickets, storeProject(store))
}

// storeProject is the namespace a store's listed IDs belong to when they do not
// carry one themselves. A MultiStore namespaces every ID it lists, so it has
// none of its own.
func storeProject(store Store) string {
	if p, ok := store.(projectStore); ok {
		return p.projectName()
	}
	return ""
}

// IsReady returns true if the ticket is actionable: not terminal, all deps
// done, and parent chain is active. Backlog counts: tickets are picked up
// straight out of backlog on stores that never groom to ready, and excluding
// them left the ready listing empty. Callers that need the groomed set alone
// match Status == StatusReady, as FrontierTickets does.
func IsReady(store Store, t *Ticket) bool {
	return isReady(t, store.Get, func(child *Ticket) (*Ticket, error) {
		return store.Get(child.Parent)
	})
}

// isReady is IsReady with the dep and parent lookups supplied, so a caller
// looping over a list it already read can answer both from that list.
func isReady(t *Ticket, depOf func(string) (*Ticket, error), parentOf func(*Ticket) (*Ticket, error)) bool {
	if isTerminal(t) {
		return false
	}
	if isBlocked(t, depOf) {
		return false
	}
	return parentChainActive(parentOf, t)
}

// IsReadyOpen is like IsReady but bypasses parent gating.
// Shows all unblocked non-terminal tickets regardless of epic status.
func IsReadyOpen(store Store, t *Ticket) bool {
	return isReadyOpen(t, store.Get)
}

// isReadyOpen is IsReadyOpen with the dep lookup supplied.
func isReadyOpen(t *Ticket, depOf func(string) (*Ticket, error)) bool {
	if isTerminal(t) {
		return false
	}
	return !isBlocked(t, depOf)
}

// parentChainActive checks that every ancestor (via parent field) is
// not terminal. If a parent is not found in the store, it's treated as active.
func parentChainActive(parentOf func(*Ticket) (*Ticket, error), t *Ticket) bool {
	visited := map[string]bool{t.ID: true}
	cur := t
	for cur.Parent != "" {
		parent, err := parentOf(cur)
		if err != nil {
			return true // parent not in store — treat as active
		}
		if isTerminal(parent) {
			return false
		}
		if visited[parent.ID] {
			return true // avoid infinite loops
		}
		visited[parent.ID] = true
		cur = parent
	}
	return true
}

// ReadyTickets returns all tickets that pass the IsReady check, so backlog
// tickets whose deps are done and whose parent is active are included. Callers
// that distinguish groomed from ungroomed sort by status.
func ReadyTickets(store Store) ([]*Ticket, error) {
	return readyTicketsImpl(store, false)
}

// ReadyTicketsOpen returns all unblocked non-terminal tickets, bypassing
// parent gating.
func ReadyTicketsOpen(store Store) ([]*Ticket, error) {
	return readyTicketsImpl(store, true)
}

func readyTicketsImpl(store Store, openMode bool) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}
	// Dep and parent gating both want a derived status, and List has already
	// derived every ticket in the store. Resolving either through store.Get
	// instead would read the whole store again for every epic named.
	depOf := depLookup(store, tickets)
	parentOf := parentLookup(store, tickets, store.Get)

	var ready []*Ticket
	for _, t := range tickets {
		var r bool
		if openMode {
			r = isReadyOpen(t, depOf)
		} else {
			r = isReady(t, depOf, parentOf)
		}
		if r {
			ready = append(ready, t)
		}
	}
	return ready, nil
}

// FrontierTickets returns the schedulable set: tickets with status ready
// whose dependencies are all terminal (done/closed). Listed through Store.List,
// so a CLI caller gets the store's warning about any file it skipped.
func FrontierTickets(store Store) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}
	return frontierOf(store, tickets), nil
}

// FrontierTicketsWithSkips is FrontierTickets with the files the listing did
// not read reported alongside, for a caller that has to say the frontier was
// computed over a store read in part. It lists through SkipLister and never
// warns: the callers that need the skips are the ones whose stderr goes
// nowhere. A store that cannot report them answers with none.
func FrontierTicketsWithSkips(store Store) ([]*Ticket, []FileSkip, error) {
	var tickets []*Ticket
	var skips []FileSkip
	var err error
	if l, ok := store.(SkipLister); ok {
		tickets, skips, err = l.ListWithSkips()
	} else {
		tickets, err = store.List()
	}
	if err != nil {
		return nil, nil, err
	}
	return frontierOf(store, tickets), skips, nil
}

// frontierOf filters a listing down to the schedulable set. Shared by both
// entry points so the two cannot disagree about what the frontier is.
func frontierOf(store Store, tickets []*Ticket) []*Ticket {
	depOf := depLookup(store, tickets)
	var frontier []*Ticket
	for _, t := range tickets {
		if t.Status == StatusReady && !isBlocked(t, depOf) {
			frontier = append(frontier, t)
		}
	}
	return frontier
}

// BlockedTickets returns all non-terminal, non-backlog tickets with unresolved deps.
func BlockedTickets(store Store) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	depOf := depLookup(store, tickets)
	var blocked []*Ticket
	for _, t := range tickets {
		if isTerminal(t) || t.Status == StatusBacklog {
			continue
		}
		if isBlocked(t, depOf) {
			blocked = append(blocked, t)
		}
	}
	return blocked, nil
}

// SameTicketID returns true if two ticket IDs refer to the same ticket,
// tolerating namespace-prefix mismatches ("project/foo-abcd" matches
// "foo-abcd"). This matters because deps and links may have been stored
// in bare form before the namespacing rollout while Get()-resolved IDs
// come back namespaced; exact-string compare would miss those.
func SameTicketID(a, b string) bool {
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
	if SameTicketID(t.ID, depID) {
		return fmt.Errorf("cannot depend on self")
	}
	for _, d := range t.Deps {
		if SameTicketID(d, depID) {
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
		if SameTicketID(k, depID) {
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
		if SameTicketID(k, depID) {
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
		if !SameTicketID(d, depID) {
			filtered = append(filtered, d)
		}
	}
	t.Deps = filtered
	for k := range t.DepCargo {
		if SameTicketID(k, depID) {
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
		if SameTicketID(v, s) {
			return true
		}
	}
	return false
}

func removeID(ss []string, s string) []string {
	filtered := ss[:0]
	for _, v := range ss {
		if !SameTicketID(v, s) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
