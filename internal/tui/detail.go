package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

var (
	fieldKeyStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Width(14)
	fieldValStyle   = lipgloss.NewStyle()
	sectionStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	timestampStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	detailHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	inputLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
)

type inputMode int

const (
	inputNone inputMode = iota
	inputAssignee
	inputNote
	inputMove
	inputMovePicker
)

const enterPathOption = "enter path..."

type detailModel struct {
	ticket       *ticket.Ticket
	lines        []string
	offset       int
	width        int
	height       int
	input        inputMode
	inputText    string
	pickerItems  []string // repo paths for move picker
	pickerCursor int
}

func newDetailModel(t *ticket.Ticket, w, h int) detailModel {
	m := detailModel{
		ticket: t,
		width:  w,
		height: h,
	}
	m.lines = m.render()
	return m
}

func (m *detailModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m detailModel) inputActive() bool {
	return m.input != inputNone
}

func (m *detailModel) startInput(mode inputMode) {
	m.input = mode
	m.inputText = ""
	if mode == inputAssignee {
		m.inputText = m.ticket.Assignee
	}
}

func (m *detailModel) startMovePicker(ticketsDir string) {
	repos := discoverSiblingRepos(ticketsDir)
	repos = append(repos, enterPathOption)
	m.pickerItems = repos
	m.pickerCursor = 0
	m.input = inputMovePicker
	m.inputText = ""
}

// discoverSiblingRepos finds git repositories adjacent to the repo that owns ticketsDir.
func discoverSiblingRepos(ticketsDir string) []string {
	repoRoot := filepath.Dir(ticketsDir) // /path/to/repo/.tickets -> /path/to/repo
	parentDir := filepath.Dir(repoRoot)  // /path/to/repo -> /path/to
	repoName := filepath.Base(repoRoot)

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil
	}

	var repos []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == repoName {
			continue
		}
		candidate := filepath.Join(parentDir, e.Name())
		if info, err := os.Stat(filepath.Join(candidate, ".git")); err == nil && info.IsDir() {
			repos = append(repos, candidate)
		}
	}
	return repos
}

func (m detailModel) visibleRows() int {
	rows := m.height - 1 // help bar
	if m.input == inputMovePicker {
		rows -= len(m.pickerItems) + 1 // label + items
	} else if m.input != inputNone {
		rows-- // input bar
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m detailModel) update(msg tea.Msg) (detailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.input == inputMovePicker {
			return m.updatePicker(msg)
		}
		if m.input != inputNone {
			return m.updateInput(msg)
		}

		maxOffset := max(0, len(m.lines)-m.visibleRows())
		switch msg.String() {
		case "up", "k":
			if m.offset > 0 {
				m.offset--
			}
		case "down", "j":
			if m.offset < maxOffset {
				m.offset++
			}
		case "pgup", "b":
			m.offset -= m.visibleRows()
			if m.offset < 0 {
				m.offset = 0
			}
		case "pgdown", "f", " ":
			m.offset += m.visibleRows()
			if m.offset > maxOffset {
				m.offset = maxOffset
			}
		case "g":
			m.offset = 0
		case "G":
			m.offset = maxOffset
		}
	}
	return m, nil
}

func (m detailModel) updateInput(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input = inputNone
		m.inputText = ""
		return m, nil
	case "enter":
		mode := m.input
		text := m.inputText
		id := m.ticket.ID
		m.input = inputNone
		m.inputText = ""

		switch mode {
		case inputAssignee:
			return m, func() tea.Msg {
				return setAssigneeMsg{id: id, assignee: strings.TrimSpace(text)}
			}
		case inputNote:
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return addNoteMsg{id: id, text: trimmed}
			}
		case inputMove:
			trimmed := strings.TrimSpace(text)
			if trimmed == "" {
				return m, nil
			}
			return m, func() tea.Msg {
				return moveTicketMsg{id: id, targetRepo: trimmed}
			}
		}
		return m, nil
	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.inputText += msg.String()
		}
	}
	return m, nil
}

func (m detailModel) updatePicker(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input = inputNone
		m.pickerItems = nil
		return m, nil
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
	case "down", "j":
		if m.pickerCursor < len(m.pickerItems)-1 {
			m.pickerCursor++
		}
	case "enter":
		selected := m.pickerItems[m.pickerCursor]
		m.pickerItems = nil
		m.pickerCursor = 0

		if selected == enterPathOption {
			m.input = inputMove
			m.inputText = ""
			return m, nil
		}

		id := m.ticket.ID
		m.input = inputNone
		return m, func() tea.Msg {
			return moveTicketMsg{id: id, targetRepo: selected}
		}
	}
	return m, nil
}

