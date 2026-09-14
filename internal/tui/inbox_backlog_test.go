package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/charmbracelet/x/ansi"
)

func TestInboxBacklogQuestion(t *testing.T) {
	question := map[string]string{ticket.QuestionField: "  Which store wins?  "}
	dir := writeBoardFixture(t, "proj",
		&ticket.Ticket{ID: "ready-0001", Title: "Ready", Type: ticket.TypeFeature, Status: ticket.StatusReady, Priority: 0},
		&ticket.Ticket{ID: "parked-0002", Title: "Parked grooming", Type: ticket.TypeBug, Status: ticket.StatusBacklog, Priority: 1, Extra: question, Parent: "epic-0009"},
		&ticket.Ticket{ID: "open-0003", Title: "Open", Type: ticket.TypeFeature, Status: ticket.StatusOpen, Priority: 2},
		&ticket.Ticket{ID: "absent-0004", Type: ticket.TypeFeature, Status: ticket.StatusBacklog},
		&ticket.Ticket{ID: "empty-0005", Type: ticket.TypeFeature, Status: ticket.StatusBacklog, Extra: map[string]string{ticket.QuestionField: ""}},
		&ticket.Ticket{ID: "blank-0006", Type: ticket.TypeFeature, Status: ticket.StatusBacklog, Extra: map[string]string{ticket.QuestionField: " \t\n"}},
		&ticket.Ticket{ID: "done-0007", Type: ticket.TypeFeature, Status: ticket.StatusDone, Extra: question},
		&ticket.Ticket{ID: "closed-0008", Type: ticket.TypeFeature, Status: ticket.StatusClosed, Extra: question},
		&ticket.Ticket{ID: "epic-0009", Type: ticket.TypeEpic, Status: ticket.StatusBacklog, Extra: question},
	)
	listed, snap := loadBoardFixture(t, "proj", dir)
	a := boardAppOver("proj", tabInbox, 160, 40, listed, snap)
	want := []string{"ready-0001", "parked-0002", "open-0003"}
	if got := itemIDs(a.dashboard.items); !slices.Equal(got, want) {
		t.Fatalf("inbox rows = %v, want %v", got, want)
	}
	if got := a.tabCounts()[tabInbox]; got != len(want) {
		t.Errorf("inbox count = %d, want %d", got, len(want))
	}
	a.dashboard.cursor = 1
	if got := a.dashboard.selected(); got == nil || got.ID != "parked-0002" || got.Status != ticket.StatusBacklog {
		t.Fatalf("selected ticket = %+v, want parked backlog", got)
	}
	for _, selected := range []bool{false, true} {
		line := ansi.Strip(a.dashboard.renderRow(a.dashboard.rows[1], selected))
		if !strings.Contains(line, "⚑ Which store wins?") {
			t.Errorf("parked row missing question indicator and text: %s", line)
		}
		assertInboxStatus(t, a.dashboard, a.dashboard.selected(), selected, "backlog")
	}
	for _, filter := range []struct {
		kind ticket.TicketType
		text string
		want []string
	}{
		{ticket.TypeBug, "", []string{"parked-0002"}},
		{ticket.TypeFeature, "", []string{"ready-0001", "open-0003"}},
		{"", "PARKED GROOMING", []string{"parked-0002"}},
		{"", "0002", []string{"parked-0002"}},
		{"", "absent", nil},
		{ticket.TypeEpic, "", nil},
	} {
		a.dashboard.typeFilter = filter.kind
		a.dashboard.filterText = filter.text
		a.dashboard.buildItems()
		if got := itemIDs(a.dashboard.items); !slices.Equal(got, filter.want) {
			t.Errorf("filter %s/%q: rows = %v, want %v", filter.kind, filter.text, got, filter.want)
		}
		if got := a.tabCounts()[tabInbox]; got != len(filter.want) {
			t.Errorf("filter %s/%q: count = %d, want %d", filter.kind, filter.text, got, len(filter.want))
		}
	}
	a.dashboard.typeFilter = ""
	a.dashboard.filterText = ""
	store := ticket.NewProjectFileStore(dir, "proj")
	parked, err := store.Get("parked-0002")
	if err != nil {
		t.Fatal(err)
	}
	delete(parked.Extra, ticket.QuestionField)
	if err := store.Update(parked); err != nil {
		t.Fatal(err)
	}
	a.tickets, a.snap = loadBoardFixture(t, "proj", dir)
	a.dashboard.refreshTickets(a.tickets, a.snap)
	if got := itemIDs(a.dashboard.items); !slices.Equal(got, []string{"ready-0001", "open-0003"}) {
		t.Errorf("rows after clearing question = %v", got)
	}
	if got := a.tabCounts()[tabInbox]; got != 2 {
		t.Errorf("count after clearing question = %d, want 2", got)
	}
	if got := boardTicket(t, a.tickets, "parked-0002").Status; got != ticket.StatusBacklog {
		t.Errorf("clearing question changed stored status to %s", got)
	}
}
