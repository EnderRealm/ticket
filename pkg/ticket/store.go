package ticket

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// Store defines the interface for ticket storage backends.
type Store interface {
	Get(id string) (*Ticket, error)
	List() ([]*Ticket, error)
	Create(t *Ticket) error
	Update(t *Ticket) error
	Delete(id string) error
}

// storedReader is a store that can read a ticket exactly as its file holds it.
type storedReader interface {
	getStored(id string) (*Ticket, error)
}

// storedLister is a store that can list tickets exactly as their files hold
// them, alongside the files in it that were not read as tickets at all.
type storedLister interface {
	listStored() ([]*Ticket, []FileSkip, error)
}

// FileSkipKind says why a file in a store was not read as one of its tickets.
type FileSkipKind string

const (
	// FileSkipUnreadable is a file the listing could not turn into one of this
	// project's tickets: one no parse could structure, or one whose stored ID
	// is this project's namespace with nothing usable after it (readFile). The
	// ticket it holds could be any epic's child, so an epic's derived status
	// cannot be trusted while one stands.
	FileSkipUnreadable FileSkipKind = "unreadable"
	// FileSkipForeignNamespace is a file that read fine but whose stored ID
	// names a project other than the directory holding it — a ticket this
	// project cannot place, and no child of any epic here.
	FileSkipForeignNamespace FileSkipKind = "foreign-namespace"
	// FileSkipNamespace is a whole namespace the snapshot could not read: its
	// directory could not be listed, or the catalog expects it and it has no
	// directory. It carries no File — Project names the namespace — and every
	// ticket it holds is unknown, so it degrades every epic's derivation the way
	// one unreadable file does. A catalog that cannot be read is the same kind
	// with an empty Project.
	FileSkipNamespace FileSkipKind = "namespace"
	// FileSkipDuplicateID is one of two or more files in one namespace whose
	// stored IDs read as the same ticket — `x.md` holding `id: x` beside
	// `zz.md` holding `id: proj/x`. Both tickets are listed and neither answers
	// a reference to the ID: a reference resolved by directory order would let
	// an impostor reading done clear a real blocker. Which file is the ticket is
	// a decision for the operator, so the derivation is degraded until it is
	// taken.
	FileSkipDuplicateID FileSkipKind = "duplicate-id"
)

// FileSkip is one file in a store that was not read as one of its tickets, and
// why. A skip is carried out of the read rather than swallowed: the file is a
// ticket somebody wrote, so a listing that drops it silently is a listing that
// under-reports the store.
//
// File is the base filename, not the path: the path names a temp directory in a
// test and the operator's central store otherwise, and the project is carried
// beside it. Project is stamped by the readers that know which store the skip
// came from — the audit and ListWithSkips; listStored on its own leaves it
// empty.
type FileSkip struct {
	Project string       `json:"project,omitempty"`
	File    string       `json:"file"`
	Kind    FileSkipKind `json:"kind"`
	Error   string       `json:"error"`
}

// DegradesEpicStatus reports whether a skip of this kind leaves the epics
// derived from a partial set of children. An unreadable file, an unreadable
// namespace and a duplicated ID all do: each is a ticket that could be any
// epic's child and cannot be placed, so a derivation that cannot see it is
// partial — across the whole store, since a child may live in any namespace. A
// file naming another project is no child of any epic (see readFile), and
// degrading on it would stop every epic reading done over a file that was
// never theirs. A kind this build does not know says nothing about a child
// either, so it does not degrade one: the claim is made only where it is known
// to hold. Every reader that has to say whether a listing was partial — the
// snapshot and the audit's report alike — asks here, so the two cannot answer
// differently.
func (k FileSkipKind) DegradesEpicStatus() bool {
	return k == FileSkipUnreadable || k == FileSkipNamespace || k == FileSkipDuplicateID
}

// ForeignNamespaceError is a ticket file whose stored ID names a project other
// than the one whose directory holds it, which is not read as that project's
// ticket at all. Carries all three so a caller can say which file names what;
// checked with errors.As, since it travels wrapped.
type ForeignNamespaceError struct {
	ID      string // the ID as the file stores it
	Named   string // the project that ID names
	Project string // the project the directory is, empty for a store with no namespace
}

// Error names the stored ID and both projects. The ID and the project it names
// came off a file another machine wrote, so both are quoted the way ticketFile
// quotes a rejected ID and storedStatus quotes an unrecognised status: this
// text reaches a terminal, and %q escapes the control bytes such a file can
// carry.
func (e *ForeignNamespaceError) Error() string {
	held := fmt.Sprintf("project %q", e.Project)
	if e.Project == "" {
		held = "a store with no project namespace"
	}
	return fmt.Sprintf("stored id %q names project %q but the file sits in %s, so it is not read as this project's ticket", e.ID, e.Named, held)
}

// UnusableIDError is a ticket file whose stored ID carries its own project's
// namespace and nothing usable after it — a remainder that is empty, or that
// carries further path separators. The file says it is this project's ticket
// and gives no ID to read it under, so it yields no ticket at all; unlike a
// file naming another project it may still be a local epic's child, which is
// why it is skipped as FileSkipUnreadable. Checked with errors.As, since it
// travels wrapped.
type UnusableIDError struct {
	ID string // the ID as the file stores it
}

// Error names the stored ID, quoted for the reason ForeignNamespaceError's is,
// and states the rule it broke through the hint the write path rejects an ID
// with.
func (e *UnusableIDError) Error() string {
	return fmt.Sprintf("stored id %q names this project, but what follows the namespace %s", e.ID, bareNameHint)
}

// oneLine flattens an error's text to a single line and drops the control runes
// from it. Both skip types quote a reason that came off a file another machine
// wrote: yaml.v3's TypeError is multi-line by construction — one indented line
// per offending key — and it quotes up to ten bytes of the offending scalar
// verbatim, so a crafted key would otherwise read as further entries in the
// audit's warning block, where every line is an entry, and an embedded escape
// would reach the terminal as an escape. Collapsing whitespace alone is not
// enough for the second half: ESC, BEL and BS are not whitespace and survive
// Fields, so the control runes are dropped after the join.
//
// Done at construction rather than at each print site, so a reason is
// single-line and inert wherever it surfaces, the JSON included. Paths inside an
// os error are left as they are — a reason that cannot name the file it is about
// is not worth printing.
func oneLine(err error) string {
	joined := strings.Join(strings.Fields(err.Error()), " ")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, joined)
}

