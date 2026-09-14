// Package tui provides the interactive terminal UI for browsing and editing tickets.
package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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
	store *ticket.FileStore
	// multi is the whole central store, for a write to a ticket the board
	// does not own: an epic's foreign child opened from its detail.
	multi        *ticket.MultiStore
	ticketsDir   string
	projectName  string
	unregistered bool
	version      string
	cwd          string
	workDir      string
	spawnCommand string
	// execDir resolves the checkout a ticket in a namespace may run in, or
	// the reason there is none; every spawn goes through it, never workDir.
	execDir func(namespace string) (string, error)
	tickets []*ticket.Ticket
	// snap is the graph tickets were listed off: the board decides epic
	// membership on it and a detail reads relationships and foreign tickets
	// off it.
	snap *ticket.Snapshot

	// Views
	activeTab tabID
	overlay   overlayID
	dashboard dashboardModel // every tab: the shared table
	detail    detailModel
	// detailStack is the details navigation left open beneath the current
	// one — an epic behind the child opened from it — popped by esc.
	detailStack []detailModel
	form        formModel

	// Command bar
	cmdBar    textinput.Model
	cmdActive bool

	// Capture prompt: the one-line idea `c` hands to a /capture spawn, and the
	// namespace the spawn runs in — the board's from the list, the ticket's
	// own from a detail.
	captureBar    textinput.Model
	captureActive bool
	captureNS     string

	// Layout
	width  int
	height int
	status string
	// warning holds what the library's warning sink reported, apart from status
	// because a mutation emits its success message immediately after the write
	// the warning came from, and one status line cannot hold both.
	warning string
	err     error
}

// New creates a new App rooted at the given ticket directory. project is the
// namespace the store answers to, resolved by the caller, and the name the
// header and a spawned work session are given — deriving it a second time from
// the tickets directory's basename produced the same string by construction.
// workDir is the project's real repo directory (also resolved by the caller
// from config), which the move picker lists targets beside; "" on the Root
// board, which has none. unregistered marks a central project with no `store:
// central` entry in config — an entry carrying only a path is not a
// registration; the header carries it for the whole session, since the alt
// screen hides a warning printed at startup. execDir is the checkout resolver a
// spawn asks for the selected ticket's own namespace; nil is replaced by one
// that refuses, so a caller that wires none gets no spawn rather than a spawn
// in a directory the ticket did not name.
func New(ticketsDir, project, version, spawnCommand, workDir string, unregistered bool, execDir func(string) (string, error)) App {
	store := ticket.NewProjectFileStore(ticketsDir, project)
	if execDir == nil {
		execDir = func(string) (string, error) { return "", errors.New("no checkout resolver") }
	}

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

	// Initialize the capture prompt.
	ci := textinput.New()
	ci.Placeholder = "Describe the idea…"
	ci.CharLimit = 256

	a := App{
		store:        store,
		multi:        ticket.NewMultiStore(filepath.Dir(ticketsDir)),
		ticketsDir:   ticketsDir,
		projectName:  project,
		unregistered: unregistered,
		version:      version,
		cwd:          cwd,
		workDir:      workDir,
		spawnCommand: spawnCommand,
		execDir:      execDir,
		activeTab:    tabInbox,
		cmdBar:       ti,
		captureBar:   ci,
	}
	a.dashboard.activeTab = tabInbox
	a.dashboard.ns = project
	a.dashboard.sortIdx, a.dashboard.sortDir = defaultSort(tabInbox)
	return a
}

// ─── Messages ───────────────────────────────────────────────────────────────

// ticketsLoadedMsg carries one reading of the store: the board's own tickets
// and the graph they were listed off, so the rows and the membership they are
// grouped by cannot come from two different instants.
type ticketsLoadedMsg struct {
	tickets []*ticket.Ticket
	snap    *ticket.Snapshot
}
type errMsg error
type statusMsg string
type clearStatusMsg struct{}

