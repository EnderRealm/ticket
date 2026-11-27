package ticket

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Claims: a lease saying who is working a ticket right now. A status cannot
// say it — `open` reads the same whether its writer set it a minute ago or
// exited yesterday — and a pid cannot either, since the store is exchanged
// between machines by git. An expiry can: a claim is live while now is before
// it, and one whose owner died stops counting by itself.
//
// Advisory. Nothing else is refused on a claimed ticket; ready and frontier
// leave it out, and a write that leaves the ticket done or closed drops it
// (writeTicket).

// ClaimField is the frontmatter key a claim is stored under, and the JSON key
// readers are served it under.
//
// One scalar extra, `claim: <owner> <machine> <claimed-at> <expires>`, for the
// reason an attachment is one: a tk released before claims existed keeps an
// unknown key as the text of a scalar and writes it back unchanged, where a
// mapping would come back as Go's print of it. The value has to be plain YAML,
// since before v8.6.0 that text was written back unquoted.
const ClaimField = "claim"

// DefaultClaimTTL is the lease a claim takes when the caller names none.
const DefaultClaimTTL = 2 * time.Hour

// Claim is who holds a ticket, on which machine, since when and until when.
// Machine is the host that took the claim, as os.Hostname named it. Times are
// UTC, to the second.
type Claim struct {
	Owner     string    `json:"owner"`
	Machine   string    `json:"machine"`
	ClaimedAt time.Time `json:"claimed_at"`
	Expires   time.Time `json:"expires"`
}

// LiveAt reports whether the claim still holds at now.
func (c Claim) LiveAt(now time.Time) bool {
	return now.Before(c.Expires)
}

func (c Claim) storedValue() string {
	return strings.Join([]string{c.Owner, c.Machine, c.ClaimedAt.UTC().Format(time.RFC3339), c.Expires.UTC().Format(time.RFC3339)}, " ")
}

func (c Claim) validate() error {
	if err := validateClaimLabel("owner", c.Owner); err != nil {
		return err
	}
	if err := validateClaimLabel("machine", c.Machine); err != nil {
		return err
	}
	if !c.Expires.After(c.ClaimedAt) {
		return fmt.Errorf("claim expires %s, not after it was claimed at %s", c.Expires.Format(time.RFC3339), c.ClaimedAt.Format(time.RFC3339))
	}
	if v := c.storedValue(); yamlScalar(v) != v {
		return fmt.Errorf("claim %q does not read back as plain YAML", v)
	}
	return nil
}

// validateClaimLabel keeps an owner or machine one space-free token the stored
// value can be split on.
func validateClaimLabel(what, v string) error {
	if v == "" {
		return fmt.Errorf("claim %s must not be empty", what)
	}
	for i, c := range v {
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alnum && (i == 0 || !strings.ContainsRune("._-:@/", c)) {
			return fmt.Errorf("invalid claim %s %q: must be letters, digits, '.', '_', '-', ':', '@' and '/', starting with a letter or digit", what, v)
		}
	}
	return nil
}

// ClaimOf returns t's claim, live or not, or nil when it has none. A stored
// value that is not a valid claim, which only a hand edit leaves, is none.
func ClaimOf(t *Ticket) *Claim {
	parts := strings.Split(t.Extra[ClaimField], " ")
	if len(parts) != 4 {
		return nil
	}
	claimedAt, err := time.Parse(time.RFC3339, parts[2])
	if err != nil {
		return nil
	}
	expires, err := time.Parse(time.RFC3339, parts[3])
	if err != nil {
		return nil
	}
	c := Claim{Owner: parts[0], Machine: parts[1], ClaimedAt: claimedAt.UTC(), Expires: expires.UTC()}
	if c.validate() != nil {
		return nil
	}
	return &c
}

// ClaimReport is a claim as readers are served it: with whether it is live at
// the read.
type ClaimReport struct {
	Claim
	Live bool `json:"live"`
}

// ReportClaim returns t's claim judged at now, or nil when it has none.
func ReportClaim(t *Ticket, now time.Time) *ClaimReport {
	c := ClaimOf(t)
	if c == nil {
		return nil
	}
	return &ClaimReport{Claim: *c, Live: c.LiveAt(now)}
}

// claimedNow reports whether t carries a claim that is live now.
func claimedNow(t *Ticket) bool {
	c := ClaimOf(t)
	return c != nil && c.LiveAt(time.Now())
}