// UnreadableTicketError is a single-ticket read that resolved to a file which
// yielded no ticket: bytes that would not parse, or a stored ID this project
// cannot use. Every other failure of such a read means no such file, and the
// two call for opposite repairs — one is a file to fix, the other a ticket to
// create — so a caller is told which rather than left to guess. It matters most
// where the listing's stderr warning does not reach, which is every MCP client.
// Checked with errors.As, since it travels wrapped through MultiStore. A file
// naming another project is not one of these: it was read in full, and
// ForeignNamespaceError already says so.
type UnreadableTicketError struct {
	File string // base filename, as FileSkip carries it
	Err  error
}

// Error names the file and the reason. The filename came off a file another
// machine wrote and arrived over a git remote, so it is quoted the way
// ForeignNamespaceError quotes a stored ID: this text reaches a terminal.
func (e *UnreadableTicketError) Error() string {
	return fmt.Sprintf("ticket file %q exists but cannot be read as a ticket: %v", e.File, e.Err)
}

func (e *UnreadableTicketError) Unwrap() error { return e.Err }

// SkipLister is a store that can list its tickets alongside the files in it
// that were not read as tickets at all. Optional: a caller that has to say a
// listing was partial asks for it and falls back to Store.List, which can only
// answer that nothing was skipped.
type SkipLister interface {
	ListWithSkips() ([]*Ticket, []FileSkip, error)
}

// projectStore is a store whose tickets all live under one project namespace.
type projectStore interface {
	projectName() string
}

// readStored reads a ticket without deriving an epic's status, for callers that
// need only stored fields. Store.Get derives, which reads the whole store for
// every epic it returns, and a parent is always an epic — so resolving one
// through Get inside a loop reads the store once per ticket.
func readStored(store Store, id string) (*Ticket, error) {
	if r, ok := store.(storedReader); ok {
		return r.getStored(id)
	}
	return store.Get(id)
}

// listStored lists a store's tickets without deriving any epic's status, for
// callers that need only stored fields — deriving one epic reads its children,
// so listing through Store.List to derive another would recurse. The second
// return is the files the store could not read; a store that cannot report them
// answers with none, which is all a caller can do with a plain Store.List.
func listStored(store Store) ([]*Ticket, []FileSkip, error) {
	if l, ok := store.(storedLister); ok {
		return l.listStored()
	}
	tickets, err := store.List()
	return tickets, nil, err
}

var _ Store = (*FileStore)(nil)

// FileStore provides filesystem-backed CRUD operations for tickets.
// Project is the namespace this store's tickets live under; it is empty for
// single-project stores that never see namespaced IDs. Source attributes this
// store's writes in the mutation log (mutation.go); empty means the human at
// the terminal, and it is set through WithSource rather than in place.
type FileStore struct {
	Dir     string
	Project string
	Source  string
}

// NewFileStore creates a FileStore rooted at the given directory, with no
// project namespace: a store whose IDs are all bare and which rejects every
// namespaced one. Nothing in tk resolves such a store any more — every store a
// repo resolves to is a central project — so this is the shape the type still
// permits and the unit tests on FileStore itself use. Dropping it is deferred
// rather than decided.
func NewFileStore(dir string) *FileStore {
	return &FileStore{Dir: dir}
}

// NewProjectFileStore creates a FileStore rooted at the given directory that
// also answers to IDs namespaced under project.
func NewProjectFileStore(dir, project string) *FileStore {
	return &FileStore{Dir: dir, Project: project}
}

// projectName is the namespace this store's bare IDs live under. Read through
// the projectStore interface so a store that wraps a FileStore answers with it
// rather than being taken for a store with no namespace at all.
func (s *FileStore) projectName() string {
	return s.Project
}

// EnsureDir creates the tickets directory if it doesn't exist.
func (s *FileStore) EnsureDir() error {
	return os.MkdirAll(s.Dir, 0o755)
}

// guardWrite is the catalog check every write entry point makes first —
// Create, Update, Delete and mutate — before any lock is taken or directory
// made. The catalog is the boundary's (centralFor): a store in the central
// layout consults the catalog at its central root, and a store outside it —
// no project, or a directory that is not <root>/tickets/<project> — is not a
// central store and has no catalog to consult. central.write repeats the
// check under the lock; this one fails fast.
func (s *FileStore) guardWrite() error {
	return centralFor(s).guard(s.Project)
}

// Create writes a new ticket to disk. The ticket must already have an ID.
// If the ID collides with an existing ticket, a new ID is generated and
// the ticket is retried (up to 5 attempts). The write passes the central
// boundary: catalog guard, exclusive store lock, validation against the
// snapshot taken under it, then the ticket lock and the write.
func (s *FileStore) Create(t *Ticket) error {
	if err := s.guardWrite(); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return centralFor(s).write(s.Project, func(o *op) error {
		return o.create(s, t)
	})
}

// createLocked writes t if nothing has claimed its ID, holding that ticket's
// lock across the check and the write so two concurrent creates of one ID
// cannot both pass the check. Reports whether the write happened; a false
// return is an ID already taken, which Create retries under a fresh one.
func (s *FileStore) createLocked(t *Ticket) (bool, error) {
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return false, fmt.Errorf("create: %w", err)
	}
	release, err := s.lockTicket(t.ID)
	if err != nil {
		return false, fmt.Errorf("create: %w", err)
	}
	defer release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return false, nil
	}
	if err := s.writeTicket(t); err != nil {
		return false, err
	}
	s.logMutation(t.ID, MutationCreate, nil)
	return true, nil
}

