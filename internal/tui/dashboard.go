package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

var (
	tabActiveStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	tabDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	dashRowStyle   = lipgloss.NewStyle()
	dashRowSel     = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("237"))
	dashHelpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type inboxTab int

const (
	tabBacklog inboxTab = iota
	tabTriage
	tabInbox
	tabDone
	tabAll
)

var tabLabels = []string{"backlog", "triage", "inbox", "done", "all"}

type dashboardModel struct {
	all            []*ticket.Ticket
	items          []ticket.InboxItem
	tab            inboxTab
	cursor         int
	offset         int
	width          int
	height         int
	filterText     string
	filterActive   bool
	typeFilter     ticket.TicketType
	confirmDelete  bool
	deleteTargetID string
}

func newDashboardModel(tickets []*ticket.Ticket, w, h int) dashboardModel {
	m := dashboardModel{
		all:    tickets,
		tab:    tabTriage,
		width:  w,
		height: h,
	}
	m.buildItems()
	return m
}

func (m *dashboardModel) setSize(w, h int) {
	m.width = w
	m.height = h
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

func (m *dashboardModel) buildItems() {
	m.items = nil
	needle := strings.ToLower(m.filterText)

	for _, t := range m.all {
		if t.Stage == "" {
			continue
		}

		// Per-tab stage filtering.
		switch m.tab {
		case tabBacklog:
			if t.Stage != ticket.StageBacklog {
				continue
			}
		case tabTriage:
			if t.Stage != ticket.StageTriage {
				continue
			}
		case tabInbox:
			if t.Stage == ticket.StageDone || t.Stage == ticket.StageBacklog {
				continue
			}
		case tabDone:
			if t.Stage != ticket.StageDone {
				continue
			}
		case tabAll:
			// Show everything except done.
			if t.Stage == ticket.StageDone {
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

		// Inbox tab only shows human-actionable tickets.
		if m.tab == tabInbox {
			if item.Action != ticket.ActionHumanReview && item.Action != ticket.ActionHumanInput {
				continue
			}
		}

		m.items = append(m.items, item)
	}

	// Sort by priority ascending, then by age (oldest first within same priority).
	// This matches the Inbox() sort order and makes the list predictable.
	sort.SliceStable(m.items, func(i, j int) bool {
		if m.items[i].Ticket.Priority != m.items[j].Ticket.Priority {
			return m.items[i].Ticket.Priority < m.items[j].Ticket.Priority
		}
		return m.items[i].Since.Before(m.items[j].Since)
	})

	if m.cursor >= len(m.items) {
		m.cursor = max(0, len(m.items)-1)
	}
	m.clampOffset()
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
	// Reserve: 1 tabs, 1 header, 1 filter line, 1 help bar.
	rows := m.height - 4
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
				m.filterActive = false
				m.filterText = ""
				m.buildItems()
			case "enter":
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
		case "tab":
			m.tab = inboxTab((int(m.tab) + 1) % len(tabLabels))
			m.buildItems()
		case "shift+tab":
			m.tab = inboxTab((int(m.tab) - 1 + len(tabLabels)) % len(tabLabels))
			m.buildItems()
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
			types := []ticket.TicketType{"", ticket.TypeFeature, ticket.TypeBug, ticket.TypeTask, ticket.TypeEpic, ticket.TypeChore}
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

	// Tabs.
	var tabs []string
	for i, label := range tabLabels {
		if inboxTab(i) == m.tab {
			tabs = append(tabs, tabActiveStyle.Render(label))
		} else {
			tabs = append(tabs, tabDimStyle.Render(label))
		}
	}
	b.WriteString(strings.Join(tabs, "  "))
	b.WriteString("\n")

	// Compute ID column width from widest ticket ID.
	idWidth := 2 // minimum: "ID" header
	for _, item := range m.items {
		if w := len(item.Ticket.ID); w > idWidth {
			idWidth = w
		}
	}

	// Header.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	b.WriteString(headerStyle.Render(fmt.Sprintf("%-3s %-6s %-10s %-*s %s", "P", "TYPE", "STAGE", idWidth, "ID", "TITLE")))
	b.WriteString("\n")

	// Rows.
	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.items) {
		end = len(m.items)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(m.items[i], i == m.cursor, idWidth))
		b.WriteString("\n")
	}

	// Pad empty rows.
	for i := end - m.offset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Filter line.
	var filterLine string
	if m.filterActive {
		filterLine = filterStyle.Render("/ " + m.filterText + "█")
	} else if m.filterText != "" {
		filterLine = filterStyle.Render("filter: " + m.filterText + "  (/ to edit, esc clears)")
	} else {
		var parts []string
		if m.typeFilter != "" {
			parts = append(parts, fmt.Sprintf("type: %s", m.typeFilter))
		} else {
			parts = append(parts, "all types")
		}
		parts = append(parts, "(t type, / search)")
		filterLine = filterStyle.Render(strings.Join(parts, "  "))
	}
	b.WriteString(filterLine)
	b.WriteString("\n")

	// Help bar / delete confirmation.
	if m.confirmDelete {
		prompt := fmt.Sprintf("Delete %s? (y)es / (n)o", m.deleteTargetID)
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render(prompt))
	} else {
		help := "tab ↑↓  │  enter (o)pen (c)reate (e)dit  │  (p)riority (m)ove (d)elete  │  (q)uit"
		b.WriteString(dashHelpStyle.Render(help))
	}

	return b.String()
}

func (m dashboardModel) renderRow(item ticket.InboxItem, selected bool, idWidth int) string {
	t := item.Ticket
	pStyle := lipgloss.NewStyle().Foreground(priorityColors[t.Priority])
	tStyle := lipgloss.NewStyle().Foreground(typeColors[t.Type])

	stage := string(t.Stage)
	stageColor := stageColors[t.Stage]
	sStyle := lipgloss.NewStyle().Foreground(stageColor)

	pri := pStyle.Render(fmt.Sprintf("P%d", t.Priority))
	typ := tStyle.Render(fmt.Sprintf("%-6s", shortType(t.Type)))
	stg := sStyle.Render(fmt.Sprintf("%-10s", stage))

	// Review indicator.
	var rev string
	if t.Review == ticket.ReviewPending {
		rev = lipgloss.NewStyle().Foreground(reviewColors[t.Review]).Render("● ")
	}

	idText := fmt.Sprintf("%-*s", idWidth, t.ID)
	if selected {
		idText = dashRowSel.Render(idText)
	}

	return fmt.Sprintf("%s  %s %s %s%s %s", pri, typ, stg, rev, idText, t.Title)
}
