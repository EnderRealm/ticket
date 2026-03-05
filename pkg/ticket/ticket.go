// Package ticket provides core types and operations for ticket management.
package ticket

import (
	"fmt"
	"time"
)

// Status represents the lifecycle state of a ticket.
type Status string

const (
	StatusOpen         Status = "open"
	StatusInProgress   Status = "in_progress"
	StatusNeedsTesting Status = "needs_testing"
	StatusClosed       Status = "closed"
)

var validStatuses = map[Status]bool{
	StatusOpen:         true,
	StatusInProgress:   true,
	StatusNeedsTesting: true,
	StatusClosed:       true,
}

// ValidateStatus returns an error if s is not a recognized status.
func ValidateStatus(s Status) error {
	if validStatuses[s] {
		return nil
	}
	return fmt.Errorf("invalid status %q: must be one of open, in_progress, needs_testing, closed", s)
}

// Stage represents a position in a type-dependent pipeline.
type Stage string

const (
	StageTriage    Stage = "triage"
	StageSpec      Stage = "spec"
	StageDesign    Stage = "design"
	StageImplement Stage = "implement"
	StageTest      Stage = "test"
	StageVerify    Stage = "verify"
	StageDone      Stage = "done"
)

var validStages = map[Stage]bool{
	StageTriage:    true,
	StageSpec:      true,
	StageDesign:    true,
	StageImplement: true,
	StageTest:      true,
	StageVerify:    true,
	StageDone:      true,
}

// ValidateStage returns an error if s is not a recognized stage.
func ValidateStage(s Stage) error {
	if validStages[s] {
		return nil
	}
	return fmt.Errorf("invalid stage %q: must be one of triage, spec, design, implement, test, verify, done", s)
}

// ReviewState tracks whether a stage is awaiting, has passed, or has failed review.
type ReviewState string

const (
	ReviewNone     ReviewState = ""
	ReviewPending  ReviewState = "pending"
	ReviewApproved ReviewState = "approved"
	ReviewRejected ReviewState = "rejected"
)

var validReviewStates = map[ReviewState]bool{
	ReviewNone:     true,
	ReviewPending:  true,
	ReviewApproved: true,
	ReviewRejected: true,
}

// ValidateReviewState returns an error if r is not a recognized review state.
func ValidateReviewState(r ReviewState) error {
	if validReviewStates[r] {
		return nil
	}
	return fmt.Errorf("invalid review state %q: must be one of pending, approved, rejected, or empty", r)
}

// RiskLevel categorizes how much scrutiny a ticket warrants.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskNormal   RiskLevel = "normal"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

var validRiskLevels = map[RiskLevel]bool{
	RiskLow:      true,
	RiskNormal:   true,
	RiskHigh:     true,
	RiskCritical: true,
}

// ValidateRiskLevel returns an error if r is not a recognized risk level.
func ValidateRiskLevel(r RiskLevel) error {
	if validRiskLevels[r] {
		return nil
	}
	return fmt.Errorf("invalid risk level %q: must be one of low, normal, high, critical", r)
}

// ReviewRecord captures a single review event on a ticket.
type ReviewRecord struct {
	Timestamp time.Time `yaml:"timestamp"`
	Reviewer  string    `yaml:"reviewer"`  // e.g. "human:steve", "agent:design-reviewer"
	Verdict   string    `yaml:"verdict"`   // "approved", "rejected", "comment"
	Comment   string    `yaml:"comment,omitempty"`
	Stage     Stage     `yaml:"stage"`
}

// WaitingOn describes what a ticket is blocked on within its current stage.
type WaitingOn struct {
	Actor  string `yaml:"actor"`  // "human", "agent:<name>", or a ticket ID
	Action string `yaml:"action"` // e.g. "review", "approve", "implement"
}

// TicketType represents the kind of work a ticket tracks.
type TicketType string

const (
	TypeTask    TicketType = "task"
	TypeFeature TicketType = "feature"
	TypeBug     TicketType = "bug"
	TypeEpic    TicketType = "epic"
	TypeChore   TicketType = "chore"
)

