package ticket

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// The observer action protocol: a scheduled observer attaches evidence to a
// ticket, reopens it, or files a discovery, and has to be able to retry after
// a lost response without duplicating the work or overwriting a decision made
// in between. Separate show / add-note / edit calls cannot promise that, so
// the protocol is one write that lands the evidence note, the transition and
// a durable receipt together, judged against a precondition the caller read.
//
// Receipts live in the ticket file itself, in the append-only `actions`
// block, for the same reason the verdict ledger does: the note, the status
// and the receipt then land in one writeTicket — a temp file and a rename —
// so nothing can observe or persist a partial action. There is no side
// journal to fall out of step with the file, and the receipt replicates with
// the ticket through `tk sync` and survives a restart of whatever wrote it.
//
// The writer boundary is one machine: the per-ticket flock and the exclusive
// store lock serialise every tk writer on a host, and every guarantee below
// is stated within it. Between machines the store is exchanged by git
// commits. Two machines applying one action ID to one ticket both append to
// that ticket's block, which surfaces as a merge conflict resolved by hand —
// the same statement ledger.go makes. Two machines discovering one finding
// key each create their own file, and separate files merge cleanly: the
// synced store then holds two tickets under the key, and CreateDiscovery
// refuses the key as ambiguous rather than answering with either. A store
// that cannot lock (an implementation outside this package) is refused
// rather than served by a retry loop, since a retry loop cannot make the
// replay check and the write one operation.

// ErrActionConflict reports an action ID or finding key that was already
// recorded with different content. A replay of the recorded content returns
// the recorded outcome; anything else under the same key is a second, distinct
// action wearing the first one's ID and is refused rather than applied or
// silently answered with the first one's outcome. Match with errors.Is.
var ErrActionConflict = errors.New("action already recorded with different content")

// ErrTransitionNotPermitted reports a transition outside the permitted set, or
// one refused on the ticket it was asked of. Match with errors.Is.
var ErrTransitionNotPermitted = errors.New("transition not permitted")

// errDamagedActions marks a parse failure that is the actions block: the
// receipts on disk are not what a writer recorded. It names the block in the
// refusal a reader reports, and in updateLocked's, so the repair is pointed
// at the right lines.
var errDamagedActions = errors.New("action receipts cannot be read")

// errActionReplayed is how the mutation callback tells ApplyAction that the
// action was already applied and nothing is to be written: Mutate writes on a
// nil return, so the non-write has to travel as an error. Never surfaced.
var errActionReplayed = errors.New("action replayed")

// Transition is the status change an action may carry. The permitted set is
// deliberately narrow: an observer reopens work it found regressed, and that
// is the one transition the protocol names. Everything else is an edit a
// human or an orchestrator makes through ticket_edit.
type Transition string

const (
	TransitionNone   Transition = ""
	TransitionReopen Transition = "reopen"
)

// ReceiptKind says what a receipt records: an action applied to an existing
// ticket, or the discovery that created the ticket holding it.
type ReceiptKind string

const (
	ReceiptApply    ReceiptKind = "apply"
	ReceiptDiscover ReceiptKind = "discover"
)

var validReceiptKinds = map[ReceiptKind]bool{
	ReceiptApply:    true,
	ReceiptDiscover: true,
}

// ActionReceipt is one recorded action. Rows are appended and never edited;
// updateLocked refuses a write that would drop or rewrite one.
//
// ID is the caller's action ID for an apply, or the finding key for a
// discovery. Digest is the sha256 of the action's canonical payload, which is
// what a replay is judged by: the same ID with an equal digest is the same
// action, and with a different one is a conflict. Source is the declared
// writer, the value the mutation log records for the same write — a label a
// cooperating tool attaches to attribute its writes, not an authenticated
// identity. Outcome is what the action did: the status the ticket held after
// an apply, or the ID the discovery created. At is a string for the reason
// VerdictRow.At is.
type ActionReceipt struct {
	ID      string      `yaml:"id" json:"id"`
	Kind    ReceiptKind `yaml:"kind" json:"kind"`
	Digest  string      `yaml:"digest" json:"digest"`
	Source  string      `yaml:"source" json:"source"`
	At      string      `yaml:"at" json:"at"`
	Outcome string      `yaml:"outcome" json:"outcome"`
}

