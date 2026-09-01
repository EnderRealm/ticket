package ticket

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// The verdict ledger: an append-only record of the review verdicts rendered on
// a ticket, one row per verdict, keyed by the ticket and the commit SHA the
// verdict was rendered against. A verdict written as prose in a note is keyed to
// nothing, so nothing can tell that it predates the head it is being read
// against; a row carries that key, so a reader can say whether the verdict is
// about the code in front of it.
//
// Appends go through Mutate, which holds the per-ticket flock across the
// read-modify-write, so two writers on one machine serialize the way note
// appends do. Between machines the store is exchanged by git commits rather
// than by concurrent access to one directory, so two machines appending to one
// ticket surface as a merge conflict in the verdicts block, resolved by hand.

// VerdictClass names what a verdict actually established.
type VerdictClass string

const (
	VerdictLiveVerified    VerdictClass = "live-verified"
	VerdictTestVerified    VerdictClass = "test-verified"
	VerdictTypeCheckOnly   VerdictClass = "type-check-only"
	VerdictVerifierBlocked VerdictClass = "verifier-blocked"
	VerdictVerifierFailed  VerdictClass = "verifier-failed"
)

var validVerdictClasses = map[VerdictClass]bool{
	VerdictLiveVerified:    true,
	VerdictTestVerified:    true,
	VerdictTypeCheckOnly:   true,
	VerdictVerifierBlocked: true,
	VerdictVerifierFailed:  true,
}

// ValidateVerdictClass returns an error if c is not a recognized verdict class.
func ValidateVerdictClass(c VerdictClass) error {
	if validVerdictClasses[c] {
		return nil
	}
	return fmt.Errorf("invalid verdict class %q: must be one of live-verified, test-verified, type-check-only, verifier-blocked, verifier-failed", c)
}

// VerdictRole says whether a row is a self-report or an independent check.
type VerdictRole string

const (
	VerdictRoleWorker   VerdictRole = "worker"
	VerdictRoleVerifier VerdictRole = "verifier"
)

var validVerdictRoles = map[VerdictRole]bool{
	VerdictRoleWorker:   true,
	VerdictRoleVerifier: true,
}

// ValidateVerdictRole returns an error if r is not a recognized verdict role.
func ValidateVerdictRole(r VerdictRole) error {
	if validVerdictRoles[r] {
		return nil
	}
	return fmt.Errorf("invalid verdict role %q: must be one of worker, verifier", r)
}

// Passes reports whether the class is evidence the work was exercised. It is
// the one place the pass rule lives, so no query invents its own: only
// live-verified and test-verified say something ran and agreed.
// verifier-blocked is a verifier that could not run at all, and reading it as a
// pass is the failure this ledger exists to prevent; type-check-only is
// deliberately not proof either — it says the code compiles, not that it works.
func (c VerdictClass) Passes() bool {
	return c == VerdictLiveVerified || c == VerdictTestVerified
}

// VerdictRow is one recorded verdict. Rows are appended and never edited: a
// correction is a new row at the same key, and updateLocked refuses a write
// that would drop or rewrite one.
//
// At is a string rather than a time.Time for the reason format.go documents on
// the ticket's own dates: a time.Time field implements TextUnmarshaler, so
// yaml.v3 fails the whole frontmatter decode on one malformed value instead of
// recording a per-field type error. A verdict row arrives over the shared
// store's git remote like any other frontmatter, and a corrupt timestamp must
// cost that row its ordering at worst, never cost the store the ticket.
type VerdictRow struct {
	// Ticket is the ID the row was recorded against, as the recorder addressed
	// it — namespaced when the write came through `tk serve`.
	Ticket   string       `yaml:"ticket" json:"ticket"`
	SHA      string       `yaml:"sha" json:"sha"`
	Class    VerdictClass `yaml:"class" json:"class"`
	Role     VerdictRole  `yaml:"role" json:"role"`
	Evidence string       `yaml:"evidence" json:"evidence"`
	By       string       `yaml:"by" json:"by"`
	At       string       `yaml:"at" json:"at"`
}

// ValidateVerdictSHA checks that sha is a full commit hash and returns it
// normalized to lower case. Staleness is decided by exact string equality
// against the head a reader supplies, so an abbreviated SHA would read as stale
// against the very head it abbreviates; requiring the full hash — 40 hex
// characters for sha1, 64 for sha256 — makes that comparison decidable.
func ValidateVerdictSHA(sha string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(sha))
	if len(s) != 40 && len(s) != 64 {
		return "", fmt.Errorf("invalid commit sha %q: must be the full 40- or 64-character hash, not an abbreviation", sha)
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", fmt.Errorf("invalid commit sha %q: contains non-hex character %q", sha, string(c))
		}
	}
	return s, nil
}

// validateVerdictText checks that an evidence pointer or a verifier identity is
// storable, on the same rules as a dep cargo annotation: the block is
// serialized through yaml.v3, which quotes what needs it, so punctuation is
// fine and only empty values, non-printable characters and surrounding
// whitespace are rejected.
func validateVerdictText(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("verdict %s must not be empty", kind)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("verdict %s %q has leading or trailing whitespace", kind, value)
	}
	for _, c := range value {
		if !unicode.IsPrint(c) {
			return fmt.Errorf("verdict %s contains non-printable character %q", kind, string(c))
		}
	}
	return nil
}