func (m detailModel) view() string {
	var b strings.Builder

	visible := m.visibleRows()
	end := m.offset + visible
	if end > len(m.lines) {
		end = len(m.lines)
	}

	for i := m.offset; i < end; i++ {
		b.WriteString(m.lines[i])
		b.WriteString("\n")
	}

	// Pad remaining.
	for i := end - m.offset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Input bar (if active).
	if m.input == inputMovePicker {
		b.WriteString(inputLabelStyle.Render("move to repo:") + "\n")
		for i, item := range m.pickerItems {
			prefix := "  "
			if i == m.pickerCursor {
				prefix = "> "
			}
			b.WriteString(prefix + item + "\n")
		}
	} else if m.input != inputNone {
		var label string
		switch m.input {
		case inputAssignee:
			label = "assignee"
		case inputNote:
			label = "note"
		case inputMove:
			label = "move to repo"
		}
		b.WriteString(inputLabelStyle.Render(label+": ") + m.inputText + "█\n")
	}

	// Help bar.
	var help string
	if m.input == inputMovePicker {
		help = "↑↓ select  enter confirm  esc cancel"
	} else if m.input != inputNone {
		help = "enter confirm  esc cancel"
	} else {
		help = "↑↓/jk scroll  │  (e)dit (p)riority (a)ssignee (n)ote (m)ove  │  esc back  (q)uit"
	}
	b.WriteString(detailHelpStyle.Render(help))

	return b.String()
}

func (m detailModel) render() []string {
	t := m.ticket
	var lines []string

	// Frontmatter fields.
	lines = append(lines, m.field("Title", t.Title))
	lines = append(lines, m.field("ID", t.ID))
	if t.Stage != "" {
		lines = append(lines, m.field("Stage", string(t.Stage)))
	}
	if t.Review != "" {
		lines = append(lines, m.field("Review", string(t.Review)))
	}
	if t.Risk != "" {
		lines = append(lines, m.field("Risk", string(t.Risk)))
	}
	lines = append(lines, m.field("Type", string(t.Type)))
	lines = append(lines, m.field("Priority", fmt.Sprintf("P%d", t.Priority)))

	if t.Assignee != "" {
		lines = append(lines, m.field("Assignee", t.Assignee))
	}
	if t.Parent != "" {
		lines = append(lines, m.field("Parent", t.Parent))
	}
	if len(t.Deps) > 0 {
		lines = append(lines, m.field("Deps", strings.Join(t.Deps, ", ")))
	}
	if len(t.Links) > 0 {
		lines = append(lines, m.field("Links", strings.Join(t.Links, ", ")))
	}
	if len(t.Tags) > 0 {
		lines = append(lines, m.field("Tags", strings.Join(t.Tags, ", ")))
	}
	if t.ExternalRef != "" {
		lines = append(lines, m.field("External Ref", t.ExternalRef))
	}
	lines = append(lines, m.field("Created", t.Created.Format("2006-01-02 15:04")))

	lines = append(lines, "")

	// Body sections.
	pad := "  "
	avail := m.width - len(pad)
	if avail < 20 {
		avail = 20
	}
	body := t.Body
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "## ") {
				lines = append(lines, pad+sectionStyle.Render(line))
			} else {
				for _, wl := range wrapText(line, avail) {
					lines = append(lines, pad+wl.text)
				}
			}
		}
	}

	// Review log.
	if len(t.Reviews) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Review Log"))
		lines = append(lines, "")
		for _, r := range t.Reviews {
			verdict := strings.ToUpper(r.Verdict)
			ts := r.Timestamp.Format("2006-01-02 15:04")
			entry := fmt.Sprintf("[%s] %s %s", r.Reviewer, verdict, ts)
			if r.Comment != "" {
				entry += " — " + r.Comment
			}
			for _, wl := range wrapText(entry, avail) {
				lines = append(lines, pad+wl.text)
			}
		}
		lines = append(lines, "")
	}

	// Notes.
	if len(t.Notes) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Notes"))
		lines = append(lines, "")
		for _, n := range t.Notes {
			lines = append(lines, pad+timestampStyle.Render(n.Timestamp.Format("2006-01-02 15:04:05")))
			for _, wl := range wrapText(n.Text, avail) {
				lines = append(lines, pad+wl.text)
			}
			lines = append(lines, "")
		}
	}

	return lines
}

func (m detailModel) field(key, val string) string {
	return "  " + fieldKeyStyle.Render(key+":") + " " + fieldValStyle.Render(val)
}
