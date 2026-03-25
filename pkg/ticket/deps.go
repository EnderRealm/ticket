package ticket

import "fmt"

// DepNode represents one entry in a dependency tree.
type DepNode struct {
	ID    string
	Title string
	Stage Stage
	Depth int
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
	var walk func(t *Ticket, depth int)
	walk = func(t *Ticket, depth int) {
		nodes = append(nodes, DepNode{
			ID:    t.ID,
			Title: t.Title,
			Stage: t.Stage,
			Depth: depth,
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
				})
				continue
			}
			walk(dep, depth+1)
		}
	}
	walk(root, 0)
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

// FindCycles detects dependency cycles among open (non-closed) tickets.
// Uses DFS with white(0)/gray(1)/black(2) coloring.
func FindCycles(store Store) ([]Cycle, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	// Index only non-done tickets.
	byID := map[string]*Ticket{}
	for _, t := range tickets {
		if t.Stage != StageDone {
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

// IsBlocked returns true if any of the ticket's dependencies are not closed.
// Only meaningful for open/in_progress tickets.
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
		if dep.Stage != StageDone {
			return true
		}
	}
	return false
}

// BlockingDeps returns the IDs of dependencies that are not done.
func BlockingDeps(store Store, t *Ticket) []string {
	var blocking []string
	for _, depID := range t.Deps {
		dep, err := store.Get(depID)
		if err != nil {
			blocking = append(blocking, depID)
			continue
		}
		if dep.Stage != StageDone {
			blocking = append(blocking, depID)
		}
	}
	return blocking
}

// IsReady returns true if the ticket is actionable: not done,
// all deps done, and parent chain is active (all ancestors not done).
func IsReady(store Store, t *Ticket) bool {
	if t.Stage == StageDone || t.Stage == "" || t.Stage == StageBacklog {
		return false
	}
	if IsBlocked(store, t) {
		return false
	}
	return parentChainActive(store, t.ID, map[string]bool{})
}

// IsReadyOpen is like IsReady but bypasses parent gating.
// Shows all unblocked non-done tickets regardless of epic status.
func IsReadyOpen(store Store, t *Ticket) bool {
	if t.Stage == StageDone || t.Stage == "" || t.Stage == StageBacklog {
		return false
	}
	return !IsBlocked(store, t)
}

// parentChainActive checks that every ancestor (via parent field) is
// in_progress. If a parent is not found in the store, it's treated as active.
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
	if parent.Stage == StageDone {
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

// BlockedTickets returns all open/in_progress tickets with unresolved deps.
func BlockedTickets(store Store) ([]*Ticket, error) {
	tickets, err := store.List()
	if err != nil {
		return nil, err
	}

	var blocked []*Ticket
	for _, t := range tickets {
		if t.Stage == StageDone || t.Stage == "" || t.Stage == StageBacklog {
			continue
		}
		if IsBlocked(store, t) {
			blocked = append(blocked, t)
		}
	}
	return blocked, nil
}

// AddDep adds depID to the ticket's deps list. Returns error if it would
// create a self-dependency.
func AddDep(t *Ticket, depID string) error {
	if t.ID == depID {
		return fmt.Errorf("cannot depend on self")
	}
	for _, d := range t.Deps {
		if d == depID {
			return nil // already present
		}
	}
	t.Deps = append(t.Deps, depID)
	return nil
}

// RemoveDep removes depID from the ticket's deps list.
func RemoveDep(t *Ticket, depID string) {
	filtered := t.Deps[:0]
	for _, d := range t.Deps {
		if d != depID {
			filtered = append(filtered, d)
		}
	}
	t.Deps = filtered
}

// AddLink adds a symmetric link between two tickets.
func AddLink(a, b *Ticket) {
	if !contains(a.Links, b.ID) {
		a.Links = append(a.Links, b.ID)
	}
	if !contains(b.Links, a.ID) {
		b.Links = append(b.Links, a.ID)
	}
}

// RemoveLink removes a symmetric link between two tickets.
func RemoveLink(a, b *Ticket) {
	a.Links = removeStr(a.Links, b.ID)
	b.Links = removeStr(b.Links, a.ID)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	filtered := ss[:0]
	for _, v := range ss {
		if v != s {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
