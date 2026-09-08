package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRefusedEditRendersItsReasonOverTheOpenForm(t *testing.T) {
	// The store refuses the write, handleEditTicket leaves the form open, and the
	// overlay branch renders no footer — so without a status row of its own the
	// keypress produces nothing on screen at all.
	const w, h = 80, 24
	a, child := legacyParentApp(t)
	a.width, a.height = w, h
	a.form = newEditFormModel(child, w, h)
	a.overlay = overlayForm

	msg := a.handleEditTicket(a.form.submit().(formSubmitMsg))()
	model, _ := a.Update(msg)
	a = model.(App)

	if a.overlay != overlayForm {
		t.Fatalf("overlay = %v after a refused save, want the form still open", a.overlay)
	}
	out := a.View()
	if !strings.Contains(out, "not an epic") {
		t.Errorf("the store's rejection is missing from the frame:\n%s", out)
	}
	// The remedy is the second sentence, past column 90 on this message: a row
	// clamped to the width would drop exactly the half the user can act on.
	if !strings.Contains(out, "Repoint the parent at an epic") {
		t.Errorf("the remedy the store named did not reach the frame:\n%s", out)
	}
}

func TestStatusRowsAreReservedAndCapped(t *testing.T) {
	const w, h = 60, 24
	a, child := legacyParentApp(t)
	a.width, a.height = w, h
	a.form = newEditFormModel(child, w, h)
	a.overlay = overlayForm

	_, footerBefore := a.footerView()
	before := frameHeight(a.View())

	a.status = "error: update: ticket c-0002: parent p-0001 is type feature, not an epic: only epics hold children. Repoint the parent at an epic, or clear it, then save again from this form"
	_, footerAfter := a.footerView()
	out := a.View()

	// Not frameHeight(out) <= h: an overlay frame already overruns a.height by
	// the footer rows it reserves and does not render, and a non-empty status
	// collapses that reservation to one line. The status rows themselves are
	// reserved, so the whole delta is the footer's — with no reservation the
	// frame would be statusRowsMax taller than this.
	if got, want := frameHeight(out), before+footerBefore-footerAfter; got != want {
		t.Errorf("frame height %d with a status, want %d — the status rows were not reserved", got, want)
	}

	rows := strings.Split(out, "\n")
	rows = rows[len(rows)-statusRowsMax:]
	for _, row := range rows {
		if lipgloss.Width(row) > w {
			t.Errorf("status row is %d columns wide, want at most %d — it would wrap: %q", lipgloss.Width(row), w, row)
		}
	}
	if !strings.Contains(rows[statusRowsMax-1], "…") {
		t.Errorf("the capped status does not mark the truncation: %q", rows[statusRowsMax-1])
	}
}

func TestSuccessfulEditClosesTheFormAndStillReportsIt(t *testing.T) {
	a, child := legacyParentApp(t)
	a.form = newEditFormModel(child, 80, 24)
	a.overlay = overlayForm

	// Clearing the parent is the remedy the rejection names, so the save lands.
	a.form.fields[fieldParent] = ""
	cmd := a.handleEditTicket(a.form.submit().(formSubmitMsg))
	if a.overlay != overlayNone {
		t.Fatalf("overlay = %v after an accepted save, want it closed", a.overlay)
	}

	model, _ := a.Update(statusMsg(statusLine(t, cmd)))
	a = model.(App)
	if !strings.Contains(a.View(), "Updated c-0002") {
		t.Errorf("the success status is missing from the frame:\n%s", a.View())
	}
}
