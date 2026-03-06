package ticket

import (
	"fmt"
	"strings"
)

// GateCheck describes a precondition for a stage transition.
type GateCheck struct {
	From        Stage
	To          Stage
	Description string
	Type        string // "structural" or "agentic"
	Check       func(t *Ticket) error
}

// GateResult describes the outcome of a single gate check.
type GateResult struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "structural" or "agentic"
	Status      string `json:"status"` // "pass", "fail", "unevaluated"
	Error       string `json:"error,omitempty"`
}

// Gates returns the gate checks for advancing from one stage to the next.
// Structural gates have executable checks; agentic gates are returned as requirements.
func Gates(from, to Stage) []GateCheck {
	key := GateKey(from, to)
	gc, ok := loadedConfig.Gates[key]
	if !ok {
		return nil
	}

	var gates []GateCheck

	for _, name := range gc.Structural {
		fn := structuralCheckFunc(name)
		gates = append(gates, GateCheck{
			From:        from,
			To:          to,
			Description: structuralDescription(name),
			Type:        "structural",
			Check:       fn,
		})
	}

	for _, desc := range gc.Agentic {
		gates = append(gates, GateCheck{
			From:        from,
			To:          to,
			Description: desc,
			Type:        "agentic",
			Check:       nil, // Agentic gates are not checked server-side.
		})
	}

	return gates
}

// CheckGates runs all gate checks for advancing ticket t from its current
// stage to the given target stage. Returns all failures.
// Agentic gates are skipped (returned separately via EvaluateGates).
func CheckGates(t *Ticket, to Stage) []error {
	gates := Gates(t.Stage, to)

	var errs []error
	for _, g := range gates {
		if g.Type == "agentic" {
			continue // Agentic gates are not evaluated server-side.
		}
		if g.Check != nil {
			if err := g.Check(t); err != nil {
				errs = append(errs, fmt.Errorf("gate %q: %w", g.Description, err))
			}
		}
	}
	return errs
}

// EvaluateGates returns structured results for all gates on a transition.
// Structural gates are evaluated; agentic gates are marked as unevaluated
// unless evidence is provided.
func EvaluateGates(t *Ticket, to Stage, evidence map[string]string) []GateResult {
	gates := Gates(t.Stage, to)

	var results []GateResult
	for _, g := range gates {
		r := GateResult{
			From:        string(g.From),
			To:          string(g.To),
			Description: g.Description,
			Type:        g.Type,
		}

		if g.Type == "structural" {
			r.Name = g.Description
			if g.Check != nil {
				if err := g.Check(t); err != nil {
					r.Status = "fail"
					r.Error = err.Error()
				} else {
					r.Status = "pass"
				}
			}
		} else {
			r.Name = g.Description
			if ev, ok := evidence[g.Description]; ok {
				r.Status = "pass"
				r.Error = ev // Store attestation as context.
			} else {
				r.Status = "unevaluated"
			}
		}

		results = append(results, r)
	}
	return results
}

// structuralCheckFunc maps a named check from config to a Go function.
func structuralCheckFunc(name string) func(*Ticket) error {
	switch name {
	case "description_exists":
		return checkDescriptionExists
	case "acceptance_exists":
		return checkACExists
	case "design_exists":
		return checkDesignExists
	case "review_approved":
		return checkReviewApproved
	case "code_review_approved":
		return checkCodeReviewApproved
	case "impl_review_approved":
		return checkImplReviewApproved
	case "advisory_review_surfaced":
		return checkAdvisoryReviewSurfaced
	case "test_results_recorded":
		return checkTestResultsRecorded
	default:
		return func(*Ticket) error {
			return fmt.Errorf("unknown structural gate %q", name)
		}
	}
}

// Structural check implementations.

func checkDescriptionExists(t *Ticket) error {
	body := strings.TrimSpace(t.Body)
	if body == "" {
		return fmt.Errorf("ticket has no description")
	}
	return nil
}

func checkACExists(t *Ticket) error {
	if !strings.Contains(t.Body, "## Acceptance Criteria") {
		return fmt.Errorf("missing ## Acceptance Criteria section")
	}
	return nil
}

func checkDesignExists(t *Ticket) error {
	if !strings.Contains(t.Body, "## Design") && !strings.Contains(t.Body, "## Implementation Plan") {
		return fmt.Errorf("missing ## Design or ## Implementation Plan section")
	}
	return nil
}

func checkReviewApproved(t *Ticket) error {
	if t.Review != ReviewApproved {
		return fmt.Errorf("review state is %q, want approved", t.Review)
	}
	return nil
}

func checkCodeReviewApproved(t *Ticket) error {
	for _, r := range t.Reviews {
		if strings.Contains(r.Reviewer, "code-review") && r.Verdict == "approved" {
			return nil
		}
	}
	return fmt.Errorf("no approved code review found")
}

func checkImplReviewApproved(t *Ticket) error {
	for _, r := range t.Reviews {
		if strings.Contains(r.Reviewer, "impl-review") && r.Verdict == "approved" {
			return nil
		}
	}
	return fmt.Errorf("no approved impl review found")
}

func checkAdvisoryReviewSurfaced(t *Ticket) error {
	if len(t.Reviews) > 0 {
		return nil
	}
	return fmt.Errorf("no advisory review recorded")
}

func checkTestResultsRecorded(t *Ticket) error {
	if !strings.Contains(t.Body, "## Test Results") {
		return fmt.Errorf("missing ## Test Results section")
	}
	return nil
}
