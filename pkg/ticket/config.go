package ticket

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
)

//go:embed pipelines.json
var pipelinesJSON []byte

// PipelineConfig holds the full pipeline configuration loaded from JSON.
type PipelineConfig struct {
	Stages    []string                                `json:"stages"`
	Pipelines map[string]map[string][]string          `json:"pipelines"`
	Gates     map[string]GateConfig                   `json:"gates"`
}

// GateConfig declares the checks required for a stage transition.
type GateConfig struct {
	Structural []string `json:"structural,omitempty"`
	Agentic    []string `json:"agentic,omitempty"`
}

// GateInfo describes a single gate check with its type and description.
type GateInfo struct {
	Name        string // check function name or agentic description
	Type        string // "structural" or "agentic"
	Description string // human-readable description
}

var loadedConfig PipelineConfig

func init() {
	if err := json.Unmarshal(pipelinesJSON, &loadedConfig); err != nil {
		log.Fatalf("failed to parse embedded pipelines.json: %v", err)
	}
	initPipelines()
}

// Config returns the loaded pipeline configuration.
func Config() *PipelineConfig {
	return &loadedConfig
}

// AllStages returns all defined stages in pipeline order.
func AllStages() []Stage {
	stages := make([]Stage, len(loadedConfig.Stages))
	for i, s := range loadedConfig.Stages {
		stages[i] = Stage(s)
	}
	return stages
}

// DisplayStages returns stages suitable for display (excludes "done").
func DisplayStages() []Stage {
	var stages []Stage
	for _, s := range loadedConfig.Stages {
		if s != "done" {
			stages = append(stages, Stage(s))
		}
	}
	return stages
}

// PipelineTypes returns all ticket types that have pipeline definitions.
func PipelineTypes() []TicketType {
	var types []TicketType
	for t := range loadedConfig.Pipelines {
		types = append(types, TicketType(t))
	}
	return types
}

// GateKey builds the gate lookup key for a stage transition.
func GateKey(from, to Stage) string {
	return string(from) + ">" + string(to)
}

// GateInfoFor returns the gate declarations for a given stage transition.
func GateInfoFor(from, to Stage) []GateInfo {
	key := GateKey(from, to)
	gc, ok := loadedConfig.Gates[key]
	if !ok {
		return nil
	}
	var infos []GateInfo
	for _, name := range gc.Structural {
		infos = append(infos, GateInfo{
			Name:        name,
			Type:        "structural",
			Description: structuralDescription(name),
		})
	}
	for _, desc := range gc.Agentic {
		infos = append(infos, GateInfo{
			Name:        "",
			Type:        "agentic",
			Description: desc,
		})
	}
	return infos
}

func structuralDescription(name string) string {
	switch name {
	case "description_exists":
		return "ticket has a description"
	case "acceptance_exists":
		return "acceptance criteria section exists"
	case "design_exists":
		return "design or implementation plan section exists"
	case "review_approved":
		return "review is approved"
	case "code_review_approved":
		return "code review is approved"
	case "impl_review_approved":
		return "implementation review is approved"
	case "advisory_review_surfaced":
		return "advisory review has been recorded"
	case "test_results_recorded":
		return "test results section exists"
	default:
		return name
	}
}

// isValidStage checks if a stage is defined in the config.
func isValidStage(s Stage) bool {
	for _, cs := range loadedConfig.Stages {
		if Stage(cs) == s {
			return true
		}
	}
	return false
}

// lookupPipeline returns the stage sequence for a type+risk combination.
// Falls back to "default" if the specific risk variant is not defined.
func lookupPipeline(t TicketType, risk RiskLevel) ([]Stage, error) {
	variants, ok := loadedConfig.Pipelines[string(t)]
	if !ok {
		return nil, fmt.Errorf("no pipeline defined for type %q", t)
	}

	riskKey := string(risk)
	if riskKey == "" {
		riskKey = "default"
	}

	stages, ok := variants[riskKey]
	if !ok {
		stages, ok = variants["default"]
		if !ok {
			return nil, fmt.Errorf("no pipeline variant %q or default for type %q", riskKey, t)
		}
	}

	result := make([]Stage, len(stages))
	for i, s := range stages {
		result[i] = Stage(s)
	}
	return result, nil
}