// warnMsg carries one line from the library's warning sink. It is its own
// message so a success statusMsg cannot take its slot.
type warnMsg string
type clearWarnMsg struct{}

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

// openTicketMsg opens a ticket's detail by qualified ID off the graph, on top
// of the detail it was chosen from: the child picked in an epic's detail.
type openTicketMsg struct{ qid string }

func loadTickets(store *ticket.FileStore) tea.Cmd {
	return func() tea.Msg {
		snap, err := store.Snapshot()
		if err != nil {
			return errMsg(err)
		}
		tickets := store.ListFromSnapshot(snap)
		ticket.SortByStatusPriorityID(tickets)
		return ticketsLoadedMsg{tickets: tickets, snap: snap}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func clearWarnAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearWarnMsg{}
	})
}

// CaptureWarnings routes pkg/ticket's warnings to the TUI's warning row for the
// life of the program, returning the restore its caller defers. The alt screen
// owns the terminal, so the library's default stderr write would land on top of
// the rendered frame instead of reaching the user.
//
// The send is detached because the sink can be invoked from the event-loop
// goroutine itself: every TUI mutation runs its store write synchronously inside
// Update, and Send blocks on the unbuffered channel that same goroutine drains,
// so an inline call would hang the program in the alt screen with no keystroke
// or signal able to reach it. The detached send lands once Update returns, and
// falls through on the program's context after shutdown.
//
// The message is flattened to one line because View renders it as a single
// reserved row, the last line of the frame in both the overlay and non-overlay
// branches — the writes that warn usually leave an overlay open, where the
// footer is not rendered at all.
func CaptureWarnings(p *tea.Program) func() {
	prev := ticket.Warnf
	ticket.Warnf = func(format string, args ...any) {
		msg := warnMsg(strings.Join(strings.Fields(fmt.Sprintf(format, args...)), " "))
		go p.Send(msg)
	}
	return func() { ticket.Warnf = prev }
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
		a.tickets = msg.tickets
		a.snap = msg.snap
		a.dashboard.activeTab = a.activeTab
		a.dashboard.refreshTickets(a.tickets, a.snap)
		if a.overlay == overlayDetail && a.detail.ticket != nil {
			// If the user is mid-input (move picker, note entry, etc.), don't
			// disturb them — a background refresh should never reset their
			// current action.
			if a.detail.inputActive() {
				return a, nil
			}
			// Off the graph by qualified ID rather than the board by bare ID:
			// the detail may be a foreign ticket the board does not list.
			if refreshed, ok := a.detailFor(a.detail.qid); ok {
				refreshed.offset = a.detail.offset
				a.detail = refreshed
			} else {
				a.overlay = overlayNone
				a.detailStack = nil
				a.status = "Ticket removed"
			}
		}
		return a, nil

	case errMsg:
		a.err = msg
		return a, tea.Quit

	case statusMsg:
		// Sanitized here rather than at either render site: a status can quote a
		// ticket ID or title read off disk, and the footer and the overlay row
		// both render this value.
		a.status = ticket.SanitizeControl(string(msg))
		return a, clearStatusAfter(3 * time.Second)

	case clearStatusMsg:
		a.status = ""
		return a, nil

	case warnMsg:
		a.warning = string(msg)
		return a, clearWarnAfter(8 * time.Second)

	case clearWarnMsg:
		a.warning = ""
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
	case openTicketMsg:
		return a, a.pushDetail(msg.qid)
	case formCancelMsg:
		a.overlay = overlayNone
		return a, nil

	case tea.KeyMsg:
		// Command bar input takes priority when active.
		if a.cmdActive {
			return a.updateCommandBar(msg)
		}
		if a.captureActive {
			return a.updateCapturePrompt(msg)
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
		a.dashboard.refreshTickets(a.tickets, a.snap)
		return a, nil
	}

	var cmd tea.Cmd
	a.cmdBar, cmd = a.cmdBar.Update(msg)
	return a, cmd
}

// ─── Capture Prompt ─────────────────────────────────────────────────────────

