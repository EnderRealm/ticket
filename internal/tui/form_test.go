package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestWrapText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		width    int
		wantText []string
	}{
		{"empty", "", 10, []string{""}},
		{"fits", "hello", 10, []string{"hello"}},
		{"exact fit", "hello", 5, []string{"hello"}},
		{"break at space", "hello world", 7, []string{"hello", "world"}},
		{"multiple words", "the quick brown fox", 10, []string{"the quick", "brown fox"}},
		{"long word hard break", "abcdefghij", 5, []string{"abcde", "fghij"}},
		{"mixed break", "hi abcdefghij end", 8, []string{"hi", "abcdefgh", "ij end"}},
		{"trailing space consumed", "abc def ghi", 4, []string{"abc", "def", "ghi"}},
		{"multiple spaces consumed", "hello  world", 7, []string{"hello", "world"}},
		{"single char width", "a b", 1, []string{"a", "b"}},
		{"multibyte runes", "café mocha", 5, []string{"café", "mocha"}},
		{"explicit newline", "hello\nworld", 20, []string{"hello", "world"}},
		{"newline with wrap", "hello\nthe quick brown fox", 10, []string{"hello", "the quick", "brown fox"}},
		{"multiple newlines", "a\nb\nc", 10, []string{"a", "b", "c"}},
		{"trailing newline", "hello\n", 10, []string{"hello", ""}},
		{"consecutive newlines", "hello\n\nworld", 10, []string{"hello", "", "world"}},
		{"newline only", "\n", 10, []string{"", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.input, tt.width)
			if len(got) != len(tt.wantText) {
				var gotTexts []string
				for _, wl := range got {
					gotTexts = append(gotTexts, wl.text)
				}
				t.Fatalf("wrapText(%q, %d) lines = %v (len %d), want %v (len %d)",
					tt.input, tt.width, gotTexts, len(got), tt.wantText, len(tt.wantText))
			}
			for i := range got {
				if got[i].text != tt.wantText[i] {
					t.Errorf("line %d: got %q, want %q", i, got[i].text, tt.wantText[i])
				}
			}
		})
	}
}

func TestWrapTextStartOffsets(t *testing.T) {
	wrapped := wrapText("hello world foo", 7)
	// "hello" starts at 0, "world" starts at 6, "foo" starts at 12
	if len(wrapped) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(wrapped))
	}
	wantStarts := []int{0, 6, 12}
	for i, ws := range wantStarts {
		if wrapped[i].start != ws {
			t.Errorf("line %d start: got %d, want %d", i, wrapped[i].start, ws)
		}
	}
}

func TestWrapTextNewlineOffsets(t *testing.T) {
	// "hello\nworld" — runes: h(0) e(1) l(2) l(3) o(4) \n(5) w(6) o(7) r(8) l(9) d(10)
	wrapped := wrapText("hello\nworld", 20)
	if len(wrapped) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(wrapped))
	}
	// "hello" starts at rune 0, "world" starts at rune 6 (after the \n)
	wantStarts := []int{0, 6}
	for i, ws := range wantStarts {
		if wrapped[i].start != ws {
			t.Errorf("line %d start: got %d, want %d", i, wrapped[i].start, ws)
		}
	}
}

func TestFormEnterAdvancesField(t *testing.T) {
	m := newFormModel(80, 40)
	m.fields[fieldTitle] = "Test ticket"
	m.focus = fieldTitle

	// Enter on a non-last field advances focus instead of submitting.
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected nil command (no submit) on enter from title, got %v", cmd)
	}
	if m.focus != fieldDescription {
		t.Errorf("focus after enter: got %v, want %v", m.focus, fieldDescription)
	}
	// Title text is untouched.
	if m.fields[fieldTitle] != "Test ticket" {
		t.Errorf("title after enter: got %q, want %q", m.fields[fieldTitle], "Test ticket")
	}

	// Enter from a selector also just advances (no cycling).
	m.focus = fieldType
	typeBefore := m.typeIdx
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected nil command on enter from type, got %v", cmd)
	}
	if m.focus != fieldPriority {
		t.Errorf("focus after enter from type: got %v, want %v", m.focus, fieldPriority)
	}
	if m.typeIdx != typeBefore {
		t.Errorf("enter should not cycle type: got %d, want %d", m.typeIdx, typeBefore)
	}
}

