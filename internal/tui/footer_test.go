package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/pkg/ticket"
)

// frameHeight counts the rendered lines of a view, treating it as a terminal
// frame. A trailing newline is ignored so the count matches visible rows.
func frameHeight(s string) int {
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

func newTestApp(w, h int) App {
	a := App{activeTab: tabInbox, width: w, height: h}
	a.dashboard.activeTab = tabInbox
	a.dashboard.setSize(w, a.contentHeight())
	a.epics.setSize(w, a.contentHeight())
	return a
}

func TestAppFooterWrapsWhenNarrow(t *testing.T) {
	const w, h = 40, 30
	a := newTestApp(w, h)

	footer, lines := a.footerView()
	if lines < 2 {
		t.Errorf("narrow footer should wrap to >= 2 lines, got %d:\n%s", lines, footer)
	}
	if strings.Count(footer, "\n") != lines-1 {
		t.Errorf("footer line count %d disagrees with newlines %d", lines, strings.Count(footer, "\n"))
	}
	if !strings.Contains(footer, "(q)uit") {
		t.Errorf("wrapped footer should still contain trailing (q)uit, got:\n%s", footer)
	}

	out := a.View()
	if got := frameHeight(out); got > h {
		t.Errorf("frame height %d exceeds terminal height %d (footer wrap clipped content):\n%s", got, h, out)
	}
}

func TestAppFooterSingleLineWhenWide(t *testing.T) {
	const w, h = 200, 30
	a := newTestApp(w, h)

	footer, lines := a.footerView()
	if lines != 1 {
		t.Errorf("wide footer should stay on one line, got %d:\n%s", lines, footer)
	}
	if !strings.Contains(footer, "(q)uit") {
		t.Errorf("footer should contain (q)uit, got:\n%s", footer)
	}

	out := a.View()
	if got := frameHeight(out); got > h {
		t.Errorf("frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}

func TestFormFooterWrapsWhenNarrow(t *testing.T) {
	const w, h = 40, 30
	m := newFormModel(w, h)
	m.focus = fieldDescription // multiline → extra hint, more likely to wrap

	lines := m.helpLines()
	if len(lines) < 2 {
		t.Fatalf("narrow form footer should wrap to >= 2 lines, got %d: %v", len(lines), lines)
	}

	out := m.view()
	if !strings.Contains(out, "esc cancel") {
		t.Errorf("wrapped form footer should still contain trailing 'esc cancel', got:\n%s", out)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("form frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}

func TestFormFooterSingleLineWhenWide(t *testing.T) {
	const w, h = 200, 30
	m := newFormModel(w, h)
	m.focus = fieldDescription

	if lines := m.helpLines(); len(lines) != 1 {
		t.Errorf("wide form footer should stay on one line, got %d: %v", len(lines), lines)
	}

	out := m.view()
	if !strings.Contains(out, "esc cancel") {
		t.Errorf("form footer should contain 'esc cancel', got:\n%s", out)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("form frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}

func detailTestModel(w, h int) detailModel {
	return newDetailModel(&ticket.Ticket{
		ID:     "test-abcd",
		Title:  "A ticket",
		Status: ticket.StatusOpen,
		Type:   ticket.TypeFeature,
	}, w, h)
}

func TestDetailFooterWrapsWhenNarrow(t *testing.T) {
	const w, h = 40, 30
	m := detailTestModel(w, h)

	lines := m.helpLines()
	if len(lines) < 2 {
		t.Fatalf("narrow detail footer should wrap to >= 2 lines, got %d: %v", len(lines), lines)
	}

	out := m.view()
	if !strings.Contains(out, "(q)uit") {
		t.Errorf("wrapped detail footer should still contain trailing (q)uit, got:\n%s", out)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("detail frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}

func TestDetailFooterSingleLineWhenWide(t *testing.T) {
	const w, h = 200, 30
	m := detailTestModel(w, h)

	if lines := m.helpLines(); len(lines) != 1 {
		t.Errorf("wide detail footer should stay on one line, got %d: %v", len(lines), lines)
	}

	out := m.view()
	if !strings.Contains(out, "(q)uit") {
		t.Errorf("detail footer should contain (q)uit, got:\n%s", out)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("detail frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}

func TestEpicsFooterWrapsWhenNarrow(t *testing.T) {
	const w, h = 40, 30
	a := newTestApp(w, h)
	a.activeTab = tabEpics

	_, lines := a.footerView()
	if lines < 2 {
		t.Fatalf("narrow epics footer should wrap to >= 2 lines, got %d", lines)
	}

	out := a.View()
	if !strings.Contains(out, "(q)uit") {
		t.Errorf("epics footer should contain (q)uit, got:\n%s", out)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("epics frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}
