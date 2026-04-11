package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

// Aliases to centralized styles (styles.go)
var (
	formLabelStyle      = StyleFieldKey
	formActiveStyle     = StyleInputText
	formCursorStyle     = StyleCursor
	formTextCursorStyle = StyleTextCursor
	formHelpStyle       = StyleHelp
)

var ticketTypes = []ticket.TicketType{
	ticket.TypeFeature,
	ticket.TypeBug,
	ticket.TypeEpic,
}

type formField int

const (
	fieldTitle formField = iota
	fieldDescription
	fieldType
	fieldPriority
	fieldStatus
	fieldNote
	fieldCount
)

var allStatuses = []ticket.Status{
	ticket.StatusBacklog,
	ticket.StatusReady,
	ticket.StatusOpen,
	ticket.StatusDone,
	ticket.StatusClosed,
}

type formModel struct {
	editID    string // non-empty = edit mode
	fields    [fieldCount]string
	cursors   [fieldCount]int // cursor position per text field
	focus     formField
	typeIdx   int
	priority  int
	statusIdx int
	offset    int
	width     int
	height    int
}

func (m formModel) lastField() formField {
	if m.editID != "" {
		return fieldNote
	}
	return fieldPriority
}

func (m formModel) isEditOnlyField(f formField) bool {
	return f == fieldStatus || f == fieldNote
}

func (m formModel) isTextField(f formField) bool {
	return f == fieldTitle || f == fieldDescription || f == fieldNote
}

func (m formModel) isMultilineField(f formField) bool {
	return f == fieldDescription || f == fieldNote
}

func newFormModel(w, h int) formModel {
	return formModel{
		typeIdx:  0, // feature
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
	statusIdx := 0
	for i, s := range allStatuses {
		if s == t.Status {
			statusIdx = i
			break
		}
	}

	m := formModel{
		editID:    t.ID,
		typeIdx:   typeIdx,
		priority:  t.Priority,
		statusIdx: statusIdx,
		width:     w,
		height:    h,
	}
	m.fields[fieldTitle] = t.Title
	m.fields[fieldDescription] = extractDescription(t.Body)
	m.cursors[fieldTitle] = len(m.fields[fieldTitle])
	m.cursors[fieldDescription] = len(m.fields[fieldDescription])
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

func (m formModel) visibleRows() int {
	rows := m.height - 2 // help bar + blank line before it
	if rows < 1 {
		rows = 1
	}
	return rows
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
			} else if m.focus == fieldStatus {
				m.statusIdx = (m.statusIdx + 1) % len(allStatuses)
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
			} else if m.focus == fieldStatus {
				m.statusIdx = (m.statusIdx - 1 + len(allStatuses)) % len(allStatuses)
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
			} else if m.focus == fieldStatus {
				m.statusIdx = (m.statusIdx + 1) % len(allStatuses)
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
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.offset -= 3
			if m.offset < 0 {
				m.offset = 0
			}
		case tea.MouseButtonWheelDown:
			m.offset += 3
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
		note:        strings.TrimSpace(m.fields[fieldNote]),
	}
	if m.editID != "" {
		msg.status = allStatuses[m.statusIdx]
	}
	return msg
}

func (m formModel) view() string {
	// Render all form content into lines first, then apply viewport clipping.
	var lines []string

	lines = append(lines, "") // leading blank

	labels := [fieldCount]string{"Title:", "Description:", "Type:", "Priority:", "Status:", "Note:"}
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
				s := ColorType(tt, string(tt))
				if j == m.typeIdx {
					s = formTextCursorStyle.Render(" " + s + " ")
				}
				parts = append(parts, s)
			}
			lines = append(lines, cursor+label+" "+strings.Join(parts, "  "))
		case fieldPriority:
			var parts []string
			for j := 0; j < 5; j++ {
				s := ColorPriority(j, "P"+string(rune('0'+j)))
				if j == m.priority {
					s = formTextCursorStyle.Render(" " + s + " ")
				}
				parts = append(parts, s)
			}
			lines = append(lines, cursor+label+" "+strings.Join(parts, "  "))
		case fieldStatus:
			var parts []string
			for j, s := range allStatuses {
				text := string(s)
				styled := ColorStatus(s, text)
				if j == m.statusIdx {
					styled = formTextCursorStyle.Render(" " + styled + " ")
				}
				parts = append(parts, styled)
			}
			lines = append(lines, cursor+label+" "+strings.Join(parts, "  "))
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
						cursorChar := " "
						afterStart := col
						if col < len(lineRunes) {
							cursorChar = string(lineRunes[col])
							afterStart = col + 1
						}
						after := string(lineRunes[afterStart:])
						lines = append(lines, prefix+formActiveStyle.Render(before)+formTextCursorStyle.Render(cursorChar)+formActiveStyle.Render(after))
					} else {
						lines = append(lines, prefix+formActiveStyle.Render(wl.text))
					}
				}
			} else {
				for j, wl := range wrapped {
					prefix := pad
					if j == 0 {
						prefix = cursor + label + " "
					}
					lines = append(lines, prefix+wl.text)
				}
			}
		}
	}

	// Viewport clipping.
	visible := m.visibleRows()
	maxOffset := len(lines) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := m.offset
	if offset > maxOffset {
		offset = maxOffset
	}

	end := offset + visible
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder
	for i := offset; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	// Pad remaining rows.
	for i := end - offset; i < visible; i++ {
		b.WriteString("\n")
	}

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
	status      ticket.Status
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
