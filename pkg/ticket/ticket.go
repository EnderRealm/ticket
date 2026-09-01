// Package ticket provides core types and operations for ticket management.
package ticket

import (
	"fmt"
	"strings"
	"time"
	"unicode"
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
	ID     string `yaml:"id"`
	Status Status `yaml:"status"`
	// Abandoned records that an epic was given up on. It is the only thing
	// about an epic's status a human still asserts — the rest is derived from
	// the children — and it is stored rather than inferred so that a generic
	// read-modify-write round-trips it instead of recomputing one.
	Abandoned   bool       `yaml:"abandoned,omitempty"`
	Type        TicketType `yaml:"type"`
	Priority    int        `yaml:"priority"`
	Parent      string     `yaml:"parent,omitempty"`
	Deps        []string   `yaml:"deps,flow"`
	Links       []string   `yaml:"links,flow"`
	Tags        []string   `yaml:"tags,omitempty,flow"`
	ExternalRef string     `yaml:"external-ref,omitempty"`
	Branch      string     `yaml:"branch,omitempty"`
	// The three dates are all read manually in Parse rather than by the struct
	// decode. A scalar yaml.v3 cannot fit a field reaches d.terror, which records
	// a TypeError and carries on decoding the rest — the mismatch Parse tolerates.
	// time.Time never reaches it: it implements encoding.TextUnmarshaler, so the
	// scalar decode hands it the raw text first (decode.go:591 in v3.0.1) and
	// calls fail() on the error time.Parse returns, which unwinds the whole
	// decode. That error is not a TypeError and every field after the bad date
	// stays zero, so one `created: soon` would cost the ticket its type, priority,
	// parent and status. parseTimeValue reads the value off the raw map instead
	// and yields the zero time, which is what updated and completed already did.
	Created   time.Time `yaml:"-"`
	Updated   time.Time `yaml:"-"`
	Completed time.Time `yaml:"-"`

	// Results the ticket produced (branch, commit, artifacts), serialized as a
	// nested block in format.go.
	Outputs map[string]string `yaml:"outputs,omitempty"`

	// What concretely flows across each dependency edge, keyed by dep ID.
	// A dep with no entry carries nothing — a candidate for deletion during
	// grooming. Serialized as a nested block in format.go.
	DepCargo map[string]string `yaml:"dep-cargo,omitempty"`

	// The append-only review-verdict ledger (ledger.go): one row per recorded
	// verdict, keyed by the head SHA it was rendered against. Serialized as a
	// nested block in format.go. Rows are appended through RecordVerdict and
	// never edited — updateLocked refuses a write that drops or rewrites one.
	Verdicts []VerdictRow `yaml:"verdicts,omitempty"`

	// Custom key/value pairs, handled manually in format.go.
	Extra map[string]string `yaml:"-"`

	// Parsed from markdown, not stored in frontmatter.
	Title string `yaml:"-"`
	Body  string `yaml:"-"`
	Notes []Note `yaml:"-"`

	// version identifies the file bytes this ticket was read from, for the
	// compare-and-swap in FileStore.Update. Unexported so it is invisible to
	// YAML and JSON and to every caller outside this package: it is not a field
	// of the work item, it is what a write is held against. Empty on a ticket
	// the caller built rather than read, which writes unconditionally.
	version string
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
	"id": true, "status": true, "abandoned": true,
	"deps": true, "links": true, "created": true, "updated": true, "completed": true, "type": true, "priority": true,
	"external-ref": true, "branch": true, "parent": true, "tags": true, "outputs": true,
	"dep-cargo": true, "verdicts": true,
	// JSON output fields derived from body sections and markdown heading.
	"title": true, "description": true, "design": true, "notes": true,
	"acceptance_criteria": true, "test_results": true, "external_ref": true,
	"dep_cargo": true,
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
	return validateKeyChars("extra field key", key)
}

