package ticket

import (
	"testing"
	"time"
)

func stageTicket(id string, stage Stage, ticketType TicketType) *Ticket {
	return &Ticket{
		ID:       id,
		Stage:    stage,
		Type:     ticketType,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Ticket " + id,
		Body:     "\nDescription.\n",
	}
}

func TestAdvance_NextStage(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-adv", StageTriage, TypeChore)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	result, err := Advance(store, "t-adv", AdvanceOptions{})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if result.From != StageTriage || result.To != StageImplement {
		t.Errorf("Advance = %s→%s, want triage→implement", result.From, result.To)
	}

	// Verify persisted.
	updated, _ := store.Get("t-adv")
	if updated.Stage != StageImplement {
		t.Errorf("persisted stage = %s, want implement", updated.Stage)
	}
}

func TestAdvance_GateFails(t *testing.T) {
	store := NewFileStore(t.TempDir())
	// Feature at spec, no AC, no review approval → should fail spec→design.
	tk := stageTicket("t-gate", StageSpec, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	result, err := Advance(store, "t-gate", AdvanceOptions{})
	if err == nil {
		t.Fatal("Advance should fail when gates fail")
	}
	if len(result.GateErrors) == 0 {
		t.Error("GateErrors should be populated")
	}

	// Verify ticket did not advance.
	check, _ := store.Get("t-gate")
	if check.Stage != StageSpec {
		t.Errorf("stage should still be spec, got %s", check.Stage)
	}
}

func TestAdvance_Force(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-force", StageSpec, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	result, err := Advance(store, "t-force", AdvanceOptions{Force: true})
	if err != nil {
		t.Fatalf("Advance with Force: %v", err)
	}
	if result.To != StageDesign {
		t.Errorf("forced advance to = %s, want design", result.To)
	}
}

func TestAdvance_AlreadyAtFinal(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-done", StageDone, TypeChore)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	_, err := Advance(store, "t-done", AdvanceOptions{})
	if err == nil {
		t.Error("Advance at done should fail")
	}
}

func TestAdvance_ResetsReview(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-rev", StageTriage, TypeChore)
	tk.Review = ReviewApproved
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	Advance(store, "t-rev", AdvanceOptions{})
	updated, _ := store.Get("t-rev")
	if updated.Review != ReviewNone {
		t.Errorf("review should reset to empty after advance, got %s", updated.Review)
	}
}

func TestSkip(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-skip", StageTriage, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	result, err := Skip(store, "t-skip", StageImplement, "trivial feature")
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if result.To != StageImplement {
		t.Errorf("skip to = %s, want implement", result.To)
	}
	if len(result.Skipped) != 2 { // skipped spec and design
		t.Errorf("skipped %d stages, want 2", len(result.Skipped))
	}

	updated, _ := store.Get("t-skip")
	if len(updated.Skipped) != 2 {
		t.Errorf("persisted skipped = %d, want 2", len(updated.Skipped))
	}
}

func TestSkip_RequiresReason(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-skip2", StageTriage, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	_, err := Skip(store, "t-skip2", StageImplement, "")
	if err == nil {
		t.Error("Skip without reason should fail")
	}
}

func TestSkip_CannotGoBackward(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-back", StageImplement, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	_, err := Skip(store, "t-back", StageTriage, "oops")
	if err == nil {
		t.Error("Skip backward should fail")
	}
}

func TestSetReview(t *testing.T) {
	store := NewFileStore(t.TempDir())
	tk := stageTicket("t-rev", StageDesign, TypeFeature)
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	err := SetReview(store, "t-rev", "human:steve", ReviewApproved, "looks good")
	if err != nil {
		t.Fatalf("SetReview: %v", err)
	}

	updated, _ := store.Get("t-rev")
	if updated.Review != ReviewApproved {
		t.Errorf("review = %s, want approved", updated.Review)
	}
	if len(updated.Reviews) != 1 {
		t.Fatalf("len(Reviews) = %d, want 1", len(updated.Reviews))
	}
	if updated.Reviews[0].Reviewer != "human:steve" {
		t.Errorf("reviewer = %s, want human:steve", updated.Reviews[0].Reviewer)
	}
	if updated.Reviews[0].Comment != "looks good" {
		t.Errorf("comment = %s, want 'looks good'", updated.Reviews[0].Comment)
	}
}

func TestPropagateStage_AllDone(t *testing.T) {
	store := NewFileStore(t.TempDir())
	epic := stageTicket("t-epic", StageDesign, TypeEpic)
	child1 := stageTicket("t-c1", StageDone, TypeTask)
	child1.Parent = "t-epic"
	child2 := stageTicket("t-c2", StageDone, TypeTask)
	child2.Parent = "t-epic"

	for _, tk := range []*Ticket{epic, child1, child2} {
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
	}

	changes, err := PropagateStage(store, "t-c2")
	if err != nil {
		t.Fatalf("PropagateStage: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].NewStage != StageDone {
		t.Errorf("new stage = %s, want done", changes[0].NewStage)
	}
}
