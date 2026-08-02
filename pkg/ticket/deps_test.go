package ticket

import (
	"testing"
	"time"
)

func depStore(t *testing.T, tickets ...*Ticket) *FileStore {
	t.Helper()
	store := NewFileStore(t.TempDir())
	for _, tk := range tickets {
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	return store
}

func mk(id string, status Status, deps ...string) *Ticket {
	if deps == nil {
		deps = []string{}
	}
	return &Ticket{
		ID:       id,
		Status:   status,
		Type:     TypeFeature,
		Priority: 2,
		Deps:     deps,
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Ticket " + id,
		Body:     "\n",
	}
}

func mkWithParent(id string, status Status, parent string, deps ...string) *Ticket {
	t := mk(id, status, deps...)
	t.Parent = parent
	return t
}

func TestIsBlocked_NoDeps(t *testing.T) {
	s := depStore(t, mk("t-1", StatusReady))
	tk, _ := s.Get("t-1")
	if IsBlocked(s, tk) {
		t.Error("ticket with no deps should not be blocked")
	}
}

func TestIsBlocked_AllDone(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-dep"),
		mk("t-dep", StatusDone),
	)
	tk, _ := s.Get("t-1")
	if IsBlocked(s, tk) {
		t.Error("ticket with all deps done should not be blocked")
	}
}

func TestIsBlocked_OpenDep(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-dep"),
		mk("t-dep", StatusReady),
	)
	tk, _ := s.Get("t-1")
	if !IsBlocked(s, tk) {
		t.Error("ticket with non-done dep should be blocked")
	}
}

func TestIsBlocked_MissingDep(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-gone"),
	)
	tk, _ := s.Get("t-1")
	if !IsBlocked(s, tk) {
		t.Error("ticket with missing dep should be blocked")
	}
}

func TestBlockingDeps(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-a", "t-b", "t-c"),
		mk("t-a", StatusDone),
		mk("t-b", StatusReady),
		mk("t-c", StatusOpen),
	)
	tk, _ := s.Get("t-1")
	blocking := BlockingDeps(s, tk)
	if len(blocking) != 2 {
		t.Errorf("len(blocking) = %d, want 2", len(blocking))
	}
}

func TestIsReady_Simple(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-dep"),
		mk("t-dep", StatusDone),
	)
	tk, _ := s.Get("t-1")
	if !IsReady(s, tk) {
		t.Error("ticket with all deps done should be ready")
	}
}

func TestIsReady_ParentGating(t *testing.T) {
	// Parent epic is done → child not ready (parent done = work complete).
	epic := mk("t-epic", StatusDone)
	epic.Type = TypeEpic
	child := mkWithParent("t-child", StatusReady, "t-epic")

	s := depStore(t, epic, child)
	tk, _ := s.Get("t-child")
	if IsReady(s, tk) {
		t.Error("child of done epic should not be ready")
	}
}

func TestIsReady_ParentActive(t *testing.T) {
	epic := mk("t-epic", StatusOpen)
	epic.Type = TypeEpic
	child := mkWithParent("t-child", StatusReady, "t-epic")

	s := depStore(t, epic, child)
	tk, _ := s.Get("t-child")
	if !IsReady(s, tk) {
		t.Error("child of active epic should be ready")
	}
}

func TestIsReadyOpen_BypassesParentGate(t *testing.T) {
	epic := mk("t-epic", StatusDone)
	epic.Type = TypeEpic
	child := mkWithParent("t-child", StatusReady, "t-epic")

	s := depStore(t, epic, child)
	tk, _ := s.Get("t-child")
	if !IsReadyOpen(s, tk) {
		t.Error("IsReadyOpen should bypass parent gating")
	}
}

func TestIsReady_DoneNotReady(t *testing.T) {
	s := depStore(t, mk("t-1", StatusDone))
	tk, _ := s.Get("t-1")
	if IsReady(s, tk) {
		t.Error("done ticket should not be ready")
	}
}

func TestIsReady_BacklogNotReady(t *testing.T) {
	s := depStore(t, mk("t-1", StatusBacklog))
	tk, _ := s.Get("t-1")
	if IsReady(s, tk) {
		t.Error("backlog ticket should not be ready")
	}
	if IsReadyOpen(s, tk) {
		t.Error("backlog ticket should not be ready (open mode)")
	}
}