// ActionRequest is one action against an existing ticket.
//
// Precondition is the opaque value Ticket.Precondition returned for the state
// the caller read and decided on, compared exactly against the stored ticket
// under the lock; a mismatch is ErrConflict and nothing is written. Empty
// means the caller makes no claim about the state it read, and the action
// lands on whatever the ticket holds. Note is required. ActionID is the
// caller's stable identity for this action: the same ID sent again with the
// same Note and Transition returns the recorded outcome without a second note
// or transition, whatever the precondition says by then.
type ActionRequest struct {
	ID           string
	ActionID     string
	Precondition string
	Note         string
	Transition   Transition
}

// ActionResult is what an action produced, or what it produced the first time
// when Replayed is set: the ticket as the store holds it after the action and
// the receipt recorded for it.
type ActionResult struct {
	Ticket   *Ticket
	Receipt  ActionReceipt
	Replayed bool
}

// DiscoveryRequest is a discovery: a ticket to create under a finding key that
// is stable across retries. The key is scoped to the project the ticket lands
// in; the same key in another project is a different finding.
type DiscoveryRequest struct {
	Ticket     *Ticket
	FindingKey string
}

// Precondition is the opaque token a write can be held against: it identifies
// the state the ticket was read at, and ApplyAction refuses an action whose
// precondition no longer matches the store. It is compared exactly and carries
// no other meaning — a caller does not derive anything from it. Empty on a
// ticket the caller built rather than read.
//
// It covers the ticket's own file. An epic's status is derived from its
// children at read time and is not in the file, so a child reopening moves the
// epic's status without moving its token: an action held against an epic's
// precondition is a claim about the epic's file, not about what its children
// have done since.
func (t *Ticket) Precondition() string {
	return t.version
}

// applyPayload is the content of an apply action, in the fixed field order the
// digest is computed over.
type applyPayload struct {
	Note       string     `json:"note"`
	Transition Transition `json:"transition"`
}

// discoverPayload is the content of a discovery, in the fixed field order the
// digest is computed over. The body is what ticket_create builds from a
// description, a design and acceptance criteria; a parent is digested as the
// caller spelled it, before the write canonicalizes it.
type discoverPayload struct {
	Title    string     `json:"title"`
	Body     string     `json:"body"`
	Type     TicketType `json:"type"`
	Priority int        `json:"priority"`
	Parent   string     `json:"parent"`
	Tags     []string   `json:"tags"`
}

// actionDigest is the sha256 of v's JSON encoding. Structs with fixed field
// order and no maps, so the digest is the same in every process that computes
// it. It is exact-content deduplication: a rephrased note or a retitled
// finding is different content, and finding that a new discovery is the same
// problem as an existing ticket stays the caller's job.
func actionDigest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		// The payload structs hold strings, an int and a string slice, none of
		// which json.Marshal can fail on.
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ValidateActionReceipt checks a whole receipt, including the fields the
// protocol stamps itself. Every field is required on write, which is what
// lets the parser tell a decode loss from a receipt a writer wrote.
func ValidateActionReceipt(r ActionReceipt) error {
	if err := validateLedgerText("action", "id", r.ID); err != nil {
		return err
	}
	if !validReceiptKinds[r.Kind] {
		return fmt.Errorf("invalid action kind %q: must be one of apply, discover", r.Kind)
	}
	if len(r.Digest) != 64 {
		return fmt.Errorf("action digest %q is not a sha256 hex digest", r.Digest)
	}
	for _, c := range r.Digest {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("action digest %q is not a sha256 hex digest", r.Digest)
		}
	}
	if err := validateLedgerText("action", "source", r.Source); err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339, r.At); err != nil {
		return fmt.Errorf("action timestamp %q is not RFC3339: %w", r.At, err)
	}
	return validateLedgerText("action", "outcome", r.Outcome)
}

