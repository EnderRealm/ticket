package ticket

import (
	"testing"
	"time"
)

func makeTickets() []*Ticket {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []*Ticket{
		{ID: "t-001", Stage: StageTriage, Type: TypeTask, Priority: 2, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend"}, Assignee: "Alice"},
		{ID: "t-002", Stage: StageImplement, Type: TypeBug, Priority: 0, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"frontend"}, Assignee: "Bob"},
		{ID: "t-003", Stage: StageTriage, Type: TypeFeature, Priority: 1, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend"}, Parent: "t-epic"},
		{ID: "t-004", Stage: StageDone, Type: TypeEpic, Priority: 1, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{}},
		{ID: "t-005", Stage: StageImplement, Type: TypeChore, Priority: 3, Created: now, Deps: []string{}, Links: []string{}, Tags: []string{"backend", "ci"}, Assignee: "Alice"},
	}
}

func TestFilter_ByStage(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Stage: StageTriage, Priority: -1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
	for _, tk := range result {
		if tk.Stage != StageTriage {
			t.Errorf("got stage %q, want triage", tk.Stage)
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

func TestFilter_ByAssignee(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Assignee: "alice", Priority: -1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2 (case-insensitive)", len(result))
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
	result := Filter(makeTickets(), ListOptions{Stage: StageTriage, Tag: "backend", Priority: -1})
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestFilter_NoMatch(t *testing.T) {
	result := Filter(makeTickets(), ListOptions{Stage: StageDone, Type: TypeBug, Priority: -1})
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

func TestSortByStagePriorityID(t *testing.T) {
	tickets := makeTickets()
	SortByStagePriorityID(tickets)

	// Stage order is pipeline-dependent. StageIndex for each ticket's type:
	// t-003: feature triage=1, t-001: task triage=1, t-002: bug implement=2,
	// t-004: epic done=4, t-005: chore implement=5
	// Within same stage index, lower priority number first.
	expected := []string{"t-003", "t-001", "t-002", "t-004", "t-005"}
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

	// Just priority then ID, stage ignored.
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
