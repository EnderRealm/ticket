package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

var (
	stageColors = map[ticket.Stage]lipgloss.Color{
		ticket.StageTriage:       "7", // white
		ticket.StageSpec:         "6", // cyan
		ticket.StageDesign:       "5", // magenta
		ticket.StageDesignReview: "5", // magenta (same family as design)
		ticket.StageImplement:    "3", // yellow
		ticket.StageCodeReview:   "3", // yellow (same family as implement)
		ticket.StageTest:         "4", // blue
		ticket.StageVerify:       "2", // green
		ticket.StageDone:         "8", // gray
	}

	reviewColors = map[ticket.ReviewState]lipgloss.Color{
		ticket.ReviewPending:  "3", // yellow
		ticket.ReviewApproved: "2", // green
		ticket.ReviewRejected: "1", // red
	}

	priorityColors = map[int]lipgloss.Color{
		0: "1", // red - critical
		1: "3", // yellow - high
		2: "7", // white - normal
		3: "8", // gray - low
		4: "8", // gray - backlog
	}

	typeColors = map[ticket.TicketType]lipgloss.Color{
		ticket.TypeBug:     "1", // red
		ticket.TypeFeature: "2", // green
		ticket.TypeEpic:    "5", // magenta
		ticket.TypeTask:    "4", // blue
		ticket.TypeChore:   "8", // gray
	}

	filterStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	colHeaderStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	cardStyle      = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	cardSelected   = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).
			Bold(true).Background(lipgloss.Color("237"))
	pipeHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// allStages defines the display order for pipeline columns (from config, excludes done).
var allStages = ticket.DisplayStages()

type pipelineModel struct {
	all          []*ticket.Ticket
	columns      []pipelineColumn
	stageCol     int // index into columns (horizontal cursor)
	cardRow      int // index into current column's tickets (vertical cursor)
	width        int
	height       int
	typeFilter   ticket.TicketType // "" = all types
	filterText   string
	filterActive bool
}

type pipelineColumn struct {
	stage   ticket.Stage
	tickets []*ticket.Ticket
}

func newPipelineModel(tickets []*ticket.Ticket, w, h int) pipelineModel {
	m := pipelineModel{
		all:    tickets,
		width:  w,
		height: h,
	}
	m.buildColumns()
	return m
}

func (m *pipelineModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

// refreshTickets updates the ticket data while preserving cursor position.
func (m *pipelineModel) refreshTickets(tickets []*ticket.Ticket) {
	var selectedID string
	if t := m.selected(); t != nil {
		selectedID = t.ID
	}
	m.all = tickets
	m.buildColumns()
	if selectedID != "" {
		for ci, col := range m.columns {
			for ri, t := range col.tickets {
				if t.ID == selectedID {
					m.stageCol = ci
					m.cardRow = ri
					return
				}
			}
		}
	}
}

func (m *pipelineModel) buildColumns() {
	m.columns = nil
	needle := strings.ToLower(m.filterText)
	for _, stage := range allStages {
		col := pipelineColumn{stage: stage}
		for _, t := range m.all {
			if m.typeFilter != "" && t.Type != m.typeFilter {
				continue
			}
			if needle != "" {
				if !strings.Contains(strings.ToLower(t.Title), needle) &&
					!strings.Contains(strings.ToLower(t.ID), needle) {
					continue
				}
			}
			s := t.Stage
			if s == "" {
				// Map legacy tickets.
				if mapped, ok := ticket.StatusToStage[t.Status]; ok {
					s = mapped
				}
			}
			if s == stage {
				col.tickets = append(col.tickets, t)
			}
		}
		ticket.SortByPriorityID(col.tickets)
		m.columns = append(m.columns, col)
	}
	m.clampCursors()
}

func (m *pipelineModel) clampCursors() {
	if m.stageCol >= len(m.columns) {
		m.stageCol = len(m.columns) - 1
	}
	if m.stageCol < 0 {
		m.stageCol = 0
	}
	col := m.columns[m.stageCol]
	if m.cardRow >= len(col.tickets) {
		m.cardRow = max(0, len(col.tickets)-1)
	}
}

func (m pipelineModel) selected() *ticket.Ticket {
	if m.stageCol < 0 || m.stageCol >= len(m.columns) {
		return nil
	}
	col := m.columns[m.stageCol]
	if m.cardRow < 0 || m.cardRow >= len(col.tickets) {
		return nil
	}
	return col.tickets[m.cardRow]
}

func (m pipelineModel) inputActive() bool {
	return m.filterActive
}

func (m pipelineModel) update(msg tea.Msg) (pipelineModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filterActive {
			switch msg.String() {
			case "esc":
				m.filterActive = false
				m.filterText = ""
				m.buildColumns()
			case "enter":
				m.filterActive = false
			case "backspace":
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
					m.buildColumns()
				}
			default:
				if len(msg.String()) == 1 {
					m.filterText += msg.String()
					m.buildColumns()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "left", "h":
			if m.stageCol > 0 {
				m.stageCol--
				m.clampCursors()
			}
		case "right", "l":
			if m.stageCol < len(m.columns)-1 {
				m.stageCol++
				m.clampCursors()
			}
		case "up", "k":
			if m.cardRow > 0 {
				m.cardRow--
			}
		case "down", "j":
			col := m.columns[m.stageCol]
			if m.cardRow < len(col.tickets)-1 {
				m.cardRow++
			}
		case "g":
			m.cardRow = 0
		case "G":
			col := m.columns[m.stageCol]
			m.cardRow = max(0, len(col.tickets)-1)
		case "t":
			// Cycle type filter.
			types := []ticket.TicketType{"", ticket.TypeFeature, ticket.TypeBug, ticket.TypeTask, ticket.TypeEpic, ticket.TypeChore}
			for i, tt := range types {
				if tt == m.typeFilter {
					m.typeFilter = types[(i+1)%len(types)]
					break
				}
			}
			m.buildColumns()
		case "/":
			m.filterActive = true
			m.filterText = ""
		case "esc":
			if m.filterText != "" {
				m.filterText = ""
				m.buildColumns()
			}
		}
	}
	return m, nil
}

func (m pipelineModel) view() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Reserve space: 1 for filter line, 1 for help bar.
	availH := m.height - 2
	if availH < 3 {
		availH = 3
	}

	if len(m.columns) == 0 {
		return ""
	}
	colWidth := m.width / len(m.columns)
	if colWidth < 14 {
		colWidth = 14
	}

	// Build columns.
	colViews := make([]string, len(m.columns))
	for ci, col := range m.columns {
		colViews[ci] = m.renderColumn(ci, col, colWidth, availH)
	}

	// Join columns horizontally.
	content := joinHorizontal(colViews)

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

	// Help bar.
	help := pipeHelpStyle.Render("←→/hl stage  ↑↓/jk card  enter open  A advance  R review  S skip  p priority  c create  q quit")

	return filterLine + "\n" + content + "\n" + help
}

