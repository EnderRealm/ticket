package tui

import (
	"os"
	"path/filepath"
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

// legacyParentApp returns an App over a store holding a feature p-0001 and a
// child c-0002 parented to it — a shape the one-level rule refuses but that a
// store written before the rule can hold — along with the child as loaded.
func legacyParentApp(t *testing.T) (App, *ticket.Ticket) {
	t.Helper()
	dir := t.TempDir()
	store := ticket.NewFileStore(dir)
	if err := store.Create(&ticket.Ticket{ID: "p-0001", Title: "A feature", Status: ticket.StatusOpen, Type: ticket.TypeFeature}); err != nil {
		t.Fatalf("create: %v", err)
	}
	child := &ticket.Ticket{ID: "c-0002", Title: "Child", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "p-0001"}
	data, err := ticket.Serialize(child)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, child.ID+".md"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	a := App{store: store, activeTab: tabInbox, width: 80, height: 24}
	a.dashboard.activeTab = tabInbox
	a.dashboard.refreshTickets([]*ticket.Ticket{child})
	return a, child
}

func TestSetStatusRejectsLegacyNonEpicParent(t *testing.T) {
	// A ticket whose parent predates the one-level rule still loads, but the
	// TUI's write path refuses it and reports why instead of writing.
	a, _ := legacyParentApp(t)

	msg := a.handleSetStatus("c-0002", ticket.StatusDone)()
	reported, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected a status message, got %T", msg)
	}
	if !strings.Contains(string(reported), "not an epic") {
		t.Errorf("status line = %q, want the parent rejection reason", reported)
	}

	got, err := a.store.Get("c-0002")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != ticket.StatusOpen {
		t.Errorf("status = %q, want unchanged %q — the write should have been refused", got.Status, ticket.StatusOpen)
	}
}

func TestEditFormClearsRejectedParent(t *testing.T) {
	// The rejection tells the user to clear the parent, so the edit form has to
	// expose the field: seeded with the ticket's parent, refusing a bad one in
	// place, and clearing it when left empty.
	a, child := legacyParentApp(t)

	form := newEditFormModel(child, 80, 24)
	if form.fields[fieldParent] != "p-0001" {
		t.Fatalf("edit form parent field = %q, want the ticket's parent p-0001", form.fields[fieldParent])
	}

	// Saving without touching the parent is still refused, and says why.
	msg := form.submit().(formSubmitMsg)
	reported, ok := a.handleEditTicket(msg)().(statusMsg)
	if !ok {
		t.Fatalf("expected a status message on a refused edit")
	}
	if !strings.Contains(string(reported), "not an epic") {
		t.Errorf("status line = %q, want the parent rejection reason", reported)
	}

	// Clearing the field performs the remedy the message names.
	form.fields[fieldParent] = ""
	a.handleEditTicket(form.submit().(formSubmitMsg))

	got, err := a.store.Get("c-0002")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Parent != "" {
		t.Errorf("parent = %q, want it cleared by the empty form field", got.Parent)
	}
}

func TestSetStatusReportsTheChildrenAnAbandonClosed(t *testing.T) {
	// Closing an epic writes its children too, so the status line names them
	// rather than reporting only the row the user was on.
	store := ticket.NewFileStore(t.TempDir())
	epic := &ticket.Ticket{ID: "e-0001", Title: "An epic", Status: ticket.StatusBacklog, Type: ticket.TypeEpic}
	if err := store.Create(epic); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	if err := store.Create(&ticket.Ticket{ID: "c-0002", Title: "Child", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "e-0001"}); err != nil {
		t.Fatalf("create child: %v", err)
	}
	a := App{store: store, activeTab: tabAll, width: 80, height: 24}
	a.dashboard.activeTab = tabAll
	a.dashboard.refreshTickets([]*ticket.Ticket{epic})

	reported := statusLine(t, a.handleSetStatus("e-0001", ticket.StatusClosed))
	if !strings.Contains(reported, "c-0002") || !strings.Contains(reported, "closed 1 child ticket(s)") {
		t.Errorf("status line = %q, want it to name the child the abandon closed", reported)
	}

	reported = statusLine(t, a.handleSetStatus("c-0002", ticket.StatusOpen))
	if strings.Contains(reported, "child ticket(s)") {
		t.Errorf("status line = %q for a write that closed nothing, want no cascade reported", reported)
	}
}

// statusLine runs a mutation command and returns the status message it produced,
// out of the batch it is issued in alongside the reload.
func statusLine(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a batch of commands, got %T", cmd())
	}
	for _, c := range batch {
		if msg, ok := c().(statusMsg); ok {
			return string(msg)
		}
	}
	t.Fatal("no status message in the batch")
	return ""
}

func TestEditFormLeavesAnUntouchedStatusAlone(t *testing.T) {
	// The form is seeded from the list held in memory and the save re-reads the
	// ticket from disk, so a status the user never touched is a snapshot that
	// may have moved since. On an epic reading closed it would land as an
	// abandon: recorded, and cascading into the child that just reopened.
	store := ticket.NewFileStore(t.TempDir())
	if err := store.Create(&ticket.Ticket{ID: "e-0001", Title: "An epic", Status: ticket.StatusBacklog, Type: ticket.TypeEpic}); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	for _, tk := range []*ticket.Ticket{
		{ID: "c-0002", Title: "Finished", Status: ticket.StatusDone, Type: ticket.TypeFeature, Parent: "e-0001"},
		{ID: "c-0003", Title: "Abandoned", Status: ticket.StatusClosed, Type: ticket.TypeFeature, Parent: "e-0001"},
	} {
		if err := store.Create(tk); err != nil {
			t.Fatalf("create %s: %v", tk.ID, err)
		}
	}

	shown, err := store.Get("e-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if shown.Status != ticket.StatusClosed {
		t.Fatalf("epic status = %q, want closed from its children", shown.Status)
	}
	form := newEditFormModel(shown, 80, 24)

	// A child reopens while the form is open.
	child, err := store.Get("c-0003")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	child.Status = ticket.StatusOpen
	if err := store.Update(child); err != nil {
		t.Fatalf("update: %v", err)
	}

	a := App{store: store, activeTab: tabAll, width: 80, height: 24}
	a.dashboard.activeTab = tabAll
	a.dashboard.refreshTickets([]*ticket.Ticket{shown})
	form.fields[fieldTitle] = "Renamed"
	msg := form.submit().(formSubmitMsg)
	if msg.statusSet {
		t.Fatal("a status field the user never touched was submitted as a status they set")
	}
	a.handleEditTicket(msg)

	if got, _ := store.Get("c-0003"); got.Status != ticket.StatusOpen {
		t.Errorf("child = %q, want %q — the stale status cascaded", got.Status, ticket.StatusOpen)
	}
	got, err := store.Get("e-0001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Renamed" {
		t.Errorf("title = %q, want the edit applied", got.Title)
	}
	if got.Status != ticket.StatusOpen {
		t.Errorf("epic status = %q, want %q — the stale status was recorded as an abandon", got.Status, ticket.StatusOpen)
	}
}
