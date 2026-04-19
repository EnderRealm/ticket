// Package tui provides the interactive terminal UI for browsing and editing tickets.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

// ─── Tabs & Overlays ────────────────────────────────────────────────────────

type tabID int

const (
	tabInbox tabID = iota
	tabBacklog
	tabEpics
	tabDone
	tabAll
	tabCount // sentinel for cycling
)

var tabNames = []string{"inbox", "backlog", "epics", "done", "all"}

type overlayID int

const (
	overlayNone overlayID = iota
	overlayDetail
	overlayForm
)

// ─── App ────────────────────────────────────────────────────────────────────

// App is the top-level bubbletea model.
type App struct {
	store      *ticket.FileStore
	ticketsDir string
	projectName string
	tickets    []*ticket.Ticket

	// Views
	activeTab tabID
	overlay   overlayID
	dashboard dashboardModel // Tickets tab
	epics     epicsModel     // Epics tab
	detail    detailModel
	form      formModel

	// Command bar
	cmdBar    textinput.Model
	cmdActive bool

	// Layout
	width  int
	height int
	status string
	err    error
}

// New creates a new App rooted at the given ticket directory.
func New(ticketsDir string) App {
	store := ticket.NewFileStore(ticketsDir)

	// Derive project name from tickets directory path.
	absDir, _ := filepath.Abs(ticketsDir)
	baseName := filepath.Base(absDir)
	projectName := baseName
	if baseName == ".tickets" {
		// Local store: .tickets is inside the project root.
		projectName = filepath.Base(filepath.Dir(absDir))
	}

	// Initialize command bar.
	ti := textinput.New()
	ti.Placeholder = "Search or /command..."
	ti.CharLimit = 256

	return App{
		store:       store,
		ticketsDir:  ticketsDir,
		projectName: projectName,
		activeTab:   tabInbox,
		cmdBar:      ti,
	}
}

// ─── Messages ───────────────────────────────────────────────────────────────

type ticketsLoadedMsg []*ticket.Ticket
type errMsg error
type statusMsg string
type clearStatusMsg struct{}

type cyclePriorityMsg struct{ id string }

type addNoteMsg struct {
	id   string
	text string
}

type deleteTicketMsg struct {
	id string
}

type moveTicketMsg struct {
	id         string
	targetRepo string
}

func loadTickets(store *ticket.FileStore) tea.Cmd {
	return func() tea.Msg {
		tickets, err := store.List()
		if err != nil {
			return errMsg(err)
		}
		ticket.SortByStatusPriorityID(tickets)
		return ticketsLoadedMsg(tickets)
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

// ─── Init ───────────────────────────────────────────────────────────────────

func (a App) Init() tea.Cmd {
	return tea.Batch(
		loadTickets(a.store),
		watchTickets(a.ticketsDir),
	)
}

// ─── Update ─────────────────────────────────────────────────────────────────

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		contentH := a.contentHeight()
		a.dashboard.setSize(a.width, contentH)
		a.epics.setSize(a.width, contentH)
		a.detail.setSize(a.width, a.height) // overlays use full height
		a.form.setSize(a.width, a.height)
		return a, nil

	case fileChangedMsg:
		return a, tea.Batch(
			loadTickets(a.store),
			watchTickets(a.ticketsDir),
		)

	case ticketsLoadedMsg:
		a.tickets = msg
		a.dashboard.activeTab = a.activeTab
		a.dashboard.refreshTickets(a.tickets)
		a.epics.refreshTickets(a.tickets)
		if a.overlay == overlayDetail && a.detail.ticket != nil {
			found := false
			for _, t := range a.tickets {
				if t.ID == a.detail.ticket.ID {
					a.detail = newDetailModel(t, a.width, a.contentHeight())
					found = true
					break
				}
			}
			if !found {
				a.overlay = overlayNone
				a.status = "Ticket removed"
			}
		}
		return a, nil

	case errMsg:
		a.err = msg
		return a, tea.Quit

	case statusMsg:
		a.status = string(msg)
		return a, clearStatusAfter(3 * time.Second)

	case clearStatusMsg:
		a.status = ""
		return a, nil

	// Mutation messages
	case cyclePriorityMsg:
		return a, a.handleCyclePriority(msg.id)
	case addNoteMsg:
		return a, a.handleAddNote(msg.id, msg.text)
	case formSubmitMsg:
		if msg.editID != "" {
			return a, a.handleEditTicket(msg)
		}
		return a, a.handleCreateTicket(msg)
	case deleteTicketMsg:
		return a, a.handleDelete(msg.id)
	case moveTicketMsg:
		return a, a.handleMove(msg.id, msg.targetRepo)
	case formCancelMsg:
		a.overlay = overlayNone
		return a, nil

	case tea.KeyMsg:
		// Command bar input takes priority when active.
		if a.cmdActive {
			return a.updateCommandBar(msg)
		}

		// Ctrl+K toggles command bar.
		if msg.String() == "ctrl+k" {
			a.cmdActive = true
			cmd := a.cmdBar.Focus()
			return a, cmd
		}

		// Overlay input takes priority.
		if a.overlay != overlayNone {
			return a.updateOverlay(msg)
		}

		// Tab-level keys.
		return a.updateTab(msg)
	}

	// Delegate to active view for non-key messages.
	if a.overlay != overlayNone {
		return a.delegateOverlay(msg)
	}
	return a.delegateTab(msg)
}

