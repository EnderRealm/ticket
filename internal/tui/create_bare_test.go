package tui

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestCreateReportsBareAcceptanceCriteria(t *testing.T) {
	// The TUI has no acceptance field, so criteria arrive inside the
	// description and land in the body; a bare one is reported on the status
	// line, in the sentence `tk create` and ticket_create use, and the ticket
	// is created regardless.
	store := ticket.NewFileStore(t.TempDir())
	a := App{store: store}

	description := "Why it matters.\n\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./...\n- Bare one."
	reported := statusLine(t, a.handleCreateTicket(formSubmitMsg{
		title:       "Bare create",
		description: description,
		ticketType:  ticket.TypeFeature,
	}))

	created, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("got %d tickets, want the create to have gone through", len(created))
	}
	got := created[0]

	want := ticket.BareAcceptanceWarning(got.ID, ticket.BareCriteria(got.Body))
	if want == "" {
		t.Fatalf("bare criteria not found in the created body:\n%s", got.Body)
	}
	if !strings.HasPrefix(reported, "Created "+got.ID+": Bare create; ") {
		t.Errorf("status line = %q, want the create confirmation first", reported)
	}
	if !strings.Contains(reported, want) {
		t.Errorf("status line = %q, want it to carry %q", reported, want)
	}
}

func TestCreateSaysNothingWhenCriteriaAreAllChecked(t *testing.T) {
	store := ticket.NewFileStore(t.TempDir())
	a := App{store: store}

	description := "Why it matters.\n\n## Acceptance Criteria\n\n- Checked.\n  verify: go test ./...\n- Not checkable.\n  unverifiable: a human judges this."
	reported := statusLine(t, a.handleCreateTicket(formSubmitMsg{
		title:       "Checked create",
		description: description,
		ticketType:  ticket.TypeFeature,
	}))

	created, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("got %d tickets, want one", len(created))
	}
	if want := "Created " + created[0].ID + ": Checked create"; reported != want {
		t.Errorf("status line = %q, want exactly %q", reported, want)
	}
}
