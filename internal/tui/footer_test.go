package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

// When an overlay (detail/form) is active it renders its own footer, so the
// App must not also append the dashboard command/help bar underneath it.
func TestOverlaySuppressesDashboardFooter(t *testing.T) {
	const w, h = 60, 24 // narrow enough that the dashboard footer would wrap
	tk := &ticket.Ticket{ID: "x", Title: "T", Status: ticket.StatusOpen, Type: ticket.TypeFeature}

	form := newTestApp(w, h)
	form.form = newEditFormModel(tk, w, h)
	form.overlay = overlayForm
	if out := form.View(); strings.Contains(out, "(/) search") {
		t.Errorf("form overlay leaked the dashboard footer:\n%s", out)
	}

	detail := newTestApp(w, h)
	detail.detail = newDetailModel(tk, w, h)
	detail.overlay = overlayDetail
	if out := detail.View(); strings.Contains(out, "(/) search") {
		t.Errorf("detail overlay leaked the dashboard footer:\n%s", out)
	}
}

// footerView wraps via wrapText, which panics on a negative width. a.width is 0
// before the first WindowSizeMsg, so the usable width (a.width-2) goes negative;
// guard against that regression.
func TestFooterViewZeroWidthNoPanic(t *testing.T) {
	a := App{activeTab: tabInbox, width: 0, height: 0}
	a.dashboard.activeTab = tabInbox
	a.footerView() // must not panic
	a.View()       // full render at zero size must not panic
}

// The filter segment is highlighted (StyleFilter) while the rest of the footer
// is muted (StyleHelp). That highlight must survive when the footer wraps.
func TestFooterHighlightSurvivesWrap(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	const w, h = 55, 24 // filter fits line 1, but the whole footer wraps
	a := newTestApp(w, h)

	footer, lines := a.footerView()
	if lines < 2 {
		t.Fatalf("expected a wrapped footer (>=2 lines), got %d:\n%s", lines, footer)
	}
	seg := StyleFilter.Render("type: all  (t)ype  (/) search")
	if !strings.Contains(footer, seg) {
		t.Errorf("wrapped footer lost the StyleFilter highlight on the filter segment:\n%s", footer)
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