// ─── Command Bar ────────────────────────────────────────────────────────────

func (a App) updateCommandBar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.cmdActive = false
		a.cmdBar.Blur()
		a.cmdBar.SetValue("")
		return a, nil
	case "enter":
		val := a.cmdBar.Value()
		a.cmdActive = false
		a.cmdBar.Blur()
		a.cmdBar.SetValue("")

		if strings.HasPrefix(val, "/") {
			// Command dispatch (stubbed).
			cmd := strings.TrimPrefix(val, "/")
			a.status = fmt.Sprintf("Unknown command: /%s", cmd)
			return a, clearStatusAfter(3 * time.Second)
		}

		// Search: apply filter to dashboard.
		if a.isTicketTab() {
			a.dashboard.filterText = val
			a.dashboard.refreshTickets(a.tickets)
		}
		return a, nil
	}

	var cmd tea.Cmd
	a.cmdBar, cmd = a.cmdBar.Update(msg)
	return a, cmd
}

// ─── Overlay Updates ────────────────────────────────────────────────────────

func (a App) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.overlay {
	case overlayDetail:
		if a.detail.inputActive() {
			break // let detail handle its text input
		}
		switch msg.String() {
		case "esc":
			a.overlay = overlayNone
			return a, nil
		case "q":
			return a, tea.Quit
		case "p":
			return a, func() tea.Msg { return cyclePriorityMsg{id: a.detail.ticket.ID} }
		case "n":
			a.detail.startInput(inputNote)
			return a, nil
		case "m":
			a.detail.startMovePicker(a.ticketsDir)
			return a, nil
		case "e":
			a.form = newEditFormModel(a.detail.ticket, a.width, a.contentHeight())
			a.overlay = overlayForm
			return a, nil
		}

	case overlayForm:
		// Form handles its own keys; just check for escape.
		// (formCancelMsg and formSubmitMsg handled at App level)
	}

	// Delegate to overlay model.
	return a.delegateOverlay(msg)
}

func (a App) delegateOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch a.overlay {
	case overlayDetail:
		var cmd tea.Cmd
		a.detail, cmd = a.detail.update(msg)
		return a, cmd
	case overlayForm:
		var cmd tea.Cmd
		a.form, cmd = a.form.update(msg)
		return a, cmd
	}
	return a, nil
}

// ─── Tab Updates ────────────────────────────────────────────────────────────

func (a App) updateTab(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When dashboard has active input (filter or delete confirm), delegate
	// immediately so keystrokes reach the input instead of triggering shortcuts.
	if a.activeTab != tabEpics && a.dashboard.inputActive() {
		return a.delegateTab(msg)
	}

	// Global keys.
	switch msg.String() {
	case "q":
		return a, tea.Quit
	case "tab":
		a.activeTab = (a.activeTab + 1) % tabCount
		a.syncDashboardTab()
		return a, nil
	case "shift+tab":
		a.activeTab = (a.activeTab - 1 + tabCount) % tabCount
		a.syncDashboardTab()
		return a, nil
	case "c":
		a.form = newFormModel(a.width, a.contentHeight())
		a.overlay = overlayForm
		return a, nil
	}

	// Tab-specific keys.
	if a.activeTab == tabEpics {
		switch msg.String() {
		case "enter", "o":
			if t := a.epics.selectedTicket(); t != nil {
				if t.Type == ticket.TypeEpic {
					a.epics.toggleExpand()
					return a, nil
				}
				a.detail = newDetailModel(t, a.width, a.height)
				a.overlay = overlayDetail
				return a, nil
			}
		case " ":
			a.epics.toggleExpand()
			return a, nil
		}
	} else {
		// Ticket tabs (backlog, triage, inbox, done, all).
		switch msg.String() {
		case "enter", "o":
			if t := a.dashboard.selected(); t != nil {
				// Epics on the backlog tab are rollups; clicking one jumps
				// to the epics tab focused on that epic rather than opening
				// a detail overlay here.
				if a.activeTab == tabBacklog && t.Type == ticket.TypeEpic {
					epicID := t.ID
					a.activeTab = tabEpics
					a.syncDashboardTab()
					a.epics.focusEpic(epicID)
					return a, nil
				}
				a.detail = newDetailModel(t, a.width, a.height)
				a.overlay = overlayDetail
				return a, nil
			}
		case "e":
			if t := a.dashboard.selected(); t != nil {
				a.form = newEditFormModel(t, a.width, a.height)
				a.overlay = overlayForm
				return a, nil
			}
		case "m":
			if t := a.dashboard.selected(); t != nil {
				a.detail = newDetailModel(t, a.width, a.height)
				a.detail.startMovePicker(a.ticketsDir)
				a.overlay = overlayDetail
				return a, nil
			}
		case "d":
			if t := a.dashboard.selected(); t != nil {
				a.dashboard.confirmDelete = true
				a.dashboard.deleteTargetID = t.ID
				return a, nil
			}
		case "p":
			if t := a.dashboard.selected(); t != nil {
				return a, func() tea.Msg { return cyclePriorityMsg{id: t.ID} }
			}
		}
	}

	// Delegate to active tab.
	return a.delegateTab(msg)
}

