package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/pkg/ticket"
)

func TestDashboardHeaderContainsPRIAndTITLE(t *testing.T) {
	m := dashboardModel{width: 80, height: 10}
	output := m.view()
	if !strings.Contains(output, "PRI") || !strings.Contains(output, "TITLE") {
		t.Errorf("header should contain 'PRI' and 'TITLE', got:\n%s", output)
	}
}

func TestRenderRowContainsIDSuffixAndTitle(t *testing.T) {
	tk := &ticket.Ticket{
		ID:     "test-abcd",
		Title:  "My title",
		Status: ticket.StatusOpen,
		Type:   ticket.TypeFeature,
	}
	item := ticket.InboxItem{Ticket: tk, Action: ticket.ActionWork}
	m := dashboardModel{width: 80, height: 10}
	row := m.renderRow(item, false, 0)
	if !strings.Contains(row, "abcd") {
		t.Errorf("row should contain ID suffix 'abcd', got:\n%s", row)
	}
	if !strings.Contains(row, tk.Title) {
		t.Errorf("row should contain title %q, got:\n%s", tk.Title, row)
	}
}

func TestDashboardEmptyNoPanic(t *testing.T) {
	m := dashboardModel{width: 80, height: 10}
	output := m.view()
	if !strings.Contains(output, "PRI") {
		t.Errorf("empty dashboard header should contain 'PRI', got:\n%s", output)
	}
}
