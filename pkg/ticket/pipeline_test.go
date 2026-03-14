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
		{TypeFeature, 8},
		{TypeBug, 6},
		{TypeChore, 5},
		{TypeEpic, 5},
		{TypeTask, 6},
	}
	for _, tt := range tests {
		p, _ := PipelineFor(tt.typ)
		if len(p) != tt.stages {
			t.Errorf("PipelineFor(%s) has %d stages, want %d", tt.typ, len(p), tt.stages)
		}
	}
}

func TestHasStage(t *testing.T) {
	// Features have all stages.
	for _, s := range []Stage{StageBacklog, StageTriage, StageSpec, StageDesign, StageImplement, StageTest, StageVerify, StageDone} {
		if !HasStage(TypeFeature, s) {
			t.Errorf("HasStage(feature, %s) = false, want true", s)
		}
	}

	// Chores skip spec, design, test.
	for _, s := range []Stage{StageSpec, StageDesign, StageTest} {
		if HasStage(TypeChore, s) {
			t.Errorf("HasStage(chore, %s) = true, want false", s)
		}
	}

	// Bugs skip spec and design.
	if HasStage(TypeBug, StageSpec) {
		t.Error("HasStage(bug, spec) = true, want false")
	}
	if HasStage(TypeBug, StageDesign) {
		t.Error("HasStage(bug, design) = true, want false")
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

	// Bug: triage → implement (skips spec/design).
	next, ok = NextStage(TypeBug, StageTriage)
	if !ok || next != StageImplement {
		t.Errorf("NextStage(bug, triage) = (%s, %v), want (implement, true)", next, ok)
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
	if idx := StageIndex(TypeFeature, StageDone); idx != 7 {
		t.Errorf("StageIndex(feature, done) = %d, want 7", idx)
	}
	if idx := StageIndex(TypeChore, StageDesign); idx != -1 {
		t.Errorf("StageIndex(chore, design) = %d, want -1", idx)
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