// startCapture opens the capture prompt for a spawn into ns. A namespace
// with no checkout — Root, or a project registered elsewhere — is refused
// here, before the prompt opens, the way `w` refuses at the keypress: the
// idea would otherwise be typed and then thrown away on the same refusal.
// spawnCapture resolves the checkout again at the shell boundary; this one
// only spares the typing.
func (a App) startCapture(ns string) (App, tea.Cmd) {
	if _, err := a.execDir(ns); err != nil {
		return a, refuseSpawn(err.Error())
	}
	a.captureActive = true
	a.captureNS = ns
	return a, a.captureBar.Focus()
}

// updateCapturePrompt routes keys while the capture prompt is open: esc
// cancels with nothing spawned, enter spawns on the idea unless it is blank.
func (a App) updateCapturePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.captureActive = false
		a.captureBar.Blur()
		a.captureBar.SetValue("")
		return a, nil
	case "enter":
		idea := strings.TrimSpace(a.captureBar.Value())
		ns := a.captureNS
		a.captureActive = false
		a.captureBar.Blur()
		a.captureBar.SetValue("")
		if idea == "" {
			return a, nil
		}
		return a, a.spawnCapture(ns, idea)
	}

	var cmd tea.Cmd
	a.captureBar, cmd = a.captureBar.Update(msg)
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
			if n := len(a.detailStack); n > 0 {
				// Back to the detail this one was opened from, re-read off the
				// graph so a write made here is reflected there.
				prev := a.detailStack[n-1]
				a.detailStack = a.detailStack[:n-1]
				if refreshed, ok := a.detailFor(prev.qid); ok {
					refreshed.offset = prev.offset
					prev = refreshed
				}
				a.detail = prev
				return a, nil
			}
			a.overlay = overlayNone
			return a, nil
		case "q":
			return a, tea.Quit
		case "p":
			qid := a.detail.qid
			return a, func() tea.Msg { return cyclePriorityMsg{id: qid} }
		case "n":
			a.detail.startInput(inputNote)
			return a, nil
		case "m":
			// MoveTicket takes the source project store, and this board's is
			// not the foreign ticket's.
			if a.detail.ns != a.projectName {
				return a, func() tea.Msg { return statusMsg("move from the ticket's own project board") }
			}
			a.detail.startMovePicker(a.workDir)
			return a, nil
		case "e":
			a.form = newEditFormModel(a.detail.ticket, a.width, a.contentHeight())
			a.overlay = overlayForm
			return a, nil
		case "y":
			return a, yankID(a.detail.ticket.ID)
		case "w":
			return a, a.spawnWork(a.detail.ticket, a.detail.qid)
		case "c":
			return a.startCapture(a.detail.ns)
		case "u":
			return a, a.openParent(a.detail.ticket, a.detail.ns)
		case "enter":
			if a.detail.ticket.Type == ticket.TypeEpic {
				if !a.detail.startChildPicker() {
					return a, func() tea.Msg { return statusMsg("No children") }
				}
				return a, nil
			}
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
	case "n":
		a.form = newFormModel(a.width, a.contentHeight())
		a.overlay = overlayForm
		return a, nil
	case "c":
		return a.startCapture(a.projectName)
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
			a.detail = newDetailModel(t, a.dashboard.qid(t), a.snap, a.width, a.height)
			a.detail.startMovePicker(a.workDir)
			a.detailStack = nil
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
			return a, a.spawnWork(t, a.dashboard.qid(t))
		}
	case "u":
		if t := a.dashboard.selected(); t != nil {
			return a, a.openParent(t, a.projectName)
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
	captureRow, captureLines := "", 0
	if a.overlay != overlayNone && a.captureActive {
		captureRow, captureLines = a.capturePromptView(), 1
	}
	statusRows, statusLines := "", 0
	if a.overlay != overlayNone && a.status != "" {
		statusRows, statusLines = a.statusView()
	}
	warnLines := 0
	if a.warning != "" {
		warnLines = 1
	}
	contentH := a.height - 3 - footerLines - captureLines - statusLines - warnLines // header(1) + topsep(1) + botsep(1) + footer + capture + status + warning
	if contentH < 1 {
		contentH = 1
	}
	a.dashboard.setSize(a.width, contentH)

	// Project name + tab bar on same line, with an info segment flush right.
	name := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(a.displayName())
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

	// An overlay renders its own footer, so footerView — the only site that
	// renders the capture prompt and a.status — never runs in that branch: the
	// prompt `c` opened from a detail would be invisible, and a store rejection
	// of a save would leave the form open with no feedback at all. Kept above
	// the warning row so the warning keeps the frame's last line.
	if captureLines > 0 {
		b.WriteString("\n")
		b.WriteString(captureRow)
	}
	if statusLines > 0 {
		b.WriteString("\n")
		b.WriteString(statusRows)
	}

	// Last line of the frame in either branch: an overlay renders its own footer
	// over the whole height, so this is the only row a warning is sure to reach
	// the user on, and the renderer keeps the frame's trailing rows when it
	// overflows.
	if a.warning != "" {
		b.WriteString("\n")
		b.WriteString(a.warningView())
	}

	return b.String()
}

