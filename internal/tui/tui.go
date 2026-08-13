// Package tui provides the interactive terminal UI for browsing and editing tickets.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	store        *ticket.FileStore
	ticketsDir   string
	projectName  string
	unregistered bool
	version      string
	cwd          string
	workDir      string
	spawnCommand string
	tickets      []*ticket.Ticket

	// Views
	activeTab tabID
	overlay   overlayID
	dashboard dashboardModel // every tab: the shared table
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

// New creates a new App rooted at the given ticket directory. project is the
// namespace the store answers to, resolved by the caller, and the name the
// header and a spawned work session are given — deriving it a second time from
// the tickets directory's basename produced the same string by construction.
// workDir is the project's real repo directory (also resolved by the caller
// from config), used to spawn `/work` sessions in the right place. unregistered
// marks a central project with no `store: central` entry in config — an entry
// carrying only a path is not a registration; the header carries it for the
// whole session, since the alt screen hides a warning printed at startup.
func New(ticketsDir, project, version, spawnCommand, workDir string, unregistered bool) App {
	store := ticket.NewProjectFileStore(ticketsDir, project)

	// Capture launch directory, abbreviating $HOME to ~.
	cwd, _ := os.Getwd()
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if cwd == home {
			cwd = "~"
		} else if strings.HasPrefix(cwd, home+string(os.PathSeparator)) {
			cwd = "~" + cwd[len(home):]
		}
	}

	// Initialize command bar.
	ti := textinput.New()
	ti.Placeholder = "Search or /command..."
	ti.CharLimit = 256

	a := App{
		store:        store,
		ticketsDir:   ticketsDir,
		projectName:  project,
		unregistered: unregistered,
		version:      version,
		cwd:          cwd,
		workDir:      workDir,
		spawnCommand: spawnCommand,
		activeTab:    tabInbox,
		cmdBar:       ti,
	}
	a.dashboard.activeTab = tabInbox
	a.dashboard.sortIdx, a.dashboard.sortDir = defaultSort(tabInbox)
	return a
}

// ─── Messages ───────────────────────────────────────────────────────────────

type ticketsLoadedMsg []*ticket.Ticket
type errMsg error
type statusMsg string
type clearStatusMsg struct{}

type cyclePriorityMsg struct{ id string }

