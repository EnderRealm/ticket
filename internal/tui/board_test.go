package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// boardFixture writes tickets as files into one project of a temp central
// store — directly, past the write boundary, so a shape the boundary refuses
// but an older store can hold (a parent that names nothing, an epic under an
// epic) can be pinned — and returns the board's view of it: the tickets as
// the board lists them and the graph they were listed off.
func boardFixture(t *testing.T, ns string, tickets ...*ticket.Ticket) ([]*ticket.Ticket, *ticket.Snapshot) {
	t.Helper()
	return loadBoardFixture(t, ns, writeBoardFixture(t, ns, tickets...))
}

// writeBoardFixture writes the tickets into one project of a temp central
// store and returns the project's directory, for a test that adds a file the
// serializer cannot produce before loading.
func writeBoardFixture(t *testing.T, ns string, tickets ...*ticket.Ticket) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "central", "tickets", ns)
	writeTicketFiles(t, dir, tickets...)
	return dir
}

// writeTicketFiles writes the tickets as files into dir, creating it, past
// the write boundary.
func writeTicketFiles(t *testing.T, dir string, tickets ...*ticket.Ticket) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, tk := range tickets {
		data, err := ticket.Serialize(tk)
		if err != nil {
			t.Fatalf("serialize %s: %v", tk.ID, err)
		}
		if err := os.WriteFile(filepath.Join(dir, tk.ID+".md"), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", tk.ID, err)
		}
	}
}

// loadBoardFixture is the board's view of a project directory.
func loadBoardFixture(t *testing.T, ns, dir string) ([]*ticket.Ticket, *ticket.Snapshot) {
	t.Helper()
	store := ticket.NewProjectFileStore(dir, ns)
	snap, err := store.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	listed := store.ListFromSnapshot(snap)
	ticket.SortByStatusPriorityID(listed)
	return listed, snap
}

// boardModel is a dashboard on tab over boardFixture's view.
func boardModel(t *testing.T, ns string, tab tabID, w, h int, tickets ...*ticket.Ticket) dashboardModel {
	t.Helper()
	listed, snap := boardFixture(t, ns, tickets...)
	m := dashboardModel{ns: ns, activeTab: tab, width: w, height: h}
	m.sortIdx, m.sortDir = defaultSort(tab)
	m.refreshTickets(listed, snap)
	return m
}

// boardApp is an App on tab over boardFixture's view.
func boardApp(t *testing.T, ns string, tab tabID, w, h int, tickets ...*ticket.Ticket) App {
	t.Helper()
	dir := writeBoardFixture(t, ns, tickets...)
	listed, snap := loadBoardFixture(t, ns, dir)
	return boardAppOver(ns, dir, tab, w, h, listed, snap)
}

// boardAppOver is an App on tab over an already loaded view of the project
// directory dir, with the stores New would wire over it.
func boardAppOver(ns, dir string, tab tabID, w, h int, listed []*ticket.Ticket, snap *ticket.Snapshot) App {
	a := App{
		store:       ticket.NewProjectFileStore(dir, ns),
		multi:       ticket.NewMultiStore(filepath.Dir(dir)),
		projectName: ns,
		tickets:     listed,
		snap:        snap,
		activeTab:   tab,
		width:       w,
		height:      h,
	}
	a.dashboard.ns = ns
	a.dashboard.setSize(w, h)
	a.dashboard.refreshTickets(listed, snap)
	a.syncDashboardTab()
	return a
}

// boardTicket finds a board ticket by bare ID.
func boardTicket(t *testing.T, tickets []*ticket.Ticket, id string) *ticket.Ticket {
	t.Helper()
	for _, tk := range tickets {
		if tk.ID == id {
			return tk
		}
	}
	t.Fatalf("board lists no %s", id)
	return nil
}

// press sends one key through the whole App the way the program does and
// drains the commands it issues back into Update, so a key that writes and
// reloads lands before the next assertion. A status message's clear timer is
// not followed: it sleeps for the status's whole display time.
func press(t *testing.T, a App, key string) App {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	}
	model, cmd := a.Update(msg)
	return drain(model.(App), cmd)
}

func drain(a App, cmd tea.Cmd) App {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return a
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				a = drain(a, c)
			}
			continue
		}
		model, next := a.Update(msg)
		a = model.(App)
		switch msg.(type) {
		case statusMsg, warnMsg:
			next = nil
		}
		cmd = next
	}
	return a
}

