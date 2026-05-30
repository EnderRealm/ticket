package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

type epicRow struct {
	epic     *ticket.Ticket
	children []*ticket.Ticket
	expanded bool
}

type epicsModel struct {
	rows     []epicRow
	cursor   int
	offset   int
	width    int
	height   int
}

func (m *epicsModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *epicsModel) refreshTickets(tickets []*ticket.Ticket) {
	// Build epic rows preserving expansion state.
	expanded := make(map[string]bool)
	for _, r := range m.rows {
		if r.expanded {
			expanded[r.epic.ID] = true
		}
	}

	// Find epics and their children.
	childMap := make(map[string][]*ticket.Ticket)
	for _, t := range tickets {
		if t.Parent != "" {
			childMap[t.Parent] = append(childMap[t.Parent], t)
		}
	}

	// Collect epics, excluding done/closed.
	var epics []*ticket.Ticket
	for _, t := range tickets {
		if t.Type == ticket.TypeEpic && t.Status != ticket.StatusDone && t.Status != ticket.StatusClosed {
			epics = append(epics, t)
		}
	}

	// Sort by status order, then priority, then newest first.
	sort.SliceStable(epics, func(i, j int) bool {
		si := ticket.StatusOrder(epics[i].Status)
		sj := ticket.StatusOrder(epics[j].Status)
		if si != sj {
			return si < sj
		}
		if epics[i].Priority != epics[j].Priority {
			return epics[i].Priority < epics[j].Priority
		}
		return epics[i].Created.After(epics[j].Created)
	})

	m.rows = nil
	for _, t := range epics {
		m.rows = append(m.rows, epicRow{
			epic:     t,
			children: childMap[t.ID],
			expanded: expanded[t.ID],
		})
	}

	// Clamp cursor.
	total := m.totalLines()
	if m.cursor >= total {
		m.cursor = total - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// totalLines returns the total number of visible lines (epic headers + expanded children).
func (m epicsModel) totalLines() int {
	n := 0
	for _, r := range m.rows {
		n++ // epic header
		if r.expanded {
			n += len(r.children)
		}
	}
	return n
}

func (m epicsModel) visibleRows() int {
	rows := m.height - 1 // reserve 1 for column header
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *epicsModel) clampOffset() {
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

// selectedEpic returns the epic at the cursor, or nil if cursor is on a child.
func (m epicsModel) selectedEpic() *ticket.Ticket {
	line := 0
	for _, r := range m.rows {
		if line == m.cursor {
			return r.epic
		}
		line++
		if r.expanded {
			line += len(r.children)
		}
	}
	return nil
}

// selectedTicket returns whatever ticket is at the cursor (epic or child).
func (m epicsModel) selectedTicket() *ticket.Ticket {
	line := 0
	for _, r := range m.rows {
		if line == m.cursor {
			return r.epic
		}
		line++
		if r.expanded {
			for _, child := range r.children {
				if line == m.cursor {
					return child
				}
				line++
			}
		}
	}
	return nil
}

// focusEpic moves the cursor to the epic with the given ID, if present.
func (m *epicsModel) focusEpic(id string) {
	line := 0
	for _, r := range m.rows {
		if r.epic.ID == id {
			m.cursor = line
			m.clampOffset()
			return
		}
		line++
		if r.expanded {
			line += len(r.children)
		}
	}
}

// toggleExpand toggles the epic at the cursor.
func (m *epicsModel) toggleExpand() {
	line := 0
	for i, r := range m.rows {
		if line == m.cursor {
			m.rows[i].expanded = !m.rows[i].expanded
			return
		}
		line++
		if r.expanded {
			line += len(r.children)
		}
	}
}

func (m epicsModel) update(msg tea.Msg) (epicsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		total := m.totalLines()
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < total-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = total - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
		case " ", "enter":
			m.toggleExpand()
		}
		m.clampOffset()

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor -= 3
				if m.cursor < 0 {
					m.cursor = 0
				}
			}
		case tea.MouseButtonWheelDown:
			total := m.totalLines()
			if m.cursor < total-1 {
				m.cursor += 3
				if m.cursor > total-1 {
					m.cursor = total - 1
				}
			}
		}
		m.clampOffset()
	}

	return m, nil
}