// ImportAll writes a batch of tickets read from somewhere other than the
// store — a repository's legacy .tickets/ directory, which `tk init` copies
// into the central store — through the boundary, as one write. The batch is
// validated as a whole against a snapshot that already holds it: a child whose
// parent is the epic beside it in the batch resolves, and the cycle check sees
// every edge the batch would add. Nothing is written until every ticket has
// passed; the first refusal comes back naming its ticket, with the store as it
// was.
//
// An ID the store already holds is the store's: the batch copy is skipped
// rather than validated or written, since a legacy file re-imported over a
// ticket the central store has since carried on with is stale by definition,
// and this is what keeps `tk init` re-runnable. An epic in the batch is
// stored with the status the merged snapshot derives for it — a stored epic
// status is advisory and never read back, so a legacy value is corrected
// rather than refused the way a create refuses one the caller chose. The IDs
// come back bare, as the batch carried them.
func (s *FileStore) ImportAll(tickets []*Ticket) (imported, skipped []string, err error) {
	if err := s.guardWrite(); err != nil {
		return nil, nil, fmt.Errorf("import: %w", err)
	}
	c := centralFor(s)
	err = c.write(s.Project, func(o *op) error {
		// The destination is bounded the way every other write's is: a
		// symlinked namespace is left out of readSources, so the batch would
		// see no target and the writes below would follow s.Dir to the
		// link's target outside the store.
		if _, err := c.store(s.Project); err != nil {
			return fmt.Errorf("import: %w", err)
		}
		sources, crossProject, err := c.readSources()
		if err != nil {
			return err
		}
		existing := map[string]bool{}
		target := -1
		for i, src := range sources {
			if src.name != s.Project {
				continue
			}
			if src.err != "" {
				return fmt.Errorf("import: namespace %q could not be read: %s", s.Project, src.err)
			}
			target = i
			for _, t := range src.tickets {
				existing[t.ID] = true
			}
		}
		if target < 0 {
			sources = append(sources, namespaceSource{name: s.Project})
			target = len(sources) - 1
		}
		var pending []*Ticket
		for _, t := range tickets {
			if !existing[t.ID] {
				pending = append(pending, t)
			}
		}
		// merge is the store with the batch in it, built over copies:
		// buildSnapshot qualifies IDs and derives epics in place, the sources
		// are built from twice, and the batch is written under its bare IDs
		// as the caller holds them.
		merge := func() *Snapshot {
			merged := make([]namespaceSource, len(sources))
			for i, src := range sources {
				merged[i] = src
				merged[i].tickets = make([]*Ticket, 0, len(src.tickets)+len(pending))
				for _, t := range src.tickets {
					dup := *t
					merged[i].tickets = append(merged[i].tickets, &dup)
				}
			}
			for _, t := range pending {
				dup := *t
				merged[target].tickets = append(merged[target].tickets, &dup)
			}
			return buildSnapshot(merged, crossProject)
		}
		// Canonicalize first, validate second, each against a graph that holds
		// the batch. A legacy parent spelled as a fragment does not place its
		// child in the first graph, so a cycle running through two such
		// children is invisible to a check made over it; once every parent is
		// the exact epic it resolves to and every reference is qualified, the
		// graph rebuilt from the batch holds every edge the write would add,
		// and the cycle check sees all of them.
		canon := &op{c: c, snap: merge()}
		for _, t := range pending {
			if err := canon.resolveParent(s.Project, nil, t); err != nil {
				return fmt.Errorf("import %s: %w", t.ID, err)
			}
			qualifyRefs(s.Project, nil, t.Deps)
			qualifyRefs(s.Project, nil, t.Links)
			qualifyCargoKeys(s.Project, nil, t)
		}
		merged := merge()
		batch := &op{c: c, snap: merged}
		for _, t := range pending {
			if err := batch.validate(s.Project, nil, t); err != nil {
				return fmt.Errorf("import %s: %w", t.ID, err)
			}
			if t.Type == TypeEpic {
				if twin, ok := merged.Get(FormatNamespacedID(s.Project, t.ID)); ok {
					t.Status = twin.Status
				}
			}
		}
		if err := s.EnsureDir(); err != nil {
			return err
		}
		for _, t := range tickets {
			if existing[t.ID] {
				skipped = append(skipped, t.ID)
				continue
			}
			written, err := s.createLocked(t)
			if err != nil {
				return fmt.Errorf("import %s: %w", t.ID, err)
			}
			if written {
				imported = append(imported, t.ID)
			} else {
				skipped = append(skipped, t.ID)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return imported, skipped, nil
}

// Get retrieves a ticket by exact or partial ID. An epic comes back with the
// status and completion date its children imply — across every namespace,
// off a snapshot taken under the shared store lock — so a single-ticket read
// agrees with List. A leaf stays a single-file read, plus a listing of its
// namespace to say the ID is its alone and one read of its parent to say
// whether the relationship is valid: no snapshot, and no store lock, so a Get
// of a leaf is safe from inside a mutation callback where a Get of an epic is
// not.
func (s *FileStore) Get(id string) (*Ticket, error) {
	t, err := s.getStored(id)
	if err != nil {
		return nil, err
	}
	if err := s.stampStored(t); err != nil {
		return nil, err
	}
	return t, nil
}

// stampStored finishes a ticket read exactly as its file holds it into what
// Get returns: the derived status and completion date for an epic, the
// relationship issue for a leaf. t carries its bare ID. Shared with the
// bare-ID path of MultiStore.Get, which already holds the stored ticket and
// must not resolve it a second time by ID — Resolve matches file names, so a
// file renamed while keeping its `id` would come back not found, and one
// claimant of a duplicated ID would come back as the other.
func (s *FileStore) stampStored(t *Ticket) error {
	c := centralFor(s)
	if t.Type == TypeEpic {
		snap, err := c.snapshot()
		if err != nil {
			return err
		}
		(&op{c: c, snap: snap}).stamp(FormatNamespacedID(s.Project, t.ID), t)
		return nil
	}
	t.relationshipIssue = c.leafIssue(s.Project, t)
	return nil
}

// leafIssue is the relationship issue a leaf carries, resolved by lock-free
// listings of its own namespace and its parent's rather than by a snapshot.
// The rule is the snapshot's (buildSnapshot, Snapshot.parentIssue), applied in
// the same order: the leaf's own ID has to be claimed by one file, then a
// bare parent is local, a qualified one names its namespace, a foreign one is
// a child only once the catalog says so, and the parent has to exist under
// exactly that ID, claimed by one file — the two things claimedStored
// answers for.
func (c *central) leafIssue(ns string, t *Ticket) string {
	if _, err := c.claimedStored(FormatNamespacedID(ns, t.ID)); err != nil {
		return oneLine(err)
	}
	if t.Parent == "" {
		return ""
	}
	parentID := qualifyRef(ns, t.Parent)
	if namespaceOf(parentID) != ns && !c.crossProjectActivated() {
		return fmt.Sprintf("parent %s is in another project, and cross-project parents are not activated (the catalog does not require %s)", parentID, FeatureCrossProjectParents)
	}
	parent, err := c.claimedStored(parentID)
	if err != nil {
		return fmt.Sprintf("parent %s does not resolve: %v", parentID, oneLine(err))
	}
	if parent.Type != TypeEpic {
		return fmt.Sprintf("parent %s is type %s, not an epic", parentID, parent.Type)
	}
	return ""
}

// claimedStored reads the ticket a qualified ID names, exactly as its file
// holds it, off a listing of that one namespace: no snapshot and no store
// lock, so it is safe wherever a leaf Get is. It is the single-ticket
// counterpart of the snapshot's index, and applies the same two rules — the
// stored ID has to be exactly the one asked for, never a fragment Resolve
// would substring-match, and it has to be claimed by one file. `x.md`
// holding `id: x` beside `zz.md` holding `id: proj/x` answers a reference to
// x with neither (listStored, FileSkipDuplicateID), because resolved by
// directory order the done one would clear a blocker the open one still
// holds. A leaf's parent check and the dep fallback both read through here,
// so a duplicate the listings refuse is refused by the single reads too.
func (c *central) claimedStored(id string) (*Ticket, error) {
	ns, bare := ParseNamespacedID(id)
	store, err := c.store(ns)
	if err != nil {
		return nil, err
	}
	tickets, _, err := store.listStored()
	if err != nil {
		return nil, err
	}
	var found *Ticket
	for _, t := range tickets {
		if t.ID != bare {
			continue
		}
		if found != nil {
			return nil, errors.New(duplicateIssue(id))
		}
		found = t
	}
	if found == nil {
		return nil, fmt.Errorf("ticket %s not found", id)
	}
	return found, nil
}

// getStored retrieves a ticket exactly as its file holds it, without deriving
// an epic's status. Deriving one reads every ticket in the store, so callers
// that need only stored fields — a parent's type, the next link in a parent
// chain — read through here rather than making an unrelated lookup quadratic.
//
// A file that resolved and yielded no ticket comes back as an
// UnreadableTicketError rather than as the bare parse failure, so a caller
// cannot mistake it for a ticket that is not there. The classification follows
// listStored's: a file naming another project was read in full and reports
// itself, and every other refusal is a file that yielded nothing.
func (s *FileStore) getStored(id string) (*Ticket, error) {
	path, err := s.Resolve(id)
	if err != nil {
		return nil, err
	}
	t, err := s.readFile(path)
	if err != nil {
		var foreign *ForeignNamespaceError
		if errors.As(err, &foreign) {
			return nil, err
		}
		return nil, &UnreadableTicketError{File: filepath.Base(path), Err: err}
	}
	return t, nil
}

// Update writes a ticket back to disk in canonical format. Every field is
// stored as the caller holds it, including an epic's status — which is
// advisory, since the derivation reads the children and the abandon flag and
// never the status field. That is what makes a read-modify-write of any other
// field of an epic idempotent: the derived status it carries back lands
// somewhere nothing reads, and the flag it carries back is the stored one.
//
// The write is refused with ErrConflict if the file changed since the ticket
// was read, so a caller that overlapped another writer is told rather than
// silently overwriting it. A caller whose change is an accumulation — appending
// a note, a dep, a link — has nothing to decide on that error and goes through
// Mutate instead, which holds the lock across the read as well.
//
// Relationships are validated on every update, not only when the parent
// changes: a ticket that predates a rule must be fixed before any of its
// fields is written back. The validation runs against the snapshot the
// exclusive store lock was taken over, so the parent it resolves and the
// cycle it refuses are the store's state at the moment of the write.
func (s *FileStore) Update(t *Ticket) error {
	if err := s.guardWrite(); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return centralFor(s).write(s.Project, func(o *op) error {
		return o.update(s, t)
	})
}

// updateLocked is the compare-and-swap half of Update, with the ticket's lock
// already held. The lock alone orders two writers; the version check is what
// tells the second one that the state it computed from is gone — the two
// writers may never have overlapped at all, having only read the same file
// before either wrote.
//
// A ticket carrying no version was built by its caller rather than read from
// the store, and is written unconditionally. That is the compatibility escape
// and it is deliberate: the CAS protects a read-modify-write, which is where
// the lost updates were.
func (s *FileStore) updateLocked(t *Ticket) error {
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	current, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("ticket %s not found", t.ID)
	}
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if t.version != "" && versionOf(current) != t.version {
		return fmt.Errorf("update %s: %w. Re-read it and apply the change again", t.ID, ErrConflict)
	}
	// The verdict ledger is append-only, and every surface's write reaches this
	// one choke point, so no call site carries a rule of its own. Keyed on the
	// file's current bytes rather than on the CAS, so it holds for the
	// version-less unconditional write too. A prior that does not parse is
	// skipped, the same tolerance logUpdate applies: there is nothing to compare
	// against — unless the prior carries action receipts. Those are what a
	// replay is judged by, and a version-less write over a file Parse refused
	// would replace them with whatever the caller rebuilt, so the next retry
	// applies the action again. Whether the file holds any is read from the
	// raw frontmatter, independent of the field that failed — a malformed
	// `deps:` returns from Parse before the actions block is looked at — and
	// the write is refused until the file is repaired. A frontmatter that
	// cannot be decoded at all leaves the question unanswerable, and is refused
	// on the same terms.
	prior, err := Parse(bytes.NewReader(current))
	if err != nil {
		receipts, rawErr := carriesReceipts(current)
		if rawErr != nil {
			return fmt.Errorf("update %s: %w; whether the file holds action receipts cannot be established, and this write would replace any it holds; repair %s by hand so it parses, then retry", t.ID, err, path)
		}
		if receipts {
			return fmt.Errorf("update %s: %w; the file's actions block holds receipts this write would replace; repair %s by hand so it parses, then retry", t.ID, err, path)
		}
	}
	if err == nil {
		if !verdictsAppendOnly(prior.Verdicts, t.Verdicts) {
			return fmt.Errorf("update %s: verdict rows are append-only — a correction is a new row, and this write would drop or rewrite recorded rows", t.ID)
		}
		// Only the rows this write adds are judged. Validating the whole slice
		// would make a ticket that synced in an invalid row permanently
		// unwritable, since append-only requires every write to preserve it.
		for _, row := range t.Verdicts[len(prior.Verdicts):] {
			if err := ValidateVerdictRow(row); err != nil {
				return fmt.Errorf("update %s: %w", t.ID, err)
			}
		}
		// The action receipts, on the same terms.
		if !actionsAppendOnly(prior.Actions, t.Actions) {
			return fmt.Errorf("update %s: action receipts are append-only — this write would drop or rewrite recorded receipts", t.ID)
		}
		for _, receipt := range t.Actions[len(prior.Actions):] {
			if err := ValidateActionReceipt(receipt); err != nil {
				return fmt.Errorf("update %s: %w", t.ID, err)
			}
		}
	}
	if err := s.writeTicket(t); err != nil {
		return err
	}
	// The bytes the write replaced are what the log diffs against. This is the
	// hook for Mutate as well as Update — mutate writes through here — so a note,
	// a dep and a link are each recorded as themselves.
	s.logUpdate(current, t)
	return nil
}

// saveEdit writes an edit, recording the abandon intent when the writer set an
// epic's status and cascading into the children when the edit abandons it.
// One boundary hold covers the decision, the epic's write and the cascade, so
// the children the intent was resolved against are the children that are
// closed, and no other writer lands between the two.
//
// An abandon is refused while the snapshot is incomplete — every child being
// terminal is not a claim a partial read can make — and while any
// non-terminal child lives in another project: closing work in another
// repository is not something an edit to this epic does invisibly, so the
// affected IDs are named, grouped by project, for the operator to close or
// reparent first. Nothing is written in either case.
func (s *FileStore) saveEdit(t *Ticket, statusSet bool) ([]string, error) {
	if err := s.guardWrite(); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	var closed []string
	err := centralFor(s).write(s.Project, func(o *op) error {
		id := FormatNamespacedID(s.Project, t.ID)
		prior, _ := o.snap.Get(id)
		abandon := false
		var children []*Ticket
		if t.Type == TypeEpic {
			if prior == nil {
				stored, err := s.getStored(t.ID)
				if err != nil {
					return err
				}
				prior = stored
			}
			children = o.snap.Children(id)
			// Only a ticket that was already an epic has an intent to carry: one
			// being promoted was read as an ordinary ticket, whose file has no
			// abandon of its own. The promotion itself is judged by the same rule
			// as any other edit — an untouched status decides nothing, so `--type
			// epic` alone stays one ordinary operation, while a status the writer
			// set with it is the epic's status they set and is read as one.
			var err error
			if abandon, err = resolveAbandonIntent(t, prior.Type == TypeEpic && prior.Abandoned, children, !o.snap.Complete, statusSet); err != nil {
				return err
			}
		}
		if abandon {
			if !o.snap.Complete {
				return o.incompleteError(fmt.Sprintf("abandoning epic %s", t.ID))
			}
			var foreign []string
			for _, c := range children {
				if !isTerminal(c) && namespaceOf(c.ID) != s.Project {
					foreign = append(foreign, c.ID)
				}
			}
			if len(foreign) > 0 {
				return fmt.Errorf("cannot abandon epic %s: it has unfinished children in other projects (%s). Close or reparent them first, then abandon the epic",
					t.ID, groupByProject(foreign))
			}
		}
		if err := o.update(s, t); err != nil {
			return err
		}
		if abandon {
			var err error
			closed, err = o.closeChildren(t, children)
			return err
		}
		return nil
	})
	return closed, err
}

// Delete removes a ticket file by exact or partial ID. Refused while any
// ticket in any namespace still names it as parent, dep or link, and while
// the snapshot cannot prove that none does.
func (s *FileStore) Delete(id string) error {
	if err := s.guardWrite(); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return centralFor(s).write(s.Project, func(o *op) error {
		path, err := s.Resolve(id)
		if err != nil {
			return err
		}
		return o.delete(s, path)
	})
}

// List reads all tickets from the directory. Epics come back with the status
// and completion date derived from their children rather than the ones on disk
// — this is the choke point every consumer reads through, so no display site
// derives its own.
//
// A file the snapshot did not take as a ticket is warned about here and
// nowhere else, in the wording its kind calls for. This is the display choke
// point, so one CLI command produces one warning per skipped file; warning
// from listStored instead would repeat it once per internal read.
func (s *FileStore) List() ([]*Ticket, error) {
	tickets, skips, err := s.ListWithSkips()
	if err != nil {
		return nil, err
	}
	warnSkips(skips)
	return tickets, nil
}

// warnSkips reports each skipped file or namespace once, in the wording its
// kind calls for. Shared by FileStore.List and MultiStore.List so a project
// view and the central view say the same thing about the same file.
func warnSkips(skips []FileSkip) {
	for _, skip := range skips {
		// Joined here rather than through FormatNamespacedID: a filename is not a
		// ticket ID — the file did not parse, so it has no ID — and nothing
		// resolves this string. It is a location for a human to go and look.
		name := skip.File
		if skip.Project != "" {
			name = skip.Project + "/" + skip.File
		}
		// Quoted: a filename arrives over a git remote like the file's contents,
		// and this goes straight to a terminal.
		switch {
		case skip.Kind == FileSkipNamespace && skip.Project == "":
			Warnf("warning: the catalog could not be read (%s), so the namespaces it names are unknown and no epic reads done or closed\n", skip.Error)
		case skip.Kind == FileSkipNamespace:
			Warnf("warning: namespace %q could not be read (%s), so its tickets are not shown and no epic reads done or closed\n", skip.Project, skip.Error)
		case skip.Kind == FileSkipDuplicateID:
			Warnf("warning: %q claims an ID another file in the namespace also claims (%s), so neither answers a reference to it and no epic reads done or closed\n", name, skip.Error)
		case !skip.Kind.DegradesEpicStatus():
			// Read fine and placed nowhere, so neither half of the unreadable
			// wording applies: the epics are not degraded by it. Keyed off the
			// derivation's own predicate rather than a kind, so the wording and
			// the degradation cannot disagree about a kind added later.
			Warnf("warning: %q is not shown as a ticket here (%s)\n", name, skip.Error)
		default:
			Warnf("warning: %q could not be read (%s), so its ticket is not shown and no epic reads done or closed\n", name, skip.Error)
		}
	}
}

// ListWithSkips is List with the skipped files handed to the caller instead of
// warned about. A caller whose stderr nobody reads — the MCP server's is
// discarded at both ends — can otherwise not tell a short result set from a
// complete one. The tickets are this project's alone, with bare IDs. A skip
// that degrades the epics is carried from any namespace, because it degrades
// the epics here and a project view that hid it would show a demoted epic
// with no cause in sight; a skip that degrades nothing says nothing about the
// tickets shown here, so only this project's own are carried.
func (s *FileStore) ListWithSkips() ([]*Ticket, []FileSkip, error) {
	snap, err := centralFor(s).snapshot()
	if err != nil {
		return nil, nil, err
	}
	return projectView(snap, s.Project), s.projectSkips(snap), nil
}

// ListFromSnapshot is List over a snapshot the caller already holds: the
// project's tickets under bare IDs, the skips warned about as List warns
// them. For a command that decides membership off the graph and lists the
// rows off the same reading rather than a second one.
func (s *FileStore) ListFromSnapshot(snap *Snapshot) []*Ticket {
	warnSkips(s.projectSkips(snap))
	return projectView(snap, s.Project)
}

// projectSkips is the skips a project listing carries, per ListWithSkips.
func (s *FileStore) projectSkips(snap *Snapshot) []FileSkip {
	var skips []FileSkip
	for _, skip := range snap.Skips {
		if !skip.Kind.DegradesEpicStatus() && skip.Project != s.Project {
			continue
		}
		skips = append(skips, skip)
	}
	return skips
}

// projectView is the snapshot's tickets in one namespace, their IDs stripped
// back to the bare form a project store reports. Copies, so the snapshot's own
// index still answers the qualified ID.
func projectView(snap *Snapshot, ns string) []*Ticket {
	var tickets []*Ticket
	for _, t := range snap.Tickets {
		tns, bare := ParseNamespacedID(t.ID)
		if tns != ns {
			continue
		}
		view := *t
		view.ID = bare
		tickets = append(tickets, &view)
	}
	return tickets
}

// Snapshot is the graph of the store this project belongs to, read under the
// shared store lock: every namespace, every ticket under its qualified ID,
// every epic derived from its full child set. The public read of the graph
// for a consumer that needs relationships rather than a listing.
func (s *FileStore) Snapshot() (*Snapshot, error) {
	return centralFor(s).snapshot()
}

// listStored reads all tickets from the directory exactly as their files hold
// them. Deriving an epic needs its children's stored fields and nothing else,
// so the derivation reads through here — going back through List would derive
// every epic in the store to use one epic's children, and would hand a nested
// epic to Get already derived, making a single read disagree with a listing.
//
// A file that cannot be read is skipped rather than failing the whole listing —
// one corrupt file must not take a store offline — but it is reported back, so
// every caller can say what it did not see. Parse tolerates a mistyped field
// (format.go), so what reaches here is a file with no readable structure at all.
// A file whose stored ID names another project is skipped for the reason
// readFile gives, and carries the kind that says so: the two are reported the
// same way and degrade an epic's status differently. Every other refusal
// readFile makes — an ID namespaced to this project with nothing usable after
// it included — is a file that yielded no ticket, which is the default here.
func (s *FileStore) listStored() ([]*Ticket, []FileSkip, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var tickets []*Ticket
	var files []string
	var skips []FileSkip
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, err := s.readFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			kind := FileSkipUnreadable
			var foreign *ForeignNamespaceError
			if errors.As(err, &foreign) {
				kind = FileSkipForeignNamespace
			}
			skips = append(skips, FileSkip{File: e.Name(), Kind: kind, Error: oneLine(err)})
			continue
		}
		tickets = append(tickets, t)
		files = append(files, e.Name())
	}
	// Two files can claim one ID: a stored prefix naming this project reads as
	// its bare remainder, so `x.md` holding `id: x` and `zz.md` holding `id:
	// proj/x` both yield x. Both tickets are listed — each is a file somebody
	// wrote — and each file is reported, naming the other, so the operator can
	// decide which is the ticket; the snapshot answers a reference to the ID
	// with neither meanwhile.
	claims := map[string][]string{}
	for i, t := range tickets {
		claims[t.ID] = append(claims[t.ID], files[i])
	}
	for i, t := range tickets {
		if names := claims[t.ID]; len(names) > 1 {
			skips = append(skips, FileSkip{File: files[i], Kind: FileSkipDuplicateID,
				Error: fmt.Sprintf("stored id %q is also claimed by %s", t.ID, strings.Join(others(names, files[i]), ", "))})
		}
	}
	return tickets, skips, nil
}

// others is names without one of them, for a skip naming its rivals. Each is
// quoted: a rival's name arrived over the git remote like the file's contents
// and reaches the terminal inside the reason (warnSkips prints it as it is),
// where warnSkips quotes only the file the skip is about.
func others(names []string, self string) []string {
	var rest []string
	for _, n := range names {
		if n != self {
			rest = append(rest, fmt.Sprintf("%q", n))
		}
	}
	return rest
}

// Resolve finds the full file path for an exact or partial ticket ID.
// An ID namespaced under this store's own project resolves against its bare
// remainder — parent, dep, and link fields written through MultiStore carry
// the prefix, and every reader resolves them back through here.
// Returns an error if the ID is ambiguous (multiple matches) or not found.
func (s *FileStore) Resolve(id string) (string, error) {
	// A prefix naming a different project is rejected, not stripped: stripping
	// it would resolve against a same-suffix ticket in this project and mutate
	// the wrong one. Cross-project references simply do not resolve here.
	if project, bare := ParseNamespacedID(id); project != "" {
		if project != s.Project {
			return "", fmt.Errorf("ticket %s not found", id)
		}
		id = bare
	}

	// Reject empty/whitespace IDs so partial matching does not silently match
	// every ticket (in a single-ticket store this resolves to the lone ticket).
	// Checked after the strip so a prefix-only "project/" is rejected too.
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("id is required")
	}

	// Try exact match first.
	exact, err := s.ticketFile(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(exact); err == nil {
		return exact, nil
	}

	// Partial match: find files containing the partial ID.
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return "", fmt.Errorf("ticket %s not found", id)
	}

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		base := strings.TrimSuffix(name, ".md")
		if strings.Contains(base, id) {
			matches = append(matches, filepath.Join(s.Dir, name))
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("ticket %s not found", id)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = strings.TrimSuffix(filepath.Base(m), ".md")
		}
		return "", fmt.Errorf("ambiguous ID %q matches: %s", id, strings.Join(ids, ", "))
	}
}

