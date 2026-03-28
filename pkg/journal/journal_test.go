package journal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJournalPath(t *testing.T) {
	p, err := JournalPath("myproject")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ticket", "state", "myproject", "commits.jsonl")
	if p != want {
		t.Errorf("JournalPath = %q, want %q", p, want)
	}
}

func TestStatePath(t *testing.T) {
	p, err := StatePath("myproject")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ticket", "state", "myproject")
	if p != want {
		t.Errorf("StatePath = %q, want %q", p, want)
	}
}

func TestReadJournal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commits.jsonl")

	content := `{"sha":"a1","ticket":"t-1","action":"ref"}
invalid json line
{"sha":"b2","ticket":"t-2","action":"close"}
`
	os.WriteFile(path, []byte(content), 0o644)

	entries, err := ReadEntriesFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (malformed line should be skipped)", len(entries))
	}
	if entries[0].SHA != "a1" {
		t.Errorf("entry[0].SHA = %q, want a1", entries[0].SHA)
	}
	if entries[1].SHA != "b2" {
		t.Errorf("entry[1].SHA = %q, want b2", entries[1].SHA)
	}
}

func TestReadJournal_Missing(t *testing.T) {
	entries, err := ReadEntriesFromPath("/nonexistent/path/commits.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries for missing file, want 0", len(entries))
	}
}

func TestAppendEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "commits.jsonl")

	entries := []Entry{
		{SHA: "a1", Ticket: "t-1", Action: "ref"},
		{SHA: "b2", Ticket: "t-2", Action: "close"},
	}
	if err := AppendEntriesToPath(path, entries); err != nil {
		t.Fatal(err)
	}

	loaded, err := ReadEntriesFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("got %d entries, want 2", len(loaded))
	}

	// Append more — should add to existing
	more := []Entry{{SHA: "c3", Ticket: "t-3", Action: "ref"}}
	if err := AppendEntriesToPath(path, more); err != nil {
		t.Fatal(err)
	}
	loaded, _ = ReadEntriesFromPath(path)
	if len(loaded) != 3 {
		t.Fatalf("got %d entries after second append, want 3", len(loaded))
	}
}

func TestDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commits.jsonl")

	entries := []Entry{
		{SHA: "a1", Ticket: "t-1", Action: "ref"},
		{SHA: "b2", Ticket: "t-2", Action: "close"},
	}
	if err := AppendEntriesToPath(path, entries); err != nil {
		t.Fatal(err)
	}

	// Append same entries again — should be deduplicated
	dupes := []Entry{
		{SHA: "a1", Ticket: "t-1", Action: "ref"},
		{SHA: "c3", Ticket: "t-3", Action: "ref"},
	}
	if err := AppendEntriesToPath(path, dupes); err != nil {
		t.Fatal(err)
	}

	loaded, _ := ReadEntriesFromPath(path)
	if len(loaded) != 3 {
		t.Fatalf("got %d entries, want 3 (a1:t-1 should be deduplicated)", len(loaded))
	}

	// Same SHA but different ticket should NOT be deduplicated
	diffTicket := []Entry{{SHA: "a1", Ticket: "t-99", Action: "ref"}}
	if err := AppendEntriesToPath(path, diffTicket); err != nil {
		t.Fatal(err)
	}
	loaded, _ = ReadEntriesFromPath(path)
	if len(loaded) != 4 {
		t.Fatalf("got %d entries, want 4 (same SHA but different ticket)", len(loaded))
	}
}

func TestFilter(t *testing.T) {
	entries := []Entry{
		{Ticket: "a"},
		{Ticket: "b"},
		{Ticket: "c"},
		{Ticket: "a"},
	}
	filtered := FilterByTickets(entries, []string{"a", "c"})
	if len(filtered) != 3 {
		t.Errorf("got %d entries, want 3", len(filtered))
	}
}

func TestCountForTicket(t *testing.T) {
	entries := []Entry{
		{Ticket: "a"},
		{Ticket: "b"},
		{Ticket: "a"},
	}
	if n := CountForTicket(entries, "a"); n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if n := CountForTicket(entries, "c"); n != 0 {
		t.Errorf("count = %d, want 0", n)
	}
}

func TestLastN(t *testing.T) {
	entries := []Entry{{SHA: "1"}, {SHA: "2"}, {SHA: "3"}}
	last := LastN(entries, 2)
	if len(last) != 2 {
		t.Fatalf("got %d, want 2", len(last))
	}
	if last[0].SHA != "2" || last[1].SHA != "3" {
		t.Errorf("got SHAs %q %q, want 2 3", last[0].SHA, last[1].SHA)
	}

	// n > len should return all
	all := LastN(entries, 10)
	if len(all) != 3 {
		t.Errorf("got %d, want 3", len(all))
	}
}