type setStatusMsg struct {
	id     string
	status ticket.Status
}

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
		if a.overlay == overlayDetail && a.detail.ticket != nil {
			// If the user is mid-input (move picker, note entry, etc.), don't
			// disturb them — a background refresh should never reset their
			// current action.
			if a.detail.inputActive() {
				return a, nil
			}
			found := false
			for _, t := range a.tickets {
				if t.ID == a.detail.ticket.ID {
					prev := a.detail
					a.detail = newDetailModel(t, a.width, a.contentHeight())
					a.detail.offset = prev.offset
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
	case setStatusMsg:
		return a, a.handleSetStatus(msg.id, msg.status)
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
		a.dashboard.filterText = val
		a.dashboard.refreshTickets(a.tickets)
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
			a.detail.startMovePicker(a.workDir)
			return a, nil
		case "e":
			a.form = newEditFormModel(a.detail.ticket, a.width, a.contentHeight())
			a.overlay = overlayForm
			return a, nil
		case "y":
			return a, yankID(a.detail.ticket.ID)
		case "w":
			return a, a.spawnWork(a.detail.ticket)
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
	if a.dashboard.inputActive() {
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

	// Epics tab: space and enter expand the epic group at the cursor. A child
	// row is not a group, so enter on one falls through to opening it.
	if a.activeTab == tabEpics {
		switch msg.String() {
		case " ":
			a.dashboard.toggleExpand()
			return a, nil
		case "enter", "o":
			if a.dashboard.toggleExpand() {
				return a, nil
			}
		}
	}

	// Row keys, shared by every tab.
	switch msg.String() {
	case "enter", "o":
		if t := a.dashboard.selected(); t != nil {
			a.openDashboardTicket(t)
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
			a.detail.startMovePicker(a.workDir)
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
	case "r":
		if a.activeTab == tabBacklog {
			if t := a.dashboard.selected(); t != nil {
				return a, func() tea.Msg { return setStatusMsg{id: t.ID, status: ticket.StatusReady} }
			}
		}
	case "b":
		if a.activeTab == tabInbox {
			if t := a.dashboard.selected(); t != nil {
				return a, func() tea.Msg { return setStatusMsg{id: t.ID, status: ticket.StatusBacklog} }
			}
		}
	case "x":
		if a.activeTab == tabInbox {
			if t := a.dashboard.selected(); t != nil {
				return a, func() tea.Msg { return setStatusMsg{id: t.ID, status: ticket.StatusDone} }
			}
		}
	case "y":
		if t := a.dashboard.selected(); t != nil {
			return a, yankID(t.ID)
		}
	case "w":
		if t := a.dashboard.selected(); t != nil {
			return a, a.spawnWork(t)
		}
	}

	// Delegate to active tab.
	return a.delegateTab(msg)
}

func (a App) delegateTab(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	a.dashboard, cmd = a.dashboard.update(msg)
	return a, cmd
}

// ─── View ───────────────────────────────────────────────────────────────────

func (a App) View() string {
	if a.err != nil {
		return fmt.Sprintf("Error: %v\n", a.err)
	}

	var b strings.Builder
	sepStyle := lipgloss.NewStyle().Foreground(colorSubtle)

	// Build the footer first so content can reserve exactly the rows it needs.
	// View has a value receiver, so re-applying content sizing here keeps the
	// frame == a.height regardless of transient state. This View-time sizing is
	// authoritative for layout; the Update-time contentHeight() only feeds the
	// scroll math, which a stale-by-one footer state can never push to overflow.
	footer, footerLines := a.footerView()
	contentH := a.height - 3 - footerLines // header(1) + topsep(1) + botsep(1) + footer
	if contentH < 1 {
		contentH = 1
	}
	a.dashboard.setSize(a.width, contentH)

	// Project name + tab bar on same line, with an info segment flush right.
	name := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(a.projectName)
	tabs := a.renderTabBar()
	tail := "  " + StyleDim.Render("—") + "  " + tabs
	left := " " + name + tail
	if a.unregistered {
		// Only when it fits: the tab bar alone is ~58 columns, so on an 80-column
		// terminal the marker can push the row past the width, and a wrapped
		// header makes the frame taller than a.height and shifts the whole
		// alt-screen layout — the degraded state the marker exists to announce.
		if marked := " " + name + " " + StyleWarning.Render("(unregistered)") + tail; lipgloss.Width(marked) <= a.width {
			left = marked
		}
	}
	right := a.renderHeaderInfo()
	if right != "" {
		gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap >= 1 {
			left += strings.Repeat(" ", gap) + right
		}
	}
	b.WriteString(left)
	b.WriteString("\n")
	b.WriteString(sepStyle.Render(strings.Repeat("─", a.width)))
	b.WriteString("\n")

	// Tab content.
	content := a.dashboard.view()

	// If overlay active, dim the background content.
	if a.overlay != overlayNone {
		content = StyleDim.Render(content)
		content += "\n" + a.renderOverlay()
	}

	b.WriteString(content)

	// Overlays (detail/form) render their own footer; the list-view separator
	// and command/help bar only belong to the dashboard and epics tabs.
	if a.overlay == overlayNone {
		b.WriteString(lipgloss.NewStyle().Foreground(colorSubtle).Render(strings.Repeat("─", a.width)))
		b.WriteString("\n")
		b.WriteString(footer)
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

// tabCounts is what each tab shows, counted with the same tabShows the
// dashboard builds its rows from and under the filters the dashboard has
// active — a count and its tab agree, filtered or not, with the one divergence
// tabShows documents: on the epics tab a count is the number of epic groups,
// so an expanded group renders more lines than it is counted as.
func (a App) tabCounts() map[tabID]int {
	epics := bareEpicIDs(a.tickets)
	needle := strings.ToLower(a.dashboard.filterText)
	counts := make(map[tabID]int)
	for tab := tabInbox; tab < tabCount; tab++ {
		for _, t := range a.tickets {
			if tabShows(tab, t, epics, a.dashboard.typeFilter, needle) {
				counts[tab]++
			}
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

// renderHeaderInfo builds the right-aligned header segment: cwd, version, and
// ticket counts by status. Returns "" when there is no room for it.
func (a App) renderHeaderInfo() string {
	var open, ready, backlog, done int
	for _, t := range a.tickets {
		switch t.Status {
		case ticket.StatusOpen:
			open++
		case ticket.StatusReady:
			ready++
		case ticket.StatusBacklog:
			backlog++
		case ticket.StatusDone, ticket.StatusClosed:
			done++
		}
	}
	counts := fmt.Sprintf("open %d · ready %d · backlog %d · done %d", open, ready, backlog, done)

	sep := StyleDim.Render(" · ")
	var parts []string
	if a.cwd != "" {
		parts = append(parts, StyleDim.Render(a.cwd))
	}
	if a.version != "" {
		parts = append(parts, StyleDim.Render(a.version))
	}
	parts = append(parts, StyleDim.Render(counts))
	seg := strings.Join(parts, sep)

	// Drop the segment entirely if it cannot fit alongside a minimal left header.
	if lipgloss.Width(seg) > a.width {
		return ""
	}
	return seg + " "
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

// filterInfoText returns the unstyled filter segment for the current state, so
// footerView can combine it with the help text for wrapping.
func (a App) filterInfoText() string {
	if a.dashboard.filterActive {
		return "/ " + a.dashboard.filterText + "█"
	}
	if a.dashboard.filterText != "" {
		return "filter: " + a.dashboard.filterText + "  (/ edit, esc clear)"
	}
	typeFilter := a.dashboard.typeFilter
	if typeFilter == "" {
		typeFilter = "all"
	}
	return fmt.Sprintf("(t)ype: %s  (/) search", typeFilter)
}

// helpText returns the unstyled help string for the current state, used by
// footerView so it can wrap the plain text and style each wrapped line.
func (a App) helpText() string {
	if a.dashboard.confirmDelete {
		return ""
	}
	// While searching, most shortcuts type into the filter; show only the
	// keys that actually work in search mode.
	if a.dashboard.filterActive {
		return "↑↓ select  enter apply  esc clear"
	}
	// Every tab is the same table, so it takes the same keys; only enter differs,
	// expanding an epic group rather than opening it.
	action := "enter (o)pen"
	if a.activeTab == tabEpics {
		action = "enter expand"
	}
	status := ""
	switch a.activeTab {
	case tabBacklog:
		status = "(r)eady "
	case tabInbox:
		status = "(b)acklog (x)done "
	}
	return "↑↓ select  │  " + action + " (c)reate (e)dit  │  " + status + "(p)riority (m)ove (d)elete (y)ank (w)ork (s)ort (S)dir  │  tab/shift+tab  ctrl+k search  (q)uit"
}

func (a App) renderHelp() string {
	return StyleHelp.Render(a.helpText())
}

// footerView builds the bottom command/help bar for the current state, wrapped
// to the window width. Returns the rendered block (styled, newline-joined) and
// its line count so the content area can reserve the right number of rows. The
// command bar and status are always single line; the filter+help bar wraps when
// it overflows the terminal width.
func (a App) footerView() (string, int) {
	pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)

	if a.cmdActive {
		return pad.Render(a.renderCommandBar()), 1
	}
	if a.status != "" {
		return pad.Render(StyleWarning.Render(a.status)), 1
	}

	// Usable footer width inside the 1-col horizontal padding. Clamp to >=1 so
	// the wrapText call below never gets a negative width (a.width is 0 before
	// the first WindowSizeMsg).
	width := a.width - 2
	if width < 1 {
		width = 1
	}

	filter := a.filterInfoText()
	help := a.helpText()

	// Common wide case: keep the existing styled "filter │ help" form when it
	// fits on one line, preserving the StyleFilter coloring on the filter
	// segment. Drop the separator when help is empty (e.g. confirmDelete) so the
	// footer doesn't trail a dangling "│".
	if filter != "" {
		sep := "  │  "
		if help == "" {
			sep = ""
		}
		combined := filter + sep + help
		if lipgloss.Width(combined) <= width {
			return pad.Render(StyleFilter.Render(filter) + sep + StyleHelp.Render(help)), 1
		}
		// Too narrow: wrap the combined plain text, but keep the filter segment
		// highlighted (StyleFilter) and the rest muted (StyleHelp) even when the
		// boundary falls mid-line, so the highlight survives wrapping.
		filterEnd := len([]rune(filter))
		wrapped := wrapText(combined, width)
		lines := make([]string, len(wrapped))
		for i, wl := range wrapped {
			lines[i] = pad.Render(styleFooterLine(wl, filterEnd))
		}
		return strings.Join(lines, "\n"), len(lines)
	}

	lines := wrapHelp(help, width)
	for i, l := range lines {
		lines[i] = pad.Render(StyleHelp.Render(l))
	}
	return strings.Join(lines, "\n"), len(lines)
}

// styleFooterLine colors a wrapped footer line: original runes [0,filterEnd)
// belong to the filter segment (StyleFilter), the rest is muted help text
// (StyleHelp). wrapText guarantees each line's text maps contiguously from its
// start offset, so the boundary is just filterEnd-start within the line.
func styleFooterLine(wl wrappedLine, filterEnd int) string {
	runes := []rune(wl.text)
	cut := filterEnd - wl.start
	if cut < 0 {
		cut = 0
	}
	if cut > len(runes) {
		cut = len(runes)
	}
	switch {
	case cut == 0:
		return StyleHelp.Render(string(runes))
	case cut == len(runes):
		return StyleFilter.Render(string(runes))
	default:
		return StyleFilter.Render(string(runes[:cut])) + StyleHelp.Render(string(runes[cut:]))
	}
}

// openDashboardTicket opens a ticket selected from a ticket tab. Epics on the
// backlog tab are rollups: opening one jumps to the epics tab focused on that
// epic rather than showing a detail overlay.
func (a *App) openDashboardTicket(t *ticket.Ticket) {
	if a.activeTab == tabBacklog && t.Type == ticket.TypeEpic {
		epicID := t.ID
		a.activeTab = tabEpics
		a.syncDashboardTab()
		a.dashboard.focusEpic(epicID)
		return
	}
	a.detail = newDetailModel(t, a.width, a.height)
	a.overlay = overlayDetail
}

// syncDashboardTab updates the dashboard's activeTab to match the app tab.
func (a *App) syncDashboardTab() {
	a.dashboard.activeTab = a.activeTab
	a.dashboard.cursor = 0
	a.dashboard.offset = 0
	a.dashboard.sortIdx, a.dashboard.sortDir = defaultSort(a.activeTab)
	a.dashboard.buildItems()
}

// contentHeight returns the available height for tab/overlay content,
// excluding header+tabs (1), separator (1), bottom separator (1), and the help
// bar — which may wrap to multiple lines on a narrow terminal.
func (a App) contentHeight() int {
	_, footerLines := a.footerView()
	h := a.height - 3 - footerLines
	if h < 1 {
		h = 1
	}
	return h
}

// yankID copies a ticket ID to the system clipboard, ready to paste into
// /work and other tk commands.
func yankID(id string) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteAll(id); err != nil {
			return statusMsg("error: " + err.Error())
		}
		return statusMsg("Copied ID")
	}
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

func (a *App) handleSetStatus(id string, status ticket.Status) tea.Cmd {
	t, err := a.store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Status = status
	closed, err := ticket.SaveEdit(a.store, t, true)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s -> %s%s", id, status, ticket.ClosedChildrenNote(closed))
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleAddNote(id, text string) tea.Cmd {
	// Through Mutate, unlike the TUI's other writes: a note is appended to what
	// the ticket already holds, so a plain read-modify-write would drop notes an
	// agent wrote while the TUI was open. The edits that set a field keep the
	// plain Update, where a conflict is something the user has to see.
	_, err := ticket.Mutate(a.store, id, func(t *ticket.Ticket) error {
		t.Notes = append(t.Notes, ticket.Note{
			Timestamp: time.Now().UTC(),
			Text:      text,
		})

		if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
			t.Body = t.Body[:idx+1]
		} else if strings.HasPrefix(t.Body, "## Notes\n") {
			t.Body = "\n"
		}
		return nil
	})
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("Note added to %s", id)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleCreateTicket(msg formSubmitMsg) tea.Cmd {
	newStatus := msg.status
	if newStatus == "" {
		newStatus = ticket.StatusBacklog
	}
	t := &ticket.Ticket{
		ID:       ticket.GenerateID(msg.title),
		Title:    msg.title,
		Type:     msg.ticketType,
		Priority: msg.priority,
		Status:   newStatus,
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
	// Only a status the user chose is applied: the form was seeded from the
	// in-memory list and this ticket was just re-read, so writing an untouched
	// selection back would assert whatever the row held when the form opened,
	// over whatever the ticket says now.
	if msg.statusSet {
		t.Status = msg.status
	}
	// Assigned unconditionally: the form is seeded with the current parent, so
	// an empty field means the user cleared it — half the remedy the one-level
	// rule's rejection names, and the half the TUI could not perform before.
	t.Parent = msg.parent

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

	closed, err := ticket.SaveEdit(a.store, t, msg.statusSet)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	a.overlay = overlayNone
	status := fmt.Sprintf("Updated %s%s", t.ID, ticket.ClosedChildrenNote(closed))
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
	// The same resolution `tk move` uses, so the two cannot land a ticket in
	// different places for the same target repo.
	dst, unregistered, err := ticket.ResolveStoreForRepo(targetRepo)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	results, err := ticket.MoveTicket(a.store, dst, id, false)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	var parts []string
	for _, r := range results {
		parts = append(parts, fmt.Sprintf("%s → %s", r.OldID, r.NewID))
	}
	msg := fmt.Sprintf("Moved %s to %s", strings.Join(parts, ", "), targetRepo)
	// The warning the CLI prints on stderr, carried on the status line: a
	// stderr write here lands in the alt screen and corrupts the frame.
	if unregistered {
		msg += "; " + ticket.UnregisteredWarning(dst)
	}
	a.overlay = overlayNone
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}
