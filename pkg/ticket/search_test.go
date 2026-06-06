package ticket

import (
	"testing"
)

func TestSearch_TitleOutranksBody(t *testing.T) {
	tickets := []*Ticket{
		{ID: "t-001", Body: "the deploy pipeline broke"},
		{ID: "t-002", Title: "deploy fails on push"},
	}
	results := Search(tickets, "deploy")
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if results[0].Ticket.ID != "t-002" {
		t.Errorf("first = %q, want t-002 (title match outranks body)", results[0].Ticket.ID)
	}
}

func TestSearch_MoreTermsOutranksOne(t *testing.T) {
	tickets := []*Ticket{
		{ID: "t-001", Title: "login crash"},
		{ID: "t-002", Title: "login flow"},
	}
	results := Search(tickets, "login crash")
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
	if results[0].Ticket.ID != "t-001" {
		t.Errorf("first = %q, want t-001 (matches two terms)", results[0].Ticket.ID)
	}
}

func TestSearch_NotesContribute(t *testing.T) {
	tickets := []*Ticket{
		{ID: "t-001", Notes: []Note{{Text: "saw the regression again"}}},
	}
	results := Search(tickets, "regression")
	if len(results) != 1 || results[0].Ticket.ID != "t-001" {
		t.Errorf("got %v, want [t-001]", results)
	}
}

func TestSearch_NoMatch(t *testing.T) {
	tickets := []*Ticket{{ID: "t-001", Title: "deploy fails"}}
	if r := Search(tickets, "nonexistent"); r != nil {
		t.Errorf("len = %d, want 0", len(r))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	tickets := []*Ticket{{ID: "t-001", Title: "deploy fails"}}
	if r := Search(tickets, "   "); r != nil {
		t.Errorf("empty query should return nil, got %v", r)
	}
}