// A child's status is read off disk like its ID and title, and the parser
// keeps a status it does not know: neither the epic detail nor the child
// picker may pass its control characters through to the terminal.
func TestEpicChildrenSanitizeStoredStatus(t *testing.T) {
	dir := writeBoardFixture(t, "proj",
		&ticket.Ticket{ID: "ep-0001", Title: "Epic", Type: ticket.TypeEpic, Status: ticket.StatusOpen, Created: time.Now()},
	)
	// The serializer writes a status plain, which yaml refuses to read back
	// with a control character in it; a remote can hand over the quoted form.
	child := "---\nid: ch-0002\nstatus: \"\\e[2J\\e[Hdone\\u202e\"\ntype: feature\nparent: ep-0001\ncreated: 2026-01-01T00:00:00Z\n---\n# Child\n"
	if err := os.WriteFile(filepath.Join(dir, "ch-0002.md"), []byte(child), 0o644); err != nil {
		t.Fatalf("write ch-0002: %v", err)
	}
	listed, snap := loadBoardFixture(t, "proj", dir)
	a := onTab(t, boardAppOver("proj", dir, tabAll, 120, 24, listed, snap), tabAll, "ch-0002")
	a = press(t, a, "u")
	if a.overlay != overlayDetail || a.detail.qid != "proj/ep-0001" {
		t.Fatalf("u on the child opened overlay %v detail %q, want the epic", a.overlay, a.detail.qid)
	}
	a = press(t, a, "enter")
	if a.detail.input != inputChildPicker {
		t.Fatalf("enter on the epic detail opened input %v, want the child picker", a.detail.input)
	}
	for name, out := range map[string]string{
		"epic detail":  strings.Join(a.detail.lines, "\n"),
		"child picker": strings.Join(a.detail.pickerItems, "\n"),
	} {
		if !strings.Contains(out, "ch-0002") {
			t.Errorf("%s does not list the child:\n%q", name, out)
		}
		for _, r := range []rune{'\x1b', '\u202e'} {
			if strings.ContainsRune(out, r) {
				t.Errorf("%s contains control or format character %U:\n%q", name, r, out)
			}
		}
	}
}

// An epic's child list is unbounded, unlike the move picker's sibling repos:
// the child picker windows it to the frame so the cursor stays in view and
// the help bar stays on screen, and truncates each line so a long title
// does not wrap onto a row the frame did not budget for.
func TestChildPickerWindowsToFrame(t *testing.T) {
	const w, h = 60, 24
	tickets := []*ticket.Ticket{
		{ID: "ep-0001", Title: "Epic", Type: ticket.TypeEpic, Status: ticket.StatusOpen, Created: time.Now()},
	}
	for i := 0; i < 40; i++ {
		tickets = append(tickets, &ticket.Ticket{
			ID: fmt.Sprintf("ch-%04d", i+2), Title: strings.Repeat("long title ", 20),
			Type: ticket.TypeFeature, Status: ticket.StatusOpen, Parent: "ep-0001", Created: time.Now(),
		})
	}
	a := onTab(t, boardApp(t, "proj", tabAll, w, h, tickets...), tabAll, "ch-0002")
	a = press(t, a, "u")
	a = press(t, a, "enter")
	if a.detail.input != inputChildPicker {
		t.Fatalf("enter on the epic detail opened input %v, want the child picker", a.detail.input)
	}
	check := func(step string, h int) {
		t.Helper()
		view := a.detail.view()
		lines := strings.Split(view, "\n")
		if len(lines) > h {
			t.Errorf("%s: view is %d rows, frame is %d", step, len(lines), h)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > w {
				t.Errorf("%s: row wider than %d: %q", step, w, l)
			}
		}
		cursor := "> " + a.detail.pickerIDs[a.detail.pickerCursor]
		if !strings.Contains(view, cursor) {
			t.Errorf("%s: cursor row %q is not in the view:\n%s", step, cursor, view)
		}
		if !strings.Contains(view, "esc cancel") {
			t.Errorf("%s: help bar is not in the view:\n%s", step, view)
		}
	}
	check("opened", h)
	for i := 0; i < 39; i++ {
		a = press(t, a, "j")
	}
	check("at the last child", h)
	for i := 0; i < 20; i++ {
		a = press(t, a, "k")
	}
	check("scrolled back up", h)
	// Growing the terminal while the picker is scrolled to the end widens
	// the window past the list unless the offset is pulled back.
	for i := 0; i < 20; i++ {
		a = press(t, a, "j")
	}
	a.detail.setSize(w, h+20)
	check("grown at the last child", h+20)
}