// bareNameHint is the shared tail of the ID and project path guards. The
// rejected value can come from a ticket file's id field or straight off the
// command line, so it states the constraint without asserting a provenance.
const bareNameHint = "must be a bare name with no path separators — check the id you passed or the id field in the ticket file it came from"

// ticketFile maps a ticket ID to its file path. IDs reach it from ticket files
// that tk did not necessarily write (hand-edited, synced, produced by another
// tool), and filepath.Join cleans traversal segments instead of failing — so an
// ID like "../../evil" would resolve outside s.Dir. Requiring the ID to equal
// its own filepath.Base keeps it a single path element, and ".md" is appended
// before the join so that element can never be "." or ".." either. Every path a
// FileStore builds from a ticket ID goes through here, which bounds the filename
// half; on a MultiStore the directory half comes from an ID's project prefix and
// is bounded in storeFor. The bound is lexical — it constrains the path string,
// not the inode it resolves to. A symlinked ticket file inside the store still
// redirects a read, which follows the link; it no longer redirects a write,
// because writeTicket replaces the path by rename and rename replaces the link
// itself rather than writing through it — so the first write lands in the store
// and leaves a regular file where the link was.
func (s *FileStore) ticketFile(id string) (string, error) {
	if id != filepath.Base(id) {
		return "", fmt.Errorf("invalid ticket ID %q in %s: %s", id, s.Dir, bareNameHint)
	}
	return filepath.Join(s.Dir, id+".md"), nil
}

