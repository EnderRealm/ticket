package ticket

import (
	"strings"
	"testing"
)

func mkEpic(id string, status Status, parent string) *Ticket {
	t := mk(id, status)
	t.Type = TypeEpic
	t.Parent = parent
	return t
}

// multiDepStore is depStore's MultiStore twin: it creates the given tickets
// (namespaced IDs) across the named projects.
func multiDepStore(t *testing.T, projects []string, tickets ...*Ticket) *MultiStore {
	t.Helper()
	ms, _ := testMultiStore(t, projects...)
	for _, tk := range tickets {
		if err := ms.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
	return ms
}

// setStatus reads a ticket, moves it to status, and writes it back.
func setStatus(t *testing.T, s Store, id string, status Status) error {
	t.Helper()
	tk, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	tk.Status = status
	return s.Update(tk)
}

// ─── ValidateStateTransition ────────────────────────────────────────────────

func TestValidate_EpicDoneWithOpenChildBlocked(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)

	epic, _ := s.Get("e-1")
	epic.Status = StatusDone
	err := s.Update(epic)
	if err == nil {
		t.Fatal("expected error marking epic done with open child, got nil")
	}
	if !strings.Contains(err.Error(), "c-1") {
		t.Errorf("error should name child c-1, got: %v", err)
	}
}

func TestValidate_EpicDoneAllChildrenTerminalAllowed(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
		mkWithParent("c-2", StatusClosed, "e-1"),
	)
	epic, _ := s.Get("e-1")
	epic.Status = StatusDone
	if err := s.Update(epic); err != nil {
		t.Fatalf("expected epic→done to succeed, got: %v", err)
	}
	got, _ := s.Get("e-1")
	if got.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", got.Status, StatusDone)
	}
}

func TestValidate_EpicClosedAlwaysAllowed(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	epic, _ := s.Get("e-1")
	epic.Status = StatusClosed
	if err := s.Update(epic); err != nil {
		t.Fatalf("expected epic→closed to succeed (escape hatch), got: %v", err)
	}
}

func TestValidate_NonEpicDoneNotBlocked(t *testing.T) {
	// Feature ticket with an "child" (someone set parent) should still be
	// able to go done. Validation only applies to epics.
	s := depStore(t,
		mk("feat", StatusOpen),
		mkWithParent("sub", StatusOpen, "feat"),
	)
	tk, _ := s.Get("feat")
	tk.Status = StatusDone
	if err := s.Update(tk); err != nil {
		t.Fatalf("non-epic done should not be blocked, got: %v", err)
	}
}

// ─── Propagation: child → ready ─────────────────────────────────────────────

func TestPropagate_ChildReadyBumpsBacklogEpicToReady(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusBacklog, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusReady
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusReady {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusReady)
	}
}

func TestPropagate_ChildReadyDoesNotDowngradeOpenEpic(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusBacklog, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusReady
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q (no downgrade)", epic.Status, StatusOpen)
	}
}

// ─── Propagation: child → open ─────────────────────────────────────────────

func TestPropagate_ChildOpenBumpsBacklogEpicToOpen(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusBacklog, ""),
		mkWithParent("c-1", StatusBacklog, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusOpen
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
}

func TestPropagate_ChildOpenBumpsReadyEpicToOpen(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusReady, ""),
		mkWithParent("c-1", StatusReady, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusOpen
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
}

// ─── Propagation: child → done ─────────────────────────────────────────────

func TestPropagate_LastChildDoneAutoClosesEpic(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusDone, "e-1"),
		mkWithParent("c-2", StatusOpen, "e-1"),
	)
	c, _ := s.Get("c-2")
	c.Status = StatusDone
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusDone)
	}
}

func TestPropagate_ChildDoneWithOpenSiblingDoesNotAutoClose(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
		mkWithParent("c-2", StatusOpen, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusDone
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q (open sibling remains)", epic.Status, StatusOpen)
	}
}

// ─── Nested epic chains ────────────────────────────────────────────────────

func TestPropagate_CascadesUpNestedEpics(t *testing.T) {
	s := depStore(t,
		mkEpic("e-top", StatusBacklog, ""),
		mkEpic("e-mid", StatusBacklog, "e-top"),
		mkWithParent("c-1", StatusBacklog, "e-mid"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusOpen
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}

	mid, _ := s.Get("e-mid")
	if mid.Status != StatusOpen {
		t.Errorf("e-mid status = %q, want %q", mid.Status, StatusOpen)
	}
	top, _ := s.Get("e-top")
	if top.Status != StatusOpen {
		t.Errorf("e-top status = %q, want %q", top.Status, StatusOpen)
	}
}

// ─── Non-propagating transitions ───────────────────────────────────────────

func TestPropagate_ChildBacklogDoesNotAffectParent(t *testing.T) {
	s := depStore(t,
		mkEpic("e-1", StatusOpen, ""),
		mkWithParent("c-1", StatusOpen, "e-1"),
	)
	c, _ := s.Get("c-1")
	c.Status = StatusBacklog
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	epic, _ := s.Get("e-1")
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q (unchanged)", epic.Status, StatusOpen)
	}
}

