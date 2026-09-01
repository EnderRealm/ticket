package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type sortDir int

const (
	asc sortDir = iota
	desc
)

// column describes one rendered column: its header, fixed width (0 = flexible,
// used by TITLE), a row renderer, and an ascending comparison for sorting.
type column struct {
	name   string
	width  int
	render func(t *ticket.Ticket, now time.Time) string
	less   func(a, b *ticket.Ticket) bool
}

// row is one rendered line: a tab item, or a child nested under an expanded
// epic on the epics tab. The cursor indexes rows, not items.
type row struct {
	item  ticket.InboxItem
	child bool
}

type dashboardModel struct {
	all            []*ticket.Ticket
	items          []ticket.InboxItem          // the tab's rows, sorted; epic groups on the epics tab
	rows           []row                       // rendered lines: items plus the children of expanded epics
	epics          map[string]bool             // set of bare epic IDs, from bareEpicIDs
	children       map[string][]*ticket.Ticket // bare parent ID -> children, from childrenByParent
	expanded       map[string]bool             // epic ID -> expanded, epics tab
	activeTab      tabID                       // set by app-level tab switching
	cursor         int
	offset         int
	width          int
	height         int
	filterText     string
	filterActive   bool
	typeFilter     ticket.TicketType
	confirmDelete  bool
	deleteTargetID string
	sortIdx        int
	sortDir        sortDir
}

// Shared column definitions. ID/PRI/TYPE/STATUS keep their existing widths and
// rendering; time columns are inserted between STATUS and TITLE.
var (
	colID = column{
		name: "ID", width: 6,
		render: func(t *ticket.Ticket, _ time.Time) string { return IDSuffix(t.ID) },
		less:   func(a, b *ticket.Ticket) bool { return a.ID < b.ID },
	}
	colPri = column{
		name: "PRI", width: 6,
		render: func(t *ticket.Ticket, _ time.Time) string { return priorityLabel(t.Priority) },
		less:   func(a, b *ticket.Ticket) bool { return a.Priority < b.Priority },
	}
	colType = column{
		name: "TYPE", width: 10,
		render: func(t *ticket.Ticket, _ time.Time) string { return shortType(t.Type) },
		less:   func(a, b *ticket.Ticket) bool { return shortType(a.Type) < shortType(b.Type) },
	}
	colStatus = column{
		name: "STATUS", width: 12,
		render: func(t *ticket.Ticket, _ time.Time) string { return string(t.Status) },
		less:   func(a, b *ticket.Ticket) bool { return ticket.StatusOrder(a.Status) < ticket.StatusOrder(b.Status) },
	}
	colCreated = column{
		name: "CREATED", width: 9,
		render: func(t *ticket.Ticket, _ time.Time) string { return shortDate(t.Created) },
		less:   func(a, b *ticket.Ticket) bool { return a.Created.Before(b.Created) },
	}
	colModified = column{
		// width 10 leaves a gap after the 8-char header even with the sort arrow.
		name: "MODIFIED", width: 10,
		render: func(t *ticket.Ticket, now time.Time) string { return relDuration(now.Sub(modifiedTime(t))) },
		less:   func(a, b *ticket.Ticket) bool { return modifiedTime(a).Before(modifiedTime(b)) },
	}
	colAge = column{
		name: "AGE", width: 6,
		render: func(t *ticket.Ticket, now time.Time) string { return relDuration(now.Sub(t.Created)) },
		less:   func(a, b *ticket.Ticket) bool { return a.Created.Before(b.Created) },
	}
	colCompleted = column{
		name: "COMPLETED", width: 11,
		render: func(t *ticket.Ticket, _ time.Time) string { return shortDate(t.Completed) },
		less:   func(a, b *ticket.Ticket) bool { return a.Completed.Before(b.Completed) },
	}
	colDuration = column{
		// width 10 leaves a gap after the 8-char header even with the sort arrow.
		name: "DURATION", width: 10,
		render: func(t *ticket.Ticket, _ time.Time) string {
			if t.Completed.IsZero() {
				return emDash
			}
			return relDuration(t.Completed.Sub(t.Created))
		},
		less: func(a, b *ticket.Ticket) bool { return durationSpan(a) < durationSpan(b) },
	}
	// colAgeOrDuration is the adaptive trailing column on the All tab: render
	// DURATION when completed, otherwise AGE; sort by the same adaptive span.
	colAgeOrDuration = column{
		name: "AGE", width: 6,
		render: func(t *ticket.Ticket, now time.Time) string {
			if !t.Completed.IsZero() {
				return relDuration(t.Completed.Sub(t.Created))
			}
			return relDuration(now.Sub(t.Created))
		},
		less: func(a, b *ticket.Ticket) bool { return adaptiveSpan(a) < adaptiveSpan(b) },
	}
	colTitle = column{
		name: "TITLE", width: 0,
		render: func(t *ticket.Ticket, _ time.Time) string { return t.Title },
		less:   func(a, b *ticket.Ticket) bool { return strings.ToLower(a.Title) < strings.ToLower(b.Title) },
	}
)