// readFile parses a ticket and stamps it with the version of the bytes it was
// parsed from, which is what Update compares against before it writes.
func (s *FileStore) readFile(path string) (*Ticket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// The directory is authoritative for a ticket's namespace. Resolve,
	// ticketFile and the whole write path key off it, while a stored prefix is
	// data that arrived from another machine over git — every file tk writes
	// holds a bare ID, since MultiStore strips the namespace before delegating.
	//
	// A prefix that agrees is redundant, and stripping it here restores the
	// invariant every reader downstream assumes — that a FileStore yields bare
	// IDs — which MultiStore.List would otherwise re-prefix into proj/proj/id.
	// A prefix that disagrees names a file this project cannot place, so no
	// reader guesses at it: read as a ticket here it would answer a bare
	// reference in this project, and silently clear a real blocker whenever it
	// read done. Refused rather than dropped without a word — `tk audit` reports
	// it. A store with no project of its own rejects every namespaced ID in
	// Resolve already, so a prefix there is foreign too.
	//
	// What the strip leaves has to be a ticket ID, by the rule ticketFile
	// applies on write: ParseNamespacedID splits on the first separator only, so
	// `proj/a/b` and `proj/` would otherwise be stripped to `a/b` and to nothing
	// at all, breaking the very invariant above — and `a/b` indexes under the
	// bare half `b`, which puts a planted file back in the way of a real
	// reference to `b`. That file claims to be this project's ticket rather than
	// another's, so it may be a local epic's child: it is refused as a file that
	// yielded no ticket (FileSkipUnreadable), not as another project's.
	if proj, bare := ParseNamespacedID(t.ID); proj != "" {
		switch {
		case proj != s.Project:
			return nil, &ForeignNamespaceError{ID: t.ID, Named: proj, Project: s.Project}
		case bare == "" || bare != filepath.Base(bare):
			return nil, &UnusableIDError{ID: t.ID}
		default:
			t.ID = bare
		}
	}
	// The file's version, not the returned struct's: Get and List derive an
	// epic's status, so what they hand back already differs from what is stored
	// and hashing the struct would conflict with itself.
	t.version = versionOf(data)
	t.namespace = s.Project
	return t, nil
}