func TestFormEnterOnLastFieldSaves(t *testing.T) {
	// Create mode: last field is Status.
	m := newFormModel(80, 40)
	m.fields[fieldTitle] = "Test ticket"
	m.focus = m.lastField()
	if m.focus != fieldStatus {
		t.Fatalf("create lastField: got %v, want %v", m.focus, fieldStatus)
	}
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit command from enter on last field, got nil")
	}
	if _, ok := cmd().(formSubmitMsg); !ok {
		t.Fatalf("expected formSubmitMsg from enter on last field")
	}

	// Edit mode: last field is the multiline Note. Enter there saves (not newline).
	em := newEditFormModel(&ticket.Ticket{
		ID:       "test-123",
		Title:    "Test ticket",
		Type:     ticket.TypeBug,
		Priority: 2,
		Status:   ticket.StatusOpen,
	}, 80, 40)
	em.focus = em.lastField()
	if em.focus != fieldNote {
		t.Fatalf("edit lastField: got %v, want %v", em.focus, fieldNote)
	}
	em, cmd = em.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit command from enter on note (last field in edit), got nil")
	}
	if em.fields[fieldNote] != "" {
		t.Errorf("enter on last field should not insert a newline, got %q", em.fields[fieldNote])
	}
	if _, ok := cmd().(formSubmitMsg); !ok {
		t.Fatalf("expected formSubmitMsg from enter on note")
	}
}

func TestCreateFormStatusBacklogReadyOnly(t *testing.T) {
	// Create mode offers only backlog/ready, defaulting to backlog.
	m := newFormModel(80, 40)
	if got := m.statusOptions(); len(got) != 2 || got[0] != ticket.StatusBacklog || got[1] != ticket.StatusReady {
		t.Fatalf("create statusOptions: got %v, want [backlog ready]", got)
	}

	// Default submit carries backlog.
	m.fields[fieldTitle] = "Test ticket"
	if msg := m.submit().(formSubmitMsg); msg.status != ticket.StatusBacklog {
		t.Errorf("default create status: got %q, want %q", msg.status, ticket.StatusBacklog)
	}

	// Cycling the Status field to ready submits ready.
	m.focus = fieldStatus
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRight})
	if m.statusIdx != 1 {
		t.Fatalf("statusIdx after right: got %d, want 1", m.statusIdx)
	}
	if msg := m.submit().(formSubmitMsg); msg.status != ticket.StatusReady {
		t.Errorf("create status after selecting ready: got %q, want %q", msg.status, ticket.StatusReady)
	}

	// Cycling stays within the two options (wraps, never reaches open).
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRight})
	if m.statusIdx != 0 {
		t.Errorf("statusIdx should wrap to 0 after ready, got %d", m.statusIdx)
	}
}

func TestEditFormStatusUnchanged(t *testing.T) {
	// Edit mode still offers all five statuses.
	m := newEditFormModel(&ticket.Ticket{
		ID:       "test-123",
		Title:    "Test",
		Type:     ticket.TypeBug,
		Priority: 2,
		Status:   ticket.StatusOpen,
	}, 80, 40)
	if got := m.statusOptions(); len(got) != len(allStatuses) {
		t.Fatalf("edit statusOptions: got %d options, want %d", len(got), len(allStatuses))
	}
}