// ValidateExtraValue checks that value is safe for unquoted YAML serialization.
// Values allow printable ASCII: letters, digits, spaces, and common punctuation.
// YAML indicator characters (% ! & * @ ` : # [ ] { } | > ' ") and control
// characters are rejected to prevent parse failures in writeField output.
func ValidateExtraValue(value string) error {
	return validateValueChars("extra field value", value)
}

// ValidateOutputKey checks that key is a valid outputs block key. Character
// rules match extra field keys, but reserved frontmatter names are allowed:
// outputs keys are namespaced by the block, so `branch` and `commit` are valid.
func ValidateOutputKey(key string) error {
	if key == "" {
		return fmt.Errorf("output key must not be empty")
	}
	return validateKeyChars("output key", key)
}

// ValidateOutputValue checks that an outputs value is safe for unquoted YAML
// serialization, using the same rules as extra field values. Leading and
// trailing whitespace is rejected as well: the serializer writes values
// unquoted, so " x " would not round-trip.
func ValidateOutputValue(value string) error {
	if err := validateValueChars("output value", value); err != nil {
		return err
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("output value %q has leading or trailing whitespace", value)
	}
	return nil
}

// ValidateCargoKey checks that key is usable as a dep-cargo key. Keys are dep
// IDs, which may be namespaced ("project/foo-abcd"), so only empty keys and
// whitespace or control characters are rejected.
func ValidateCargoKey(key string) error {
	if key == "" {
		return fmt.Errorf("cargo dep ID must not be empty")
	}
	for _, c := range key {
		if unicode.IsSpace(c) || !unicode.IsPrint(c) {
			return fmt.Errorf("cargo dep ID %q contains invalid character %q", key, string(c))
		}
	}
	return nil
}

// ValidateCargoValue checks that a cargo annotation is storable. Cargo is human
// prose naming what flows across a dep edge, so punctuation the extra/outputs
// rules reject is allowed here — format.go serializes the block through yaml.v3,
// which quotes as needed. Only empty values, non-printable characters (newlines
// included) and surrounding whitespace are rejected.
func ValidateCargoValue(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("cargo must not be empty")
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("cargo %q has leading or trailing whitespace", value)
	}
	for _, c := range value {
		if !unicode.IsPrint(c) {
			return fmt.Errorf("cargo contains non-printable character %q", string(c))
		}
	}
	return nil
}

func validateKeyChars(kind, key string) error {
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("%s %q contains invalid character %q (allowed: letters, digits, hyphens, underscores)", kind, key, string(c))
		}
	}
	return nil
}

func validateValueChars(kind, value string) error {
	for _, c := range value {
		if c < ' ' || c > '~' {
			return fmt.Errorf("%s contains non-printable character %q", kind, string(c))
		}
		switch c {
		case ':', '#', '[', ']', '{', '}', '%', '!', '&', '*', '@', '`', '|', '>', '\'', '"':
			return fmt.Errorf("%s contains invalid character %q", kind, string(c))
		}
	}
	return nil
}

// Well-known outputs keys populated when a ticket lands.
const (
	OutputKeyBranch = "branch"
	OutputKeyCommit = "commit"
)

// PopulateDoneOutputs records the branch and commit a ticket landed on in its
// outputs block. Fill-if-absent: values already recorded (by hand or by an
// earlier commit) are never overwritten, and empty values are skipped. Values
// that would not survive serialization are dropped — this is best-effort
// metadata derived from git, and a branch name like `%wip` must never corrupt
// the ticket file.
func PopulateDoneOutputs(t *Ticket, commit, branch string) {
	set := func(key, value string) {
		if value == "" || t.Outputs[key] != "" {
			return
		}
		if ValidateOutputValue(value) != nil {
			return
		}
		if t.Outputs == nil {
			t.Outputs = map[string]string{}
		}
		t.Outputs[key] = value
	}
	set(OutputKeyCommit, commit)
	set(OutputKeyBranch, branch)
}