func (m epicsModel) view() string {
	if len(m.rows) == 0 {
		return StyleDim.Render("\n  No epics found.\n")
	}

	var b strings.Builder

	// Column header — matches ticket view layout.
	hdrStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	b.WriteString(fmt.Sprintf("  %s%s%s%s%s %s\n",
		padRight(hdrStyle.Render("ID"), 6),
		padRight(hdrStyle.Render("PRI"), 6),
		padRight(hdrStyle.Render("TYPE"), 10),
		padRight(hdrStyle.Render("STATUS"), 12),
		padRight(hdrStyle.Render("AGE"), 6),
		hdrStyle.Render("TITLE"),
	))

	visible := m.visibleRows()
	lineIdx := 0
	linesRendered := 0

	for _, r := range m.rows {
		if linesRendered >= visible {
			break
		}

		// Epic header line.
		if lineIdx >= m.offset && linesRendered < visible {
			b.WriteString(m.renderEpicRow(r, lineIdx == m.cursor))
			b.WriteString("\n")
			linesRendered++
		}
		lineIdx++

		// Children (if expanded).
		if r.expanded {
			for _, child := range r.children {
				if linesRendered >= visible {
					break
				}
				if lineIdx >= m.offset {
					b.WriteString(m.renderChildRow(child, lineIdx == m.cursor))
					b.WriteString("\n")
					linesRendered++
				}
				lineIdx++
			}
		}
	}

	// Pad empty rows.
	for i := linesRendered; i < visible; i++ {
		b.WriteString("\n")
	}

	return b.String()
}

func (m epicsModel) renderEpicRow(r epicRow, selected bool) string {
	// Expand indicator.
	indicator := "▸"
	if r.expanded {
		indicator = "▾"
	}

	// Progress.
	var done, total int
	for _, child := range r.children {
		total++
		if child.Status == ticket.StatusDone || child.Status == ticket.StatusClosed {
			done++
		}
	}

	selBg := lipgloss.NewStyle()
	if selected {
		selBg = lipgloss.NewStyle().Background(colorSurface)
	}
	var bg *lipgloss.Style
	if selected {
		bg = &selBg
	}

	id := padRightBg(selBg.Foreground(colorGray).Render(IDSuffix(r.epic.ID)), 6, bg)
	pri := padRightBg(priorityBadge(r.epic.Priority, selected), 6, bg)
	typ := padRightBg(typeBadge(r.epic.Type, selected), 10, bg)
	sts := padRightBg(selBg.Foreground(StatusColors[r.epic.Status]).Render(string(r.epic.Status)), 12, bg)
	age := padRightBg(selBg.Foreground(colorGray).Render(formatAge(r.epic.Created)), 6, bg)
	title := selBg.Foreground(colorWhite).Render(r.epic.Title)
	progress := ""
	if total > 0 {
		progress = selBg.Render(fmt.Sprintf("  %s  %s", StyleDim.Render(fmt.Sprintf("%d/%d", done, total)), ProgressBar(done, total, 15)))
	}

	sp := " "
	if selected {
		sp = selBg.Render(" ")
	}
	ind := selBg.Render(indicator)
	line := ind + sp + id + pri + typ + sts + age + sp + title + progress

	// Pad to full width for selection highlight.
	if selected && m.width > 0 {
		rendered := lipgloss.Width(line)
		if rendered < m.width {
			line += selBg.Render(strings.Repeat(" ", m.width-rendered))
		}
	}

	return line
}

func (m epicsModel) renderChildRow(t *ticket.Ticket, selected bool) string {
	selBg := lipgloss.NewStyle()
	if selected {
		selBg = lipgloss.NewStyle().Background(colorSurface)
	}
	var bg *lipgloss.Style
	if selected {
		bg = &selBg
	}

	id := padRightBg(selBg.Foreground(colorGray).Render(IDSuffix(t.ID)), 6, bg)
	pri := padRightBg(priorityBadge(t.Priority, selected), 6, bg)
	typ := padRightBg(typeBadge(t.Type, selected), 10, bg)
	sts := padRightBg(selBg.Foreground(StatusColors[t.Status]).Render(string(t.Status)), 12, bg)
	age := padRightBg(selBg.Foreground(colorGray).Render(formatAge(t.Created)), 6, bg)
	title := selBg.Foreground(colorWhite).Render(t.Title)

	sp := "  "
	gap := " "
	if selected {
		sp = selBg.Render("  ")
		gap = selBg.Render(" ")
	}
	line := sp + id + pri + typ + sts + age + gap + title

	// Pad to full width for selection highlight.
	if selected && m.width > 0 {
		rendered := lipgloss.Width(line)
		if rendered < m.width {
			line += selBg.Render(strings.Repeat(" ", m.width-rendered))
		}
	}

	return line
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
