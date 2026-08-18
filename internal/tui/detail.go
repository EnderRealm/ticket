package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

// Aliases to centralized styles (styles.go)
var (
	fieldKeyStyle   = StyleFieldKey
	fieldValStyle   = StyleFieldVal
	sectionStyle    = StyleSection
	timestampStyle  = StyleTimestamp
	detailHelpStyle = StyleHelp
	inputLabelStyle = StyleInputLabel
)

type inputMode int

const (
	inputNone inputMode = iota
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
}

func (m *detailModel) startMovePicker(repoRoot string) {
	repos := discoverSiblingRepos(repoRoot)
	repos = append(repos, enterPathOption)
	m.pickerItems = repos
	m.pickerCursor = 0
	m.input = inputMovePicker
	m.inputText = ""
}

// discoverSiblingRepos finds git repositories adjacent to the given repo. The
// repo is the caller's working directory, not the tickets directory: a central
// project's tickets live under the store root, whose siblings are other
// projects' ticket directories rather than repos.
func discoverSiblingRepos(repoRoot string) []string {
	parentDir := filepath.Dir(repoRoot) // /path/to/repo -> /path/to
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

// helpLines returns the footer help wrapped to the view width so a narrow
// terminal shows every shortcut instead of clipping the trailing ones.
func (m detailModel) helpLines() []string {
	var help string
	if m.input == inputMovePicker {
		help = "↑↓ select  enter confirm  esc cancel"
	} else if m.input != inputNone {
		help = "enter confirm  esc cancel"
	} else {
		help = "↑↓/jk scroll  │  (e)dit (p)riority (n)ote (m)ove (y)ank (w)ork  │  esc back  (q)uit"
	}
	return wrapHelp(help, m.width)
}

func (m detailModel) visibleRows() int {
	rows := m.height - len(m.helpLines())
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
	case tea.MouseMsg:
		maxOffset := max(0, len(m.lines)-m.visibleRows())
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.offset -= 3
			if m.offset < 0 {
				m.offset = 0
			}
		case tea.MouseButtonWheelDown:
			m.offset += 3
			if m.offset > maxOffset {
				m.offset = maxOffset
			}
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
		case inputNote:
			label = "note"
		case inputMove:
			label = "move to repo"
		}
		b.WriteString(inputLabelStyle.Render(label+": ") + m.inputText + "█\n")
	}

	// Help bar.
	for i, l := range m.helpLines() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(detailHelpStyle.Render(l))
	}

	return b.String()
}

func (m detailModel) render() []string {
	t := m.ticket
	var lines []string

	// Frontmatter fields.
	lines = append(lines, m.field("Title", ticket.SanitizeControl(t.Title)))
	lines = append(lines, m.field("ID", ticket.SanitizeControl(t.ID)))
	if t.Status != "" {
		lines = append(lines, m.field("Status", StatusBadge(t.Status)))
	}
	lines = append(lines, m.field("Type", TypeBadge(t.Type)))
	lines = append(lines, m.field("Priority", PriorityBadge(t.Priority)))

	if t.Parent != "" {
		lines = append(lines, m.field("Parent", ticket.SanitizeControl(t.Parent)))
	}
	if len(t.Deps) > 0 {
		lines = append(lines, m.field("Deps", ticket.SanitizeControl(strings.Join(t.Deps, ", "))))
	}
	if len(t.Links) > 0 {
		lines = append(lines, m.field("Links", ticket.SanitizeControl(strings.Join(t.Links, ", "))))
	}
	if len(t.Tags) > 0 {
		lines = append(lines, m.field("Tags", ticket.SanitizeControl(strings.Join(t.Tags, ", "))))
	}
	if t.ExternalRef != "" {
		lines = append(lines, m.field("External Ref", ticket.SanitizeControl(t.ExternalRef)))
	}
	lines = append(lines, m.field("Created", t.Created.Local().Format("2006-01-02 15:04")))

	if len(t.Extra) > 0 {
		keys := make([]string, 0, len(t.Extra))
		for k := range t.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, m.field(ticket.SanitizeControl(k), ticket.SanitizeControl(t.Extra[k])))
		}
	}

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
			line = ticket.SanitizeControl(line)
			if strings.HasPrefix(line, "## ") {
				lines = append(lines, pad+sectionStyle.Render(line))
			} else {
				for _, wl := range wrapText(line, avail) {
					lines = append(lines, pad+wl.text)
				}
			}
		}
	}

	// Notes.
	if len(t.Notes) > 0 {
		lines = append(lines, pad+sectionStyle.Render("## Notes"))
		lines = append(lines, "")
		for _, n := range t.Notes {
			lines = append(lines, pad+timestampStyle.Render(n.Timestamp.Local().Format("2006-01-02 15:04:05")))
			for _, wl := range wrapText(n.Text, avail) {
				lines = append(lines, pad+ticket.SanitizeControl(wl.text))
			}
			lines = append(lines, "")
		}
	}

	return lines
}

func (m detailModel) field(key, val string) string {
	return "  " + fieldKeyStyle.Render(key+":") + " " + fieldValStyle.Render(val)
}
