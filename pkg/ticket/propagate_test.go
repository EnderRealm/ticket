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
