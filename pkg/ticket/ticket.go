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

// Stage represents a position in a type-dependent pipeline.
type Stage string

const (
	StageTriage       Stage = "triage"
	StageSpec         Stage = "spec"
	StageDesign       Stage = "design"
	StageDesignReview Stage = "design-review"
	StageImplement    Stage = "implement"
	StageCodeReview   Stage = "code-review"
	StageTest         Stage = "test"
	StageVerify       Stage = "verify"
	StageDone         Stage = "done"
)

// ValidateStage returns an error if s is not a recognized stage.
// Valid stages are defined by the embedded pipeline configuration.
func ValidateStage(s Stage) error {
	if isValidStage(s) {
		return nil
	}
	return fmt.Errorf("invalid stage %q: not defined in pipeline configuration", s)
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

	// Custom key/value pairs, handled manually in format.go.
	Extra map[string]string `yaml:"-"`

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

	if t.Stage == "" {
		return fmt.Errorf("ticket must have a stage")
	}
	if err := ValidateStage(t.Stage); err != nil {
		return err
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

// reservedKeys lists YAML frontmatter keys and JSON output keys used by known Ticket fields.
// Extra fields are flattened to the top level in JSON, so both namespaces must be reserved.
var reservedKeys = map[string]bool{
	// YAML frontmatter fields.
	"id": true, "status": true, "stage": true, "review": true, "risk": true,
	"deps": true, "links": true, "created": true, "type": true, "priority": true,
	"assignee": true, "external-ref": true, "branch": true, "parent": true,
	"tags": true, "skipped": true, "conversations": true,
	// JSON output fields derived from body sections and markdown heading.
	"title": true, "description": true, "design": true, "notes": true, "reviews": true,
	"acceptance_criteria": true, "test_results": true, "external_ref": true,
}

// IsReservedKey reports whether key is a known frontmatter field name.
func IsReservedKey(key string) bool {
	return reservedKeys[key]
}

// ValidateExtraKey checks that key is a valid extra field name.
// Keys must be non-empty identifiers: letters, digits, hyphens, underscores.
func ValidateExtraKey(key string) error {
	if key == "" {
		return fmt.Errorf("extra field key must not be empty")
	}
	if reservedKeys[key] {
		return fmt.Errorf("extra field key %q is reserved", key)
	}
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("extra field key %q contains invalid character %q (allowed: letters, digits, hyphens, underscores)", key, string(c))
		}
	}
	return nil
}

// ValidateExtraValue checks that value is safe for unquoted YAML serialization.
// Values allow printable ASCII: letters, digits, spaces, and common punctuation.
// YAML indicator characters (% ! & * @ ` : # [ ] { } | > ' ") and control
// characters are rejected to prevent parse failures in writeField output.
func ValidateExtraValue(value string) error {
	for _, c := range value {
		if c < ' ' || c > '~' {
			return fmt.Errorf("extra field value contains non-printable character %q", string(c))
		}
		switch c {
		case ':', '#', '[', ']', '{', '}', '%', '!', '&', '*', '@', '`', '|', '>', '\'', '"':
			return fmt.Errorf("extra field value contains invalid character %q", string(c))
		}
	}
	return nil
}