// bareEpicIDs is the set of epic IDs present, keyed bare. A map key can't
// tolerate the namespace mismatch the way SameTicketID does, and the central
// store records children with a namespaced parent while tickets written before
// the namespacing rollout record it bare.
func bareEpicIDs(tickets []*ticket.Ticket) map[string]bool {
	epics := make(map[string]bool)
	for _, t := range tickets {
		if t.Type == ticket.TypeEpic {
			_, bareID := ticket.ParseNamespacedID(t.ID)
			epics[bareID] = true
		}
	}
	return epics
}

// epicOf returns the bare ID of the epic a ticket belongs to, or "" when it
// belongs to none. Parent names the epic directly — a set lookup, not a walk up
// a chain. A parent that names no epic in the set reads as epic-less: `tk
// delete` on an epic leaves its children pointing at nothing, and such a ticket
// must stay visible on the backlog tab rather than rolling up under an epic
// that the epics tab no longer shows.
func epicOf(t *ticket.Ticket, epics map[string]bool) string {
	if t.Parent == "" {
		return ""
	}
	_, bareID := ticket.ParseNamespacedID(t.Parent)
	if !epics[bareID] {
		return ""
	}
	return bareID
}

// epicColumn renders the epic the ticket belongs to, or an em-dash when it
// belongs to none. It needs the epic set, so unlike the other columns it is
// built per call; a nil set renders every row as epic-less, which is what
// defaultSort's name-only lookup wants.
func epicColumn(epics map[string]bool) column {
	return column{
		name: "EPIC", width: 6,
		render: func(t *ticket.Ticket, _ time.Time) string {
			if epicID := epicOf(t, epics); epicID != "" {
				return IDSuffix(epicID)
			}
			return emDash
		},
		less: func(a, b *ticket.Ticket) bool {
			ea, eb := epicOf(a, epics), epicOf(b, epics)
			// Epic-less rows sort last ascending (and first descending, since
			// sortItems inverts this comparator rather than the ordering).
			if (ea == "") != (eb == "") {
				return eb == ""
			}
			return IDSuffix(ea) < IDSuffix(eb)
		},
	}
}

// modifiedTime returns Updated, falling back to Created when Updated is zero.
func modifiedTime(t *ticket.Ticket) time.Time {
	if t.Updated.IsZero() {
		return t.Created
	}
	return t.Updated
}

// durationSpan is the completed-minus-created span, or a large sentinel so
// uncompleted tickets sort consistently.
func durationSpan(t *ticket.Ticket) time.Duration {
	if t.Completed.IsZero() {
		return 1<<63 - 1
	}
	return t.Completed.Sub(t.Created)
}

// adaptiveSpan mirrors colAgeOrDuration: completed span if done, else age.
func adaptiveSpan(t *ticket.Ticket) time.Duration {
	if !t.Completed.IsZero() {
		return t.Completed.Sub(t.Created)
	}
	return time.Since(t.Created)
}

