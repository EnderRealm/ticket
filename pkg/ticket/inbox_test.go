package ticket

import (
	"testing"
	"time"
)

func TestNextAction_Triage(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Stage: StageTriage, Type: TypeFeature, Priority: 1,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionHumanInput {
		t.Errorf("NextAction(triage) = %s, want human-input", item.Action)
	}
}

func TestNextAction_PendingReview(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Stage: StageDesign, Review: ReviewPending, Type: TypeFeature,
		Created: time.Now(),
	}
	item := NextAction(tk)
	// Design is not conversational → agent review.
	if item.Action != ActionAgentReview {
		t.Errorf("NextAction(design, pending) = %s, want agent-review", item.Action)
	}
}

func TestNextAction_PendingReviewConversational(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Stage: StageSpec, Review: ReviewPending, Type: TypeFeature,
		Created: time.Now(),
	}
	item := NextAction(tk)
	// Spec is conversational → human review.
	if item.Action != ActionHumanReview {
		t.Errorf("NextAction(spec, pending) = %s, want human-review", item.Action)
	}
}

func TestNextAction_Rejected(t *testing.T) {
	tk := &Ticket{
		ID: "t-1", Stage: StageImplement, Review: ReviewRejected, Type: TypeFeature,
		Created: time.Now(),
	}
	item := NextAction(tk)
	if item.Action != ActionAgentWork {
		t.Errorf("NextAction(implement, rejected) = %s, want agent-work", item.Action)
	}
}

func TestNextAction_Done(t *testing.T) {
	tk := &Ticket{ID: "t-1", Stage: StageDone, Type: TypeFeature, Created: time.Now()}
	item := NextAction(tk)
	if item.Action != ActionReady {
		t.Errorf("NextAction(done) = %s, want ready", item.Action)
	}
}

func TestNextAction_Backlog(t *testing.T) {
	tk := &Ticket{ID: "t-1", Stage: StageBacklog, Type: TypeFeature, Created: time.Now()}
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
		ID: "t-backlog", Stage: StageBacklog, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Backlog idea", Body: "\n",
	}
	// Triage ticket — should appear.
	t2 := &Ticket{
		ID: "t-triage", Stage: StageTriage, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Triaged", Body: "\n",
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
	if items[0].Ticket.ID != "t-triage" {
		t.Errorf("inbox item = %s, want t-triage", items[0].Ticket.ID)
	}
}

func TestInbox_FiltersHumanActions(t *testing.T) {
	store := NewFileStore(t.TempDir())

	// Triage ticket (human-input) — should appear in inbox.
	t1 := &Ticket{
		ID: "t-1", Stage: StageTriage, Type: TypeFeature, Priority: 1,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Feature", Body: "\n",
	}
	// Implement ticket (agent-work) — should NOT appear.
	t2 := &Ticket{
		ID: "t-2", Stage: StageImplement, Type: TypeBug, Priority: 2,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Bug", Body: "\n",
	}
	// Verify ticket (human-review) — should appear.
	t3 := &Ticket{
		ID: "t-3", Stage: StageVerify, Type: TypeFeature, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Verify", Body: "\n",
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

	// Should be sorted: P0 verify first, then P1 triage.
	if items[0].Ticket.ID != "t-3" {
		t.Errorf("first inbox item = %s, want t-3 (P0)", items[0].Ticket.ID)
	}
	if items[1].Ticket.ID != "t-1" {
		t.Errorf("second inbox item = %s, want t-1 (P1)", items[1].Ticket.ID)
	}
}

func TestProjects(t *testing.T) {
	store := NewFileStore(t.TempDir())

	epic := &Ticket{
		ID: "t-epic", Stage: StageDesign, Type: TypeEpic, Priority: 0,
		Deps: []string{}, Links: []string{}, Created: time.Now(), Title: "Epic", Body: "\n",
	}
	child1 := &Ticket{
		ID: "t-c1", Stage: StageDone, Type: TypeTask, Priority: 1,
		Parent: "t-epic", Deps: []string{}, Links: []string{}, Created: time.Now(),
		Title: "Done child", Body: "\n",
	}
	child2 := &Ticket{
		ID: "t-c2", Stage: StageImplement, Type: TypeTask, Priority: 1,
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
	if p.StageBreakdown[StageDone] != 1 {
		t.Errorf("StageBreakdown[done] = %d, want 1", p.StageBreakdown[StageDone])
	}
}
