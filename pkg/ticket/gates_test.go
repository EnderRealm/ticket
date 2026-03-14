package ticket

import (
	"testing"
	"time"
)

func gateTicket(stage Stage, ticketType TicketType) *Ticket {
	return &Ticket{
		ID:       "t-gate",
		Stage:    stage,
		Type:     ticketType,
		Priority: 2,
		Deps:     []string{},
		Links:    []string{},
		Created:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:    "Gate test",
		Body:     "\nSome description.\n",
	}
}

func TestCheckGates_TriageToSpec_FeatureNeedsDescription(t *testing.T) {
	tk := gateTicket(StageTriage, TypeFeature)
	tk.Body = "\n" // Empty body.
	errs := CheckGates(tk, StageSpec)
	if len(errs) == 0 {
		t.Error("triage→spec with empty body should fail gate")
	}
}

func TestCheckGates_TriageToSpec_FeatureWithDescription(t *testing.T) {
	tk := gateTicket(StageTriage, TypeFeature)
	errs := CheckGates(tk, StageSpec)
	if len(errs) != 0 {
		t.Errorf("triage→spec with description should pass, got %v", errs)
	}
}

func TestCheckGates_SpecToDesign_NeedsACAndReview(t *testing.T) {
	tk := gateTicket(StageSpec, TypeFeature)
	errs := CheckGates(tk, StageDesign)
	// Should fail: no AC section and no review approval.
	if len(errs) < 2 {
		t.Errorf("spec→design without AC and review should have 2+ failures, got %d", len(errs))
	}
}

func TestCheckGates_SpecToDesign_WithACAndReview(t *testing.T) {
	tk := gateTicket(StageSpec, TypeFeature)
	tk.Body = "\n## Acceptance Criteria\n\n- Must work\n"
	tk.Review = ReviewApproved
	errs := CheckGates(tk, StageDesign)
	if len(errs) != 0 {
		t.Errorf("spec→design with AC and approved review should pass, got %v", errs)
	}
}

func TestCheckGates_ImplementToTest_MandatoryReviews(t *testing.T) {
	tk := gateTicket(StageImplement, TypeFeature)
	errs := CheckGates(tk, StageTest)
	// Should fail: no code review, no impl review.
	if len(errs) < 2 {
		t.Errorf("implement→test without reviews should have 2+ failures, got %d", len(errs))
	}
}

func TestCheckGates_ImplementToTest_WithReviews(t *testing.T) {
	tk := gateTicket(StageImplement, TypeFeature)
	tk.Reviews = []ReviewRecord{
		{Reviewer: "agent:code-review", Verdict: "approved"},
		{Reviewer: "agent:impl-review", Verdict: "approved"},
	}
	errs := CheckGates(tk, StageTest)
	if len(errs) != 0 {
		t.Errorf("implement→test with reviews should pass, got %v", errs)
	}
}

func TestCheckGates_TestToVerify_NeedsResults(t *testing.T) {
	tk := gateTicket(StageTest, TypeFeature)
	errs := CheckGates(tk, StageVerify)
	if len(errs) == 0 {
		t.Error("test→verify without test results should fail")
	}
}

func TestCheckGates_TestToVerify_WithResults(t *testing.T) {
	tk := gateTicket(StageTest, TypeFeature)
	tk.Body = "\n## Test Results\n\nAll pass.\n"
	errs := CheckGates(tk, StageVerify)
	if len(errs) != 0 {
		t.Errorf("test→verify with results should pass, got %v", errs)
	}
}

func TestCheckGates_LowRisk_NotAdvisory(t *testing.T) {
	// Risk-based variants replace risk scaling. Low-risk tickets get
	// different pipelines that skip review stages, but gates on
	// transitions that exist are still enforced.
	tk := gateTicket(StageSpec, TypeFeature)
	tk.Risk = RiskLow
	errs := CheckGates(tk, StageDesign)
	if len(errs) == 0 {
		t.Error("spec→design without AC and review should still fail for low risk")
	}
}

func TestCheckGates_BacklogToTriage_NeedsRisk(t *testing.T) {
	tk := gateTicket(StageBacklog, TypeFeature)
	// Has description and valid priority, but no risk.
	errs := CheckGates(tk, StageTriage)
	if len(errs) == 0 {
		t.Error("backlog→triage without risk should fail gate")
	}
	found := false
	for _, err := range errs {
		if err.Error() != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one gate error")
	}
}

func TestCheckGates_BacklogToTriage_AllFields(t *testing.T) {
	tk := gateTicket(StageBacklog, TypeFeature)
	tk.Risk = RiskNormal
	errs := CheckGates(tk, StageTriage)
	if len(errs) != 0 {
		t.Errorf("backlog→triage with description, priority, and risk should pass, got %v", errs)
	}
}

func TestCheckGates_BacklogToTriage_NoDescription(t *testing.T) {
	tk := gateTicket(StageBacklog, TypeFeature)
	tk.Body = "\n"
	tk.Risk = RiskNormal
	errs := CheckGates(tk, StageTriage)
	if len(errs) == 0 {
		t.Error("backlog→triage without description should fail gate")
	}
}

func TestCheckGates_ChoreVerifyToDone(t *testing.T) {
	tk := gateTicket(StageVerify, TypeChore)
	// No reviews — should fail.
	errs := CheckGates(tk, StageDone)
	if len(errs) == 0 {
		t.Error("chore verify→done without review should fail")
	}

	// Approve the review.
	tk.Review = ReviewApproved
	errs = CheckGates(tk, StageDone)
	if len(errs) != 0 {
		t.Errorf("chore verify→done with review should pass, got %v", errs)
	}
}
