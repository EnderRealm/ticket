package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type dashboardModel struct {
	all            []*ticket.Ticket
	items          []ticket.InboxItem
	childCounts    map[string]int // epic ID -> total child count (shown on backlog tab)
	activeTab      tabID          // set by app-level tab switching
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

// columnsFor returns the column set for a tab.
func columnsFor(tab tabID) []column {
	switch tab {
	case tabDone:
		return []column{colID, colPri, colType, colStatus, colCreated, colModified, colCompleted, colDuration, colTitle}
	case tabAll:
		return []column{colID, colPri, colType, colStatus, colCreated, colModified, colCompleted, colAgeOrDuration, colTitle}
	default: // inbox, backlog
		return []column{colID, colPri, colType, colStatus, colCreated, colModified, colAge, colTitle}
	}
}

// defaultSort returns the natural sort column index and direction for a tab.
func defaultSort(tab tabID) (int, sortDir) {
	cols := columnsFor(tab)
	switch tab {
	case tabDone, tabAll:
		for i, c := range cols {
			if c.name == "COMPLETED" {
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
	for i, item := range m.items {
		if item.Ticket.ID == selectedID {
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
		for i, item := range m.items {
			if item.Ticket.ID == selectedID {
				m.cursor = i
				m.clampOffset()
				return
			}
		}
	}
}

// nearestEpicAncestor walks t's parent chain and returns the ID of the closest
// epic ancestor, or "" if none. Handles missing parents and cycles defensively.
func nearestEpicAncestor(t *ticket.Ticket, byID map[string]*ticket.Ticket) string {
	seen := make(map[string]bool)
	cur := t.Parent
	for cur != "" {
		if seen[cur] {
			return ""
		}
		seen[cur] = true
		p, ok := byID[cur]
		if !ok {
			return ""
		}
		if p.Type == ticket.TypeEpic {
			return p.ID
		}
		cur = p.Parent
	}
	return ""
}

func (m *dashboardModel) buildItems() {
	m.items = nil
	m.childCounts = nil
	needle := strings.ToLower(m.filterText)

	// Index tickets so we can walk parent chains to find epic ancestors:
	//  - backlog hides any ticket with an epic anywhere up the chain,
	//  - backlog shows epics as rollups with a descendant count,
	//  - inbox/all hide epics themselves.
	byID := make(map[string]*ticket.Ticket, len(m.all))
	for _, t := range m.all {
		byID[t.ID] = t
	}
	childCounts := make(map[string]int)
	for _, t := range m.all {
		if t.Type == ticket.TypeEpic {
			continue
		}
		if epicID := nearestEpicAncestor(t, byID); epicID != "" {
			childCounts[epicID]++
		}
	}

	for _, t := range m.all {
		if t.Status == "" {
			continue
		}

		isEpic := t.Type == ticket.TypeEpic
		hasEpicAncestor := !isEpic && nearestEpicAncestor(t, byID) != ""

		// Per-tab status filtering.
		switch m.activeTab {
		case tabBacklog:
			if t.Status != ticket.StatusBacklog {
				continue
			}
			// Descendants of epics roll up under the epic; hide them here.
			if hasEpicAncestor {
				continue
			}
		case tabInbox:
			if t.Status != ticket.StatusOpen && t.Status != ticket.StatusReady {
				continue
			}
			// Epics live in the epics tab.
			if isEpic {
				continue
			}
		case tabDone:
			// Done shows every done/closed ticket, including epics.
			if t.Status != ticket.StatusDone && t.Status != ticket.StatusClosed {
				continue
			}
		case tabAll:
			// Show everything except done/closed.
			if t.Status == ticket.StatusDone || t.Status == ticket.StatusClosed {
				continue
			}
			if isEpic {
				continue
			}
		}

		if m.typeFilter != "" && t.Type != m.typeFilter {
			continue
		}
		if needle != "" {
			if !strings.Contains(strings.ToLower(t.Title), needle) &&
				!strings.Contains(strings.ToLower(t.ID), needle) {
				continue
			}
		}

		item := ticket.NextAction(t)
		m.items = append(m.items, item)
	}

	if m.activeTab == tabBacklog {
		m.childCounts = childCounts
	}

	m.sortItems()

	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.clampOffset()
}

// sortItems stably sorts items by the active column, honoring direction, with
// priority as a secondary tiebreaker when priority is not the active column.
func (m *dashboardModel) sortItems() {
	cols := columnsFor(m.activeTab)
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
}

func (m dashboardModel) selected() *ticket.Ticket {
	if m.cursor >= 0 && m.cursor < len(m.items) {
		return m.items[m.cursor].Ticket
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
				if m.cursor < len(m.items)-1 {
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
			if m.cursor < len(m.items)-1 {
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
			if m.cursor > len(m.items)-1 {
				m.cursor = max(0, len(m.items)-1)
			}
			m.clampOffset()
		case "g":
			m.cursor = 0
			m.clampOffset()
		case "G":
			m.cursor = max(0, len(m.items)-1)
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
			cols := columnsFor(m.activeTab)
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
			if m.cursor > len(m.items)-1 {
				m.cursor = max(0, len(m.items)-1)
			}
			m.clampOffset()
		}
	}
	return m, nil
}

func (m dashboardModel) view() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(renderColumnHeader(columnsFor(m.activeTab), m.sortIdx, m.sortDir))
	b.WriteString("\n")

	// Rows.
	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.items) {
		end = len(m.items)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(m.items[i], i == m.cursor, 0))
		b.WriteString("\n")
	}

	// Pad empty rows.
	for i := end - m.offset; i < visible; i++ {
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
	switch c.name {
	case "PRI":
		return padRightBg(priorityBadge(t.Priority, selected), c.width, bg)
	case "TYPE":
		return padRightBg(typeBadge(t.Type, selected), c.width, bg)
	case "STATUS":
		return padRightBg(selBg.Foreground(StatusColors[t.Status]).Render(string(t.Status)), c.width, bg)
	case "ID":
		return padRightBg(selBg.Foreground(colorGray).Render(c.render(t, now)), c.width, bg)
	case "TITLE":
		return selBg.Foreground(colorWhite).Render(c.render(t, now))
	default:
		// Time columns: subtle gray text.
		return padRightBg(selBg.Foreground(colorSubtle).Render(c.render(t, now)), c.width, bg)
	}
}

func (m dashboardModel) renderRow(item ticket.InboxItem, selected bool, _ int) string {
	t := item.Ticket
	now := time.Now()

	selBg := lipgloss.NewStyle()
	if selected {
		selBg = lipgloss.NewStyle().Background(colorSurface)
	}
	var bg *lipgloss.Style
	if selected {
		bg = &selBg
	}

	sp := "  "
	if selected {
		sp = selBg.Render("  ")
	}
	line := sp
	for _, c := range columnsFor(m.activeTab) {
		line += renderCell(c, t, now, selBg, bg, selected)
	}

	// On the backlog tab, show "(N children)" next to epic rows so they act as rollups.
	if m.activeTab == tabBacklog && t.Type == ticket.TypeEpic {
		n := m.childCounts[t.ID]
		label := fmt.Sprintf("  (%d children)", n)
		line += selBg.Foreground(colorSubtle).Render(label)
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
