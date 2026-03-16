package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/pkg/ticket"
)

func TestDashboardHeaderThreeSpacesBetweenIDAndTitle(t *testing.T) {
	m := dashboardModel{width: 80, height: 10}
	output := m.view()
	if !strings.Contains(output, "ID   TITLE") {
		t.Errorf("header should contain 'ID   TITLE' (3 spaces), got:\n%s", output)
	}
}

func TestRenderRowThreeSpacesBetweenIDAndTitle(t *testing.T) {
	tk := &ticket.Ticket{
		ID:    "test-abcd",
		Title: "My title",
		Stage: ticket.StageTriage,
		Type:  ticket.TypeFeature,
	}
	item := ticket.InboxItem{Ticket: tk, Action: ticket.ActionHumanInput}
	m := dashboardModel{width: 80, height: 10}
	row := m.renderRow(item, false, len(tk.ID))
	if !strings.Contains(row, tk.ID+"   "+tk.Title) {
		t.Errorf("row should contain %q, got:\n%s", tk.ID+"   "+tk.Title, row)
	}
}

func TestRenderRowReviewPendingThreeSpaces(t *testing.T) {
	tk := &ticket.Ticket{
		ID:     "test-abcd",
		Title:  "My title",
		Stage:  ticket.StageTriage,
		Type:   ticket.TypeFeature,
		Review: ticket.ReviewPending,
	}
	item := ticket.InboxItem{Ticket: tk, Action: ticket.ActionHumanReview}
	m := dashboardModel{width: 80, height: 10}
	row := m.renderRow(item, false, len(tk.ID))
	if !strings.Contains(row, tk.ID+"   "+tk.Title) {
		t.Errorf("row should contain %q, got:\n%s", tk.ID+"   "+tk.Title, row)
	}
}

func TestDashboardEmptyNoPanic(t *testing.T) {
	m := dashboardModel{width: 80, height: 10}
	output := m.view()
	if !strings.Contains(output, "ID   TITLE") {
		t.Errorf("empty dashboard header should contain 'ID   TITLE' (3 spaces), got:\n%s", output)
	}
}
