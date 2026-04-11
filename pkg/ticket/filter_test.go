package ticket

import (
	"testing"
	"time"
)

func makeTickets() []*Ticket {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []*Ticket{
		{ID: "t-001", Status: StatusReady, Type: TypeFeature, Priority: 2, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend"}},
		{ID: "t-002", Status: StatusOpen, Type: TypeBug, Priority: 0, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"frontend"}},
		{ID: "t-003", Status: StatusReady, Type: TypeFeature, Priority: 1, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend"}, Parent: "t-epic"},
		{ID: "t-004", Status: StatusDone, Type: TypeEpic, Priority: 1, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{}},
		{ID: "t-005", Status: StatusOpen, Type: TypeFeature, Priority: 3, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend", "ci"}},
	}
}

func TestFilter_ByStatus(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Status: StatusReady, Priority: -1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
	for _, tk := range result {
		if tk.Status != StatusReady {
			t.Errorf("got status %q, want ready", tk.Status)
		}
	}
}

func TestFilter_ByType(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Type: TypeBug, Priority: -1})
	if len(result) != 1 || result[0].ID != "t-002" {
		t.Errorf("got %v, want [t-002]", ids(result))
	}
}

func TestFilter_ByPriority(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Priority: 1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestFilter_ByTag(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Tag: "backend", Priority: -1})
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}
}

func TestFilter_ByParent(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Parent: "t-epic", Priority: -1})
	if len(result) != 1 || result[0].ID != "t-003" {
		t.Errorf("got %v, want [t-003]", ids(result))
	}
}

func TestFilter_Combined(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Status: StatusReady, Tag: "backend", Priority: -1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestFilter_NoMatch(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Status: StatusDone, Type: TypeBug, Priority: -1})
	if len(result) != 0 {
		t.Errorf("len = %d, want 0", len(result))
	}
}

func TestFilter_NoFilters(t *testing.T) {
	all := makeTickets()
	result := Filter(all, DefaultListOptions())
	if len(result) != len(all) {
		t.Errorf("len = %d, want %d", len(result), len(all))
	}
}

func TestSortByStatusPriorityID(t *testing.T) {
	tickets := makeTickets()
	SortByStatusPriorityID(tickets)

	// StatusOrder: open=0, ready=1, backlog=2, done=3, closed=4.
	// t-002: open P0, t-005: open P3, t-003: ready P1, t-001: ready P2, t-004: done P1
	expected := []string{"t-002", "t-005", "t-003", "t-001", "t-004"}
	got := ids(tickets)
	for i, id := range expected {
		if got[i] != id {
			t.Errorf("position %d: got %q, want %q (full order: %v)", i, got[i], id, got)
			break
		}
	}
}

func TestSortByPriorityID(t *testing.T) {
	tickets := makeTickets()
	SortByPriorityID(tickets)

	// Just priority then ID, status ignored.
	expected := []string{"t-002", "t-003", "t-004", "t-001", "t-005"}
	got := ids(tickets)
	for i, id := range expected {
		if got[i] != id {
			t.Errorf("position %d: got %q, want %q (full order: %v)", i, got[i], id, got)
			break
		}
	}
}

func ids(tickets []*Ticket) []string {
	out := make([]string, len(tickets))
	for i, t := range tickets {
		out[i] = t.ID
	}
	return out
}
