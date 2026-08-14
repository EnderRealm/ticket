package ticket

import (
	"fmt"
	"strings"
	"time"
)

// DeriveEpicStatus computes an epic's status from its children plus the one
// thing about an epic a human still asserts: the intent to abandon it, carried
// by the epic's own `abandoned` flag.
//
//	abandoned and every child terminal        -> closed
//	no children                               -> backlog
//	any child open                            -> open
//	every child terminal, at least one closed -> closed
//	every child done                          -> done
//	otherwise                                 -> backlog
//
// The terminal bucket splits because done and closed say different things:
// done means the work finished, closed means it did not. An epic whose children
// were every one abandoned, or moved to another repo, has not completed, and
// reading it as done would overstate what happened.
//
// ready never appears: it means "available to pick up" and an epic is not
// picked up directly. A childless epic derives backlog, not done — derivation
// runs on every read rather than on a child's write, so treating "no children"
// as trivially all-terminal would read every freshly created epic as done.
//
// The epic's own status field is not an input. It is what every reader was
// shown and therefore what every read-modify-write carries back, so consulting
// it would let an unrelated edit persist a value nobody chose; the intent lives
// in a field a writer round-trips unchanged instead.
//
// The abandon intent is honoured, not cleared: it applies only while every
// child is terminal, so reopening a child un-closes the epic and it derives
// from its children again. Written as a condition on the derivation rather than
// as a write that clears a flag, no write path can leave a closed epic holding
// a non-terminal child.
//
// Abandoning closes every non-terminal child, so an epic that had live work in
// it derives closed from those children alone. The flag is what the two cases
// with nothing to close need: a childless epic, and one whose children had all
// finished before it was abandoned.
//
// This is the undegraded rule, over the children it is handed. It is not what a
// reader sees: every reader gets an epic's status through the store, which
// applies one further condition this function cannot know about — a store
// holding a file it could not read has no business claiming every child is
// terminal, so a derived done or closed demotes to backlog there
// (derivedEpicStatus). Call this to ask what a set of children implies; read the
// store to ask what an epic says.
func DeriveEpicStatus(abandoned bool, children []*Ticket) Status {
	allTerminal := true
	anyOpen := false
	anyClosed := false
	for _, c := range children {
		if !isTerminal(c) {
			allTerminal = false
		}
		if c.Status == StatusOpen {
			anyOpen = true
		}
		if c.Status == StatusClosed {
			anyClosed = true
		}
	}
	switch {
	case abandoned && allTerminal:
		return StatusClosed
	case len(children) == 0:
		return StatusBacklog
	case anyOpen:
		return StatusOpen
	case allTerminal && anyClosed:
		return StatusClosed
	case allTerminal:
		return StatusDone
	default:
		return StatusBacklog
	}
}

// deriveEpicCompleted is the completion date that goes with a derived status:
// the day the epic's last child reached a terminal state. Nothing writes an
// epic when a child of it finishes, so a date on the epic's own file is either
// missing — leaving `tk show`, `tk query` and the TUI's COMPLETED and DURATION
// columns blank on a finished epic — or stale, rendering a completion date
// beside an epic a reopened child has made non-terminal again.
//
// An epic's file therefore stores no completion date at all (stampTimestamps
// clears it), and there is no fallback to one: an abandoned epic with no
// children to have finished renders no date rather than the date of whatever
// edit last touched it.
func deriveEpicCompleted(derived Status, children []*Ticket) time.Time {
	if derived != StatusDone && derived != StatusClosed {
		return time.Time{}
	}
	last := time.Time{}
	for _, c := range children {
		if c.Completed.After(last) {
			last = c.Completed
		}
	}
	return last
}

// derivedEpicStatus is DeriveEpicStatus over a store that may not have been
// read in full. incomplete says a file in it could not be read at all; that
// file is a ticket, it could name any epic as its parent, and nothing about it
// is knowable — so `every child is terminal` is not a claim the store is in a
// position to make, and done and closed are exactly the two values that rest on
// it. Both demote to backlog, which is what the derivation already returns for
// children it cannot call finished: the outcome is identical to a phantom child
// of unknown, non-terminal, non-open status.
//
// Applied to every path that derives or compares a derived status, so a Get, a
// List and the audit agree — an epic that quietly read done while live work sat
// in an unreadable sibling is the failure this exists to remove. It degrades
// visibly rather than silently: the epic drops off the finished list, and List
// warns about the file that put it there.
func derivedEpicStatus(abandoned bool, children []*Ticket, incomplete bool) Status {
	status := DeriveEpicStatus(abandoned, children)
	if incomplete && (status == StatusDone || status == StatusClosed) {
		return StatusBacklog
	}
	return status
}

