package ticket

import "testing"

func TestPipelineFor_AllTypes(t *testing.T) {
	types := []TicketType{TypeFeature, TypeBug, TypeChore, TypeEpic, TypeTask}
	for _, tt := range types {
		p, err := PipelineFor(tt)
		if err != nil {
			t.Errorf("PipelineFor(%s): %v", tt, err)
			continue
		}
		if len(p) == 0 {
			t.Errorf("PipelineFor(%s) returned empty pipeline", tt)
		}
		// Every pipeline starts at backlog and ends at done.
		if p[0] != StageBacklog {
			t.Errorf("PipelineFor(%s) starts at %s, want backlog", tt, p[0])
		}
		if p[len(p)-1] != StageDone {
			t.Errorf("PipelineFor(%s) ends at %s, want done", tt, p[len(p)-1])
		}
	}
}

func TestPipelineFor_InvalidType(t *testing.T) {
	_, err := PipelineFor("invalid")
	if err == nil {
		t.Error("PipelineFor(invalid) should fail")
	}
}

func TestPipelineLengths(t *testing.T) {
	tests := []struct {
		typ    TicketType
		stages int
	}{
		{TypeFeature, 10},
		{TypeBug, 7},
		{TypeChore, 10},
		{TypeEpic, 5},
		{TypeTask, 3},
	}
	for _, tt := range tests {
		p, _ := PipelineFor(tt.typ)
		if len(p) != tt.stages {
			t.Errorf("PipelineFor(%s) has %d stages, want %d", tt.typ, len(p), tt.stages)
		}
	}
}

func TestPipelineFor_FeatureLow(t *testing.T) {
	p, err := PipelineFor(TypeFeature, RiskLow)
	if err != nil {
		t.Fatalf("PipelineFor(feature, low): %v", err)
	}
	if len(p) != 8 {
		t.Errorf("feature/low has %d stages, want 8", len(p))
	}
	// Low risk features skip design-review and code-review.
	for _, s := range p {
		if s == StageDesignReview || s == StageCodeReview {
			t.Errorf("feature/low should not include %s", s)
		}
	}
}

func TestPipelineFor_BugLow(t *testing.T) {
	p, err := PipelineFor(TypeBug, RiskLow)
	if err != nil {
		t.Fatalf("PipelineFor(bug, low): %v", err)
	}
	if len(p) != 5 {
		t.Errorf("bug/low has %d stages, want 5", len(p))
	}
}

func TestPipelineFor_BugHighFull(t *testing.T) {
	p, err := PipelineFor(TypeBug, RiskHigh)
	if err != nil {
		t.Fatalf("PipelineFor(bug, high): %v", err)
	}
	if len(p) != 10 {
		t.Errorf("bug/high has %d stages, want 10", len(p))
	}
	// High risk bugs get the full pipeline including spec and design.
	hasSpec, hasDesign, hasDesignReview := false, false, false
	for _, s := range p {
		switch s {
		case StageSpec:
			hasSpec = true
		case StageDesign:
			hasDesign = true
		case StageDesignReview:
			hasDesignReview = true
		}
	}
	if !hasSpec {
		t.Error("bug/high should include spec")
	}
	if !hasDesign {
		t.Error("bug/high should include design")
	}
	if !hasDesignReview {
		t.Error("bug/high should include design-review")
	}
}

func TestPipelineFor_BugCriticalEqHigh(t *testing.T) {
	high, _ := PipelineFor(TypeBug, RiskHigh)
	crit, _ := PipelineFor(TypeBug, RiskCritical)
	if len(high) != len(crit) {
		t.Fatalf("bug/critical has %d stages, bug/high has %d", len(crit), len(high))
	}
	for i := range high {
		if high[i] != crit[i] {
			t.Errorf("bug/critical[%d] = %s, bug/high[%d] = %s", i, crit[i], i, high[i])
		}
	}
}

func TestPipelineFor_ChoreLow(t *testing.T) {
	p, err := PipelineFor(TypeChore, RiskLow)
	if err != nil {
		t.Fatalf("PipelineFor(chore, low): %v", err)
	}
	if len(p) != 8 {
		t.Errorf("chore/low has %d stages, want 8", len(p))
	}
}

func TestPipelineFor_TaskAllRisks(t *testing.T) {
	for _, risk := range []RiskLevel{RiskLow, RiskNormal, RiskHigh, RiskCritical} {
		p, err := PipelineFor(TypeTask, risk)
		if err != nil {
			t.Fatalf("PipelineFor(task, %s): %v", risk, err)
		}
		if len(p) != 3 {
			t.Errorf("task/%s has %d stages, want 3", risk, len(p))
		}
	}
}