// columnsFor returns the column set for a tab. epics feeds the EPIC column and
// may be nil when only column names/widths matter.
func columnsFor(tab tabID, epics map[string]bool) []column {
	colEpic := epicColumn(epics)
	switch tab {
	case tabDone:
		return []column{colID, colPri, colEpic, colType, colStatus, colCreated, colModified, colCompleted, colDuration, colTitle}
	case tabAll:
		return []column{colID, colPri, colEpic, colType, colStatus, colCreated, colModified, colCompleted, colAgeOrDuration, colTitle}
	case tabBacklog, tabEpics:
		// No EPIC column: backlog rows are epics or tickets whose parent names
		// no known epic, and every epics-tab row is an epic or is drawn directly
		// under the epic it belongs to.
		return []column{colID, colPri, colType, colStatus, colCreated, colModified, colAge, colTitle}
	default: // inbox
		return []column{colID, colPri, colEpic, colType, colStatus, colCreated, colModified, colAge, colTitle}
	}
}

// defaultSort returns the natural sort column index and direction for a tab.
func defaultSort(tab tabID) (int, sortDir) {
	cols := columnsFor(tab, nil)
	switch tab {
	case tabDone, tabAll:
		for i, c := range cols {
			if c.name == "COMPLETED" {
				return i, desc
			}
		}
	case tabEpics:
		for i, c := range cols {
			if c.name == "AGE" {
				return i, desc
			}
		}
	}
	// Inbox and backlog default to PRI ascending.
	for i, c := range cols {
		if c.name == "PRI" {
			return i, asc
		}
	}
	return 0, asc
}

func newDashboardModel(tickets []*ticket.Ticket, w, h int) dashboardModel {
	m := dashboardModel{
		all:       tickets,
		activeTab: tabInbox,
		width:     w,
		height:    h,
	}
	m.sortIdx, m.sortDir = defaultSort(tabInbox)
	m.buildItems()
	return m
}

