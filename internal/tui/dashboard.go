package tui

import (
	"fmt"
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
	tabAll inboxTab = iota
	tabTriage
	tabVerify
	tabReview
)

var tabLabels = []string{"all", "triage", "verify", "review"}

type dashboardModel struct {
	all          []*ticket.Ticket
	items        []ticket.InboxItem
	tab          inboxTab
	cursor       int
	offset       int
	width        int
	height       int
	filterText   string
	filterActive bool
	typeFilter   ticket.TicketType
}

func newDashboardModel(tickets []*ticket.Ticket, w, h int) dashboardModel {
	m := dashboardModel{
		all:    tickets,
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

func (m *dashboardModel) buildItems() {
	m.items = nil
	needle := strings.ToLower(m.filterText)

	for _, t := range m.all {
		if t.Stage == "" || t.Stage == ticket.StageDone {
			continue
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
		if item.Action != ticket.ActionHumanReview && item.Action != ticket.ActionHumanInput {
			continue
		}

		switch m.tab {
		case tabTriage:
			if t.Stage != ticket.StageTriage {
				continue
			}
		case tabVerify:
			if t.Stage != ticket.StageVerify {
				continue
			}
		case tabReview:
			if t.Review != ticket.ReviewPending {
				continue
			}
		}

		m.items = append(m.items, item)
	}

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
	return m.filterActive
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
	// Reserve: 1 tabs, 1 header, 1 filter/help bar.
	rows := m.height - 3
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m dashboardModel) update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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

	// Header.
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	b.WriteString(headerStyle.Render(fmt.Sprintf("%-3s %-5s %-10s %-24s %s", "P", "TYPE", "STAGE", "ID", "TITLE")))
	b.WriteString("\n")

	// Rows.
	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.items) {
		end = len(m.items)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(m.items[i], i == m.cursor))
		b.WriteString("\n")
	}

	// Pad empty rows.
	for i := end - m.offset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Filter / help bar.
	if m.filterActive {
		b.WriteString(filterStyle.Render("/ " + m.filterText + "█"))
	} else if m.filterText != "" {
		b.WriteString(filterStyle.Render("filter: " + m.filterText + "  (/ to edit, esc clears)"))
	} else {
		help := "tab ↑↓ / t filter  │  enter (o)pen (c)reate (e)dit  │  (p)riority"
		switch m.tab {
		case tabVerify:
			help += "  (v)erify"
		case tabReview:
			help += "  (R)eview"
		}
		help += "  │  (q)uit"
		b.WriteString(dashHelpStyle.Render(help))
	}

	return b.String()
}

func (m dashboardModel) renderRow(item ticket.InboxItem, selected bool) string {
	t := item.Ticket
	pStyle := lipgloss.NewStyle().Foreground(priorityColors[t.Priority])
	tStyle := lipgloss.NewStyle().Foreground(typeColors[t.Type])

	stage := string(t.Stage)
	stageColor := stageColors[t.Stage]
	sStyle := lipgloss.NewStyle().Foreground(stageColor)

	pri := pStyle.Render(fmt.Sprintf("P%d", t.Priority))
	typ := tStyle.Render(fmt.Sprintf("%-5s", shortType(t.Type)))
	stg := sStyle.Render(fmt.Sprintf("%-10s", stage))

	// Review indicator.
	var rev string
	if t.Review == ticket.ReviewPending {
		rev = lipgloss.NewStyle().Foreground(reviewColors[t.Review]).Render("● ")
	}

	idText := fmt.Sprintf("%-24s", t.ID)
	if selected {
		idText = dashRowSel.Render(idText)
	}

	return fmt.Sprintf("%s  %s %s %s%s %s", pri, typ, stg, rev, idText, t.Title)
}