var validTypes = map[TicketType]bool{
	TypeTask:    true,
	TypeFeature: true,
	TypeBug:     true,
	TypeEpic:    true,
	TypeChore:   true,
}

// ValidateType returns an error if t is not a recognized ticket type.
func ValidateType(t TicketType) error {
	if validTypes[t] {
		return nil
	}
	return fmt.Errorf("invalid type %q: must be one of task, feature, bug, epic, chore", t)
}

// ValidatePriority returns an error if p is outside the range 0-4.
func ValidatePriority(p int) error {
	if p >= 0 && p <= 4 {
		return nil
	}
	return fmt.Errorf("invalid priority %d: must be 0-4", p)
}

// Note is a timestamped comment appended to a ticket.
type Note struct {
	Timestamp time.Time
	Text      string
}

// Ticket is the core data structure representing a work item.
// YAML frontmatter fields are mapped via yaml tags. Title and body
// content are parsed from the markdown outside the frontmatter.
type Ticket struct {
	ID          string     `yaml:"id"`
	Status      Status     `yaml:"status,omitempty"`
	Stage       Stage      `yaml:"stage,omitempty"`
	Review      ReviewState `yaml:"review,omitempty"`
	Risk        RiskLevel  `yaml:"risk,omitempty"`
	Type        TicketType `yaml:"type"`
	Priority    int        `yaml:"priority"`
	Assignee    string     `yaml:"assignee,omitempty"`
	Parent      string     `yaml:"parent,omitempty"`
	Deps        []string   `yaml:"deps,flow"`
	Links       []string   `yaml:"links,flow"`
	Tags        []string   `yaml:"tags,omitempty,flow"`
	ExternalRef string     `yaml:"external-ref,omitempty"`
	Branch      string     `yaml:"branch,omitempty"`
	Created     time.Time  `yaml:"created"`
	Skipped     []Stage    `yaml:"skipped,omitempty,flow"`
	Conversations []string `yaml:"conversations,omitempty,flow"`

	// Parsed from markdown, not stored in frontmatter.
	Title   string         `yaml:"-"`
	Body    string         `yaml:"-"`
	Notes   []Note         `yaml:"-"`
	Reviews []ReviewRecord `yaml:"-"`
}

// Validate checks all fields for consistency. Returns the first error found.
func (t *Ticket) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("ticket ID is required")
	}

	// Dual mode: tickets must have either status (legacy) or stage (pipeline).
	hasStatus := t.Status != ""
	hasStage := t.Stage != ""
	if !hasStatus && !hasStage {
		return fmt.Errorf("ticket must have either status or stage")
	}
	if hasStatus {
		if err := ValidateStatus(t.Status); err != nil {
			return err
		}
	}
	if hasStage {
		if err := ValidateStage(t.Stage); err != nil {
			return err
		}
	}

	if t.Review != ReviewNone {
		if err := ValidateReviewState(t.Review); err != nil {
			return err
		}
	}
	if t.Risk != "" {
		if err := ValidateRiskLevel(t.Risk); err != nil {
			return err
		}
	}
	for _, s := range t.Skipped {
		if err := ValidateStage(s); err != nil {
			return fmt.Errorf("invalid skipped stage: %w", err)
		}
	}

	if err := ValidateType(t.Type); err != nil {
		return err
	}
	if err := ValidatePriority(t.Priority); err != nil {
		return err
	}
	return nil
}

// ValidateStageForType checks that the ticket's stage is valid for its type's pipeline.
func ValidateStageForType(t *Ticket) error {
	if t.Stage == "" {
		return nil // Legacy ticket without stage — nothing to validate.
	}
	if !HasStage(t.Type, t.Stage) {
		return fmt.Errorf("stage %q is not part of the %s pipeline", t.Stage, t.Type)
	}
	return nil
}

// ValidateGates checks all gate preconditions for advancing from the ticket's
// current stage to the given target stage, without actually advancing.
func ValidateGates(t *Ticket, to Stage) []error {
	return CheckGates(t, to)
}