// ValidateVerdictRow checks a whole row, including the fields RecordVerdict
// stamps itself. The SHA must already be normalized: a row is looked up by
// exact equality, so one that would still change under ValidateVerdictSHA is
// refused rather than silently rewritten after the fact.
func ValidateVerdictRow(r VerdictRow) error {
	if r.Ticket == "" {
		return fmt.Errorf("verdict row must name a ticket")
	}
	if err := ValidateVerdictClass(r.Class); err != nil {
		return err
	}
	if err := ValidateVerdictRole(r.Role); err != nil {
		return err
	}
	normalized, err := ValidateVerdictSHA(r.SHA)
	if err != nil {
		return err
	}
	if normalized != r.SHA {
		return fmt.Errorf("verdict sha %q is not normalized", r.SHA)
	}
	if err := validateVerdictText("evidence", r.Evidence); err != nil {
		return err
	}
	if err := validateVerdictText("identity", r.By); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, r.At); err != nil {
		return fmt.Errorf("verdict timestamp %q is not RFC3339: %w", r.At, err)
	}
	return nil
}

// RecordVerdict appends a verdict row to a ticket. Every argument is validated
// before the store is touched, so a rejected class costs nothing on disk; the
// timestamp is stamped here rather than taken from the caller, since a row a
// recorder could backdate says nothing about when the verdict was rendered.
//
// The append goes through Mutate, which holds the ticket's lock across the read
// and the write, so a row lands on top of whatever rows the ticket already
// holds. Under MultiStore, Mutate hands fn the namespaced ID, so a row recorded
// through `tk serve` carries the namespace — the ID the recorder addressed is
// the one the row keeps.
func RecordVerdict(store Store, id, sha string, class VerdictClass, role VerdictRole, evidence, by string) (*Ticket, VerdictRow, error) {
	if err := ValidateVerdictClass(class); err != nil {
		return nil, VerdictRow{}, err
	}
	if err := ValidateVerdictRole(role); err != nil {
		return nil, VerdictRow{}, err
	}
	normalized, err := ValidateVerdictSHA(sha)
	if err != nil {
		return nil, VerdictRow{}, err
	}
	// Trimmed rather than refused: evidence arrives as free text from a tool
	// call, and a trailing newline on it would cost the recorder a round trip for
	// a value the store is happy to hold. Whitespace is all that is forgiven —
	// what trims to nothing is still empty. ValidateVerdictRow stays strict, so a
	// row built anywhere else is judged as it stands.
	evidence = strings.TrimSpace(evidence)
	by = strings.TrimSpace(by)
	if err := validateVerdictText("evidence", evidence); err != nil {
		return nil, VerdictRow{}, err
	}
	if err := validateVerdictText("identity", by); err != nil {
		return nil, VerdictRow{}, err
	}

	var row VerdictRow
	t, err := Mutate(store, id, func(t *Ticket) error {
		row = VerdictRow{
			Ticket:   t.ID,
			SHA:      normalized,
			Class:    class,
			Role:     role,
			Evidence: evidence,
			By:       by,
			At:       time.Now().UTC().Format(time.RFC3339),
		}
		if err := ValidateVerdictRow(row); err != nil {
			return err
		}
		t.Verdicts = append(t.Verdicts, row)
		return nil
	})
	if err != nil {
		return nil, VerdictRow{}, err
	}
	return t, row, nil
}

// CurrentVerdict picks the operative verdict at head out of a ticket's rows,
// and reports the rest as stale. A row recorded against another SHA is a
// verdict about other code: it is returned as stale and never as the current
// verdict, which is what keeps a verdict from outliving the commit it judged.
// head is validated rather than compared as given, since an abbreviation would
// match nothing and read as "no verdict" instead of as the caller's mistake.
//
// Among the rows at head, a verifier row supersedes every worker row — a worker
// may self-report, and an independent check overrides that self-report on the
// same key. Within one role the last row in slice order wins: rows are
// append-only, so slice order is record order, and a correction is a new row
// whose value is the operative one. Deliberately not decided from At, which is
// untrusted text off a synced file, where slice order is the append order the
// store itself guarantees.
//
// The returned pointer points into rows; a caller that mutates through it is
// mutating the ticket's own ledger.
func CurrentVerdict(rows []VerdictRow, head string) (*VerdictRow, []VerdictRow, error) {
	normalized, err := ValidateVerdictSHA(head)
	if err != nil {
		return nil, nil, err
	}
	var current *VerdictRow
	var stale []VerdictRow
	for i := range rows {
		if rows[i].SHA != normalized {
			stale = append(stale, rows[i])
			continue
		}
		// A held verifier row is only replaced by another verifier row. Any other
		// role — worker, or one a file carries that tk never wrote — is
		// last-one-wins among itself.
		if current != nil && current.Role == VerdictRoleVerifier && rows[i].Role != VerdictRoleVerifier {
			continue
		}
		current = &rows[i]
	}
	return current, stale, nil
}

// verdictsAppendOnly reports whether next preserves every row prior held, in
// order and unchanged. VerdictRow is all strings, so equality is the whole
// comparison.
func verdictsAppendOnly(prior, next []VerdictRow) bool {
	if len(next) < len(prior) {
		return false
	}
	for i := range prior {
		if prior[i] != next[i] {
			return false
		}
	}
	return true
}
