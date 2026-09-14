package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/charmbracelet/x/ansi"
)

func TestInboxBlockedDependencies(t *testing.T) {
	for _, status := range []ticket.Status{ticket.StatusReady, ticket.StatusOpen} {
		for _, tc := range []struct {
			name string
			deps []string
			want string
		}{
			{"no deps", nil, string(status)},
			{"terminal deps", []string{"done-0001", "proj/closed-0002"}, string(status)},
			{"open dep", []string{"open-0003"}, "blocked"},
			{"ready dep", []string{"ready-0004"}, "blocked"},
			{"backlog dep", []string{"backlog-0005"}, "blocked"},
			{"mixed deps", []string{"done-0001", "open-0003"}, "blocked"},
			{"missing dep", []string{"missing-0006"}, "blocked"},
			{"foreign missing dep", []string{"other/done-0001"}, "blocked"},
		} {
			t.Run(string(status)+"/"+tc.name, func(t *testing.T) {
				m := boardModel(t, "proj", tabInbox, 160, 40,
					&ticket.Ticket{ID: "waiter-0000", Type: ticket.TypeFeature, Status: status, Deps: tc.deps},
					&ticket.Ticket{ID: "done-0001", Status: ticket.StatusDone},
					&ticket.Ticket{ID: "closed-0002", Status: ticket.StatusClosed},
					&ticket.Ticket{ID: "open-0003", Status: ticket.StatusOpen},
					&ticket.Ticket{ID: "ready-0004", Status: ticket.StatusReady},
					&ticket.Ticket{ID: "backlog-0005", Status: ticket.StatusBacklog},
				)
				waiter := boardTicket(t, m.all, "waiter-0000")
				for _, selected := range []bool{false, true} {
					assertInboxStatus(t, m, waiter, selected, tc.want)
				}
				m.activeTab = tabAll
				assertInboxStatus(t, m, waiter, false, string(status))
				if waiter.Status != status {
					t.Fatalf("display changed stored status to %q", waiter.Status)
				}
			})
		}
	}
}

func TestInboxBlockedForeignRefreshAndStoredStatus(t *testing.T) {
	f := newGlobalFixture(t)
	f.create(t, "ticket", &ticket.Ticket{ID: "waiter-0008", Title: "Waiting", Type: ticket.TypeFeature, Status: ticket.StatusReady, Deps: []string{"loom/b-0003", "loom/c-0004"}})
	path := filepath.Join(f.ticketsDir("ticket"), "waiter-0008.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := f.app(t, "ticket", "")
	waiter := boardTicket(t, a.tickets, "waiter-0008")
	assertInboxStatus(t, a.dashboard, waiter, false, "blocked")
	a = onTab(t, a, tabInbox, waiter.ID)
	a = press(t, a, "enter")
	if a.detail.ticket.Status != ticket.StatusReady || !strings.Contains(ansi.Strip(a.View()), "ready") {
		t.Fatalf("detail lost the stored ready status:\n%s", a.View())
	}
	stored, err := f.store("ticket").Get(waiter.ID)
	if err != nil || stored.Status != ticket.StatusReady {
		t.Fatalf("stored read = %v, %v; want ready", stored, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("display changed the ticket file: %v", err)
	}
	f.setStatus(t, "loom", "b-0003", ticket.StatusDone)
	// The current frame keeps its snapshot until the next load.
	assertInboxStatus(t, a.dashboard, waiter, false, "blocked")
	a = f.reload(t, a)
	assertInboxStatus(t, a.dashboard, boardTicket(t, a.tickets, waiter.ID), false, "ready")
	f.setStatus(t, "loom", "b-0003", ticket.StatusOpen)
	a = f.reload(t, a)
	assertInboxStatus(t, a.dashboard, boardTicket(t, a.tickets, waiter.ID), false, "blocked")
}

func TestInboxBlockedStatusSort(t *testing.T) {
	m := boardModel(t, "proj", tabInbox, 160, 40,
		&ticket.Ticket{ID: "blocked-0001", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Deps: []string{"missing"}, Priority: 0},
		&ticket.Ticket{ID: "ready-0002", Type: ticket.TypeFeature, Status: ticket.StatusReady, Priority: 2},
		&ticket.Ticket{ID: "open-0003", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Priority: 2},
		&ticket.Ticket{ID: "blocked-0004", Type: ticket.TypeFeature, Status: ticket.StatusReady, Deps: []string{"missing"}, Priority: 1},
	)
	m.sortIdx = colIndex(tabInbox, "STATUS")
	for _, tc := range []struct {
		tab  tabID
		dir  sortDir
		want []string
	}{
		{tabInbox, asc, []string{"open-0003", "ready-0002", "blocked-0001", "blocked-0004"}},
		{tabInbox, desc, []string{"blocked-0001", "blocked-0004", "ready-0002", "open-0003"}},
		{tabAll, asc, []string{"blocked-0001", "open-0003", "blocked-0004", "ready-0002"}},
	} {
		m.activeTab, m.sortDir = tc.tab, tc.dir
		m.buildItems()
		var got []string
		for _, r := range m.rows {
			got = append(got, r.item.Ticket.ID)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("tab %v direction %v: got %v, want %v", tc.tab, tc.dir, got, tc.want)
		}
	}
}

func TestInboxBlockedColor(t *testing.T) {
	blocked, ok := StatusColors["blocked"]
	if !ok || blocked == StatusColors[ticket.StatusReady] || blocked == StatusColors[ticket.StatusOpen] {
		t.Fatalf("blocked color %q must exist and differ from ready/open", blocked)
	}
}

func assertInboxStatus(t *testing.T, m dashboardModel, tk *ticket.Ticket, selected bool, want string) {
	t.Helper()
	line := ansi.Strip(m.renderRow(row{item: ticket.NextAction(tk)}, selected))
	start := 2
	for _, c := range columnsFor(m.activeTab, &m) {
		if c.name == "STATUS" {
			if got := strings.TrimSpace(ansi.Cut(line, start, start+c.width)); got != want {
				t.Errorf("%s STATUS = %q, want %q; row: %s", tk.ID, got, want, line)
			}
			return
		}
		start += c.width
	}
	t.Fatal("no STATUS column")
}