// warningView renders the warning as one row, clamped to the width inside the
// footer's padding: a mutation-log warning carries a path and an errno, so it is
// the likeliest line to exceed the terminal width, and an overflowing row wraps
// and pushes the frame past a.height.
func (a App) warningView() string {
	width := a.width - 2
	if width < 1 {
		width = 1
	}
	text := a.warning
	if runes := []rune(text); len(runes) > width {
		text = string(runes[:width-1]) + "…"
	}
	return lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).Render(StyleWarning.Render(text))
}

// statusRowsMax caps the rows the status may claim. Both sentences of the
// store's parent rejections fit in three at 80 columns, and an uncapped count
// would let one message starve the content area on a short terminal.
const statusRowsMax = 3

// statusView renders the status wrapped over as many rows as it needs, up to
// statusRowsMax, and reports the row count so View reserves exactly the rows it
// writes. Clamping to one row the way warningView does would cut a store
// rejection mid-ID: the remedy it names — "Repoint the parent at an epic" —
// sits in the second sentence, and that clause is the half the user acts on.
// The last row kept marks the cut when the message outruns the cap.
func (a App) statusView() (string, int) {
	width := a.width - 2
	if width < 1 {
		width = 1
	}
	pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)

	wrapped := wrapText(a.status, width)
	lines := make([]string, 0, statusRowsMax)
	for i, wl := range wrapped {
		if i == statusRowsMax {
			break
		}
		text := wl.text
		if i == statusRowsMax-1 && len(wrapped) > statusRowsMax {
			if runes := []rune(text); len(runes) >= width {
				text = string(runes[:width-1])
			}
			text += "…"
		}
		lines = append(lines, pad.Render(StyleWarning.Render(text)))
	}
	return strings.Join(lines, "\n"), len(lines)
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
	needle := strings.ToLower(a.dashboard.filterText)
	counts := make(map[tabID]int)
	for tab := tabInbox; tab < tabCount; tab++ {
		for _, t := range a.tickets {
			if a.dashboard.tabShows(tab, t, a.dashboard.typeFilter, needle) {
				counts[tab]++
			}
		}
	}
	return counts
}

