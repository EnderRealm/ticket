package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpAdvertisesStatusKeysPerTab(t *testing.T) {
	cases := []struct {
		tab     tabID
		present []string
		absent  []string
	}{
		{tabBacklog, []string{"(r)eady"}, []string{"(b)acklog", "(x)done"}},
		{tabInbox, []string{"(b)acklog", "(x)done"}, []string{"(r)eady"}},
		{tabDone, nil, []string{"(r)eady", "(b)acklog", "(x)done"}},
		{tabAll, nil, []string{"(r)eady", "(b)acklog", "(x)done"}},
	}
	for _, c := range cases {
		a := App{activeTab: c.tab}
		a.dashboard.activeTab = c.tab
		help := a.helpText()
		for _, k := range c.present {
			if !strings.Contains(help, k) {
				t.Errorf("tab %d: help should advertise %q, got:\n%s", c.tab, k, help)
			}
		}
		for _, k := range c.absent {
			if strings.Contains(help, k) {
				t.Errorf("tab %d: help should not advertise %q, got:\n%s", c.tab, k, help)
			}
		}
	}
}

// statusKeyApp returns an App backed by a real FileStore in a temp dir,
// preloaded with the given ticket and focused on tab.
func statusKeyApp(t *testing.T, tab tabID, tk *ticket.Ticket) App {
	t.Helper()
	store := ticket.NewFileStore(t.TempDir())
	if err := store.Create(tk); err != nil {
		t.Fatalf("create: %v", err)
	}
	a := App{store: store, activeTab: tab, width: 80, height: 24}
	a.dashboard.activeTab = tab
	a.dashboard.refreshTickets([]*ticket.Ticket{tk})
	return a
}

// runKey dispatches a rune key through updateTab and drains the returned cmd's
// messages back into Update, mirroring the bubbletea run loop closely enough to
// drive a mutation end to end.
func runKey(t *testing.T, a App, key string) App {
	t.Helper()
	model, cmd := a.updateTab(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	a = model.(App)
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				model, next := a.Update(c())
				a = model.(App)
				if next != nil && cmd == nil {
					cmd = next
				}
			}
			continue
		}
		model, cmd = a.Update(msg)
		a = model.(App)
	}
	return a
}

func TestBacklogReadyKey(t *testing.T) {
	tk := &ticket.Ticket{ID: "a-0001", Title: "T", Status: ticket.StatusBacklog, Type: ticket.TypeFeature}
	a := statusKeyApp(t, tabBacklog, tk)

	a = runKey(t, a, "r")

	got, err := a.store.Get("a-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ticket.StatusReady {
		t.Errorf("after r on backlog tab: status = %q, want %q", got.Status, ticket.StatusReady)
	}
}

func TestInboxBacklogKey(t *testing.T) {
	tk := &ticket.Ticket{ID: "a-0001", Title: "T", Status: ticket.StatusReady, Type: ticket.TypeFeature}
	a := statusKeyApp(t, tabInbox, tk)

	a = runKey(t, a, "b")

	got, err := a.store.Get("a-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ticket.StatusBacklog {
		t.Errorf("after b on inbox tab: status = %q, want %q", got.Status, ticket.StatusBacklog)
	}
}

func TestInboxDoneKey(t *testing.T) {
	tk := &ticket.Ticket{ID: "a-0001", Title: "T", Status: ticket.StatusOpen, Type: ticket.TypeFeature}
	a := statusKeyApp(t, tabInbox, tk)

	a = runKey(t, a, "x")

	got, err := a.store.Get("a-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ticket.StatusDone {
		t.Errorf("after x on inbox tab: status = %q, want %q", got.Status, ticket.StatusDone)
	}
}

func TestReadyKeyInertOnInbox(t *testing.T) {
	tk := &ticket.Ticket{ID: "a-0001", Title: "T", Status: ticket.StatusOpen, Type: ticket.TypeFeature}
	a := statusKeyApp(t, tabInbox, tk)

	a = runKey(t, a, "r")

	got, err := a.store.Get("a-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ticket.StatusOpen {
		t.Errorf("after r on inbox tab: status = %q, want unchanged %q", got.Status, ticket.StatusOpen)
	}
}