func TestFormCtrlJInsertsNewlineMultiline(t *testing.T) {
	m := newEditFormModel(&ticket.Ticket{
		ID:       "test-123",
		Title:    "Test ticket",
		Type:     ticket.TypeBug,
		Priority: 2,
		Status:   ticket.StatusOpen,
	}, 80, 40)
	m.focus = fieldNote

	for _, ch := range "hello" {
		m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}

	// ctrl+j inserts a newline without submitting.
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if cmd != nil {
		t.Fatalf("expected nil command from ctrl+j, got %v", cmd)
	}
	if m.fields[fieldNote] != "hello\n" {
		t.Errorf("note after ctrl+j: got %q, want %q", m.fields[fieldNote], "hello\n")
	}
	if m.cursors[fieldNote] != len("hello\n") {
		t.Errorf("cursor after ctrl+j: got %d, want %d", m.cursors[fieldNote], len("hello\n"))
	}

	// ctrl+s submits, trailing newline trimmed.
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected submit command from ctrl+s, got nil")
	}
	submit, ok := cmd().(formSubmitMsg)
	if !ok {
		t.Fatalf("expected formSubmitMsg, got %T", cmd())
	}
	if submit.note != "hello" {
		t.Errorf("submitted note: got %q, want %q", submit.note, "hello")
	}
}

func TestFormFooterHints(t *testing.T) {
	m := newFormModel(120, 40)

	// Non-last, single-line field: "enter next", no newline hint.
	m.focus = fieldTitle
	out := m.view()
	if !strings.Contains(out, "enter next") {
		t.Error("footer on title should show 'enter next'")
	}
	if strings.Contains(out, "ctrl+j newline") {
		t.Error("footer on single-line field should not show 'ctrl+j newline'")
	}

	// Multiline field: newline hint present.
	m.focus = fieldDescription
	out = m.view()
	if !strings.Contains(out, "ctrl+j newline") {
		t.Error("footer on multiline field should show 'ctrl+j newline'")
	}

	// Last field: "enter save" alongside ctrl+s.
	m.focus = m.lastField()
	out = m.view()
	if !strings.Contains(out, "enter save") {
		t.Error("footer on last field should show 'enter save'")
	}
	if !strings.Contains(out, "ctrl+s save") {
		t.Error("footer should always show 'ctrl+s save'")
	}

	// Edit mode: last field (Note) is both multiline and last, so the footer
	// shows both 'enter save' and 'ctrl+j newline'.
	em := newEditFormModel(&ticket.Ticket{
		ID:       "test-123",
		Title:    "Test",
		Type:     ticket.TypeBug,
		Priority: 2,
		Status:   ticket.StatusOpen,
	}, 120, 40)
	em.focus = em.lastField()
	out = em.view()
	if !strings.Contains(out, "enter save") {
		t.Error("footer on edit last field should show 'enter save'")
	}
	if !strings.Contains(out, "ctrl+j newline") {
		t.Error("footer on edit last field (multiline note) should show 'ctrl+j newline'")
	}
}

func TestFormPasteIntoTextFields(t *testing.T) {
	// Multiline Note field: newline preserved, cursor at byte end.
	m := formModel{
		editID:    "test-123",
		typeIdx:   0,
		priority:  2,
		width:     80,
		height:    40,
		statusIdx: 0,
	}
	m.focus = fieldNote

	pasted := "line1\nline2"
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(pasted)})
	if cmd != nil {
		t.Fatalf("expected nil command on paste, got %v", cmd)
	}
	if m.fields[fieldNote] != pasted {
		t.Errorf("note after paste: got %q, want %q", m.fields[fieldNote], pasted)
	}
	if m.cursors[fieldNote] != len(pasted) {
		t.Errorf("cursor after paste: got %d, want %d", m.cursors[fieldNote], len(pasted))
	}

	// Single-line Title field: newline becomes a space, no submit.
	m.focus = fieldTitle
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("line1\nline2")})
	if cmd != nil {
		t.Fatalf("expected nil command on title paste, got %v", cmd)
	}
	if m.fields[fieldTitle] != "line1 line2" {
		t.Errorf("title after paste: got %q, want %q", m.fields[fieldTitle], "line1 line2")
	}
}