func TestReadyTickets(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady),
		mk("t-2", StatusReady, "t-3"),
		mk("t-3", StatusReady),
	)
	ready, err := ReadyTickets(s)
	if err != nil {
		t.Fatalf("ReadyTickets: %v", err)
	}
	// t-1 and t-3 are ready; t-2 is blocked by t-3.
	if len(ready) != 2 {
		t.Errorf("len(ready) = %d, want 2", len(ready))
	}
}

func TestFrontierTickets(t *testing.T) {
	closed := mk("t-closed", StatusClosed)
	s := depStore(t,
		mk("t-nodeps", StatusReady),
		mk("t-donedep", StatusReady, "t-done"),
		mk("t-closeddep", StatusReady, "t-closed"),
		mk("t-opendep", StatusReady, "t-open"),
		mk("t-readydep", StatusReady, "t-nodeps"),
		mk("t-missingdep", StatusReady, "t-gone"),
		mk("t-open", StatusOpen),
		mk("t-backlog", StatusBacklog),
		mk("t-done", StatusDone),
		closed,
	)

	frontier, err := FrontierTickets(s)
	if err != nil {
		t.Fatalf("FrontierTickets: %v", err)
	}

	got := map[string]bool{}
	for _, tk := range frontier {
		got[tk.ID] = true
	}
	want := []string{"t-nodeps", "t-donedep", "t-closeddep"}
	for _, id := range want {
		if !got[id] {
			t.Errorf("frontier missing %s: %v", id, ids2(frontier))
		}
	}
	if len(frontier) != len(want) {
		t.Errorf("frontier = %v, want exactly %v", ids2(frontier), want)
	}
}

func TestFrontierTickets_DepFlipsToDone(t *testing.T) {
	s := depStore(t,
		mk("t-a", StatusReady, "t-b"),
		mk("t-b", StatusOpen),
	)

	frontier, err := FrontierTickets(s)
	if err != nil {
		t.Fatalf("FrontierTickets: %v", err)
	}
	if len(frontier) != 0 {
		t.Fatalf("frontier = %v, want empty while t-b is open", ids2(frontier))
	}

	b, _ := s.Get("t-b")
	b.Status = StatusDone
	if err := s.Update(b); err != nil {
		t.Fatalf("Update t-b: %v", err)
	}

	frontier, err = FrontierTickets(s)
	if err != nil {
		t.Fatalf("FrontierTickets: %v", err)
	}
	if len(frontier) != 1 || frontier[0].ID != "t-a" {
		t.Errorf("frontier = %v, want [t-a] after dep flipped to done", ids2(frontier))
	}
}

func TestBlockedTickets(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady),
		mk("t-2", StatusReady, "t-3"),
		mk("t-3", StatusReady),
	)
	blocked, err := BlockedTickets(s)
	if err != nil {
		t.Fatalf("BlockedTickets: %v", err)
	}
	if len(blocked) != 1 || blocked[0].ID != "t-2" {
		t.Errorf("blocked = %v, want [t-2]", ids2(blocked))
	}
}

func TestFindCycles_NoCycles(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-2"),
		mk("t-2", StatusReady, "t-3"),
		mk("t-3", StatusReady),
	)
	cycles, err := FindCycles(s)
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %d", len(cycles))
	}
}

func TestFindCycles_SimpleCycle(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-2"),
		mk("t-2", StatusReady, "t-1"),
	)
	cycles, err := FindCycles(s)
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	if len(cycles[0].IDs) != 2 {
		t.Errorf("cycle length = %d, want 2", len(cycles[0].IDs))
	}
}

func TestFindCycles_IgnoresDone(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusDone, "t-2"),
		mk("t-2", StatusDone, "t-1"),
	)
	cycles, err := FindCycles(s)
	if err != nil {
		t.Fatalf("FindCycles: %v", err)
	}
	if len(cycles) != 0 {
		t.Error("done tickets should not generate cycles")
	}
}