// stampTimestamps maintains the created/updated/completed fields. It is the
// single write choke point so CLI, MCP, and TUI callers all get consistent
// timestamps. Rules are stateless (no previous status needed):
//   - updated is always set to now.
//   - created is set to now only if unset (a move carries it over).
//   - completed is set to now when the status is done/closed and it is unset;
//     it is cleared when the status is neither done nor closed.
//   - an epic stores no completed at all: it is derived from the children on
//     read, and a date stamped from the writer's clock would be the day of an
//     edit rather than the day the work ended.
func stampTimestamps(t *Ticket) {
	now := time.Now().UTC()
	t.Updated = now
	if t.Created.IsZero() {
		t.Created = now
	}
	if t.Status == StatusDone || t.Status == StatusClosed {
		if t.Completed.IsZero() {
			t.Completed = now
		}
	} else {
		t.Completed = time.Time{}
	}
	if t.Type == TypeEpic {
		t.Completed = time.Time{}
	}
}

// createMode is the mode a ticket file that does not yet exist is created with:
// 0644 masked by the process umask, which is what os.WriteFile produced before
// the write became a temp file plus a rename. The kernel applies the umask to
// an open, not to the chmod that has to follow os.CreateTemp's 0600, so it is
// read here instead.
//
// Read once during package initialization by setting the umask and putting it
// straight back, because the call is process-global: doing it at write time
// would leave a window in which another goroutine's file creation saw a zero
// umask. Initialization runs on one goroutine before main, which closes that
// window rather than narrowing it.
var createMode = fs.FileMode(0o644 &^ processUmask())

