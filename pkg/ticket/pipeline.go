package ticket

import "fmt"

// Pipelines provides backward-compatible access to the default pipeline map.
// Callers that need risk-aware pipelines should use PipelineFor directly.
var Pipelines map[TicketType][]Stage

func initPipelines() {
	Pipelines = make(map[TicketType][]Stage)
	for typeName, variants := range loadedConfig.Pipelines {
		if stages, ok := variants["default"]; ok {
			result := make([]Stage, len(stages))
			for i, s := range stages {
				result[i] = Stage(s)
			}
			Pipelines[TicketType(typeName)] = result
		}
	}
}

// PipelineFor returns the stage sequence for the given ticket type and risk level.
// If risk is empty, the default pipeline is returned.
func PipelineFor(t TicketType, risk ...RiskLevel) ([]Stage, error) {
	var r RiskLevel
	if len(risk) > 0 {
		r = risk[0]
	}
	return lookupPipeline(t, r)
}

// HasStage reports whether the default pipeline for the given type includes the stage.
func HasStage(t TicketType, s Stage) bool {
	p, ok := Pipelines[t]
	if !ok {
		return false
	}
	for _, ps := range p {
		if ps == s {
			return true
		}
	}
	return false
}

// HasStageInPipeline reports whether a specific pipeline (type+risk) includes the stage.
func HasStageInPipeline(t TicketType, risk RiskLevel, s Stage) bool {
	p, err := lookupPipeline(t, risk)
	if err != nil {
		return false
	}
	for _, ps := range p {
		if ps == s {
			return true
		}
	}
	return false
}

// StageIndex returns the zero-based position of a stage in a type's default pipeline,
// or -1 if the stage is not part of that pipeline.
func StageIndex(t TicketType, s Stage) int {
	p, ok := Pipelines[t]
	if !ok {
		return -1
	}
	for i, ps := range p {
		if ps == s {
			return i
		}
	}
	return -1
}

// StageIndexInPipeline returns the position of a stage in a specific pipeline variant.
func StageIndexInPipeline(pipeline []Stage, s Stage) int {
	for i, ps := range pipeline {
		if ps == s {
			return i
		}
	}
	return -1
}

// NextStage returns the stage after s in the default pipeline for type t.
// Returns empty string and false if s is the final stage or not in the pipeline.
func NextStage(t TicketType, s Stage) (Stage, bool) {
	idx := StageIndex(t, s)
	p := Pipelines[t]
	if idx < 0 || idx >= len(p)-1 {
		return "", false
	}
	return p[idx+1], true
}

// NextStageInPipeline returns the stage after s in a specific pipeline.
func NextStageInPipeline(pipeline []Stage, s Stage) (Stage, bool) {
	idx := StageIndexInPipeline(pipeline, s)
	if idx < 0 || idx >= len(pipeline)-1 {
		return "", false
	}
	return pipeline[idx+1], true
}

// PrevStage returns the stage before s in the default pipeline for type t.
// Returns empty string and false if s is the first stage or not in the pipeline.
func PrevStage(t TicketType, s Stage) (Stage, bool) {
	idx := StageIndex(t, s)
	if idx <= 0 {
		return "", false
	}
	return Pipelines[t][idx-1], true
}

// IsFinalStage reports whether s is the last stage in the default pipeline for type t.
func IsFinalStage(t TicketType, s Stage) bool {
	p, ok := Pipelines[t]
	if !ok {
		return false
	}
	return p[len(p)-1] == s
}

// PipelineDescription generates a human-readable summary of all pipelines.
func PipelineDescription() string {
	var desc string
	for _, t := range []TicketType{TypeFeature, TypeBug, TypeTask, TypeChore, TypeEpic} {
		p, ok := Pipelines[t]
		if !ok {
			continue
		}
		names := make([]string, len(p))
		for i, s := range p {
			names[i] = string(s)
		}
		desc += fmt.Sprintf("  %-10s", t)
		for i, n := range names {
			if i > 0 {
				desc += " → "
			}
			desc += n
		}
		desc += "\n"
	}
	return desc
}