func TestDepTree(t *testing.T) {
	s := depStore(t,
		mk("t-1", StatusReady, "t-2", "t-3"),
		mk("t-2", StatusReady, "t-3"),
		mk("t-3", StatusDone),
	)
	nodes, err := DepTree(s, "t-1", false)
	if err != nil {
		t.Fatalf("DepTree: %v", err)
	}
	// t-1 at depth 0, t-2 at depth 1, t-3 at depth 2, then t-3 skipped for t-1's second dep
	if len(nodes) != 3 {
		t.Errorf("deduped tree: len = %d, want 3; nodes: %v", len(nodes), depNodeIDs(nodes))
	}

	// Full mode shows all.
	nodesFull, err := DepTree(s, "t-1", true)
	if err != nil {
		t.Fatalf("DepTree full: %v", err)
	}
	if len(nodesFull) != 4 {
		t.Errorf("full tree: len = %d, want 4; nodes: %v", len(nodesFull), depNodeIDs(nodesFull))
	}
}

func TestAddDep(t *testing.T) {
	tk := mk("t-1", StatusReady)
	if err := AddDep(tk, "t-2"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}
	if len(tk.Deps) != 1 || tk.Deps[0] != "t-2" {
		t.Errorf("Deps = %v, want [t-2]", tk.Deps)
	}
	// Duplicate add is idempotent.
	if err := AddDep(tk, "t-2"); err != nil {
		t.Fatalf("AddDep duplicate: %v", err)
	}
	if len(tk.Deps) != 1 {
		t.Errorf("duplicate add: len = %d, want 1", len(tk.Deps))
	}
}

func TestAddDep_Self(t *testing.T) {
	tk := mk("t-1", StatusReady)
	if err := AddDep(tk, "t-1"); err == nil {
		t.Error("self-dep should fail")
	}
}

func TestRemoveDep(t *testing.T) {
	tk := mk("t-1", StatusReady, "t-2", "t-3")
	RemoveDep(tk, "t-2")
	if len(tk.Deps) != 1 || tk.Deps[0] != "t-3" {
		t.Errorf("Deps = %v, want [t-3]", tk.Deps)
	}
}

func TestRemoveDep_NamespacedStoredBareRequested(t *testing.T) {
	tk := mk("t-1", StatusReady, "ticket/foo-abcd")
	RemoveDep(tk, "foo-abcd")
	if len(tk.Deps) != 0 {
		t.Errorf("RemoveDep should match across namespace forms: deps=%v", tk.Deps)
	}
}

func TestRemoveDep_BareStoredNamespacedRequested(t *testing.T) {
	tk := mk("t-1", StatusReady, "foo-abcd")
	RemoveDep(tk, "ticket/foo-abcd")
	if len(tk.Deps) != 0 {
		t.Errorf("RemoveDep should match across namespace forms: deps=%v", tk.Deps)
	}
}

func TestAddDep_DedupAcrossNamespace(t *testing.T) {
	tk := mk("t-1", StatusReady, "foo-abcd")
	if err := AddDep(tk, "ticket/foo-abcd"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}
	if len(tk.Deps) != 1 {
		t.Errorf("AddDep should dedup across namespace forms: deps=%v", tk.Deps)
	}
}

func TestAddDep_SelfAcrossNamespace(t *testing.T) {
	tk := mk("ticket/foo-abcd", StatusReady)
	if err := AddDep(tk, "foo-abcd"); err == nil {
		t.Error("AddDep should reject self-dep across namespace forms")
	}
}

func TestSetDepCargo(t *testing.T) {
	tk := mk("t-1", StatusReady, "t-2")
	if err := SetDepCargo(tk, "t-2", "  event schema for the ingest table  "); err != nil {
		t.Fatalf("SetDepCargo: %v", err)
	}
	if got := CargoFor(tk, "t-2"); got != "event schema for the ingest table" {
		t.Errorf("CargoFor = %q, want trimmed cargo", got)
	}
	// Empty cargo deletes rather than storing "".
	if err := SetDepCargo(tk, "t-2", "  "); err != nil {
		t.Fatalf("SetDepCargo empty: %v", err)
	}
	if _, ok := tk.DepCargo["t-2"]; ok {
		t.Errorf("DepCargo = %v, want entry deleted", tk.DepCargo)
	}
}

func TestSetDepCargo_RejectsInvalid(t *testing.T) {
	tk := mk("t-1", StatusReady, "t-2")
	if err := SetDepCargo(tk, "t-2", "two\nlines"); err == nil {
		t.Error("newline in cargo should fail")
	}
	if err := SetDepCargo(tk, "has space", "schema"); err == nil {
		t.Error("whitespace in dep ID should fail")
	}
}