func (a App) delegateTab(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.activeTab == tabEpics {
		var cmd tea.Cmd
		a.epics, cmd = a.epics.update(msg)
		return a, cmd
	}
	// All other tabs use the dashboard.
	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.update(msg)
	return a, cmd
}

// ─── View ───────────────────────────────────────────────────────────────────

func (a App) View() string {
	if a.err != nil {
		return fmt.Sprintf("Error: %v\n", a.err)
	}

	pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)

	var b strings.Builder
	sepStyle := lipgloss.NewStyle().Foreground(colorSubtle)

	// Project name + tab bar on same line.
	name := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(a.projectName)
	tabs := a.renderTabBar()
	b.WriteString(" " + name + "  " + StyleDim.Render("—") + "  " + tabs)
	b.WriteString("\n")
	b.WriteString(sepStyle.Render(strings.Repeat("─", a.width)))
	b.WriteString("\n")

	// Tab content.
	var content string
	if a.activeTab == tabEpics {
		content = a.epics.view()
	} else {
		content = a.dashboard.view()
	}

	// If overlay active, dim the background content.
	if a.overlay != overlayNone {
		content = StyleDim.Render(content)
		content += "\n" + a.renderOverlay()
	}

	b.WriteString(content)

	// Bottom separator and status/help bar.
	b.WriteString(lipgloss.NewStyle().Foreground(colorSubtle).Render(strings.Repeat("─", a.width)))
	b.WriteString("\n")
	if a.cmdActive {
		b.WriteString(pad.Render(a.renderCommandBar()))
	} else if a.status != "" {
		b.WriteString(pad.Render(StyleWarning.Render(a.status)))
	} else {
		// Filter info + help on the same line.
		filter := a.renderFilterInfo()
		help := a.renderHelp()
		if filter != "" {
			b.WriteString(pad.Render(filter + "  │  " + help))
		} else {
			b.WriteString(pad.Render(help))
		}
	}

	return b.String()
}

// ─── Render Components ──────────────────────────────────────────────────────


var tabColors = []lipgloss.Color{
	colorCyan,    // inbox
	colorGray,    // backlog
	colorMagenta, // epics
	colorGreen,   // done
	colorYellow,  // all
}

func (a App) tabCounts() map[tabID]int {
	counts := make(map[tabID]int)

	// Mirror dashboard.buildItems filtering so counts match what each tab shows.
	byID := make(map[string]*ticket.Ticket, len(a.tickets))
	for _, t := range a.tickets {
		byID[t.ID] = t
	}

	for _, t := range a.tickets {
		isEpic := t.Type == ticket.TypeEpic
		hasEpicAncestor := !isEpic && nearestEpicAncestor(t, byID) != ""
		done := t.Status == ticket.StatusDone || t.Status == ticket.StatusClosed

		switch t.Status {
		case ticket.StatusBacklog:
			// Backlog shows epics as rollups; descendants roll up under them.
			if !hasEpicAncestor {
				counts[tabBacklog]++
			}
		case ticket.StatusDone, ticket.StatusClosed:
			// Done includes epics.
			counts[tabDone]++
		case ticket.StatusOpen, ticket.StatusReady:
			if !isEpic {
				counts[tabInbox]++
			}
		}

		if isEpic && !done {
			counts[tabEpics]++
		}
		if !done && !isEpic {
			counts[tabAll]++
		}
	}
	return counts
}

func (a App) renderTabBar() string {
	counts := a.tabCounts()
	var parts []string
	for i, name := range tabNames {
		c := tabColors[i]
		label := fmt.Sprintf("%s (%d)", name, counts[tabID(i)])
		if tabID(i) == a.activeTab {
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlack).
				Background(c).
				Padding(0, 1).
				Render(label))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(c).
				Padding(0, 1).
				Render(label))
		}
	}
	return strings.Join(parts, " ")
}

