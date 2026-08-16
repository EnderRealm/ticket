package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// warnProbe calls the warning sink from inside Update — the shape every TUI
// mutation has, since a store write runs synchronously on the goroutine that
// drains the program's messages — and reports the message the sink produced.
type warnProbe struct{ seen chan string }

type warnTriggerMsg struct{}

func (m warnProbe) Init() tea.Cmd { return nil }

func (m warnProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case warnTriggerMsg:
		ticket.Warnf("warning: mutation log for project %s: %v\n", "alpha", "permission denied")
		return m, nil
	case warnMsg:
		m.seen <- string(msg)
		return m, tea.Quit
	}
	return m, nil
}

func (m warnProbe) View() string { return "" }

// A sink that sent inline would block on the unbuffered message channel the
// calling goroutine is the one draining, hanging the program in the alt screen.
// The timeouts fail that regression fast instead of hanging the suite.
func TestCaptureWarningsSurvivesASinkCallFromUpdate(t *testing.T) {
	seen := make(chan string, 1)
	p := tea.NewProgram(warnProbe{seen: seen},
		tea.WithInput(nil), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	t.Cleanup(CaptureWarnings(p))

	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	go p.Send(warnTriggerMsg{})

	select {
	case got := <-seen:
		if !strings.Contains(got, "alpha") || !strings.Contains(got, "permission denied") {
			t.Errorf("warning = %q, want the sink's formatted message", got)
		}
		if strings.Contains(got, "\n") {
			t.Errorf("warning = %q, want it flattened to one line", got)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the warning never reached the model: the sink's send blocked the event loop it was called from")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not exit after quitting")
	}
}

func TestWarningSurvivesTheSuccessStatusThatFollowsIt(t *testing.T) {
	// The write that warns emits its success message right after, so the warning
	// has to hold a slot of its own.
	a := newTestApp(80, 24)
	model, _ := a.Update(warnMsg("warning: mutation log for project alpha: permission denied"))
	model, _ = model.(App).Update(statusMsg("Note added to a-0001"))
	a = model.(App)

	if a.warning == "" {
		t.Error("a success status cleared the warning")
	}
	if a.status != "Note added to a-0001" {
		t.Errorf("status = %q, want the success message", a.status)
	}

	model, _ = a.Update(clearWarnMsg{})
	if got := model.(App).warning; got != "" {
		t.Errorf("warning = %q after its clear message, want it cleared", got)
	}
}

func TestWarningRendersWithAnOverlayOpen(t *testing.T) {
	// The writes that warn (note, priority, status) leave the detail overlay open,
	// and the overlay branch renders no footer at all.
	const w, h = 80, 24
	a := newTestApp(w, h)
	a.detail = detailTestModel(w, h)
	a.overlay = overlayDetail
	model, _ := a.Update(warnMsg("warning: mutation log for project alpha: permission denied"))

	out := model.(App).View()
	if !strings.Contains(out, "permission denied") {
		t.Errorf("warning missing from the overlay frame:\n%s", out)
	}
	if last := lastLine(out); !strings.Contains(last, "permission denied") {
		t.Errorf("warning is not the frame's last row, got %q", last)
	}
}

func TestWarningRowIsClampedAndReserved(t *testing.T) {
	const w, h = 60, 24
	a := newTestApp(w, h)
	before := frameHeight(a.View())

	model, _ := a.Update(warnMsg("warning: mutation log for project alpha: open /Users/someone/.ticket/state/alpha/mutations.jsonl: permission denied"))
	out := model.(App).View()

	if got := frameHeight(out); got != before {
		t.Errorf("frame height %d with a warning, want the %d rows the warning reserved one of", got, before)
	}
	row := lastLine(out)
	if lipgloss.Width(row) > w {
		t.Errorf("warning row is %d columns wide, want at most %d — it would wrap: %q", lipgloss.Width(row), w, row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("clamped warning row does not mark the truncation: %q", row)
	}
}

// lastLine returns the frame's final rendered row.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	return lines[len(lines)-1]
}
