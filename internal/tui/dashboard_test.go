package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
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

func itemIDs(items []ticket.InboxItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Ticket.ID
	}
	return out
}

func colIndex(tab tabID, name string) int {
	for i, c := range columnsFor(tab) {
		if c.name == name {
			return i
		}
	}
	return -1
}

func TestDashboardSortByColumn(t *testing.T) {
	now := time.Now()
	tickets := []*ticket.Ticket{
		{ID: "a-0001", Title: "Older", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now.Add(-48 * time.Hour)},
		{ID: "b-0002", Title: "Newer", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now.Add(-1 * time.Hour)},
	}
	m := newDashboardModel(tickets, 80, 24)
	m.activeTab = tabInbox

	// Ascending by CREATED → oldest first.
	m.sortIdx = colIndex(tabInbox, "CREATED")
	m.sortDir = asc
	m.buildItems()
	if got := itemIDs(m.items); got[0] != "a-0001" {
		t.Errorf("created asc: got %v, want a-0001 first", got)
	}

	// Descending by CREATED → newest first.
	m.sortDir = desc
	m.sortItems()
	if got := itemIDs(m.items); got[0] != "b-0002" {
		t.Errorf("created desc: got %v, want b-0002 first", got)
	}
}

func TestDashboardSortByTitle(t *testing.T) {
	// Titles whose alphabetical order differs from priority order, so a
	// regression to the old "TITLE falls back to PRI" bug would be caught.
	now := time.Now()
	tickets := []*ticket.Ticket{
		{ID: "z-0001", Title: "zebra", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Priority: 0, Created: now},
		{ID: "a-0002", Title: "apple", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Priority: 1, Created: now},
		{ID: "m-0003", Title: "mango", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Priority: 2, Created: now},
	}
	m := newDashboardModel(tickets, 80, 24)
	m.activeTab = tabInbox

	m.sortIdx = colIndex(tabInbox, "TITLE")
	m.sortDir = asc
	m.buildItems()

	want := []string{"apple", "mango", "zebra"}
	for i, w := range want {
		if m.items[i].Ticket.Title != w {
			t.Errorf("title asc position %d: got %s, want %s", i, m.items[i].Ticket.Title, w)
		}
	}
}

func TestDashboardDefaultSortPerTab(t *testing.T) {
	idx, dir := defaultSort(tabInbox)
	if columnsFor(tabInbox)[idx].name != "PRI" || dir != asc {
		t.Errorf("inbox default: got %s %v, want PRI asc", columnsFor(tabInbox)[idx].name, dir)
	}
	idx, dir = defaultSort(tabDone)
	if columnsFor(tabDone)[idx].name != "COMPLETED" || dir != desc {
		t.Errorf("done default: got %s %v, want COMPLETED desc", columnsFor(tabDone)[idx].name, dir)
	}
	idx, dir = defaultSort(tabAll)
	if columnsFor(tabAll)[idx].name != "COMPLETED" || dir != desc {
		t.Errorf("all default: got %s %v, want COMPLETED desc", columnsFor(tabAll)[idx].name, dir)
	}
}

func TestDashboardSortKeys(t *testing.T) {
	tickets := []*ticket.Ticket{
		{ID: "a-0001", Title: "One", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: time.Now()},
	}
	m := newDashboardModel(tickets, 80, 24)
	m.activeTab = tabInbox
	m.sortIdx = 0
	m.sortDir = asc

	cols := columnsFor(tabInbox)

	// 's' advances the sort column (wrapping) and resets direction to asc.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.sortIdx != 1 {
		t.Errorf("after s: sortIdx = %d, want 1", m.sortIdx)
	}
	if m.sortDir != asc {
		t.Errorf("after s: dir = %v, want asc", m.sortDir)
	}

	// 'S' toggles direction.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if m.sortDir != desc {
		t.Errorf("after S: dir = %v, want desc", m.sortDir)
	}

	// 's' wraps around the column count.
	m.sortIdx = len(cols) - 1
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if m.sortIdx != 0 {
		t.Errorf("after wrap s: sortIdx = %d, want 0", m.sortIdx)
	}
}
