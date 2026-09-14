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
			dep, err := depOf(t, depID)
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

	// Index only non-terminal tickets, keyed the way depLookup keys them: by
	// qualified ID, with each dep walked relative to the ticket holding it. A
	// project store lists bare IDs while every dep written through the
	// boundary is stored qualified, and the MultiStore lists qualified IDs
	// over legacy bare deps; keyed by the ID as listed, either edge misses
	// and the cycle it closes goes unreported. The cycle is reported in the
	// IDs the listing uses.
	project := storeProject(store)
	byID := map[string]*Ticket{}
	for _, t := range tickets {
		if t.Status != StatusDone && t.Status != StatusClosed {
			byID[qualifyRef(project, t.ID)] = t
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
				cycle = append([]string{byID[path[i]].ID}, cycle...)
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
			dfs(qualifyRef(ownerNS(store, t), depID))
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
	return isBlocked(t, storeLookup(store))
}

// depResolver resolves a reference the owner ticket holds — a dep, a parent —
// to the ticket it names. The owner is what the reference is relative to: a
// bare reference names the owner's own namespace.
type depResolver func(owner *Ticket, ref string) (*Ticket, error)

// storeLookup resolves owner-relative references through the store, one read
// each, for the single-ticket entry points.
func storeLookup(store Store) depResolver {
	return func(owner *Ticket, ref string) (*Ticket, error) {
		return resolveRef(store, qualifyRef(ownerNS(store, owner), ref))
	}
}

// resolveRef reads the ticket a qualified reference names, and only that
// ticket. It is the one store read every reference lookup falls back to, so
// the two rules below cannot drift apart between the single-ticket and the
// listing entry points.
//
// A reference into another namespace is read through the boundary's store
// for that namespace rather than through this one: FileStore.Resolve refuses
// a prefix that is not its own project, so from a project store — the CLI's
// default — an acceptance leaf's dep on another project, or its parent once
// cross-project parents are activated, would never resolve and the leaf would
// read blocked here while the MultiStore reads it ready. A leaf costs one
// read; an epic derives off a snapshot, as Get always does. The ticket comes
// back qualified, the way the MultiStore hands it back, so a reference it
// holds is read relative to its own namespace and not this store's.
//
// The ticket read back has to be the one asked for. Resolve substring-matches
// a fragment, so a missing `warp/task-0001` would otherwise read as done off
// `warp/other-task-0001`, and a blocker that no longer exists would clear a
// dependent — the snapshot and the cycle check treat the two as different
// identities, and so does this. And it has to be the only file claiming that
// ID: a single-file Get cannot see a second claimant in the namespace, so the
// identity is checked through claimedStored first, the way the snapshot's
// index refuses a claimed-twice ID — a foreign dep falling through from a
// project listing would otherwise read done off `warp/x.md` while
// `warp/zz.md` holds the same ID open.
func resolveRef(store Store, id string) (*Ticket, error) {
	project := storeProject(store)
	ns, bare := ParseNamespacedID(id)
	p, central := store.(centralProvider)
	if central {
		if _, err := p.central().claimedStored(id); err != nil {
			return nil, err
		}
	}
	var t *Ticket
	if central && ns != "" && ns != project {
		s, err := p.central().store(ns)
		if err != nil {
			return nil, err
		}
		if t, err = s.Get(bare); err != nil {
			return nil, err
		}
		t.ID = FormatNamespacedID(ns, t.ID)
	} else {
		var err error
		if t, err = store.Get(id); err != nil {
			return nil, err
		}
	}
	if qualifyRef(project, t.ID) != id {
		return nil, fmt.Errorf("ticket %s not found", id)
	}
	return t, nil
}

// ownerNS is the namespace a ticket's bare references are relative to: the
// one its own ID carries, or the store's when the ID is bare.
func ownerNS(store Store, t *Ticket) string {
	if ns := namespaceOf(t.ID); ns != "" {
		return ns
	}
	return storeProject(store)
}

// isBlocked is IsBlocked with the dep lookup supplied, so a caller looping over
// a list it already read can answer the deps from that list.
func isBlocked(t *Ticket, depOf depResolver) bool {
	if len(t.Deps) == 0 {
		return false
	}
	for _, depID := range t.Deps {
		dep, err := depOf(t, depID)
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

// BlockingDeps returns the IDs of dependencies that are not done/closed, as
// the ticket spells them.
func BlockingDeps(store Store, t *Ticket) []string {
	depOf := storeLookup(store)
	var blocking []string
	for _, depID := range t.Deps {
		dep, err := depOf(t, depID)
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

// depLookup resolves a reference against a set the caller has already read.
// Store resolution is the fallback, not the path: a dep naming an epic
// resolves through Store.Get, which derives — reading the whole store — so a
// loop over the store would re-read it once per epic dep. Whether an epic dep
// is terminal genuinely depends on that derivation, so the answer has to come
// from a derived set rather than a stored read; the index is the derived set
// the caller is already holding.
//
// The index is keyed by qualified ID and a reference is qualified relative to
// the ticket holding it before it is looked up, so a bare dep names the
// owner's own namespace and nothing else: an ID two listed tickets share
// across projects resolves to the one in the owner's project, never by luck.
// The fallback reads the qualified form too, so no path searches every
// project for a bare ID. An ID two listed tickets both claim within one
// namespace resolves to neither.
func depLookup(store Store, tickets []*Ticket) depResolver {
	project := storeProject(store)
	byID := make(map[string]*Ticket, len(tickets))
	claimed := map[string]bool{}
	for _, t := range tickets {
		id := qualifyRef(project, t.ID)
		if _, seen := byID[id]; seen {
			claimed[id] = true
		}
		byID[id] = t
	}
	return func(owner *Ticket, ref string) (*Ticket, error) {
		id := qualifyRef(ownerNS(store, owner), ref)
		if claimed[id] {
			return nil, fmt.Errorf("ticket %s is claimed by more than one file", id)
		}
		if t, ok := byID[id]; ok {
			return t, nil
		}
		return resolveRef(store, id)
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
// Display lookup only: it is what `tk show` and the TUI match a reference
// against a listing with. Nothing that decides graph membership — children,
// readiness, blocking, the audit — uses it, since those resolve a reference
// relative to its owner (qualifyRef) and never by bare-suffix equality.
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
// done, parent chain active, and its parent relationship valid. Backlog
// counts: tickets are picked up straight out of backlog on stores that never
// groom to ready, and excluding them left the ready listing empty. Callers
// that need the groomed set alone match Status == StatusReady, as
// FrontierTickets does.
func IsReady(store Store, t *Ticket) bool {
	depOf := storeLookup(store)
	return isReady(t, depOf, parentOfVia(depOf))
}

// parentOfVia resolves a child's parent through the same owner-relative lookup
// its deps resolve through: the parent field is one more reference the child
// holds.
func parentOfVia(depOf depResolver) func(*Ticket) (*Ticket, error) {
	return func(child *Ticket) (*Ticket, error) {
		return depOf(child, child.Parent)
	}
}

// isReady is IsReady with the dep and parent lookups supplied, so a caller
// looping over a list it already read can answer both from that list. A
// ticket carrying a relationship issue is never ready: its parent does not
// resolve, is not an epic, or is one it may not belong to, and none of those
// is evidence that nothing gates the work.
func isReady(t *Ticket, depOf depResolver, parentOf func(*Ticket) (*Ticket, error)) bool {
	if isTerminal(t) || t.relationshipIssue != "" {
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
	return isReadyOpen(t, storeLookup(store))
}

// isReadyOpen is IsReadyOpen with the dep lookup supplied. The parent gate is
// bypassed; a relationship issue is not a gate, it is an invalid leaf.
func isReadyOpen(t *Ticket, depOf depResolver) bool {
	if isTerminal(t) || t.relationshipIssue != "" {
		return false
	}
	return !isBlocked(t, depOf)
}

// parentChainActive checks that every ancestor (via parent field) is not
// terminal. A parent that cannot be resolved is not treated as active: an
// unresolved relationship says nothing about whether the work is gated.
func parentChainActive(parentOf func(*Ticket) (*Ticket, error), t *Ticket) bool {
	visited := map[string]bool{t.ID: true}
	cur := t
	for cur.Parent != "" {
		parent, err := parentOf(cur)
		if err != nil {
			return false
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
	parentOf := parentOfVia(depOf)

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

// FrontierOf is the frontier over a listing the caller already holds — a
// snapshot's tickets — for a consumer that reports the snapshot's revision and
// completeness beside the frontier and has to compute all of them off one
// reading of the store. The rule is frontierOf's; nothing outside restates it.
func FrontierOf(store Store, tickets []*Ticket) []*Ticket {
	return frontierOf(store, tickets)
}

// frontierOf filters a listing down to the schedulable set. Shared by both
// entry points so the two cannot disagree about what the frontier is. A leaf
// whose parent relationship is invalid is left out: it is not automatically
// runnable, and RelationshipIssue says why.
func frontierOf(store Store, tickets []*Ticket) []*Ticket {
	depOf := depLookup(store, tickets)
	var frontier []*Ticket
	for _, t := range tickets {
		if t.Status == StatusReady && t.relationshipIssue == "" && !isBlocked(t, depOf) {
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
//
// Display lookup only, like ticketsByID: two qualified IDs that differ only
// in namespace are two tickets, and this reads them as one. The helpers that
// edit a ticket's references compare with sameRef.
func SameTicketID(a, b string) bool {
	if a == b {
		return true
	}
	_, ab := ParseNamespacedID(a)
	_, bb := ParseNamespacedID(b)
	return ab == bb
}

// sameRef reports whether two references the owner holds name one ticket, by
// the reading rule: each is qualified relative to the owner's namespace and
// the two are equal only as strings after that — the central store holds
// identical bare IDs in different projects, and `warp/epic-1` beside
// `loom/epic-1` is two edges, not one. A bare side names the owner's own
// project, so a bare `epic-1` typed in warp is warp's edge and never loom's,
// and a legacy bare reference stored before qualification matches the
// qualified argument in that project alone. An owner whose namespace is
// unknown — a ticket built rather than read, or one from a store with no
// project — has nothing to qualify against, and there bare equality is the
// fallback for a side that carries no namespace.
func sameRef(ownerNS, a, b string) bool {
	if ownerNS != "" {
		return qualifyRef(ownerNS, a) == qualifyRef(ownerNS, b)
	}
	pa, ba := ParseNamespacedID(a)
	pb, bb := ParseNamespacedID(b)
	if pa != "" && pb != "" {
		return a == b
	}
	return ba == bb
}

// editNS is the namespace a ticket's references are read relative to by the
// helpers that edit them: the one its ID carries, or the project it was read
// from when the ID is bare. ownerNS answers the same question with the store
// in hand; the edit helpers have only the ticket.
func editNS(t *Ticket) string {
	if ns := namespaceOf(t.ID); ns != "" {
		return ns
	}
	return t.namespace
}

// AddDep adds depID to the ticket's deps list. Returns error if it would
// create a self-dependency.
func AddDep(t *Ticket, depID string) error {
	ns := editNS(t)
	if sameRef(ns, t.ID, depID) {
		return fmt.Errorf("cannot depend on self")
	}
	for _, d := range t.Deps {
		if sameRef(ns, d, depID) {
			return nil // already present (maybe in the other ID form)
		}
	}
	t.Deps = append(t.Deps, depID)
	return nil
}

// SetDepCargo records what concretely flows across the edge from t to depID.
// An empty cargo removes the annotation. Matching tolerates a bare side
// (sameRef), so annotating an already-stored dep overwrites its entry instead
// of adding a second one under the other ID form.
func SetDepCargo(t *Ticket, depID, cargo string) error {
	if err := ValidateCargoKey(depID); err != nil {
		return err
	}
	key := depID
	for k := range t.DepCargo {
		if sameRef(editNS(t), k, depID) {
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
		if sameRef(editNS(t), k, depID) {
			return v
		}
	}
	return ""
}

// RemoveDep removes depID from the ticket's deps list, along with any cargo
// recorded for that edge. Matching tolerates a bare side (sameRef) between
// the stored dep ID and the caller's argument.
func RemoveDep(t *Ticket, depID string) {
	ns := editNS(t)
	filtered := t.Deps[:0]
	for _, d := range t.Deps {
		if !sameRef(ns, d, depID) {
			filtered = append(filtered, d)
		}
	}
	t.Deps = filtered
	for k := range t.DepCargo {
		if sameRef(ns, k, depID) {
			delete(t.DepCargo, k)
		}
	}
}

// AddLink adds a symmetric link between two tickets. Each side's links are
// read relative to that side: the far ticket's ID is the argument, and a bare
// one names the near ticket's own project.
func AddLink(a, b *Ticket) {
	if !containsID(editNS(a), a.Links, b.ID) {
		a.Links = append(a.Links, b.ID)
	}
	if !containsID(editNS(b), b.Links, a.ID) {
		b.Links = append(b.Links, a.ID)
	}
}

// RemoveLink removes a symmetric link between two tickets. Matching
// tolerates a bare side (sameRef).
func RemoveLink(a, b *Ticket) {
	a.Links = removeID(editNS(a), a.Links, b.ID)
	b.Links = removeID(editNS(b), b.Links, a.ID)
}

func containsID(ownerNS string, ss []string, s string) bool {
	for _, v := range ss {
		if sameRef(ownerNS, v, s) {
			return true
		}
	}
	return false
}

func removeID(ownerNS string, ss []string, s string) []string {
	filtered := ss[:0]
	for _, v := range ss {
		if !sameRef(ownerNS, v, s) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
