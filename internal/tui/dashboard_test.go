package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
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

func TestColumnWidthsFitSortArrow(t *testing.T) {
	// When a column is the active sort, its header renders as name+arrow (one
	// extra cell). Fixed-width columns must reserve at least one space after
	// that so adjacent columns don't collide. Flexible columns (width 0, TITLE)
	// are exempt.
	seen := map[string]bool{}
	check := func(cols []column) {
		for _, c := range cols {
			if c.width == 0 || seen[c.name] {
				continue
			}
			seen[c.name] = true
			if min := len(c.name) + 2; c.width < min {
				t.Errorf("column %q width %d too narrow for header+arrow+gap (need >= %d)", c.name, c.width, min)
			}
		}
	}
	for _, tab := range []tabID{tabInbox, tabBacklog, tabDone, tabAll} {
		check(columnsFor(tab))
	}
	check(epicColumns)
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

func filterTestModel() dashboardModel {
	now := time.Now()
	tickets := []*ticket.Ticket{
		{ID: "a-0001", Title: "alpha", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
		{ID: "b-0002", Title: "alphabet", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
		{ID: "c-0003", Title: "alphanumeric", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
	}
	m := newDashboardModel(tickets, 80, 24)
	m.activeTab = tabInbox
	m.sortIdx = colIndex(tabInbox, "ID")
	m.sortDir = asc
	m.filterActive = true
	m.filterText = "alpha"
	m.buildItems()
	return m
}

func TestDashboardFilterArrowsMoveCursor(t *testing.T) {
	m := filterTestModel()
	if len(m.items) != 3 {
		t.Fatalf("expected 3 filtered items, got %d", len(m.items))
	}

	m, _ = m.update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("after down: cursor = %d, want 1", m.cursor)
	}
	if !m.filterActive {
		t.Error("after down: filter should stay active")
	}

	m, _ = m.update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("after up: cursor = %d, want 0", m.cursor)
	}
	if !m.filterActive {
		t.Error("after up: filter should stay active")
	}
}

func TestDashboardFilterWheelMovesCursor(t *testing.T) {
	m := filterTestModel()

	m, _ = m.update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.cursor == 0 {
		t.Errorf("after wheel down: cursor = %d, want > 0", m.cursor)
	}

	m, _ = m.update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.cursor != 0 {
		t.Errorf("after wheel up: cursor = %d, want 0", m.cursor)
	}
}

func TestRenderHelpHidesInertKeysInSearch(t *testing.T) {
	a := App{activeTab: tabInbox}
	a.dashboard.activeTab = tabInbox

	full := a.renderHelp()
	for _, k := range []string{"(c)reate", "(e)dit", "(d)elete", "(s)ort"} {
		if !strings.Contains(full, k) {
			t.Errorf("normal help should advertise %q, got:\n%s", k, full)
		}
	}

	a.dashboard.filterActive = true
	search := a.renderHelp()
	for _, k := range []string{"(c)reate", "(e)dit", "(d)elete", "(p)riority", "(m)ove", "(y)ank", "(s)ort"} {
		if strings.Contains(search, k) {
			t.Errorf("search-mode help should not advertise inert key %q, got:\n%s", k, search)
		}
	}
	for _, k := range []string{"select", "apply", "clear"} {
		if !strings.Contains(search, k) {
			t.Errorf("search-mode help should mention %q, got:\n%s", k, search)
		}
	}
}

func TestDashboardFilterEscPreservesSelection(t *testing.T) {
	now := time.Now()
	// Non-matching tickets sort ahead of the matches by ID, so clearing the
	// filter shifts the matched rows to a different index — a cursor kept as a
	// bare index would land on the wrong ticket.
	tickets := []*ticket.Ticket{
		{ID: "a-0001", Title: "zzz one", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
		{ID: "b-0002", Title: "zzz two", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
		{ID: "c-0003", Title: "match alpha", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
		{ID: "d-0004", Title: "match beta", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Created: now},
	}
	m := newDashboardModel(tickets, 80, 24)
	m.activeTab = tabInbox
	m.sortIdx = colIndex(tabInbox, "ID")
	m.sortDir = asc
	m.filterActive = true
	m.filterText = "match"
	m.buildItems()
	if len(m.items) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(m.items))
	}

	// Arrow down to the second match (d-0004), then clear the filter with esc.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyDown})
	wantID := m.items[m.cursor].Ticket.ID
	if wantID != "d-0004" {
		t.Fatalf("setup: arrowed to %q, want d-0004", wantID)
	}

	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.filterActive {
		t.Error("after esc: filter should be inactive")
	}
	if m.filterText != "" {
		t.Errorf("after esc: filterText = %q, want empty", m.filterText)
	}
	if len(m.items) != 4 {
		t.Fatalf("after esc: expected full list of 4, got %d", len(m.items))
	}
	if got := m.items[m.cursor].Ticket.ID; got != wantID {
		t.Errorf("after esc: selected %q, want %q (cursor should stay on the arrowed row)", got, wantID)
	}
}

func TestDashboardFilterEnterCommitsFilter(t *testing.T) {
	m := filterTestModel()
	m.cursor = 1

	m, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.filterActive {
		t.Error("after enter: filter box should be closed")
	}
	if m.filterText != "alpha" {
		t.Errorf("after enter: filterText = %q, want %q (filter stays applied)", m.filterText, "alpha")
	}
	if m.cursor != 1 {
		t.Errorf("after enter: cursor = %d, want 1 (cursor unchanged)", m.cursor)
	}
	if cmd != nil {
		t.Error("after enter: expected nil command (no ticket opened)")
	}
	// Row commands now act on the committed selection.
	if sel := m.selected(); sel == nil || sel.ID != "b-0002" {
		t.Errorf("after enter: selected = %v, want b-0002", sel)
	}
}

func TestDashboardFilterEnterNoResultsCommits(t *testing.T) {
	m := filterTestModel()
	m.filterText = "zzz-no-match"
	m.buildItems()
	if len(m.items) != 0 {
		t.Fatalf("expected 0 filtered items, got %d", len(m.items))
	}

	m, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("after enter with no results: expected nil command")
	}
	if m.filterActive {
		t.Error("after enter with no results: filter box should be closed")
	}
	if m.filterText != "zzz-no-match" {
		t.Errorf("after enter with no results: filterText = %q, want it preserved", m.filterText)
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