func TestFormCtrlSSubmitsFromChoiceField(t *testing.T) {
	m := formModel{
		editID:    "test-456",
		typeIdx:   1,
		priority:  2,
		width:     80,
		height:    40,
		statusIdx: 0,
	}
	m.fields[fieldTitle] = "Test ticket"

	// Focus on a choice field where enter would cycle instead of submit.
	m.focus = fieldType

	// ctrl+s should submit regardless.
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("expected submit command from ctrl+s, got nil")
	}

	msg := cmd()
	submit, ok := msg.(formSubmitMsg)
	if !ok {
		t.Fatalf("expected formSubmitMsg, got %T", msg)
	}
	if submit.title != "Test ticket" {
		t.Errorf("submitted title: got %q, want %q", submit.title, "Test ticket")
	}
	if submit.ticketType != ticketTypes[1] {
		t.Errorf("submitted type: got %v, want %v", submit.ticketType, ticketTypes[1])
	}
}

func TestFormViewSelectorsRenderOptions(t *testing.T) {
	m := newEditFormModel(&ticket.Ticket{
		ID:       "test-789",
		Title:    "Test",
		Type:     ticket.TypeFeature,
		Priority: 2,
		Status:   ticket.StatusOpen,
	}, 80, 40)

	output := m.view()

	wantLabels := []string{
		string(ticket.TypeFeature), string(ticket.TypeBug), string(ticket.TypeEpic),
		"P0", "P1", "P2", "P3", "P4",
		string(ticket.StatusBacklog), string(ticket.StatusReady), string(ticket.StatusOpen),
		string(ticket.StatusDone), string(ticket.StatusClosed),
	}
	for _, label := range wantLabels {
		if !strings.Contains(output, label) {
			t.Errorf("form view missing selector option %q", label)
		}
	}
}

func TestFormViewSelectorFocusChangesRendering(t *testing.T) {
	// Force a true-color profile so chip styles emit their color codes. Without
	// it the styled substrings collapse to bare/padded text that collides with
	// layout spacing, making the negative assertions below unreliable.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	base := newFormModel(80, 40)
	base.fields[fieldTitle] = "Test"

	focused := base
	focused.focus = fieldType
	focusedOut := focused.view()

	unfocused := base
	unfocused.focus = fieldTitle
	unfocusedOut := unfocused.view()

	if focusedOut == unfocusedOut {
		t.Fatal("expected type selector rendering to differ when its row is focused vs not")
	}

	// Focused: selected option renders as the inverse chip; unfocused: as bold
	// bright plain text (no chip).
	selected := ticketTypes[base.typeIdx]
	focusedChip := StyleChipSelectedFocused.Render(string(selected))
	if !strings.Contains(focusedOut, focusedChip) {
		t.Errorf("focused type selector should render selected option as the focused chip %q", focusedChip)
	}
	if strings.Contains(unfocusedOut, focusedChip) {
		t.Errorf("unfocused type selector should not render the focused chip %q", focusedChip)
	}
	if !strings.Contains(unfocusedOut, StyleChipSelected.Render(string(selected))) {
		t.Error("unfocused type selector should render selected option as bold bright text")
	}

	// An unselected option always uses the dim/muted styling, never a selected chip.
	unselected := string(ticketTypes[1])
	if !strings.Contains(focusedOut, StyleChipUnselected.Render(unselected)) {
		t.Error("unselected option should render with dim/muted styling")
	}
	if strings.Contains(focusedOut, StyleChipSelectedFocused.Render(unselected)) {
		t.Error("unselected option should not be rendered with the focused chip styling")
	}
}

func TestFormViewWrapsTextField(t *testing.T) {
	// width=38 → avail = 38-18 = 20 chars per line
	m := newFormModel(38, 40)
	m.fields[fieldTitle] = "Short title"
	m.fields[fieldDescription] = "the quick brown fox jumps over the lazy dog"

	output := m.view()

	// Should not truncate with ellipsis.
	if strings.Contains(output, "…") {
		t.Error("wrapped text should not contain ellipsis truncation")
	}

	// Words should not be split across lines.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// No line should end with a partial word like "th" from "the"
		// (this is a sanity check — hard to test exhaustively)
		if strings.HasSuffix(trimmed, "the laz") {
			t.Error("word 'lazy' should not be split across lines")
		}
	}
}
