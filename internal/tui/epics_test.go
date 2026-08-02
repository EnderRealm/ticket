package tui

import (
	"testing"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestEpicsGroupsBareAndNamespacedChildren(t *testing.T) {
	// The central store records a child's parent namespaced; tickets written
	// before the namespacing rollout record it bare. Both belong to the epic.
	tickets := []*ticket.Ticket{
		{ID: "ep-0001", Title: "Epic", Type: ticket.TypeEpic, Status: ticket.StatusOpen},
		{ID: "ch-bare", Title: "Bare child", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Parent: "ep-0001"},
		{ID: "ch-ns", Title: "Namespaced child", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Parent: "proj/ep-0001"},
	}

	var m epicsModel
	m.refreshTickets(tickets)

	if len(m.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(m.rows))
	}
	got := map[string]bool{}
	for _, c := range m.rows[0].children {
		got[c.ID] = true
	}
	for _, id := range []string{"ch-bare", "ch-ns"} {
		if !got[id] {
			t.Errorf("epic children missing %s, got %v", id, got)
		}
	}
}
