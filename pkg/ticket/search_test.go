package ticket

import (
	"strings"
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

func TestSearch_SnippetFieldSelection(t *testing.T) {
	tickets := []*Ticket{
		{ID: "t-001", Body: "the deploy pipeline broke today"},
	}
	results := Search(tickets, "deploy")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Field != "body" {
		t.Errorf("field = %q, want body", results[0].Field)
	}
	if !strings.Contains(results[0].Snippet, "deploy") {
		t.Errorf("snippet = %q, want it to contain the match", results[0].Snippet)
	}
}

func TestSearch_SnippetTitlePreferred(t *testing.T) {
	tickets := []*Ticket{
		{ID: "t-001", Title: "deploy fails", Body: "deploy pipeline broke"},
	}
	results := Search(tickets, "deploy")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Field != "title" {
		t.Errorf("field = %q, want title (preferred over body)", results[0].Field)
	}
}

func TestExcerpt_CentersAndEllipses(t *testing.T) {
	before := strings.Repeat("a ", 60)
	after := strings.Repeat("b ", 60)
	text := before + "NEEDLE " + after
	got, ok := excerpt(text, []string{"needle"}, snippetWidth)
	if !ok {
		t.Fatal("excerpt returned ok=false")
	}
	if !strings.Contains(got, "NEEDLE") {
		t.Errorf("excerpt %q missing match", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt %q should be ellipsized on both ends", got)
	}
}

func TestExcerpt_CollapsesWhitespace(t *testing.T) {
	got, ok := excerpt("foo\n\t  bar   baz", []string{"bar"}, snippetWidth)
	if !ok {
		t.Fatal("excerpt returned ok=false")
	}
	if got != "foo bar baz" {
		t.Errorf("excerpt = %q, want collapsed whitespace", got)
	}
}

func TestExcerpt_NoMatch(t *testing.T) {
	if _, ok := excerpt("nothing here", []string{"absent"}, snippetWidth); ok {
		t.Error("excerpt should return ok=false when no term occurs")
	}
}

func TestToLowerASCII_PreservesByteLength(t *testing.T) {
	in := "İABC"
	got := ToLowerASCII(in)
	if len(got) != len(in) {
		t.Errorf("len = %d, want %d (byte length must be preserved)", len(got), len(in))
	}
}

// 'İ' (U+0130) lowercases under strings.ToLower to a longer byte sequence, so
// offsets derived from strings.ToLower would misalign with the original string
// and could panic. Placing it before the ASCII match with enough surrounding
// text to exceed snippetWidth exercises the centering/slicing path.
func TestExcerpt_NonASCIIOffsetStable(t *testing.T) {
	before := "İ" + strings.Repeat("a ", 60)
	after := strings.Repeat("b ", 60)
	text := before + "NEEDLE " + after
	got, ok := excerpt(text, []string{"needle"}, snippetWidth)
	if !ok {
		t.Fatal("excerpt returned ok=false")
	}
	if got == "" {
		t.Fatal("excerpt returned empty snippet")
	}
	if !strings.Contains(got, "NEEDLE") {
		t.Errorf("excerpt %q missing match", got)
	}
}

func TestSearch_NonASCIIBodyNoPanic(t *testing.T) {
	body := "İ" + strings.Repeat("a ", 60) + "needle " + strings.Repeat("b ", 60)
	tickets := []*Ticket{{ID: "t-001", Body: body}}
	results := Search(tickets, "needle")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Snippet, "needle") {
		t.Errorf("snippet = %q, want it to contain the match", results[0].Snippet)
	}
}
