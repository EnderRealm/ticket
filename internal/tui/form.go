package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

var (
	formLabelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Width(14)
	formActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	formCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	formHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var ticketTypes = []ticket.TicketType{
	ticket.TypeTask,
	ticket.TypeFeature,
	ticket.TypeBug,
	ticket.TypeEpic,
	ticket.TypeChore,
}

type formField int

const (
	fieldTitle formField = iota
	fieldDescription
	fieldType
	fieldPriority
	fieldAssignee
	fieldStage
	fieldNote
	fieldCount
)

type formModel struct {
	editID   string // non-empty = edit mode
	fields   [fieldCount]string
	cursors  [fieldCount]int // cursor position per text field
	focus    formField
	typeIdx  int
	priority int
	stageIdx int
	stages   []ticket.Stage // valid stages for ticket type
	width    int
	height   int
}

func (m formModel) lastField() formField {
	if m.editID != "" {
		return fieldNote
	}
	return fieldAssignee
}

func (m formModel) isEditOnlyField(f formField) bool {
	return f == fieldStage || f == fieldNote
}

func (m formModel) isTextField(f formField) bool {
	return f == fieldTitle || f == fieldDescription || f == fieldAssignee || f == fieldNote
}

func (m formModel) isMultilineField(f formField) bool {
	return f == fieldDescription || f == fieldNote
}

func newFormModel(w, h int) formModel {
	return formModel{
		typeIdx:  0, // task
		priority: 2,
		width:    w,
		height:   h,
	}
}

func newEditFormModel(t *ticket.Ticket, w, h int) formModel {
	typeIdx := 0
	for i, tt := range ticketTypes {
		if tt == t.Type {
			typeIdx = i
			break
		}
	}
	stages, _ := ticket.PipelineFor(t.Type, t.Risk)
	stageIdx := 0
	for i, s := range stages {
		if s == t.Stage {
			stageIdx = i
			break
		}
	}

	m := formModel{
		editID:   t.ID,
		typeIdx:  typeIdx,
		priority: t.Priority,
		stageIdx: stageIdx,
		stages:   stages,
		width:    w,
		height:   h,
	}
	m.fields[fieldTitle] = t.Title
	m.fields[fieldDescription] = extractDescription(t.Body)
	m.fields[fieldAssignee] = t.Assignee
	m.cursors[fieldTitle] = len(m.fields[fieldTitle])
	m.cursors[fieldDescription] = len(m.fields[fieldDescription])
	m.cursors[fieldAssignee] = len(m.fields[fieldAssignee])
	return m
}

func extractDescription(body string) string {
	idx := strings.Index(body, "\n## ")
	if idx >= 0 {
		return strings.TrimSpace(body[:idx])
	}
	return strings.TrimSpace(body)
}

func (m *formModel) setSize(w, h int) {
	m.width = w
	m.height = h
}

func (m formModel) update(msg tea.Msg) (formModel, tea.Cmd) {
	numFields := int(m.lastField()) + 1

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return formCancelMsg{} }

		case "ctrl+s":
			return m, m.submit

		case "tab", "down":
			m.focus = formField((int(m.focus) + 1) % numFields)
		case "shift+tab", "up":
			m.focus = formField((int(m.focus) - 1 + numFields) % numFields)

		case "ctrl+j":
			if m.isMultilineField(m.focus) {
				pos := m.cursors[m.focus]
				text := m.fields[m.focus]
				m.fields[m.focus] = text[:pos] + "\n" + text[pos:]
				m.cursors[m.focus] = pos + 1
			}

		case "enter":
			if m.focus == fieldType {
				m.typeIdx = (m.typeIdx + 1) % len(ticketTypes)
			} else if m.focus == fieldPriority {
				m.priority = (m.priority + 1) % 5
			} else if m.focus == fieldStage && len(m.stages) > 0 {
				m.stageIdx = (m.stageIdx + 1) % len(m.stages)
			} else {
				return m, m.submit
			}

		case "left":
			if m.isTextField(m.focus) {
				if m.cursors[m.focus] > 0 {
					m.cursors[m.focus]--
				}
			} else if m.focus == fieldType {
				m.typeIdx = (m.typeIdx - 1 + len(ticketTypes)) % len(ticketTypes)
			} else if m.focus == fieldPriority {
				if m.priority > 0 {
					m.priority--
				}
			} else if m.focus == fieldStage && len(m.stages) > 0 {
				m.stageIdx = (m.stageIdx - 1 + len(m.stages)) % len(m.stages)
			}
		case "right":
			if m.isTextField(m.focus) {
				if m.cursors[m.focus] < len(m.fields[m.focus]) {
					m.cursors[m.focus]++
				}
			} else if m.focus == fieldType {
				m.typeIdx = (m.typeIdx + 1) % len(ticketTypes)
			} else if m.focus == fieldPriority {
				if m.priority < 4 {
					m.priority++
				}
			} else if m.focus == fieldStage && len(m.stages) > 0 {
				m.stageIdx = (m.stageIdx + 1) % len(m.stages)
			}
		case "home", "ctrl+a":
			if m.isTextField(m.focus) {
				m.cursors[m.focus] = 0
			}
		case "end", "ctrl+e":
			if m.isTextField(m.focus) {
				m.cursors[m.focus] = len(m.fields[m.focus])
			}

		case "backspace":
			if m.isTextField(m.focus) {
				pos := m.cursors[m.focus]
				text := m.fields[m.focus]
				if pos > 0 {
					m.fields[m.focus] = text[:pos-1] + text[pos:]
					m.cursors[m.focus] = pos - 1
				}
			}

		default:
			if m.isTextField(m.focus) {
				if len(msg.String()) == 1 {
					pos := m.cursors[m.focus]
					text := m.fields[m.focus]
					m.fields[m.focus] = text[:pos] + msg.String() + text[pos:]
					m.cursors[m.focus] = pos + 1
				}
			}
		}
	}
	return m, nil
}