func TestHasStage(t *testing.T) {
	// Features have all standard stages in default pipeline.
	for _, s := range []Stage{StageBacklog, StageTriage, StageSpec, StageDesign, StageImplement, StageTest, StageVerify, StageDone} {
		if !HasStage(TypeFeature, s) {
			t.Errorf("HasStage(feature, %s) = false, want true", s)
		}
	}

	// Chores now include spec, design, and test (mirrors feature pipeline).
	for _, s := range []Stage{StageSpec, StageDesign, StageTest} {
		if !HasStage(TypeChore, s) {
			t.Errorf("HasStage(chore, %s) = false, want true", s)
		}
	}

	// Bugs skip spec and design in default pipeline.
	if HasStage(TypeBug, StageSpec) {
		t.Error("HasStage(bug, spec) = true, want false")
	}
	if HasStage(TypeBug, StageDesign) {
		t.Error("HasStage(bug, design) = true, want false")
	}

	// Tasks only have backlog, triage, done.
	if HasStage(TypeTask, StageImplement) {
		t.Error("HasStage(task, implement) = true, want false")
	}
	if HasStage(TypeTask, StageSpec) {
		t.Error("HasStage(task, spec) = true, want false")
	}
}

func TestHasStageInPipeline_BugHigh(t *testing.T) {
	if !HasStageInPipeline(TypeBug, RiskHigh, StageSpec) {
		t.Error("HasStageInPipeline(bug, high, spec) = false, want true")
	}
	if !HasStageInPipeline(TypeBug, RiskHigh, StageDesign) {
		t.Error("HasStageInPipeline(bug, high, design) = false, want true")
	}
	if !HasStageInPipeline(TypeBug, RiskHigh, StageDesignReview) {
		t.Error("HasStageInPipeline(bug, high, design-review) = false, want true")
	}
}

func TestNextStage(t *testing.T) {
	// Feature: backlog → triage.
	next, ok := NextStage(TypeFeature, StageBacklog)
	if !ok || next != StageTriage {
		t.Errorf("NextStage(feature, backlog) = (%s, %v), want (triage, true)", next, ok)
	}

	// Feature: triage → spec.
	next, ok = NextStage(TypeFeature, StageTriage)
	if !ok || next != StageSpec {
		t.Errorf("NextStage(feature, triage) = (%s, %v), want (spec, true)", next, ok)
	}

	// Bug: triage → implement (skips spec/design in default pipeline).
	next, ok = NextStage(TypeBug, StageTriage)
	if !ok || next != StageImplement {
		t.Errorf("NextStage(bug, triage) = (%s, %v), want (implement, true)", next, ok)
	}

	// Task: triage → done.
	next, ok = NextStage(TypeTask, StageTriage)
	if !ok || next != StageDone {
		t.Errorf("NextStage(task, triage) = (%s, %v), want (done, true)", next, ok)
	}

	// Chore: triage → spec (now follows feature pipeline).
	next, ok = NextStage(TypeChore, StageTriage)
	if !ok || next != StageSpec {
		t.Errorf("NextStage(chore, triage) = (%s, %v), want (spec, true)", next, ok)
	}

	// Done is final for all types.
	_, ok = NextStage(TypeFeature, StageDone)
	if ok {
		t.Error("NextStage(feature, done) should return false")
	}
}

func TestPrevStage(t *testing.T) {
	prev, ok := PrevStage(TypeFeature, StageSpec)
	if !ok || prev != StageTriage {
		t.Errorf("PrevStage(feature, spec) = (%s, %v), want (triage, true)", prev, ok)
	}

	prev, ok = PrevStage(TypeFeature, StageTriage)
	if !ok || prev != StageBacklog {
		t.Errorf("PrevStage(feature, triage) = (%s, %v), want (backlog, true)", prev, ok)
	}

	_, ok = PrevStage(TypeFeature, StageBacklog)
	if ok {
		t.Error("PrevStage(feature, backlog) should return false")
	}
}

func TestStageIndex(t *testing.T) {
	if idx := StageIndex(TypeFeature, StageBacklog); idx != 0 {
		t.Errorf("StageIndex(feature, backlog) = %d, want 0", idx)
	}
	if idx := StageIndex(TypeFeature, StageTriage); idx != 1 {
		t.Errorf("StageIndex(feature, triage) = %d, want 1", idx)
	}
	// Feature default now has 10 stages: done is at index 9.
	if idx := StageIndex(TypeFeature, StageDone); idx != 9 {
		t.Errorf("StageIndex(feature, done) = %d, want 9", idx)
	}
	// Chore default now includes design at index 3.
	if idx := StageIndex(TypeChore, StageDesign); idx != 3 {
		t.Errorf("StageIndex(chore, design) = %d, want 3", idx)
	}
}

func TestIsFinalStage(t *testing.T) {
	if !IsFinalStage(TypeFeature, StageDone) {
		t.Error("IsFinalStage(feature, done) = false, want true")
	}
	if IsFinalStage(TypeFeature, StageImplement) {
		t.Error("IsFinalStage(feature, implement) = true, want false")
	}
}