// ClaimConflictPrefix opens every claim refusal, so a caller can tell one from
// any other failure by its text.
const ClaimConflictPrefix = "claim conflict:"

// ClaimConflictError is a claim or a release refused because another owner
// holds a live claim on the ticket. Its message opens with ClaimConflictPrefix
// and names the holder, its machine and its expiry. Match with errors.As.
type ClaimConflictError struct {
	ID   string
	Held Claim
}

func (e *ClaimConflictError) Error() string {
	return fmt.Sprintf("%s %s is claimed by %s on %s until %s", ClaimConflictPrefix, e.ID, e.Held.Owner, e.Held.Machine, e.Held.Expires.Format(time.RFC3339))
}

// errClaimUnchanged is how the release callback tells ReleaseTicket there is
// nothing to write: Mutate writes on a nil return. Never surfaced.
var errClaimUnchanged = errors.New("claim unchanged")

// ClaimResult is what a claim did: the ticket as written, the claim it now
// holds, and the claim that one replaced — an expired one, or a live one of
// another owner's taken over — or nil when there was none or the owner renewed
// its own.
type ClaimResult struct {
	Ticket   *Ticket
	Claim    Claim
	Replaced *Claim
}

// ClaimTicket claims id for owner until ttl from now. The owner's own live
// claim is renewed, keeping when it was claimed. Another owner's live claim
// refuses the claim with a *ClaimConflictError and leaves the ticket
// unchanged, unless takeover, which replaces it. An expired claim is replaced
// whoever held it.
//
// The decision is taken against the ticket as read under its lock, in the same
// read-modify-write as the write (Mutate), so two claims racing on one machine
// cannot both succeed.
func ClaimTicket(store Store, id, owner string, ttl time.Duration, takeover bool) (*ClaimResult, error) {
	if err := validateClaimLabel("owner", owner); err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	if ttl < time.Second {
		return nil, fmt.Errorf("claim: ttl %s must be at least 1s", ttl)
	}
	machine, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("claim: naming the machine that holds it: %w", err)
	}
	if err := validateClaimLabel("machine", machine); err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}

	var result ClaimResult
	t, err := Mutate(store, id, func(t *Ticket) error {
		now := time.Now()
		at := now.UTC().Truncate(time.Second)
		c := Claim{Owner: owner, Machine: machine, ClaimedAt: at, Expires: at.Add(ttl)}
		held := ClaimOf(t)
		switch {
		case held == nil:
		case held.LiveAt(now) && held.Owner == owner:
			c.ClaimedAt = held.ClaimedAt
		case held.LiveAt(now) && !takeover:
			return &ClaimConflictError{ID: t.ID, Held: *held}
		default:
			result.Replaced = held
		}
		if err := c.validate(); err != nil {
			return err
		}
		if t.Extra == nil {
			t.Extra = map[string]string{}
		}
		t.Extra[ClaimField] = c.storedValue()
		result.Claim = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.Ticket = t
	return &result, nil
}

// ReleaseResult is what a release did: the ticket as it stands, and the claim
// released, or nil when the release found nothing of the owner's to clear.
type ReleaseResult struct {
	Ticket   *Ticket
	Released *Claim
}

// ReleaseTicket clears owner's claim on id, live or expired. Another owner's
// live claim refuses the release with a *ClaimConflictError. No claim, or
// another owner's expired one, is nothing to release: the ticket is left as it
// is, unwritten, and Released is nil.
func ReleaseTicket(store Store, id, owner string) (*ReleaseResult, error) {
	if err := validateClaimLabel("owner", owner); err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}
	var result ReleaseResult
	t, err := Mutate(store, id, func(t *Ticket) error {
		held := ClaimOf(t)
		switch {
		case held != nil && held.Owner == owner:
			delete(t.Extra, ClaimField)
			result.Released = held
			return nil
		case held != nil && held.LiveAt(time.Now()):
			return &ClaimConflictError{ID: t.ID, Held: *held}
		}
		// A copy: under MultiStore the namespaced ID is put back to bare once
		// this callback returns, and the caller is owed the ID Get would hand it.
		unchanged := *t
		result.Ticket = &unchanged
		return errClaimUnchanged
	})
	if errors.Is(err, errClaimUnchanged) {
		return &result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Ticket = t
	return &result, nil
}