func (m pipelineModel) renderColumn(colIdx int, col pipelineColumn, colWidth, maxH int) string {
	stageColor := stageColors[col.stage]
	style := colHeaderStyle.Foreground(stageColor)

	header := style.Render(fmt.Sprintf(" %s (%d)", col.stage, len(col.tickets)))
	// Pad header to column width.
	headerLen := lipgloss.Width(header)
	if headerLen < colWidth {
		header += strings.Repeat(" ", colWidth-headerLen)
	}

	var lines []string
	lines = append(lines, header)

	cardH := maxH - 1 // reserve 1 for header
	for i := 0; i < cardH && i < len(col.tickets); i++ {
		t := col.tickets[i]
		selected := colIdx == m.stageCol && i == m.cardRow

		line := m.renderCard(t, colWidth, selected)
		lines = append(lines, line)
	}

	// Pad empty rows.
	for i := len(col.tickets); i < cardH; i++ {
		lines = append(lines, strings.Repeat(" ", colWidth))
	}

	return strings.Join(lines, "\n")
}

func (m pipelineModel) renderCard(t *ticket.Ticket, colWidth int, selected bool) string {
	pStyle := lipgloss.NewStyle().Foreground(priorityColors[t.Priority])
	tStyle := lipgloss.NewStyle().Foreground(typeColors[t.Type])

	// Compact format: "P1 feat tic-xxxx"
	pri := pStyle.Render(fmt.Sprintf("P%d", t.Priority))
	typ := tStyle.Render(fmt.Sprintf("%-6s", shortType(t.Type)))
	id := t.ID

	// Review indicator.
	var rev string
	if t.Review == ticket.ReviewPending {
		rev = lipgloss.NewStyle().Foreground(reviewColors[t.Review]).Render(" ●")
	} else if t.Review == ticket.ReviewApproved {
		rev = lipgloss.NewStyle().Foreground(reviewColors[t.Review]).Render(" ✓")
	} else if t.Review == ticket.ReviewRejected {
		rev = lipgloss.NewStyle().Foreground(reviewColors[t.Review]).Render(" ✗")
	}

	line := fmt.Sprintf("%s %s %s%s", pri, typ, id, rev)

	// Truncate title to fit.
	usedWidth := lipgloss.Width(line) + 1
	titleSpace := colWidth - usedWidth - 2
	title := t.Title
	if titleSpace > 0 && len(title) > 0 {
		if len(title) > titleSpace {
			title = title[:titleSpace-1] + "…"
		}
		line += " " + title
	}

	// Pad to column width.
	lineWidth := lipgloss.Width(line)
	if lineWidth < colWidth {
		line += strings.Repeat(" ", colWidth-lineWidth)
	}

	if selected {
		return cardSelected.Render(line)
	}
	return cardStyle.Render(line)
}

func shortType(t ticket.TicketType) string {
	switch t {
	case ticket.TypeFeature:
		return "feat"
	case ticket.TypeBug:
		return "bug"
	case ticket.TypeTask:
		return "task"
	case ticket.TypeEpic:
		return "epic"
	case ticket.TypeChore:
		return "chore"
	default:
		return string(t)
	}
}

// joinHorizontal joins multiple column views side by side.
func joinHorizontal(cols []string) string {
	if len(cols) == 0 {
		return ""
	}

	// Split each column into lines.
	colLines := make([][]string, len(cols))
	maxLines := 0
	for i, col := range cols {
		colLines[i] = strings.Split(col, "\n")
		if len(colLines[i]) > maxLines {
			maxLines = len(colLines[i])
		}
	}

	var result []string
	for row := 0; row < maxLines; row++ {
		var line string
		for ci, cl := range colLines {
			var part string
			if row < len(cl) {
				part = cl[row]
			}
			_ = ci
			line += part
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}
