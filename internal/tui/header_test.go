package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The alt screen hides anything written to stderr before the program starts, so
// the unregistered-project warning has to live in the frame itself.
func TestHeaderMarksUnregisteredProject(t *testing.T) {
	a := newTestApp(120, 30)
	a.projectName = "stray"

	if out := a.View(); strings.Contains(out, "unregistered") {
		t.Errorf("a registered project must not be marked:\n%s", out)
	}

	a.unregistered = true
	out := a.View()
	if !strings.Contains(out, "unregistered") {
		t.Errorf("header should mark an unregistered project:\n%s", out)
	}
}

// The tab bar alone is ~58 columns, so on an 80-column terminal the marker can
// push the header past the width. A wrapped header row makes the frame taller
// than a.height and shifts the alt-screen layout, so the marker is dropped
// instead.
func TestHeaderUnregisteredMarkerFitsWidth(t *testing.T) {
	const w, h = 80, 30
	a := newTestApp(w, h)
	a.projectName = "stray"
	a.unregistered = true

	out := a.View()
	header := strings.SplitN(out, "\n", 2)[0]
	if got := lipgloss.Width(header); got > w {
		t.Errorf("header width %d exceeds terminal width %d:\n%s", got, w, header)
	}
	if got := frameHeight(out); got > h {
		t.Errorf("frame height %d exceeds terminal height %d:\n%s", got, h, out)
	}
}
