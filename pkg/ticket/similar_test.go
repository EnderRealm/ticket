package ticket

import "testing"

func TestSimilarTickets(t *testing.T) {
	mk := func(id, title string, st Status) *Ticket {
		return &Ticket{ID: id, Title: title, Status: st}
	}
	existing := []*Ticket{
		mk("p/a", "Swift test fails only under the tk MCP server", StatusOpen),
		mk("p/b", "Swift test fails only under the tk MCP server", StatusClosed),
		mk("p/c", "Swift test fails only under the tk MCP server", StatusDone),
		mk("q/d", "Swift test fails only under the tk MCP server", StatusOpen),
		mk("p/e", "Add dark mode to settings page", StatusBacklog),
	}
	nt := mk("p/new", "swift test fails only under tk MCP server", StatusBacklog)
	got := SimilarTickets(existing, nt)
	if len(got) != 1 || got[0].ID != "p/a" {
		t.Fatalf("got %+v, want only p/a", got)
	}
	if SimilarTicketsWarning(nt.ID, got) == "" {
		t.Fatal("expected warning")
	}
	if got := SimilarTickets(existing, mk("p/x", "Rewrite the billing exporter", StatusBacklog)); len(got) != 0 {
		t.Fatalf("unexpected %+v", got)
	}
	if SimilarTicketsWarning("p/x", nil) != "" {
		t.Fatal("warning for no candidates")
	}
}