func TestDepCargo_AcrossNamespace(t *testing.T) {
	tk := mk("t-1", StatusReady, "ticket/foo-abcd")
	if err := SetDepCargo(tk, "ticket/foo-abcd", "event schema"); err != nil {
		t.Fatalf("SetDepCargo: %v", err)
	}
	if got := CargoFor(tk, "foo-abcd"); got != "event schema" {
		t.Errorf("CargoFor(bare) = %q, want event schema", got)
	}
	// Setting via the other ID form overwrites instead of duplicating.
	if err := SetDepCargo(tk, "foo-abcd", "migration doc"); err != nil {
		t.Fatalf("SetDepCargo bare: %v", err)
	}
	if len(tk.DepCargo) != 1 {
		t.Fatalf("DepCargo = %v, want a single entry", tk.DepCargo)
	}
	if got := CargoFor(tk, "ticket/foo-abcd"); got != "migration doc" {
		t.Errorf("CargoFor(namespaced) = %q, want migration doc", got)
	}
}

func TestRemoveDep_ClearsCargo(t *testing.T) {
	tk := mk("t-1", StatusReady, "ticket/foo-abcd", "t-3")
	if err := SetDepCargo(tk, "ticket/foo-abcd", "event schema"); err != nil {
		t.Fatalf("SetDepCargo: %v", err)
	}
	if err := SetDepCargo(tk, "t-3", "migration doc"); err != nil {
		t.Fatalf("SetDepCargo: %v", err)
	}
	RemoveDep(tk, "foo-abcd")
	if got := CargoFor(tk, "foo-abcd"); got != "" {
		t.Errorf("CargoFor after RemoveDep = %q, want empty", got)
	}
	if got := CargoFor(tk, "t-3"); got != "migration doc" {
		t.Errorf("CargoFor(t-3) = %q, want migration doc", got)
	}
}

func TestDepTreeCargo(t *testing.T) {
	root := mk("t-1", StatusReady, "t-2", "t-3")
	root.DepCargo = map[string]string{"t-2": "event schema"}
	s := depStore(t, root, mk("t-2", StatusReady), mk("t-3", StatusReady))

	nodes, err := DepTree(s, "t-1", false)
	if err != nil {
		t.Fatalf("DepTree: %v", err)
	}
	want := map[string]string{"t-1": "", "t-2": "event schema", "t-3": ""}
	if len(nodes) != len(want) {
		t.Fatalf("nodes = %v, want %d", depNodeIDs(nodes), len(want))
	}
	for _, n := range nodes {
		if n.Cargo != want[n.ID] {
			t.Errorf("%s cargo = %q, want %q", n.ID, n.Cargo, want[n.ID])
		}
	}
}

func TestRemoveLink_AcrossNamespace(t *testing.T) {
	a := mk("t-1", StatusReady)
	b := mk("t-2", StatusReady)
	// Simulate a pre-namespacing stored link.
	a.Links = []string{"ticket/t-2"}
	b.Links = []string{"t-1"}
	// Both sides use the other's current ID, which differs in namespace form.
	RemoveLink(a, b)
	if len(a.Links) != 0 || len(b.Links) != 0 {
		t.Errorf("links should be empty after remove: a=%v b=%v", a.Links, b.Links)
	}
}

func TestAddRemoveLink(t *testing.T) {
	a := mk("t-1", StatusReady)
	b := mk("t-2", StatusReady)
	AddLink(a, b)
	if len(a.Links) != 1 || a.Links[0] != "t-2" {
		t.Errorf("a.Links = %v, want [t-2]", a.Links)
	}
	if len(b.Links) != 1 || b.Links[0] != "t-1" {
		t.Errorf("b.Links = %v, want [t-1]", b.Links)
	}

	RemoveLink(a, b)
	if len(a.Links) != 0 || len(b.Links) != 0 {
		t.Error("links should be empty after remove")
	}
}

func ids2(tickets []*Ticket) []string {
	out := make([]string, len(tickets))
	for i, t := range tickets {
		out[i] = t.ID
	}
	return out
}

func depNodeIDs(nodes []DepNode) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}
