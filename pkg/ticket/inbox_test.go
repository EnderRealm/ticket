package ticket

import (
	"testing"
	"time"
)

func TestNextAction_Ready(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionWork {
		t.Errorf("NextAction(ready) = %s, want work", item.Action)
	}
}

func TestNextAction_Open(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionWork {
		t.Errorf("NextAction(open) = %s, want work", item.Action)
	}
}

func TestNextAction_Done(t *testing.T) {
	tk := &Ticket{ID: "t-1", Status: StatusDone, Type: TypeFeature, Created: time.Now()}
	item := NextAction(tk)
	if item.Action != ActionReady {
		t.Errorf("NextAction(done) = %s, want ready", item.Action)
	}
}

func TestNextAction_Backlog(t *testing.T) {
	tk := &Ticket{ID: "t-1", Status: StatusBacklog, Type: TypeFeature, Created: time.Now()}
	item := NextAction(tk)
	if item.Action != ActionReady {
		t.Errorf("NextAction(backlog) = %s, want ready", item.Action)
	}
	if item.Detail != "no action needed" {
		t.Errorf("NextAction(backlog) detail = %q, want %q", item.Detail, "no action needed")
	}
}

func TestInbox_ExcludesBacklog(t *testing.T) {
	store := NewFileStore(t.TempDir())

	// Backlog ticket — should NOT appear in inbox.
	t1 := &Ticket{
		ID: "t-backlog", Status: StatusBacklog, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Backlog idea", Body: "\n",
	}
	// Ready ticket — should appear.
	t2 := &Ticket{
		ID: "t-ready", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Ready", Body: "\n",
	}

	for _, tk := range []*Ticket{t1, t2} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Inbox(store)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Inbox returned %d items, want 1", len(items))
	}
	if items[0].Ticket.ID != "t-ready" {
		t.Errorf("inbox item = %s, want t-ready", items[0].Ticket.ID)
	}
}

func TestInbox_FiltersActionableStatuses(t *testing.T) {
	store := NewFileStore(t.TempDir())

	// Ready ticket — should appear in inbox.
	t1 := &Ticket{
		ID: "t-1", Status: StatusReady, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Feature", Body: "\n",
	}
	// Open ticket — should appear.
	t2 := &Ticket{
		ID: "t-2", Status: StatusOpen, Type: TypeBug, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Bug", Body: "\n",
	}
	// Done ticket — should NOT appear.
	t3 := &Ticket{
		ID: "t-3", Status: StatusDone, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Done", Body: "\n",
	}

	for _, tk := range []*Ticket{t1, t2, t3} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Inbox(store)
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Inbox returned %d items, want 2", len(items))
	}

	// Should be sorted: P0 open first, then P1 ready.
	if items[0].Ticket.ID != "t-2" {
		t.Errorf("first inbox item = %s, want t-2 (P0)", items[0].Ticket.ID)
	}
	if items[1].Ticket.ID != "t-1" {
		t.Errorf("second inbox item = %s, want t-1 (P1)", items[1].Ticket.ID)
	}
}

func TestProjects(t *testing.T) {
	store := NewFileStore(t.TempDir())

	epic := &Ticket{
		ID: "t-epic", Status: StatusOpen, Type: TypeEpic, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Epic", Body: "\n",
	}
	child1 := &Ticket{
		ID: "t-c1", Status: StatusDone, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Done child", Body: "\n",
	}
	child2 := &Ticket{
		ID: "t-c2", Status: StatusOpen, Type: TypeFeature, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "WIP child", Body: "\n",
	}

	for _, tk := range []*Ticket{epic, child1, child2} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := Projects(store)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("Projects returned %d, want 1", len(projects))
	}

	p := projects[0]
	if p.Total != 2 {
		t.Errorf("Total = %d, want 2", p.Total)
	}
	if p.CompletionPct != 50 {
		t.Errorf("CompletionPct = %f, want 50", p.CompletionPct)
	}
	if p.StatusBreakdown[StatusDone] != 1 {
		t.Errorf("StatusBreakdown[done] = %d, want 1", p.StatusBreakdown[StatusDone])
	}
}
