package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)


type dashboardModel struct {
	all            []*ticket.Ticket
	items          []ticket.InboxItem
	activeTab      tabID // set by app-level tab switching
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
		activeTab: tabTriage,
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
		switch m.activeTab {
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
		if m.activeTab == tabInbox {
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

	// Column header.
	hdrStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	b.WriteString(fmt.Sprintf("  %s%s%s%s %s",
		padRight(hdrStyle.Render("ID"), 6),
		padRight(hdrStyle.Render("PRI"), 6),
		padRight(hdrStyle.Render("TYPE"), 10),
		padRight(hdrStyle.Render("STAGE"), 12),
		hdrStyle.Render("TITLE"),
	))
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

func (m dashboardModel) renderRow(item ticket.InboxItem, selected bool, _ int) string {
	t := item.Ticket

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
	stg := padRightBg(selBg.Foreground(StageColors[t.Stage]).Render(string(t.Stage)), 12, bg)
	title := selBg.Foreground(colorWhite).Render(t.Title)

	// Review indicator.
	rev := ""
	if t.Review == ticket.ReviewPending {
		rev = " " + selBg.Foreground(ReviewColors[t.Review]).Render("●")
	}

	sp := "  "
	gap := " "
	if selected {
		sp = selBg.Render("  ")
		gap = selBg.Render(" ")
	}
	line := sp + id + pri + typ + stg + gap + title + rev

	// Pad to full width for selection highlight.
	if selected && m.width > 0 {
		rendered := lipgloss.Width(line)
		if rendered < m.width {
			line += selBg.Render(strings.Repeat(" ", m.width-rendered))
		}
	}

	return line
}