func (m *dashboardModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

// buildItemsPreservingCursor rebuilds the visible list but keeps the cursor on
// the currently selected ticket when it survives the rebuild (e.g. clearing a
// filter shouldn't reset the selection to the top of the list).
func (m *dashboardModel) buildItemsPreservingCursor() {
	var selectedID string
	if t := m.selected(); t != nil {
		selectedID = t.ID
	}
	m.buildItems()
	if selectedID == "" {
		return
	}
	for i, r := range m.rows {
		if r.item.Ticket.ID == selectedID {
			m.cursor = i
			m.clampOffset()
			return
		}
	}
}

// refreshTickets updates the ticket data while preserving cursor position.
func (m *dashboardModel) refreshTickets(tickets []*ticket.Ticket) {
	var selectedID string
	if t := m.selected(); t != nil {
		selectedID = t.ID
	}
	m.all = tickets
	m.buildItems()
	if selectedID != "" {
		for i, r := range m.rows {
			if r.item.Ticket.ID == selectedID {
				m.cursor = i
				m.clampOffset()
				return
			}
		}
	}
}

// tabShows reports whether a ticket is a row on tab, under the active type
// filter and the lowercased search needle. This is the one place the per-tab
// rules are written down — buildItems builds the rows with it and App.tabCounts
// counts with it, so a tab's rows and its tab-bar count cannot disagree:
//
//	tab      rows                      epics             an epic's children
//	inbox    open, ready               hidden            shown
//	backlog  backlog                   shown as rollups  hidden (rolled up)
//	epics    epics, not done/closed    shown as groups   nested, when expanded
//	done     done, closed              shown             shown
//	all      anything but done/closed  hidden            shown
//
// A ticket with no status is a row on no tab.
//
// One divergence, on the epics tab: a child nested under an expanded epic
// belongs to a group this predicate already counted, so it is not a row of its
// own and the tab bar counts epic groups rather than rendered lines —
// expanding a group must not inflate the count. That is the agreed contract,
// pinned by TestEpicsCountsGroupsNotExpandedChildren. A filter therefore
// selects epics, and an expanded epic still shows all of its children.
func tabShows(tab tabID, t *ticket.Ticket, epics map[string]bool, typeFilter ticket.TicketType, needle string) bool {
	if t.Status == "" {
		return false
	}
	isEpic := t.Type == ticket.TypeEpic

	switch tab {
	case tabBacklog:
		if t.Status != ticket.StatusBacklog {
			return false
		}
		// Children roll up under their epic; hide them here. An epic is never
		// hidden: a store written before the one-level rule can hold an epic
		// under an epic, and hiding it would take its own children — which roll
		// up under it — off the tab with it.
		if !isEpic && epicOf(t, epics) != "" {
			return false
		}
	case tabInbox:
		if t.Status != ticket.StatusOpen && t.Status != ticket.StatusReady {
			return false
		}
		if isEpic {
			return false
		}
	case tabEpics:
		if !isEpic || t.Status == ticket.StatusDone || t.Status == ticket.StatusClosed {
			return false
		}
	case tabDone:
		if t.Status != ticket.StatusDone && t.Status != ticket.StatusClosed {
			return false
		}
	case tabAll:
		if t.Status == ticket.StatusDone || t.Status == ticket.StatusClosed {
			return false
		}
		if isEpic {
			return false
		}
	}

	if typeFilter != "" && t.Type != typeFilter {
		return false
	}
	if needle != "" {
		if !strings.Contains(strings.ToLower(t.Title), needle) &&
			!strings.Contains(strings.ToLower(t.ID), needle) {
			return false
		}
	}
	return true
}

// rowsFor returns the rows tab renders under the given filters, unordered:
// buildItems sorts them. epics is the bareEpicIDs set for tickets.
func rowsFor(tab tabID, tickets []*ticket.Ticket, epics map[string]bool, typeFilter ticket.TicketType, filterText string) []ticket.InboxItem {
	needle := strings.ToLower(filterText)

	var items []ticket.InboxItem
	for _, t := range tickets {
		if tabShows(tab, t, epics, typeFilter, needle) {
			items = append(items, ticket.NextAction(t))
		}
	}
	return items
}

func (m *dashboardModel) buildItems() {
	// The epic set decides membership and feeds the EPIC column; the child map
	// feeds both the backlog rollup counts and the epics tab's expansion.
	m.epics = bareEpicIDs(m.all)
	m.children = childrenByParent(m.all)
	m.items = rowsFor(m.activeTab, m.all, m.epics, m.typeFilter, m.filterText)

	m.sortItems()

	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	m.clampOffset()
}

// rebuildRows flattens items into rendered lines: on the epics tab an expanded
// epic is followed by its children, which are not items of their own.
func (m *dashboardModel) rebuildRows() {
	m.rows = nil
	for _, item := range m.items {
		m.rows = append(m.rows, row{item: item})
		if m.activeTab != tabEpics || !m.expanded[item.Ticket.ID] {
			continue
		}
		for _, child := range m.epicChildren(item.Ticket) {
			m.rows = append(m.rows, row{item: ticket.NextAction(child), child: true})
		}
	}
}

// sortItems stably sorts items by the active column, honoring direction, with
// priority as a secondary tiebreaker when priority is not the active column.
// Children follow their epic, so only the items are reordered; the rendered
// rows are rebuilt from the new order before returning, so callers need not.
func (m *dashboardModel) sortItems() {
	cols := columnsFor(m.activeTab, m.epics)
	if m.sortIdx < 0 || m.sortIdx >= len(cols) {
		m.sortIdx = 0
	}
	col := cols[m.sortIdx]
	if col.less == nil {
		col = colPri
	}
	sort.SliceStable(m.items, func(i, j int) bool {
		a, b := m.items[i].Ticket, m.items[j].Ticket
		if col.less(a, b) != col.less(b, a) {
			if m.sortDir == desc {
				return col.less(b, a)
			}
			return col.less(a, b)
		}
		// Equal on the active key — fall back to priority ascending unless
		// priority is already the active key.
		if col.name != "PRI" {
			return a.Priority < b.Priority
		}
		return false
	})
	m.rebuildRows()
}

func (m dashboardModel) selected() *ticket.Ticket {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].item.Ticket
	}
	return nil
}

func (m dashboardModel) inputActive() bool {
	return m.filterActive || m.confirmDelete
}