// actionsAppendOnly reports whether next preserves every receipt prior held,
// in order and unchanged. ActionReceipt is all strings, so equality is the
// whole comparison.
func actionsAppendOnly(prior, next []ActionReceipt) bool {
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

// findReceipt is the receipt of the given kind recorded under id, if any.
func findReceipt(receipts []ActionReceipt, kind ReceiptKind, id string) (ActionReceipt, bool) {
	for _, r := range receipts {
		if r.Kind == kind && r.ID == id {
			return r, true
		}
	}
	return ActionReceipt{}, false
}

// declaredSource is who a store's writes are attributed to: the same answer
// the mutation log gives, so a receipt's source and the log line for the write
// that recorded it name one writer.
func declaredSource(store Store) string {
	switch s := store.(type) {
	case *FileStore:
		return s.mutationSource()
	case *MultiStore:
		return attributedSource(s.Source)
	}
	return attributedSource("")
}

// errNoAtomicStore is the fail-closed refusal for a store outside this
// package: it has no lock the protocol can hold across the replay check and
// the write, and a retry loop cannot make the two one operation.
func errNoAtomicStore(what string, store Store) error {
	return fmt.Errorf("%s: store %T cannot hold a lock across the replay check and the write, so it cannot provide the protocol's atomicity guarantee; use a FileStore or a MultiStore", what, store)
}

// ApplyAction applies one action to an existing ticket — the evidence note,
// the optional transition and the receipt, in one write — or answers a
// replay with the recorded outcome. Under the ticket's lock, in order:
//
//  1. Replay. A receipt under ActionID already exists: with an equal digest
//     the recorded receipt and the ticket as stored come back with Replayed
//     set and nothing is written — the precondition is not re-checked, since
//     the recorded outcome is the answer whatever has happened since; with a
//     different digest the call is refused with ErrActionConflict and nothing
//     is written.
//  2. Precondition. A non-empty Precondition that differs from the stored
//     ticket's is refused with ErrConflict and nothing is written.
//  3. Apply. The note is appended, the transition applied and the receipt
//     appended, and the ticket written once.
//
// Every argument that can be judged without the store is judged first, so a
// refused request costs nothing on disk. The store must be a FileStore or a
// MultiStore; any other Store is refused rather than served by Mutate's
// retry fallback, which cannot make the replay check and the write atomic.
func ApplyAction(store Store, req ActionRequest) (*ActionResult, error) {
	m, ok := store.(mutator)
	if !ok {
		return nil, errNoAtomicStore("apply action", store)
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, fmt.Errorf("apply action: ticket id is required")
	}
	if err := validateLedgerText("action", "id", req.ActionID); err != nil {
		return nil, fmt.Errorf("apply action: %w", err)
	}
	if strings.TrimSpace(req.Note) == "" {
		return nil, fmt.Errorf("apply action %q: note is required", req.ActionID)
	}
	switch req.Transition {
	case TransitionNone, TransitionReopen:
	default:
		return nil, fmt.Errorf("%w: %q is not one of the permitted transitions (reopen)", ErrTransitionNotPermitted, req.Transition)
	}
	digest := actionDigest(applyPayload{Note: req.Note, Transition: req.Transition})
	source := declaredSource(store)

	var result ActionResult
	t, err := m.mutate(req.ID, func(t *Ticket) error {
		if r, found := findReceipt(t.Actions, ReceiptApply, req.ActionID); found {
			if r.Digest != digest {
				return fmt.Errorf("%w: action %q on %s was recorded with other content; a different action needs its own id", ErrActionConflict, req.ActionID, t.ID)
			}
			// A copy: under MultiStore the namespaced ID is put back to bare
			// once this callback returns, and the caller is owed the ID Get
			// would hand it.
			replayed := *t
			result = ActionResult{Ticket: &replayed, Receipt: r, Replayed: true}
			return errActionReplayed
		}
		if req.Precondition != "" && req.Precondition != t.version {
			return fmt.Errorf("apply action %q to %s: %w. Re-read it and decide the action again", req.ActionID, t.ID, ErrConflict)
		}
		if req.Transition == TransitionReopen && t.Type == TypeEpic {
			return fmt.Errorf("%w: reopen on epic %s — an epic's status is derived from its children; reopen the child that regressed", ErrTransitionNotPermitted, t.ID)
		}
		now := time.Now().UTC()
		t.Notes = append(t.Notes, Note{Timestamp: now, Text: req.Note})
		if req.Transition == TransitionReopen {
			t.Status = StatusOpen
		}
		receipt := ActionReceipt{
			ID:      req.ActionID,
			Kind:    ReceiptApply,
			Digest:  digest,
			Source:  source,
			At:      now.Format(time.RFC3339),
			Outcome: string(t.Status),
		}
		if err := ValidateActionReceipt(receipt); err != nil {
			return err
		}
		t.Actions = append(t.Actions, receipt)
		result.Receipt = receipt
		return nil
	})
	if errors.Is(err, errActionReplayed) {
		return &result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Ticket = t
	return &result, nil
}

// discoverer is the store side of CreateDiscovery: the replay scan and the
// create under one hold of the exclusive store lock. Unexported like mutator,
// so no Store outside this package is forced to grow one — and none can
// satisfy it by accident, which is the fail-closed half.
type discoverer interface {
	createDiscovery(t *Ticket, key, digest, source string) (*ActionResult, error)
}

var (
	_ discoverer = (*FileStore)(nil)
	_ discoverer = (*MultiStore)(nil)
)

// CreateDiscovery creates a ticket under a finding key, or answers a retry
// with the ticket the key already created. Under the exclusive store lock the
// namespace the ticket lands in is scanned for a discovery receipt under the
// key: found on one ticket with an equal digest, that ticket comes back with
// Replayed set and nothing is created; found with a different digest, the
// call is refused with ErrActionConflict naming the ticket and nothing is
// created; found on more than one ticket — what two machines discovering the
// key separately leave behind once their files sync, since separate files
// merge without conflict — the call is refused with ErrActionConflict naming
// every claimant, and nothing is created; not found, the receipt is appended
// to the new ticket and it is created. Two concurrent calls under one key on
// one machine therefore yield one ticket, and both callers get its ID.
//
// The digest is exact content — title, body, type, priority, parent and tags
// as the request carries them. It deduplicates retries of one finding, not
// findings that describe one problem differently: a semantic duplicate search
// stays the caller's job. The namespace is refused while its listing is not
// complete — an unreadable file there could be the ticket holding the key,
// so absence cannot be proved.
func CreateDiscovery(store Store, req DiscoveryRequest) (*ActionResult, error) {
	d, ok := store.(discoverer)
	if !ok {
		return nil, errNoAtomicStore("create discovery", store)
	}
	if req.Ticket == nil {
		return nil, fmt.Errorf("create discovery: ticket is required")
	}
	if err := validateLedgerText("action", "finding key", req.FindingKey); err != nil {
		return nil, fmt.Errorf("create discovery: %w", err)
	}
	t := req.Ticket
	tags := t.Tags
	if len(tags) == 0 {
		tags = nil
	}
	digest := actionDigest(discoverPayload{
		Title:    t.Title,
		Body:     strings.TrimSpace(t.Body),
		Type:     t.Type,
		Priority: t.Priority,
		Parent:   t.Parent,
		Tags:     tags,
	})
	return d.createDiscovery(t, req.FindingKey, digest, declaredSource(store))
}

// createDiscovery is CreateDiscovery's body for one project store, with the
// boundary held across the scan and the create.
func (s *FileStore) createDiscovery(t *Ticket, key, digest, source string) (*ActionResult, error) {
	if err := s.guardWrite(); err != nil {
		return nil, fmt.Errorf("create discovery: %w", err)
	}
	var result *ActionResult
	err := centralFor(s).write(s.Project, func(o *op) error {
		for _, skip := range o.snap.Skips {
			if skip.Project == s.Project && skip.Kind.DegradesEpicStatus() {
				return o.incompleteError(fmt.Sprintf("create discovery: proving that finding key %q has no ticket in %s", key, s.projectLabel()))
			}
		}
		// Every claimant is collected before any is answered: two machines
		// that each created a discovery under the key hold it in separate
		// files, which git merges cleanly, and once synced the namespace has
		// two tickets under one key. Picking either would answer a replay
		// with a ticket that may not carry the caller's content, so the
		// ambiguity is refused for an operator to resolve.
		var claimants []string
		var claimed *Ticket
		var recorded ActionReceipt
		for _, existing := range o.snap.Tickets {
			if namespaceOf(existing.ID) != s.Project {
				continue
			}
			r, found := findReceipt(existing.Actions, ReceiptDiscover, key)
			if !found {
				continue
			}
			// The snapshot's copy carries the qualified ID; this store hands
			// back bare ones, as Get does.
			_, bare := ParseNamespacedID(existing.ID)
			claimants = append(claimants, bare)
			claimed, recorded = existing, r
		}
		if len(claimants) > 1 {
			return fmt.Errorf("%w: finding key %q is claimed by more than one ticket (%s), which is what two machines discovering it separately look like once synced; keep one and remove the receipt from the others by hand", ErrActionConflict, key, strings.Join(claimants, ", "))
		}
		if claimed != nil {
			replayed := *claimed
			replayed.ID = claimants[0]
			if recorded.Digest != digest {
				return fmt.Errorf("%w: finding key %q already created %s with other content; a different finding needs its own key", ErrActionConflict, key, replayed.ID)
			}
			result = &ActionResult{Ticket: &replayed, Receipt: recorded, Replayed: true}
			return nil
		}
		// The receipt names the ID the file is created under, so the ID has to
		// be settled before the write rather than left to create's collision
		// retry. Every tk create holds the exclusive store lock this call holds,
		// so an ID free here is free when createLocked writes it.
		if err := s.EnsureDir(); err != nil {
			return err
		}
		const maxRetries = 5
		settled := false
		for i := 0; i < maxRetries && !settled; i++ {
			path, err := s.ticketFile(t.ID)
			if err != nil {
				return fmt.Errorf("create discovery: %w", err)
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				settled = true
				break
			}
			t.ID = GenerateID(t.Title)
		}
		if !settled {
			return fmt.Errorf("ticket ID collision after %d attempts", maxRetries)
		}
		receipt := ActionReceipt{
			ID:      key,
			Kind:    ReceiptDiscover,
			Digest:  digest,
			Source:  source,
			At:      time.Now().UTC().Format(time.RFC3339),
			Outcome: t.ID,
		}
		if err := ValidateActionReceipt(receipt); err != nil {
			return fmt.Errorf("create discovery: %w", err)
		}
		t.Actions = append(t.Actions, receipt)
		if err := o.create(s, t); err != nil {
			t.Actions = t.Actions[:len(t.Actions)-1]
			return err
		}
		result = &ActionResult{Ticket: t, Receipt: receipt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// projectLabel names the store in a message: its project, or its directory
// for a store with none.
func (s *FileStore) projectLabel() string {
	if s.Project != "" {
		return "project " + s.Project
	}
	return s.Dir
}

// createDiscovery routes to the project store the ticket's namespaced ID
// names, on the same terms as Create, and hands the ID back namespaced.
func (m *MultiStore) createDiscovery(t *Ticket, key, digest, source string) (*ActionResult, error) {
	proj, ticketID := ParseNamespacedID(t.ID)
	if proj == "" {
		return nil, fmt.Errorf("project is required for MultiStore discovery — use project/ticket-id format")
	}
	store, err := m.createStore(proj)
	if err != nil {
		return nil, err
	}
	t.ID = ticketID
	result, err := store.createDiscovery(t, key, digest, source)
	if err != nil {
		t.ID = FormatNamespacedID(proj, t.ID)
		return nil, fmt.Errorf("project %s: %w", proj, err)
	}
	// On a create the result is the caller's ticket and is prefixed once; on
	// a replay it is the stored one, and the caller's is put back as given.
	if result.Ticket != t {
		t.ID = FormatNamespacedID(proj, t.ID)
	}
	result.Ticket.ID = FormatNamespacedID(proj, result.Ticket.ID)
	return result, nil
}
