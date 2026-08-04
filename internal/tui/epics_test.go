package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

func epicsTabModel(tickets []*ticket.Ticket) dashboardModel {
	m := newDashboardModel(tickets, 120, 24)
	m.activeTab = tabEpics
	m.sortIdx, m.sortDir = defaultSort(tabEpics)
	m.buildItems()
	return m
}

func childIDs(m dashboardModel) []string {
	var ids []string
	for _, r := range m.rows {
		if r.child {
			ids = append(ids, r.item.Ticket.ID)
		}
	}
	return ids
}

func epicsTestTickets() []*ticket.Ticket {
	now := time.Now()
	return []*ticket.Ticket{
		{ID: "ep-0001", Title: "Epic", Type: ticket.TypeEpic, Status: ticket.StatusOpen, Created: now},
		{ID: "ch-bare", Title: "Bare child", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Parent: "ep-0001", Created: now},
		{ID: "ch-ns", Title: "Namespaced child", Type: ticket.TypeFeature, Status: ticket.StatusDone, Parent: "proj/ep-0001", Created: now},
		{ID: "ep-0002", Title: "Other epic", Type: ticket.TypeEpic, Status: ticket.StatusBacklog, Created: now},
		{ID: "ep-0003", Title: "Shipped epic", Type: ticket.TypeEpic, Status: ticket.StatusDone, Created: now},
	}
}

func TestEpicsGroupsBareAndNamespacedChildren(t *testing.T) {
	// The central store records a child's parent namespaced; tickets written
	// before the namespacing rollout record it bare. Both belong to the epic.
	m := epicsTabModel(epicsTestTickets())

	if got := itemIDs(m.items); len(got) != 2 {
		t.Fatalf("epic groups = %v, want ep-0001 and ep-0002 (done epics excluded)", got)
	}
	m.focusEpic("ep-0001")
	m.toggleExpand()

	got := map[string]bool{}
	for _, id := range childIDs(m) {
		got[id] = true
	}
	for _, id := range []string{"ch-bare", "ch-ns"} {
		if !got[id] {
			t.Errorf("epic children missing %s, got %v", id, got)
		}
	}
}

func TestEpicsExpandCollapse(t *testing.T) {
	m := epicsTabModel(epicsTestTickets())
	collapsed := len(m.rows)

	m.focusEpic("ep-0001")
	if !m.toggleExpand() {
		t.Fatal("toggleExpand on an epic group should report a toggle")
	}
	if len(m.rows) != collapsed+2 {
		t.Fatalf("expanded rows = %d, want %d (two children nested under the epic)", len(m.rows), collapsed+2)
	}

	// A child row is part of the group, not a group of its own: enter on one
	// opens it instead of toggling.
	m.cursor++
	if m.toggleExpand() {
		t.Error("toggleExpand on a child row should not toggle")
	}
	if sel := m.selected(); sel == nil || sel.ID != "ch-bare" {
		t.Errorf("cursor on the first child selected %v, want ch-bare", sel)
	}

	m.cursor--
	m.toggleExpand()
	if len(m.rows) != collapsed {
		t.Errorf("collapsed rows = %d, want %d", len(m.rows), collapsed)
	}
}

func TestEpicsCountsGroupsNotExpandedChildren(t *testing.T) {
	// The agreed divergence from "a count is the rows the tab renders": on the
	// epics tab a count is the number of epic groups, because a child is nested
	// under a group that is already counted, so expanding must not inflate the
	// tab bar. See tabShows.
	tickets := epicsTestTickets()
	a := App{tickets: tickets, activeTab: tabEpics}
	a.dashboard.all = tickets
	a.dashboard.setSize(120, 24)
	a.syncDashboardTab()

	want := a.tabCounts()[tabEpics]
	if want != len(a.dashboard.items) {
		t.Fatalf("epics count = %d, want %d groups", want, len(a.dashboard.items))
	}
	a.dashboard.focusEpic("ep-0001")
	a.dashboard.toggleExpand()
	if got := a.tabCounts()[tabEpics]; got != want {
		t.Errorf("epics count after expanding = %d, want %d (children are not rows of their own)", got, want)
	}
}

func TestEpicsRowShowsIndicatorAndProgress(t *testing.T) {
	m := epicsTabModel(epicsTestTickets())
	m.focusEpic("ep-0001")

	line := m.renderRow(m.rows[m.cursor], false)
	if !strings.HasPrefix(line, "▸") {
		t.Errorf("collapsed epic row should lead with ▸, got:\n%s", line)
	}
	if !strings.Contains(line, "1/2") {
		t.Errorf("epic row should show child progress 1/2, got:\n%s", line)
	}

	m.toggleExpand()
	if line := m.renderRow(m.rows[m.cursor], false); !strings.HasPrefix(line, "▾") {
		t.Errorf("expanded epic row should lead with ▾, got:\n%s", line)
	}
}

func TestBacklogEpicOpensEpicsTabFocused(t *testing.T) {
	tickets := epicsTestTickets()
	a := App{tickets: tickets, activeTab: tabBacklog}
	a.dashboard.all = tickets
	a.dashboard.setSize(120, 24)
	a.syncDashboardTab()

	a.openDashboardTicket(tickets[3]) // ep-0002, a backlog rollup
	if a.activeTab != tabEpics {
		t.Fatalf("active tab = %s, want epics", tabNames[a.activeTab])
	}
	if a.overlay != overlayNone {
		t.Error("opening a backlog rollup should jump to the epics tab, not open a detail overlay")
	}
	if sel := a.dashboard.selected(); sel == nil || sel.ID != "ep-0002" {
		t.Errorf("epics tab focused %v, want ep-0002", sel)
	}
}

func TestEpicsSpaceTogglesFromApp(t *testing.T) {
	tickets := epicsTestTickets()
	a := App{tickets: tickets, activeTab: tabEpics}
	a.dashboard.all = tickets
	a.dashboard.setSize(120, 24)
	a.syncDashboardTab()
	a.dashboard.focusEpic("ep-0001")
	collapsed := len(a.dashboard.rows)

	model, _ := a.updateTab(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if got := len(model.(App).dashboard.rows); got != collapsed+2 {
		t.Errorf("rows after space = %d, want %d", got, collapsed+2)
	}
}