func TestPropagate_NonEpicParentIgnored(t *testing.T) {
	// Parent is a feature (not an epic); propagation should skip it.
	s := depStore(t,
		mk("feat", StatusBacklog),
		mkWithParent("sub", StatusBacklog, "feat"),
	)
	c, _ := s.Get("sub")
	c.Status = StatusOpen
	if err := s.Update(c); err != nil {
		t.Fatal(err)
	}
	parent, _ := s.Get("feat")
	if parent.Status != StatusBacklog {
		t.Errorf("feature parent status = %q, want %q (no propagation to non-epic)", parent.Status, StatusBacklog)
	}
}

// ─── MultiStore (namespaced parents) ───────────────────────────────────────

func TestPropagationThroughMultiStore(t *testing.T) {
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-1111", StatusBacklog, ""),
		mkWithParent("proj/child-2222", StatusOpen, "proj/epic-1111"),
	)
	if err := setStatus(t, s, "proj/child-2222", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, err := s.Get("proj/epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusDone)
	}
}

func TestPropagate_ChildAdvancesEpicThroughMultiStore(t *testing.T) {
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-9999", StatusBacklog, ""),
		mkWithParent("proj/child-aaaa", StatusBacklog, "proj/epic-9999"),
	)
	if err := setStatus(t, s, "proj/child-aaaa", StatusReady); err != nil {
		t.Fatal(err)
	}
	epic, err := s.Get("proj/epic-9999")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusReady {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusReady)
	}

	if err := setStatus(t, s, "proj/child-aaaa", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic, err = s.Get("proj/epic-9999")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusOpen)
	}
}

func TestPropagate_ChildDoneWithOpenSiblingThroughMultiStore(t *testing.T) {
	// Two namespaced children, one still open. If allChildrenTerminal misses
	// the namespace it sees no children at all, calls the epic done, and the
	// epic guard then rejects that write — surfacing as a confusing error out
	// of the child's own Update.
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-bbbb", StatusOpen, ""),
		mkWithParent("proj/child-cccc", StatusOpen, "proj/epic-bbbb"),
		mkWithParent("proj/child-dddd", StatusOpen, "proj/epic-bbbb"),
	)
	if err := setStatus(t, s, "proj/child-cccc", StatusDone); err != nil {
		t.Fatalf("marking one of two children done should succeed: %v", err)
	}
	epic, err := s.Get("proj/epic-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("epic status = %q, want %q (open sibling remains)", epic.Status, StatusOpen)
	}
}

func TestEpicGuardThroughMultiStore(t *testing.T) {
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-3333", StatusBacklog, ""),
		mkWithParent("proj/child-4444", StatusOpen, "proj/epic-3333"),
	)
	err := setStatus(t, s, "proj/epic-3333", StatusDone)
	if err == nil {
		t.Fatal("expected error marking epic done with open child, got nil")
	}
	if !strings.Contains(err.Error(), "child-4444") {
		t.Errorf("error should name child child-4444, got: %v", err)
	}
}

func TestPropagate_CascadesUpNestedEpicsThroughMultiStore(t *testing.T) {
	// Each level's parent is read fresh from disk still namespaced, so this
	// only passes if the store tolerates the prefix at every recursion level.
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/e-top-1111", StatusBacklog, ""),
		mkEpic("proj/e-mid-2222", StatusBacklog, "proj/e-top-1111"),
		mkWithParent("proj/c-3333", StatusOpen, "proj/e-mid-2222"),
	)
	if err := setStatus(t, s, "proj/c-3333", StatusDone); err != nil {
		t.Fatal(err)
	}

	mid, err := s.Get("proj/e-mid-2222")
	if err != nil {
		t.Fatal(err)
	}
	if mid.Status != StatusDone {
		t.Errorf("e-mid status = %q, want %q", mid.Status, StatusDone)
	}
	top, err := s.Get("proj/e-top-1111")
	if err != nil {
		t.Fatal(err)
	}
	if top.Status != StatusDone {
		t.Errorf("e-top status = %q, want %q", top.Status, StatusDone)
	}
}

func TestPropagate_BareStoredParentThroughMultiStore(t *testing.T) {
	// Tickets written before the namespacing rollout record a bare parent.
	s := multiDepStore(t, []string{"proj"},
		mkEpic("proj/epic-5555", StatusBacklog, ""),
		mkWithParent("proj/child-6666", StatusOpen, "epic-5555"),
	)
	if err := setStatus(t, s, "proj/child-6666", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic, err := s.Get("proj/epic-5555")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != StatusDone {
		t.Errorf("epic status = %q, want %q", epic.Status, StatusDone)
	}
}

func TestPropagate_CrossProjectParentDoesNotResolve(t *testing.T) {
	// Both projects hold an epic-7777; the child in alpha points at beta's.
	// Neither may move: alpha's store must not mis-resolve the beta prefix.
	s := multiDepStore(t, []string{"alpha", "beta"},
		mkEpic("alpha/epic-7777", StatusBacklog, ""),
		mkEpic("beta/epic-7777", StatusBacklog, ""),
		mkWithParent("alpha/child-8888", StatusOpen, "beta/epic-7777"),
	)
	if err := setStatus(t, s, "alpha/child-8888", StatusDone); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha/epic-7777", "beta/epic-7777"} {
		epic, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if epic.Status != StatusBacklog {
			t.Errorf("%s status = %q, want %q (cross-project parent must not propagate)", id, epic.Status, StatusBacklog)
		}
	}
}