func (m formModel) submit() tea.Msg {
	title := strings.TrimSpace(m.fields[fieldTitle])
	if title == "" {
		return nil
	}
	msg := formSubmitMsg{
		editID:      m.editID,
		title:       title,
		description: strings.TrimSpace(m.fields[fieldDescription]),
		ticketType:  ticketTypes[m.typeIdx],
		priority:    m.priority,
		assignee:    strings.TrimSpace(m.fields[fieldAssignee]),
		note:        strings.TrimSpace(m.fields[fieldNote]),
	}
	if len(m.stages) > 0 {
		msg.stage = m.stages[m.stageIdx]
	}
	return msg
}

func (m formModel) view() string {
	var b strings.Builder

	if m.editID != "" {
		title := m.fields[fieldTitle]
		if title == "" {
			title = "Untitled"
		}
		b.WriteString(titleStyle.Render("# " + title))
	} else {
		b.WriteString(titleStyle.Render("# New Ticket"))
	}
	b.WriteString("\n\n")

	labels := [fieldCount]string{"Title:", "Description:", "Type:", "Priority:", "Assignee:", "Stage:", "Note:"}
	last := m.lastField()

	for i := formField(0); i <= last; i++ {
		label := formLabelStyle.Render(labels[i])
		cursor := "  "
		if i == m.focus {
			cursor = formCursorStyle.Render("> ")
		}

		switch i {
		case fieldType:
			var parts []string
			for j, tt := range ticketTypes {
				s := string(tt)
				if j == m.typeIdx {
					s = formCursorStyle.Render("[" + s + "]")
				}
				parts = append(parts, s)
			}
			b.WriteString(cursor + label + " " + strings.Join(parts, "  ") + "\n")
		case fieldPriority:
			var parts []string
			for j := 0; j < 5; j++ {
				s := lipgloss.NewStyle().Foreground(priorityColors[j]).Render(
					strings.Repeat("P", 1) + string(rune('0'+j)),
				)
				if j == m.priority {
					s = formCursorStyle.Render("[" + s + "]")
				}
				parts = append(parts, s)
			}
			b.WriteString(cursor + label + " " + strings.Join(parts, "  ") + "\n")
		case fieldStage:
			var parts []string
			for j, s := range m.stages {
				text := string(s)
				stageColor := stageColors[s]
				styled := lipgloss.NewStyle().Foreground(stageColor).Render(text)
				if j == m.stageIdx {
					styled = formCursorStyle.Render("[" + styled + "]")
				}
				parts = append(parts, styled)
			}
			b.WriteString(cursor + label + " " + strings.Join(parts, "  ") + "\n")
		default:
			// Text fields: wrap long text across multiple lines at word boundaries.
			text := m.fields[i]
			avail := m.width - 18 // 2 cursor + 14 label + 1 space + 1 block cursor
			if avail < 1 {
				avail = 1
			}
			wrapped := wrapText(text, avail)
			pad := strings.Repeat(" ", 17) // visual width of cursor + label + space

			if i == m.focus {
				pos := m.cursors[i]
				if pos > len(text) {
					pos = len(text)
				}
				runePos := utf8.RuneCountInString(text[:pos])
				totalRunes := utf8.RuneCountInString(text)

				// Map flat rune position to wrapped line/column.
				cursorLine := len(wrapped) - 1
				cursorCol := runePos - wrapped[cursorLine].start
				for j := range wrapped {
					nextStart := totalRunes
					if j+1 < len(wrapped) {
						nextStart = wrapped[j+1].start
					}
					if runePos < nextStart {
						cursorLine = j
						cursorCol = runePos - wrapped[j].start
						break
					}
				}

				// Cursor at EOF may need an extra line.
				if cursorLine == len(wrapped)-1 {
					lineRunes := []rune(wrapped[cursorLine].text)
					if cursorCol > len(lineRunes) {
						wrapped = append(wrapped, wrappedLine{text: "", start: totalRunes})
						cursorLine = len(wrapped) - 1
						cursorCol = 0
					}
				}

				for j, wl := range wrapped {
					prefix := pad
					if j == 0 {
						prefix = cursor + label + " "
					}
					if j == cursorLine {
						lineRunes := []rune(wl.text)
						col := cursorCol
						if col > len(lineRunes) {
							col = len(lineRunes)
						}
						before := string(lineRunes[:col])
						after := string(lineRunes[col:])
						b.WriteString(prefix + formActiveStyle.Render(before) + formCursorStyle.Render("█") + formActiveStyle.Render(after) + "\n")
					} else {
						b.WriteString(prefix + formActiveStyle.Render(wl.text) + "\n")
					}
				}
			} else {
				for j, wl := range wrapped {
					prefix := pad
					if j == 0 {
						prefix = cursor + label + " "
					}
					b.WriteString(prefix + wl.text + "\n")
				}
			}
		}
	}

	b.WriteString("\n")
	help := "tab/↑↓ fields  ←→ move/cycle  ctrl+j newline  ctrl+s/enter save  esc cancel"
	b.WriteString(formHelpStyle.Render(help))

	return b.String()
}