func (m *dashboardModel) clampOffset() {
	visible := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m dashboardModel) visibleRows() int {
	// Reserve: 1 header. Filter and help bar are rendered by app shell.
	rows := m.height - 1
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m dashboardModel) update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmDelete {
			switch msg.String() {
			case "y":
				id := m.deleteTargetID
				m.confirmDelete = false
				m.deleteTargetID = ""
				return m, func() tea.Msg { return deleteTicketMsg{id: id} }
			default:
				m.confirmDelete = false
				m.deleteTargetID = ""
			}
			return m, nil
		}

		if m.filterActive {
			switch msg.String() {
			case "esc":
				// Clear the filter but keep the cursor on the row the user
				// arrowed to, rather than resetting to the top of the rebuilt list.
				m.filterActive = false
				m.filterText = ""
				m.buildItemsPreservingCursor()
			case "up":
				if m.cursor > 0 {
					m.cursor--
					m.clampOffset()
				}
			case "down":
				if m.cursor < len(m.rows)-1 {
					m.cursor++
					m.clampOffset()
				}
			case "enter":
				// Commit the filter: close the search box but keep the list
				// narrowed so row commands operate on the filtered set. Press
				// enter/o again to open the highlighted ticket.
				m.filterActive = false
			case "backspace":
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
					m.buildItems()
				}
			default:
				if len(msg.String()) == 1 {
					m.filterText += msg.String()
					m.buildItems()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.clampOffset()
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.clampOffset()
			}
		case "pgup":
			m.cursor -= m.visibleRows()
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.clampOffset()
		case "pgdown":
			m.cursor += m.visibleRows()
			if m.cursor > len(m.rows)-1 {
				m.cursor = max(0, len(m.rows)-1)
			}
			m.clampOffset()
		case "g":
			m.cursor = 0
			m.clampOffset()
		case "G":
			m.cursor = max(0, len(m.rows)-1)
			m.clampOffset()
		case "t":
			types := []ticket.TicketType{"", ticket.TypeFeature, ticket.TypeBug, ticket.TypeEpic}
			for i, tt := range types {
				if tt == m.typeFilter {
					m.typeFilter = types[(i+1)%len(types)]
					break
				}
			}
			m.buildItems()
		case "/":
			m.filterActive = true
			m.filterText = ""
		case "s":
			cols := columnsFor(m.activeTab, m.epics)
			m.sortIdx = (m.sortIdx + 1) % len(cols)
			m.sortDir = asc
			m.sortItems()
			m.clampOffset()
		case "S":
			if m.sortDir == asc {
				m.sortDir = desc
			} else {
				m.sortDir = asc
			}
			m.sortItems()
			m.clampOffset()
		case "esc":
			if m.filterText != "" {
				m.filterText = ""
				m.buildItems()
			}
		}
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.cursor -= 3
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.clampOffset()
		case tea.MouseButtonWheelDown:
			m.cursor += 3
			if m.cursor > len(m.rows)-1 {
				m.cursor = max(0, len(m.rows)-1)
			}
			m.clampOffset()
		}
	}
	return m, nil
}

// emptyRowsLabel is the placeholder a tab draws when nothing passes its rules
// and filters — "no rows" alone reads as a rendering fault on any tab.
const emptyRowsLabel = "No tickets found."

func (m dashboardModel) view() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(renderColumnHeader(columnsFor(m.activeTab, m.epics), m.sortIdx, m.sortDir))
	b.WriteString("\n")

	// Rows.
	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.rows) {
		end = len(m.rows)
	}

	// A tab with no rows draws the placeholder on the first line instead.
	lines := end - m.offset
	if len(m.rows) == 0 {
		b.WriteString(StyleDim.Render("  " + emptyRowsLabel))
		b.WriteString("\n")
		lines = 1
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(m.rows[i], i == m.cursor))
		b.WriteString("\n")
	}

	// Pad empty rows.
	for i := lines; i < visible; i++ {
		b.WriteString("\n")
	}

	// Delete confirmation only — filter/help rendered by app shell.
	if m.confirmDelete {
		prompt := fmt.Sprintf("Delete %s? (y)es / (n)o", m.deleteTargetID)
		b.WriteString(StyleDanger.Bold(true).Render(prompt))
	}

	return b.String()
}

