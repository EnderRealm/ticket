package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/EnderRealm/ticket/pkg/ticket"
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

func TestFormEditNoteSubmits(t *testing.T) {
	m := formModel{
		editID:   "test-123",
		typeIdx:  0,
		priority: 2,
		width:    80,
		height:   40,
		stages:   []ticket.Stage{ticket.StageTriage, ticket.StageSpec},
	}
	m.fields[fieldTitle] = "Test ticket"
	m.fields[fieldDescription] = "description"

	// Tab to the note field.
	m.focus = fieldNote

	// Type "hello" into the note field.
	for _, ch := range "hello" {
		m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}

	if m.fields[fieldNote] != "hello" {
		t.Fatalf("note field: got %q, want %q", m.fields[fieldNote], "hello")
	}

	// Press enter to submit.
	var cmd tea.Cmd
	m, cmd = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit command, got nil")
	}

	msg := cmd()
	submit, ok := msg.(formSubmitMsg)
	if !ok {
		t.Fatalf("expected formSubmitMsg, got %T", msg)
	}
	if submit.note != "hello" {
		t.Errorf("submitted note: got %q, want %q", submit.note, "hello")
	}
}

func TestFormCtrlSSubmitsFromChoiceField(t *testing.T) {
	m := formModel{
		editID:   "test-456",
		typeIdx:  1,
		priority: 2,
		width:    80,
		height:   40,
		stages:   []ticket.Stage{ticket.StageTriage, ticket.StageSpec},
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