func processUmask() fs.FileMode {
	mask := syscall.Umask(0)
	syscall.Umask(mask)
	return fs.FileMode(mask)
}

// writeTicket serializes a ticket and replaces its file atomically: the bytes
// go to a temp file in the same directory (rename cannot cross filesystems) and
// are renamed over the target, so no reader can observe a half-written or
// zero-length ticket the way os.WriteFile's truncate-then-write allowed. The
// temp name must not end in ".md" — listStored and Resolve select on that
// suffix and would otherwise pick up a file mid-write.
//
// It is the inner half of the write mechanism, which is:
//
//   - a per-ticket flock (lock.go) serialises writers to one ticket, and only
//     to that one, so unrelated tickets stay uncontended;
//   - Update compares the version the ticket was read at against the file's
//     current bytes and refuses with ErrConflict on a mismatch, rather than
//     overwriting a change it never saw;
//   - Mutate holds the lock across the read as well, for the accumulating
//     writes that would have nothing to do with a conflict but re-read.
//
// No fsync before the rename. The rename is atomic against other readers on a
// running system, which is the race here; fsync buys crash durability, at a
// disk flush per note, for a store whose loss window on a power cut is already
// the one every other file in the git working tree has.
//
// The written bytes are stamped back onto the ticket as its new version, so a
// caller updating the same in-memory ticket twice does not conflict with its
// own first write.
//
// Residual: a writer killed between CreateTemp and Rename strands a
// `.tk-write-*` file in the store directory, which no later run cleans up and
// which `tk sync` stages wholesale along with the tickets. Bounded rather than
// handled — the window is one write, every error path inside it removes the
// temp file itself, and a stranded file is inert: it parses as nothing, and
// listStored and Resolve select on the `.md` suffix it does not have.
func (s *FileStore) writeTicket(t *Ticket) error {
	// Resolve the path before the stamping below, which mutates the caller's
	// ticket: a rejected ID must not leave it looking as if it were persisted.
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return err
	}
	stampTimestamps(t)
	// The abandon intent is an epic's alone. Cleared at the write choke point
	// rather than trusted from the caller, so demoting an epic drops the flag
	// however the demotion was written.
	if t.Type != TypeEpic {
		t.Abandoned = false
	}
	// A landed ticket records the branch it landed on. Done at the same write
	// choke point as the timestamps so CLI, MCP, and TUI callers all get it;
	// fill-if-absent leaves any value the caller set explicitly.
	if t.Status == StatusDone {
		PopulateDoneOutputs(t, "", t.Branch)
	}
	data, err := Serialize(t)
	if err != nil {
		return err
	}

	// The mode the rename is about to publish. os.WriteFile left an existing
	// file's mode untouched and created a new one at 0644 masked by the umask;
	// a temp file plus rename publishes whatever the temp file carries, so both
	// halves are reproduced here. Without the first, a store under umask 077
	// whose tickets are 0600 would find every one of them widened to
	// world-readable by the next note — ticket bodies carry work detail and
	// whatever an agent pasted into a note.
	mode := createMode
	info, err := os.Stat(path)
	switch {
	case err == nil:
		// A target the current user cannot write is refused rather than
		// replaced. Renaming over a file consults the directory's mode and not
		// the target's, so without this a ticket someone deliberately made
		// read-only would be silently overwritten where os.WriteFile refused.
		// The owner-write bit is what such a chmod leaves; a file owned by
		// another user is not modelled — a store belongs to one user.
		if info.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "write", Path: path, Err: fs.ErrPermission}
		}
		mode = info.Mode().Perm()
	case !os.IsNotExist(err):
		return err
	}

	tmp, err := os.CreateTemp(s.Dir, ".tk-write-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	// CreateTemp opens at 0600 and a chmod is not umask-masked, so the mode has
	// to be set explicitly before the rename publishes the file — nothing
	// chmods it afterwards.
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	t.version = versionOf(data)
	// v7 retired the review system and the parser strips a legacy
	// `## Review Log` out of every body it reads, so this write is the moment
	// the section leaves the file. Reported here rather than at the parse: a
	// read happens for every listing, and only a write drops anything. Cleared
	// afterwards because the bytes now on disk carry no section, so a second
	// write of the same in-memory ticket has nothing left to report.
	//
	// Named in the store's namespace: MultiStore strips it before delegating, so
	// the bare ID here would not match the namespaced one `tk audit` reports for
	// the same ticket.
	if t.droppedReviewLog > 0 {
		Warnf("warning: ticket %q: this write dropped a legacy Review Log section of %d bytes from its body — the section has not been read since v7 retired the review system, and its content stays recoverable from the store's git history\n", qualifyForStore(s, t.ID), t.droppedReviewLog)
		t.droppedReviewLog = 0
	}
	return nil
}