func (a App) renderOverlay() string {
	switch a.overlay {
	case overlayDetail:
		return a.detail.view()
	case overlayForm:
		return a.form.view()
	}
	return ""
}

func (a App) renderCommandBar() string {
	prompt := StyleInputLabel.Render("❯ ")
	return prompt + a.cmdBar.View()
}

func (a App) renderFilterInfo() string {
	if !a.isTicketTab() {
		return ""
	}
	if a.dashboard.filterActive {
		return StyleFilter.Render("/ " + a.dashboard.filterText + "█")
	}
	if a.dashboard.filterText != "" {
		return StyleFilter.Render("filter: " + a.dashboard.filterText + "  (/ edit, esc clear)")
	}
	var parts []string
	if a.dashboard.typeFilter != "" {
		parts = append(parts, fmt.Sprintf("type: %s", a.dashboard.typeFilter))
	} else {
		parts = append(parts, "all types")
	}
	parts = append(parts, "(t) type  (/) search")
	return StyleFilter.Render(strings.Join(parts, "  "))
}

func (a App) renderHelp() string {
	var help string
	if a.activeTab == tabEpics {
		help = "↑↓ select  enter expand  │  tab/shift+tab  ctrl+k search  (c)reate  (q)uit"
	} else {
		if a.dashboard.confirmDelete {
			return ""
		}
		help = "↑↓ select  │  enter (o)pen (c)reate (e)dit  │  (p)riority (m)ove (d)elete  │  tab/shift+tab  ctrl+k search  (q)uit"
	}
	return StyleHelp.Render(help)
}

// isTicketTab returns true if the active tab shows ticket list (not epics).
func (a App) isTicketTab() bool {
	return a.activeTab != tabEpics
}

// syncDashboardTab updates the dashboard's activeTab to match the app tab.
func (a *App) syncDashboardTab() {
	a.dashboard.activeTab = a.activeTab
	a.dashboard.cursor = 0
	a.dashboard.offset = 0
	a.dashboard.buildItems()
}

// contentHeight returns the available height for tab/overlay content,
// excluding header+tabs (1), separator (1), bottom separator (1), and help bar (1).
func (a App) contentHeight() int {
	h := a.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

// ─── Mutation Handlers ──────────────────────────────────────────────────────

func (a *App) handleCyclePriority(id string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Priority = (t.Priority + 1) % 5
	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s -> P%d", id, t.Priority)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleAddNote(id, text string) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Notes = append(t.Notes, ticket.Note{
		Timestamp: time.Now().UTC(),
		Text:      text,
	})

	if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
		t.Body = t.Body[:idx+1]
	} else if strings.HasPrefix(t.Body, "## Notes\n") {
		t.Body = "\n"
	}

	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("Note added to %s", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}


func (a *App) handleCreateTicket(msg formSubmitMsg) tea.Cmd {
	t := &ticket.Ticket{
		ID:       ticket.GenerateID(msg.title),
		Title:    msg.title,
		Type:     msg.ticketType,
		Priority: msg.priority,
		Status:   ticket.StatusBacklog,
		Created:  time.Now().UTC(),
	}

	if msg.description != "" {
		t.Body = msg.description + "\n"
	}

	if err := a.store.Create(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	a.overlay = overlayNone
	status := fmt.Sprintf("Created %s: %s", t.ID, t.Title)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(status) },
	)
}

func (a *App) handleEditTicket(msg formSubmitMsg) tea.Cmd {
	t, err := a.store.Get(msg.editID)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Title = msg.title
	t.Type = msg.ticketType
	t.Priority = msg.priority
	if msg.status != "" {
		t.Status = msg.status
	}

	t.Body = ticket.UpdateSection(t.Body, "", msg.description)

	if msg.note != "" {
		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      msg.note,
		})
		if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
			t.Body = t.Body[:idx+1]
		} else if strings.HasPrefix(t.Body, "## Notes\n") {
			t.Body = "\n"
		}
	}

	if err := a.store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	a.overlay = overlayNone
	status := fmt.Sprintf("Updated %s", t.ID)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(status) },
	)
}

func (a *App) handleDelete(id string) tea.Cmd {
	if err := a.store.Delete(id); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("Deleted %s", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleMove(id, targetRepo string) tea.Cmd {
	targetDir := filepath.Join(targetRepo, ".tickets")
	dst := ticket.NewFileStore(targetDir)

	results, err := ticket.MoveTicket(a.store, dst, id, false)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	var parts []string
	for _, r := range results {
		parts = append(parts, fmt.Sprintf("%s → %s", r.OldID, r.NewID))
	}
	msg := fmt.Sprintf("Moved %s to %s", strings.Join(parts, ", "), targetRepo)
	a.overlay = overlayNone
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

