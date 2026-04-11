// Package ticket provides core types and operations for ticket management.
package ticket

import (
	"fmt"
	"time"
)

// Status represents the lifecycle state of a ticket.
type Status string

const (
	StatusBacklog Status = "backlog"
	StatusReady   Status = "ready"
	StatusOpen    Status = "open"
	StatusDone    Status = "done"
	StatusClosed  Status = "closed"
)

var validStatuses = map[Status]bool{
	StatusBacklog: true,
	StatusReady:   true,
	StatusOpen:    true,
	StatusDone:    true,
	StatusClosed:  true,
}

// ValidateStatus returns an error if s is not a recognized status.
func ValidateStatus(s Status) error {
	if validStatuses[s] {
		return nil
	}
	return fmt.Errorf("invalid status %q: must be one of backlog, ready, open, done, closed", s)
}

// StatusOrder returns the sort rank for a status (lower = earlier in display).
func StatusOrder(s Status) int {
	switch s {
	case StatusOpen:
		return 0
	case StatusReady:
		return 1
	case StatusBacklog:
		return 2
	case StatusDone:
		return 3
	case StatusClosed:
		return 4
	default:
		return 5
	}
}

// TicketType represents the kind of work a ticket tracks.
type TicketType string

const (
	TypeFeature TicketType = "feature"
	TypeBug     TicketType = "bug"
	TypeEpic    TicketType = "epic"
)

var validTypes = map[TicketType]bool{
	TypeFeature: true,
	TypeBug:     true,
	TypeEpic:    true,
}

// ValidateType returns an error if t is not a recognized ticket type.
func ValidateType(t TicketType) error {
	if validTypes[t] {
		return nil
	}
	return fmt.Errorf("invalid type %q: must be one of feature, bug, epic", t)
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
	ID            string     `yaml:"id"`
	Status        Status     `yaml:"status"`
	Type          TicketType `yaml:"type"`
	Priority      int        `yaml:"priority"`
	Parent      string     `yaml:"parent,omitempty"`
	Deps        []string   `yaml:"deps,flow"`
	Links       []string   `yaml:"links,flow"`
	Tags        []string   `yaml:"tags,omitempty,flow"`
	ExternalRef string     `yaml:"external-ref,omitempty"`
	Branch      string     `yaml:"branch,omitempty"`
	Created     time.Time  `yaml:"created"`

	// Custom key/value pairs, handled manually in format.go.
	Extra map[string]string `yaml:"-"`

	// Parsed from markdown, not stored in frontmatter.
	Title string `yaml:"-"`
	Body  string `yaml:"-"`
	Notes []Note `yaml:"-"`
}

// Validate checks all fields for consistency. Returns the first error found.
func (t *Ticket) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("ticket ID is required")
	}
	if t.Status == "" {
		return fmt.Errorf("ticket must have a status")
	}
	if err := ValidateStatus(t.Status); err != nil {
		return err
	}
	if err := ValidateType(t.Type); err != nil {
		return err
	}
	if err := ValidatePriority(t.Priority); err != nil {
		return err
	}
	return nil
}

// reservedKeys lists YAML frontmatter keys and JSON output keys used by known Ticket fields.
// Extra fields are flattened to the top level in JSON, so both namespaces must be reserved.
var reservedKeys = map[string]bool{
	// YAML frontmatter fields.
	"id": true, "status": true,
	"deps": true, "links": true, "created": true, "type": true, "priority": true,
	"external-ref": true, "branch": true, "parent": true, "tags": true,
	// JSON output fields derived from body sections and markdown heading.
	"title": true, "description": true, "design": true, "notes": true,
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