// renderCell renders one row cell with the column's visual style, padded to the
// column width (TITLE is flexible). bg/selBg carry the selection highlight.
func renderCell(c column, t *ticket.Ticket, now time.Time, selBg lipgloss.Style, bg *lipgloss.Style, selected bool) string {
	// Stored values are sanitized before Lipgloss adds its own terminal controls;
	// sanitizing the completed view would remove the TUI's styling as well.
	switch c.name {
	case "PRI":
		return padRightBg(priorityBadge(t.Priority, selected), c.width, bg)
	case "TYPE":
		return padRightBg(typeBadge(t.Type, selected), c.width, bg)
	case "STATUS":
		return padRightBg(selBg.Foreground(StatusColors[t.Status]).Render(ticket.SanitizeControl(string(t.Status))), c.width, bg)
	case "ID", "EPIC":
		return padRightBg(selBg.Foreground(colorGray).Render(ticket.SanitizeControl(c.render(t, now))), c.width, bg)
	case "TITLE":
		return selBg.Foreground(colorWhite).Render(ticket.SanitizeControl(c.render(t, now)))
	default:
		// Time columns: subtle gray text, brightened when selected so it stays
		// legible against the selection background (colorSubtle ≈ colorSurface).
		fg := colorSubtle
		if selected {
			fg = colorGray
		}
		return padRightBg(selBg.Foreground(fg).Render(ticket.SanitizeControl(c.render(t, now))), c.width, bg)
	}
}

func (m dashboardModel) renderRow(r row, selected bool) string {
	t := r.item.Ticket
	now := time.Now()

	selBg := lipgloss.NewStyle()
	if selected {
		selBg = lipgloss.NewStyle().Background(colorSurface)
	}
	var bg *lipgloss.Style
	if selected {
		bg = &selBg
	}

	// An epic group carries its expand indicator in the two-column lead; every
	// other row, children included, keeps the plain indent.
	lead := "  "
	group := m.activeTab == tabEpics && !r.child
	if group {
		lead = expandIndicator(m.expanded[t.ID]) + " "
	}
	if selected {
		lead = selBg.Render(lead)
	}
	line := lead
	for _, c := range columnsFor(m.activeTab, m.epics) {
		line += renderCell(c, t, now, selBg, bg, selected)
	}

	// On the backlog tab, show "(N children)" next to epic rows so they act as rollups.
	if m.activeTab == tabBacklog && t.Type == ticket.TypeEpic {
		label := fmt.Sprintf("  (%d children)", len(m.epicChildren(t)))
		line += selBg.Foreground(colorSubtle).Render(label)
	}
	// On the inbox tab, a parked ticket carries the question it is blocked on —
	// the row's status column still reads open, so the flag is what marks it.
	if m.activeTab == tabInbox && r.item.Action == ticket.ActionBlocked {
		const flag = "  ⚑ "
		label := flag + ticket.SanitizeControl(r.item.Detail)
		// A question long enough to overrun the row wraps the line and defeats
		// the selection padding below, so clip it by display width — a question
		// is free text and a rune count is not its width — to what the row has
		// left. Width 0 means the size isn't known yet — leave the label alone.
		if room := m.width - lipgloss.Width(line); m.width > 0 && lipgloss.Width(label) > room {
			// The flag is all that marks a parked row apart from an ordinary
			// open one, so keep it when it fits and drop only the question.
			if room < lipgloss.Width(flag) {
				label = ""
			} else if room < lipgloss.Width(flag)+2 {
				label = flag
			} else {
				label = ansi.Truncate(label, room, "…")
			}
		}
		if label != "" {
			line += selBg.Foreground(colorWarning).Render(label)
		}
	}
	if group {
		line += m.epicProgress(t, selBg)
	}

	// Pad to full width for selection highlight.
	if selected && m.width > 0 {
		rendered := lipgloss.Width(line)
		if rendered < m.width {
			line += selBg.Render(strings.Repeat(" ", m.width-rendered))
		}
	}

	return line
}