// Messages
type formSubmitMsg struct {
	editID      string // non-empty = edit mode
	title       string
	description string
	ticketType  ticket.TicketType
	priority    int
	assignee    string
	stage       ticket.Stage
	note        string
}

type formCancelMsg struct{}

type wrappedLine struct {
	text  string
	start int // rune offset in original text
}

// wrapText breaks s into lines of at most width runes, respecting explicit
// newlines first, then preferring spaces as break points so words stay
// intact. Falls back to hard breaks for words longer than width.
func wrapText(s string, width int) []wrappedLine {
	segments := strings.Split(s, "\n")
	var result []wrappedLine
	runeOffset := 0

	for i, seg := range segments {
		if i > 0 {
			runeOffset++ // account for the \n rune
		}
		segRunes := []rune(seg)
		if len(segRunes) <= width {
			result = append(result, wrappedLine{text: seg, start: runeOffset})
		} else {
			result = append(result, wrapSegment(segRunes, width, runeOffset)...)
		}
		runeOffset += len(segRunes)
	}

	return result
}

// wrapSegment word-wraps a single line (no newlines) into wrapped lines.
func wrapSegment(runes []rune, width, baseOffset int) []wrappedLine {
	var result []wrappedLine
	lineStart := 0
	lastSpace := -1

	for i := 0; i < len(runes); i++ {
		if runes[i] == ' ' {
			lastSpace = i
		}
		if i-lineStart+1 > width {
			if lastSpace > lineStart {
				breakAt := lastSpace
				for breakAt > lineStart && runes[breakAt-1] == ' ' {
					breakAt--
				}
				result = append(result, wrappedLine{
					text:  string(runes[lineStart:breakAt]),
					start: baseOffset + lineStart,
				})
				lineStart = lastSpace + 1
				for lineStart < len(runes) && runes[lineStart] == ' ' {
					lineStart++
				}
			} else {
				result = append(result, wrappedLine{
					text:  string(runes[lineStart:i]),
					start: baseOffset + lineStart,
				})
				lineStart = i
			}
			lastSpace = -1
		}
	}
	result = append(result, wrappedLine{
		text:  string(runes[lineStart:]),
		start: baseOffset + lineStart,
	})
	return result
}