// deriveEpicFrom returns the status and completion date an epic's children
// imply, given the abandon intent stored on the epic's own file and whether the
// store it was read from held a file that could not be read.
func deriveEpicFrom(abandoned bool, children []*Ticket, incomplete bool) (Status, time.Time) {
	status := derivedEpicStatus(abandoned, children, incomplete)
	return status, deriveEpicCompleted(status, children)
}

// deriveEpics replaces every epic's status and completion date with the values
// derived from its children. FileStore.List applies it to the whole set it just
// read, so CLI, TUI and MCP all see derived values without a display site of
// its own — and no stored value has to be kept in sync by a guard on every
// write path.
//
// Values are computed against stored fields and assigned afterwards, so an epic
// under an epic — only a store predating the one-level rule holds one — derives
// the same way whatever order the files were read in, and the same way it does
// through FileStore.Get, which reads its children stored too. Such a child
// contributes the advisory status its file holds rather than a derived one;
// the one-level rule makes the shape unwritable, and nothing else can hold a
// child that is itself an epic.
func deriveEpics(tickets []*Ticket, project string, incomplete bool) {
	children := childrenByBareParent(tickets, project)
	type derivation struct {
		epic      *Ticket
		status    Status
		completed time.Time
	}
	var derived []derivation
	for _, t := range tickets {
		if t.Type != TypeEpic {
			continue
		}
		_, bare := ParseNamespacedID(t.ID)
		status, completed := deriveEpicFrom(t.Abandoned, children[bare], incomplete)
		derived = append(derived, derivation{epic: t, status: status, completed: completed})
	}
	for _, d := range derived {
		d.epic.Status, d.epic.Completed = d.status, d.completed
	}
}

// childrenByBareParent groups tickets by the bare ID of the parent they name.
// A map key can't tolerate the namespace mismatch the way SameTicketID does,
// and the central store records children with a namespaced parent while tickets
// written before the namespacing rollout record it bare. project is the
// namespace the set's own tickets live under.
//
// A parent naming another project is dropped rather than stripped down to its
// bare ID, which would make it a child of a same-named epic here — the rule
// FileStore.Resolve applies to every other cross-project reference.
func childrenByBareParent(tickets []*Ticket, project string) map[string][]*Ticket {
	children := make(map[string][]*Ticket)
	for _, t := range tickets {
		if t.Parent == "" {
			continue
		}
		parentProject, bare := ParseNamespacedID(t.Parent)
		if parentProject != "" && parentProject != project {
			continue
		}
		children[bare] = append(children[bare], t)
	}
	return children
}

// epicChildren returns the tickets naming epicID as their parent, read exactly
// as their files hold them. The derivation reads a child's stored status and
// completion date and nothing else, so listing through Store.List would derive
// every epic in the store to discard all but this one's children.
//
// A ticket whose parent names another project is skipped: SameTicketID
// tolerates a namespace mismatch, which would make it a child of this store's
// same-named epic — and the abandon cascade writes what it matches.
//
// The bool reports that the store held a file the listing could not read. Such
// a file names no parent this can match, so it is absent from the children
// returned; it is reported instead, because it may be a child of this very epic
// and the derivation has to know it is working from a partial set.
func epicChildren(store Store, epicID string) ([]*Ticket, bool, error) {
	tickets, skips, err := listStored(store)
	if err != nil {
		return nil, false, err
	}
	var children []*Ticket
	for _, t := range tickets {
		if SameTicketID(t.ID, epicID) {
			continue
		}
		if isCrossProjectParent(store, t) {
			continue
		}
		if SameTicketID(t.Parent, epicID) {
			children = append(children, t)
		}
	}
	return children, len(skips) > 0, nil
}

