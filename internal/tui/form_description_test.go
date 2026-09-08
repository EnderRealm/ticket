package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// The edit form seeds its description field from the same region UpdateSection
// writes back to, so opening the form and saving with no edit is a no-op. When
// the two disagreed, every `## ` section tk does not name was deleted by a save
// that changed nothing.
func TestEditFormDescriptionRoundTrips(t *testing.T) {
	body := "\nFirst paragraph.\n\nSecond paragraph.\n\n" +
		"## Scope\n\nScope text.\n\n" +
		"## Shape\n\nShape text.\n\n" +
		"## Acceptance Criteria\n\n- a criterion\n"

	tk := &ticket.Ticket{
		ID: "a-0001", Title: "T", Status: ticket.StatusOpen,
		Type: ticket.TypeFeature, Body: body,
	}

	form := newEditFormModel(tk, 100, 40)
	seeded := form.fields[fieldDescription]
	for _, want := range []string{"First paragraph.", "## Scope", "Scope text.", "## Shape", "Shape text."} {
		if !strings.Contains(seeded, want) {
			t.Errorf("form description field is missing %q; it must show the whole region it writes back", want)
		}
	}
	if strings.Contains(seeded, "## Acceptance Criteria") {
		t.Error("acceptance criteria is its own field and must not be folded into the description")
	}

	got := ticket.UpdateSection(tk.Body, "", strings.TrimSpace(seeded))
	for _, want := range []string{"## Scope", "Scope text.", "## Shape", "Shape text.", "## Acceptance Criteria", "- a criterion"} {
		if !strings.Contains(got, want) {
			t.Errorf("a no-op save dropped %q from the body:\n%s", want, got)
		}
	}
}