// displayName is the board's name as the header shows it: the reserved Root
// namespace by its display name, any project by its own.
func (a App) displayName() string {
	if project.IsRoot(a.projectName) {
		return "Root"
	}
	return a.projectName
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

// capturePromptView renders the capture prompt as one padded row.
func (a App) capturePromptView() string {
	pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	return pad.Render(StyleInputLabel.Render("capture: ") + a.captureBar.View())
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
	return "↑↓ select  │  " + action + " (n)ew (c)apture (e)dit  │  " + status + "(p)riority (m)ove (d)elete (y)ank (w)ork (u)p (s)ort (S)dir  │  tab/shift+tab  ctrl+k search  (q)uit"
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

	if a.captureActive {
		return a.capturePromptView(), 1
	}
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
	a.detail = newDetailModel(t, a.dashboard.qid(t), a.snap, a.width, a.height)
	a.detailStack = nil
	a.overlay = overlayDetail
}

// detailFor builds the detail of the ticket with this qualified ID off the
// graph. The ID is presented bare when the ticket is the board's own, as the
// board lists it, and qualified when it is foreign — the form the edit and
// mutation paths resolve the store from.
func (a App) detailFor(qid string) (detailModel, bool) {
	if a.snap == nil {
		return detailModel{}, false
	}
	t, ok := a.snap.Get(qid)
	if !ok {
		return detailModel{}, false
	}
	view := *t
	if ns, bare := ticket.ParseNamespacedID(qid); ns == a.projectName {
		view.ID = bare
	}
	return newDetailModel(&view, qid, a.snap, a.width, a.height), true
}

// pushDetail opens the ticket's detail over the current one, which esc
// returns to. From the board — no detail open — it opens as the first.
func (a *App) pushDetail(qid string) tea.Cmd {
	next, ok := a.detailFor(qid)
	if !ok {
		return func() tea.Msg { return statusMsg(fmt.Sprintf("ticket %s does not resolve", qid)) }
	}
	if a.overlay == overlayDetail {
		a.detailStack = append(a.detailStack, a.detail)
	} else {
		// A nested detail left through the edit form leaves its stack
		// behind; the first detail from the board must not sit over it.
		a.detailStack = nil
	}
	a.detail = next
	a.overlay = overlayDetail
	return nil
}

// openParent opens the full detail of the epic t belongs to, foreign or not,
// with the parent read relative to ns — the namespace t itself lives in. A
// parent that does not resolve is reported by the graph's own wording.
func (a *App) openParent(t *ticket.Ticket, ns string) tea.Cmd {
	if t.Parent == "" {
		return func() tea.Msg { return statusMsg("No parent") }
	}
	if issue := ticket.RelationshipIssue(t); issue != "" {
		return func() tea.Msg { return statusMsg(issue) }
	}
	return a.pushDetail(ticket.QualifyRef(ns, t.Parent))
}

// storeFor is the store a mutation of this ID writes through, and the ID
// that store answers to: the board's own project store for a bare ID or one
// qualified to the board's namespace, the whole central store for a foreign
// one. Board rows pass bare IDs and a detail passes qualified ones, so both
// forms resolve here rather than at every call site.
func (a *App) storeFor(id string) (ticket.Store, string) {
	ns, bare := ticket.ParseNamespacedID(id)
	if ns == "" || ns == a.projectName {
		return a.store, bare
	}
	return a.multi, id
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
	store, id := a.storeFor(id)
	t, err := store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Priority = (t.Priority + 1) % 5
	if err := store.Update(t); err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	msg := fmt.Sprintf("%s -> P%d", id, t.Priority)
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(msg) },
	)
}

func (a *App) handleSetStatus(id string, status ticket.Status) tea.Cmd {
	store, id := a.storeFor(id)
	t, err := store.Get(id)
	if err != nil {
		return func() tea.Msg { return statusMsg("error: " + err.Error()) }
	}

	t.Status = status
	closed, err := ticket.SaveEdit(store, t, true)
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
	store, id := a.storeFor(id)
	_, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
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
	status := fmt.Sprintf("Created %s: %s", ticket.SanitizeControl(t.ID), ticket.SanitizeControl(t.Title))
	return tea.Batch(
		loadTickets(a.store),
		func() tea.Msg { return statusMsg(status) },
	)
}

func (a *App) handleEditTicket(msg formSubmitMsg) tea.Cmd {
	store, id := a.storeFor(msg.editID)
	t, err := store.Get(id)
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

	closed, err := ticket.SaveEdit(store, t, msg.statusSet)
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