// resolveAbandonIntent records on t the abandon intent the writer expressed and
// reports whether this edit abandons the epic — the one thing that has to
// cascade into the children. priorAbandoned is the intent the epic stands with
// on disk, children are its children as their files hold them, incomplete says
// the store held a file the listing could not read, and statusSet says whether
// the writer set the status field rather than carrying back the one it was
// shown.
//
// Nothing is inferred from the status value. An editor is handed the epic's
// derived status and hands it back with whatever field it did change, so a
// status that merely arrived is not a decision: comparing it against what the
// epic derives would read every unrelated edit of an epic reading closed as an
// abandon, and would read a value that went stale between the read and the
// write as the opposite decision. Whether the field was touched is knowable
// only where the edit was composed, so each edit path carries it.
//
// With the field untouched the flag is left exactly as the file holds it and
// the carried status is not judged at all — an epic's status is advisory, so a
// value landing there means nothing. Set explicitly, closed abandons the epic,
// and any other status takes an abandon back; where there is no abandon to take
// back, an epic's status is not a thing to set, so it is refused.
func resolveAbandonIntent(t *Ticket, priorAbandoned bool, children []*Ticket, incomplete, statusSet bool) (bool, error) {
	t.Abandoned = priorAbandoned
	switch {
	case !statusSet:
		return false, nil
	case t.Status == StatusClosed:
		// Cascades on an already-abandoned epic too: a child reopened since the
		// abandon makes the epic read from its children again, and closing it a
		// second time has to close that child.
		t.Abandoned = true
		return true, nil
	case priorAbandoned:
		t.Abandoned = false
		return false, nil
	default:
		// The refusal stands whatever the value was, but a status that happens to
		// equal the derived one has nothing to change about the children: naming
		// them as the remedy for a value they already produce reads as a
		// contradiction, so that case says what it means instead. Derived the way
		// a reader sees it, degradation included: the refusal quotes a status back
		// to the writer, and quoting one no view of the store shows would be worse
		// than useless.
		derived := derivedEpicStatus(false, children, incomplete)
		if t.Status == derived {
			return false, fmt.Errorf("epic %s already reads %s — an epic's status is derived from its children and cannot be set. "+
				"Change the children, or set the epic closed to abandon it", t.ID, derived)
		}
		return false, fmt.Errorf("cannot set epic %s to %s: an epic's status is derived from its children, and it currently reads %s. "+
			"Change the children, or set the epic closed to abandon it", t.ID, t.Status, derived)
	}
}

// SaveEdit writes a ticket whose fields a user or agent just chose. It is
// Store.Update plus the one thing that cannot be read off the ticket — the
// writer's intent: an epic whose status the writer set to closed is being
// abandoned, so the intent is recorded and every non-terminal child is closed
// with it, which is what makes the epic read closed instead of reading as its
// children imply.
//
// statusSet is whether this edit set the status field at all, which the caller
// knows and the ticket cannot say: `tk edit` has --status among its flags, MCP
// has an omitted status meaning no change, and the TUI form has a status the
// user either cycled or left as it was seeded. An edit that did not set it
// leaves the abandon intent exactly as stored.
//
// Writes tk makes on its own behalf go through Store.Update and touch no other
// ticket. A move closes the ticket it left behind in the source to record that
// it went elsewhere, which is not a decision about the children staying put.
//
// Returns the children the abandon closed, so a caller reporting the edit can
// name the tickets it mutated besides the one it was asked to write.
func SaveEdit(store Store, t *Ticket, statusSet bool) ([]string, error) {
	if s, ok := store.(editSaver); ok {
		return s.saveEdit(t, statusSet)
	}
	return nil, store.Update(t)
}

// editSaver is the store side of SaveEdit. The cascade has to run against the
// one project's store: across a MultiStore, matching children by ID tolerates a
// namespace mismatch and would reach into a same-named epic in another project.
type editSaver interface {
	saveEdit(t *Ticket, statusSet bool) ([]string, error)
}

// ClosedChildrenNote renders the children an abandon closed, for a caller
// reporting a successful edit. Empty when the edit closed none, so it appends
// to whatever the caller was going to say: a write that mutated other tickets
// says so on success as well as in the partial-failure error.
func ClosedChildrenNote(closed []string) string {
	if len(closed) == 0 {
		return ""
	}
	return fmt.Sprintf(" (closed %d child ticket(s): %s)", len(closed), strings.Join(closed, ", "))
}

// closeEpicChildren closes every non-terminal child of an abandoned epic. The
// derivation honours the abandon intent only while every child is terminal, so
// without the cascade closing an epic would not stick. Children that already
// finished keep their record: they are terminal, which is all the derivation
// asks, and rewriting a done child as closed would erase that it completed.
//
// The children are the ones the abandon was decided against, handed through
// rather than listed again: a second listing would read the store twice and
// could disagree with the one the decision was made on.
//
// Every child is attempted rather than stopping at the first failure, so the
// error can name what was closed and what was not — a partial cascade leaves
// the epic reading as its children imply, not as closed, and the rest have to
// be closed by hand.
//
// The IDs it closed come back either way: on success for the caller to report,
// and beside the error so a partial cascade names both halves.
func closeEpicChildren(store Store, epic *Ticket, children []*Ticket) ([]string, error) {
	var closed, failed []string
	for _, child := range children {
		if isTerminal(child) {
			continue
		}
		child.Status = StatusClosed
		if err := store.Update(child); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", child.ID, err))
			continue
		}
		closed = append(closed, child.ID)
	}
	if len(failed) == 0 {
		return closed, nil
	}
	closedList := "none"
	if len(closed) > 0 {
		closedList = strings.Join(closed, ", ")
	}
	return closed, fmt.Errorf("epic %s was closed but %d child ticket(s) were not: %s. Closed: %s. "+
		"Close the rest by hand — the epic reads as closed only while every child is terminal",
		epic.ID, len(failed), strings.Join(failed, "; "), closedList)
}

// EpicDriftKind classifies why an epic's stored status differs from the one it
// derives.
type EpicDriftKind string

const (
	// EpicDriftStoredClosed is an epic whose file stores closed with no abandon
	// flag beside it. A human closing an epic by hand before statuses were
	// derived wrote exactly that, and the stored value is the only surviving
	// trace of the decision — but a write that merely carried a derived closed
	// into the file leaves the same bytes, and nothing in the file tells the two
	// apart. Reported so an operator can decide which it was; the audit does not
	// assert an intent it cannot read.
	EpicDriftStoredClosed EpicDriftKind = "stored-closed"
	// EpicDriftStale is a stored status the children never agreed with — the
	// drift deriving the status removes. Nothing to do: the epic now reads what
	// its children say.
	EpicDriftStale EpicDriftKind = "stale-status"
)

// EpicStatusDrift is one epic whose stored status differs from the status it
// derives from its children.
type EpicStatusDrift struct {
	ID      string        `json:"id"`
	Stored  Status        `json:"stored"`
	Derived Status        `json:"derived"`
	Kind    EpicDriftKind `json:"kind"`
}

// auditStoreEpicStatus reports every epic in one store whose file stores a
// status other than the one it now derives. Deriving replaced the stored value
// rather than migrating it, so the change is invisible to every reader — the
// derivation happens at the read choke point, and nothing else can show what
// the file holds.
//
// Read through listStored, which is the only path left that reports a stored
// epic status, and derived with the same function every reader gets its value
// from — degradation included, or the audit would compare against a status no
// reader is shown and report drift that is not there while missing drift that
// is. The files the listing could not read come back with the drift: they are
// what makes the derived values degraded, and a report that named neither would
// call a store clean it never read in full.
//
// Strictly read-only, like the rest of the audit.
func auditStoreEpicStatus(store Store) ([]EpicStatusDrift, []FileSkip, error) {
	tickets, skips, err := listStored(store)
	if err != nil {
		return nil, nil, err
	}
	children := childrenByBareParent(tickets, storeProject(store))

	var drift []EpicStatusDrift
	for _, t := range tickets {
		if t.Type != TypeEpic {
			continue
		}
		_, bare := ParseNamespacedID(t.ID)
		derived := derivedEpicStatus(t.Abandoned, children[bare], len(skips) > 0)
		if derived == t.Status {
			continue
		}
		// A stored closed is the one drift that may have been a decision, and
		// the file cannot say whether it was — it is separated out so the
		// operator sees the candidates rather than a verdict.
		kind := EpicDriftStale
		if t.Status == StatusClosed && !t.Abandoned {
			kind = EpicDriftStoredClosed
		}
		drift = append(drift, EpicStatusDrift{ID: t.ID, Stored: t.Status, Derived: derived, Kind: kind})
	}
	return drift, skips, nil
}
