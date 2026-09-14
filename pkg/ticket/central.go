package ticket

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// ErrStoreLockTimeout is a store lock that could not be taken within
// storeLockTimeout. Checked with errors.Is; the wrapped message names the lock
// file, the wait, and the two things that produce it: another writer stuck
// holding the lock, or a nested entry point — a Get of an epic, an Update, a
// Mutate — called from inside a mutation callback, which is refused rather than
// allowed to wait on the lock its own caller holds.
var ErrStoreLockTimeout = errors.New("store lock timed out")

// storeLockTimeout bounds every wait for the store lock. A package variable so
// the tests that provoke a nested acquisition can shrink it.
var storeLockTimeout = 30 * time.Second

// storeLockPoll is how often a waiter re-tries the lock. flock has no timed
// wait, so the lock is taken non-blocking and retried; a few milliseconds is
// well under the cost of a snapshot and keeps a same-process waiter from
// spinning.
const storeLockPoll = 2 * time.Millisecond

// storeLockState is one process's hold on one store's lock file. flock belongs
// to the open file description, so a process holds at most one flock per file
// however many goroutines share it — the readers count and writer flag are
// what let those goroutines share a shared hold and exclude one another for an
// exclusive one, and the flock underneath is what excludes other processes.
type storeLockState struct {
	f       *os.File
	readers int
	writer  bool
}

// storeLocks is the process-wide registry of store locks, keyed by lock file
// path. One descriptor per store per process, opened on first use and never
// closed: the lock file is never removed, so every process opens the same
// inode.
var storeLocks = struct {
	sync.Mutex
	held map[string]*storeLockState
}{held: map[string]*storeLockState{}}

// tryAcquire takes the lock if it is free, under the registry mutex. Reports
// false when it is not — held by another goroutine in the wrong mode, or by
// another process — and the caller retries.
func (st *storeLockState) tryAcquire(exclusive bool) (bool, error) {
	if exclusive {
		if st.writer || st.readers > 0 {
			return false, nil
		}
		held, err := flockNB(st.f, syscall.LOCK_EX)
		if held {
			st.writer = true
		}
		return held, err
	}
	if st.writer {
		return false, nil
	}
	if st.readers == 0 {
		held, err := flockNB(st.f, syscall.LOCK_SH)
		if !held {
			return false, err
		}
	}
	st.readers++
	return true, nil
}

// flockNB attempts the lock without blocking and reports whether it is now
// held. EWOULDBLOCK is not an error: it is the answer the caller polls for.
func flockNB(f *os.File, how int) (bool, error) {
	err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}

// release gives the hold back and drops the flock when the last holder leaves.
func (st *storeLockState) release(exclusive bool) {
	if exclusive {
		st.writer = false
	} else {
		st.readers--
	}
	if !st.writer && st.readers == 0 {
		syscall.Flock(int(st.f.Fd()), syscall.LOCK_UN)
	}
}

// acquireStoreLock takes the store lock at path, shared or exclusive, and
// returns the release. It waits up to storeLockTimeout, polling: a
// same-process waiter simply waits its turn, and a nested acquisition from
// inside a write times out instead of deadlocking — nothing piggybacks on a
// hold its caller already has, because a callback that could reach the store
// through a second entry point could also reach it through one that writes.
func acquireStoreLock(path string, exclusive bool) (func(), error) {
	mode := "shared"
	if exclusive {
		mode = "exclusive"
	}
	deadline := time.Now().Add(storeLockTimeout)
	for {
		storeLocks.Lock()
		st := storeLocks.held[path]
		if st == nil {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
			if err != nil {
				storeLocks.Unlock()
				return nil, fmt.Errorf("store lock %s: %w", path, err)
			}
			st = &storeLockState{f: f}
			storeLocks.held[path] = st
		}
		ok, err := st.tryAcquire(exclusive)
		storeLocks.Unlock()
		if err != nil {
			return nil, fmt.Errorf("store lock %s: %w", path, err)
		}
		if ok {
			return func() {
				storeLocks.Lock()
				st.release(exclusive)
				storeLocks.Unlock()
			}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: waited %v for the %s lock on %s — either another writer is stuck holding it, or this call is nested inside a mutation callback, which is refused: a callback must not read an epic or write through the store",
				ErrStoreLockTimeout, storeLockTimeout, mode, path)
		}
		time.Sleep(storeLockPoll)
	}
}

// central is the relationship and mutation boundary over one central store —
// or, for a store with no central layout, over that one directory. Every
// snapshot is read and every write validated through it, so CLI, TUI, MCP,
// `ticket_create(repo=)` and Mutate all pass one boundary: FileStore stays the
// persistence primitive and is not an alternate write authority.
type central struct {
	// root is the central root, "" for a single store.
	root string
	// ticketsDir is <root>/tickets, the directory holding one directory per
	// namespace; unused for a single store.
	ticketsDir string
	// single is the one store of a non-central layout: a FileStore with no
	// project, or one whose directory is not <root>/tickets/<project>. Its
	// tickets live in namespace single.Project, which may be "".
	single *FileStore
	// source attributes the writes made through this boundary, carried into
	// every per-namespace store it builds.
	source string
}

// isCentralLayout reports whether a store sits in a central store as
// <root>/tickets/<project>, the shape CentralProjectDir fixes and every
// central caller builds. Both halves of the layout are required: a directory
// merely named for its project — an isolated store at /work/app for project
// app — has siblings that are not namespaces, and reading them as namespaces
// would extend the boundary, and the catalog it consults, past the store the
// caller supplied.
//
// The directory is inspected cleaned: a trailing slash or a `.` segment leaves
// Base and Dir looking one level off, and `<root>/tickets/warp/` would read as
// an isolated store — its snapshot omitting every other namespace, its lock
// keyed on the project directory rather than the root — while the same store
// spelled without the slash is central.
func isCentralLayout(s *FileStore) bool {
	dir := filepath.Clean(s.Dir)
	return s.Project != "" && filepath.Base(dir) == s.Project && filepath.Base(filepath.Dir(dir)) == ticketsDirName
}

// centralFor derives the boundary from a store's layout: under the central
// layout the central root is two levels up, and a store outside it has no
// central to belong to and is a boundary over itself. This is the one place
// the layout is decided — guardWrite reaches the catalog through the boundary
// this returns — so the catalog a write is judged by and the namespaces it is
// validated against cannot disagree. The root and tickets directory come from
// the same cleaned path isCentralLayout judged, so the lock and every path the
// boundary reaches are keyed on one spelling.
func centralFor(s *FileStore) *central {
	if isCentralLayout(s) {
		ticketsDir := filepath.Dir(filepath.Clean(s.Dir))
		return &central{root: filepath.Dir(ticketsDir), ticketsDir: ticketsDir, source: s.Source}
	}
	return &central{single: s, source: s.Source}
}

// centralForMulti is the boundary over a MultiStore's root, which is the
// tickets directory.
func centralForMulti(m *MultiStore) *central {
	return &central{root: filepath.Dir(m.rootDir), ticketsDir: m.rootDir, source: m.Source}
}

// centralProvider is a store that can hand out its boundary. An unexported
// interface, like mutator: a wrapper embedding a FileStore gets it promoted.
type centralProvider interface {
	central() *central
}

func (s *FileStore) central() *central  { return centralFor(s) }
func (m *MultiStore) central() *central { return centralForMulti(m) }
func (c *central) isSingle() bool       { return c.single != nil }

// lockPath is the store lock file: <cache>/tk/locks/<hash6>.store.lock, in the
// directory the ticket locks use and for the same reasons (lockFile). The hash
// is of the canonical central root, or of the single store's directory, so two
// spellings of one store share a lock and two stores never do. For a single
// store that directory is the one lockFile hashes for its ticket locks, so
// the suffix is joined with a dot rather than lockFile's dash: a ticket whose
// ID is literally `store` would otherwise share the store lock's file, and
// taking that ticket's lock while holding the store lock would wait on itself.
func (c *central) lockPath() (string, error) {
	dir := c.root
	if c.isSingle() {
		dir = c.single.Dir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if eval, err := filepath.EvalSymlinks(abs); err == nil {
		abs = eval
	}
	locks, err := locksDir()
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(abs))
	return filepath.Join(locks, hex.EncodeToString(key[:6])+".store.lock"), nil
}

// sameCentral reports whether two boundaries are one store, by lock file —
// the canonical identity every other decision about the store is keyed on.
func (c *central) sameCentral(other *central) (bool, error) {
	a, err := c.lockPath()
	if err != nil {
		return false, err
	}
	b, err := other.lockPath()
	if err != nil {
		return false, err
	}
	return a == b, nil
}

// store returns the FileStore for one namespace. Every path the boundary
// reaches goes through here, so the namespace-into-path join is bounded once,
// the way MultiStore.storeFor bounds it — and so is the directory itself: a
// symlinked namespace, which git can sync in, is refused before any read,
// the way readSources leaves it out of the snapshot. Otherwise a leaf's dep
// on `foreign/x` would fall through the listing to claimedStored and read a
// done ticket at the link's target that the snapshot never saw. A missing
// directory is still allowed — a registered project that has never held a
// ticket has none.
func (c *central) store(ns string) (*FileStore, error) {
	if c.isSingle() {
		if ns != c.single.Project {
			return nil, fmt.Errorf("namespace %q is not in this store", ns)
		}
		return c.single, nil
	}
	if !project.ValidName(ns) {
		return nil, fmt.Errorf("invalid project %q in %s: %s", ns, c.ticketsDir, bareNameHint)
	}
	if _, err := lstatProjectDir(c.ticketsDir, ns); err != nil {
		return nil, err
	}
	s := NewProjectFileStore(filepath.Join(c.ticketsDir, ns), ns)
	s.Source = c.source
	return s, nil
}

// catalog loads the store's catalog; a single store has none.
func (c *central) catalog() (*Catalog, error) {
	if c.isSingle() {
		return nil, nil
	}
	return LoadCatalog(c.root)
}

// guard is the catalog check for a write into namespace ns (checkWrite): the
// required features against what this binary implements, and Root's
// activation. A single store has no catalog and nothing to refuse.
func (c *central) guard(ns string) error {
	if c.isSingle() {
		return nil
	}
	return checkWrite(c.root, ns)
}

// crossProjectActivated is whether the catalog allows a foreign parent, for
// the single-file read of a leaf that has no snapshot in hand. A catalog that
// cannot be read activates nothing.
func (c *central) crossProjectActivated() bool {
	cat, err := c.catalog()
	return err == nil && cat.CrossProjectParentsActivated()
}

// readSources reads every namespace once, exactly as its files hold it. Called
// with the store lock held. The namespaces are the directories under
// ticketsDir — IsDir, so a symlinked directory is not one, as MultiStore
// always held — joined with the ones the catalog names and Root once
// activated, mirroring ReadInventory: a catalogued namespace with no directory
// is a namespace whose tickets are missing, not an empty one.
//
// A single store that cannot be listed is an error, not a namespace skip: it
// is the whole store, so there is no listing to degrade, and a skip in its
// name — "" for a store with no project — would read as the catalog it does
// not have. List and Get fail the way they did before the boundary. A central
// store keeps the per-namespace skip, since the other namespaces still list.
func (c *central) readSources() ([]namespaceSource, bool, error) {
	if c.isSingle() {
		tickets, skips, err := c.single.listStored()
		if err != nil {
			return nil, false, fmt.Errorf("listing %s: %w", c.single.Dir, err)
		}
		return []namespaceSource{{name: c.single.Project, tickets: tickets, skips: skips}}, false, nil
	}

	var sources []namespaceSource
	names := map[string]bool{}
	dirs := map[string]bool{}
	entries, err := os.ReadDir(c.ticketsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
			names[e.Name()] = true
		}
	}
	cat, err := c.catalog()
	crossProject := false
	if err != nil {
		sources = append(sources, namespaceSource{name: "", err: oneLine(err)})
	} else {
		crossProject = cat.CrossProjectParentsActivated()
		if cat != nil {
			for name := range cat.Namespaces {
				names[name] = true
			}
		}
		if cat.RootActivated() {
			names[project.RootNamespace] = true
		}
	}
	for _, name := range sortedKeys(names) {
		src := namespaceSource{name: name}
		if !dirs[name] {
			src.err = fmt.Sprintf("expected (catalogued, or Root once activated) but has no directory under %s: not cloned, or its %s marker was never written", c.ticketsDir, NamespaceMarker)
			sources = append(sources, src)
			continue
		}
		store, err := c.store(name)
		if err != nil {
			src.err = oneLine(err)
			sources = append(sources, src)
			continue
		}
		tickets, skips, err := store.listStored()
		if err != nil {
			src.err = oneLine(fmt.Errorf("listing %s: %w", store.Dir, err))
			sources = append(sources, src)
			continue
		}
		src.tickets, src.skips = tickets, skips
		sources = append(sources, src)
	}
	return sources, crossProject, nil
}

// snapshot reads the whole store under the shared lock and returns the graph.
// The snapshot is a point-in-time copy, so the lock covers the read alone.
func (c *central) snapshot() (*Snapshot, error) {
	path, err := c.lockPath()
	if err != nil {
		return nil, err
	}
	release, err := acquireStoreLock(path, false)
	if err != nil {
		return nil, err
	}
	defer release()
	return c.snapshotLocked()
}

// snapshotLocked is snapshot with the store lock already held in either mode.
func (c *central) snapshotLocked() (*Snapshot, error) {
	sources, crossProject, err := c.readSources()
	if err != nil {
		return nil, err
	}
	return buildSnapshot(sources, crossProject), nil
}

// write runs fn under the exclusive store lock with a fresh snapshot in hand.
// Every write entry point comes through here, so relationship validation
// happens inside the lock and against the state the write lands on. ns is the
// namespace the write lands in, and its catalog guard is re-run under the lock
// before anything is read or written: the entry points check it before
// waiting for the lock, which fails fast, but a catalog that arrives while the
// write waits — a sync landing an activation on a newer tk — is the one the
// write lands under, so the check here is the authoritative one. The lock
// order is fixed: store lock first, then per-ticket locks, which op's helpers
// take in sorted qualified-ID order where they take several.
func (c *central) write(ns string, fn func(*op) error) error {
	path, err := c.lockPath()
	if err != nil {
		return err
	}
	release, err := acquireStoreLock(path, true)
	if err != nil {
		return err
	}
	defer release()
	if err := c.guard(ns); err != nil {
		return err
	}
	snap, err := c.snapshotLocked()
	if err != nil {
		return err
	}
	return fn(&op{c: c, snap: snap})
}

// WithStoreLock runs fn holding the store lock of the central store at
// centralRoot — the lock every snapshot and every write through this package
// takes — shared or exclusive. It is for a caller that changes the store's
// files by a route other than a ticket write and has to keep every reader and
// writer out while it does: `tk sync` applying a fetched git tree under the
// boundary, so no snapshot reads a half-applied merge and no write lands
// between the tree change and the commit.
//
// fn must not call back into a store entry point that takes the lock — a
// Create, an Update, a Mutate, a List, a Get of an epic — because the lock is
// not reentrant: the nested acquisition waits on the hold its own caller has
// and fails with ErrStoreLockTimeout, as acquireStoreLock says.
func WithStoreLock(centralRoot string, exclusive bool, fn func() error) error {
	c := &central{root: centralRoot, ticketsDir: filepath.Join(centralRoot, ticketsDirName)}
	path, err := c.lockPath()
	if err != nil {
		return err
	}
	release, err := acquireStoreLock(path, exclusive)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// SnapshotOf is the graph a Store answers with, for a consumer that holds the
// Store interface rather than one of the stores this package owns — the MCP
// server, which derives a listing, its skips, its revision and its
// completeness from one reading so a response never mixes two. Through the
// boundary for a FileStore or a MultiStore; from the listing for any other
// implementation, which has no lock and no catalog and is read as one
// namespace, the way the audit falls back.
func SnapshotOf(store Store) (*Snapshot, error) {
	return snapshotOf(store)
}

// snapshotOf is the graph a Store answers with: through its boundary for the
// stores this package owns, and from its listing for any other implementation,
// which has no lock and no catalog and is read as one namespace.
func snapshotOf(store Store) (*Snapshot, error) {
	if p, ok := store.(centralProvider); ok {
		return p.central().snapshot()
	}
	tickets, skips, err := listStored(store)
	if err != nil {
		return nil, err
	}
	return buildSnapshot([]namespaceSource{{name: storeProject(store), tickets: tickets, skips: skips}}, false), nil
}

// op is a write in progress: the snapshot the exclusive lock was taken over
// and the helpers that write through it. None of them reacquires the store
// lock — a nested acquisition times out, by design — so every write inside a
// cascade or a move goes through these rather than back through the store's
// entry points.
type op struct {
	c    *central
	snap *Snapshot
}

func (o *op) store(ns string) (*FileStore, error) { return o.c.store(ns) }

// incompleteError is the refusal every absence proof shares: the snapshot did
// not read the whole store, so "no child", "no referrer" and "every child is
// terminal" are not claims it can make. Names each diagnostic — the evidence
// that is missing is the remedy.
func (o *op) incompleteError(what string) error {
	return fmt.Errorf("%s requires a complete snapshot of the store, and this one is not: %s. Repair or remove what could not be read, then retry",
		what, strings.Join(o.snap.Diagnostics(), "; "))
}

// validate is every relationship rule a write has to pass, against the
// snapshot in hand. ns is the namespace next is written into, prior is the
// ticket as its file holds it (nil on create), and next is rewritten in place:
// a parent canonicalized to the epic it resolved to, new references qualified.
func (o *op) validate(ns string, prior, next *Ticket) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if err := o.resolveParent(ns, prior, next); err != nil {
		return err
	}
	newDeps := qualifyRefs(ns, priorRefs(prior, func(t *Ticket) []string { return t.Deps }), next.Deps)
	qualifyRefs(ns, priorRefs(prior, func(t *Ticket) []string { return t.Links }), next.Links)
	qualifyCargoKeys(ns, prior, next)
	// An epic derives completion from its children and never reads its own
	// deps, so a dep on one would be a requirement the epic can read done
	// over. An existing dep on a legacy epic is left for the audit; only a new
	// one, or a promotion that would bring deps along, is refused.
	if next.Type == TypeEpic {
		promoted := prior != nil && prior.Type != TypeEpic && len(next.Deps) > 0
		if len(newDeps) > 0 || promoted {
			return fmt.Errorf("epic %s cannot depend on %s: epics derive completion from their children; put the requirement on an acceptance leaf",
				next.ID, strings.Join(next.Deps, ", "))
		}
	}
	if o.graphChanged(ns, prior, next) {
		if err := o.cycleCheck(ns, next); err != nil {
			return err
		}
	}
	if prior != nil && prior.Type == TypeEpic && next.Type != TypeEpic {
		if err := o.leafConversion(ns, next); err != nil {
			return err
		}
	}
	return nil
}

// priorRefs is a field of prior, or nothing on create.
func priorRefs(prior *Ticket, field func(*Ticket) []string) []string {
	if prior == nil {
		return nil
	}
	return field(prior)
}

// qualifyRefs rewrites, in place, every reference in next that no reference in
// prior already names, to its qualified form; a reference prior already holds
// keeps its spelling, so a legacy bare reference stays exactly as written
// through an unrelated edit. Returns the qualified IDs of the new ones.
func qualifyRefs(ns string, prior, next []string) []string {
	existing := map[string]bool{}
	for _, ref := range prior {
		existing[qualifyRef(ns, ref)] = true
	}
	var added []string
	for i, ref := range next {
		q := qualifyRef(ns, ref)
		if existing[q] {
			continue
		}
		next[i] = q
		added = append(added, q)
	}
	return added
}

// qualifyCargoKeys is qualifyRefs for the cargo map's keys: a new key is
// written qualified and its value survives the rekey.
func qualifyCargoKeys(ns string, prior, next *Ticket) {
	existing := map[string]bool{}
	if prior != nil {
		for k := range prior.DepCargo {
			existing[qualifyRef(ns, k)] = true
		}
	}
	for k, v := range next.DepCargo {
		q := qualifyRef(ns, k)
		if q == k || existing[q] {
			continue
		}
		delete(next.DepCargo, k)
		next.DepCargo[q] = v
	}
}

// resolveParent enforces the one-level hierarchy against the snapshot and
// canonicalizes next.Parent to the epic it resolved to. A bare parent names
// the child's own namespace; a foreign one must be qualified, and is accepted
// only once the catalog requires cross-project parents. A fragment resolves
// by substring within that one namespace, ambiguity refused, as
// FileStore.Resolve resolves a typed ID.
func (o *op) resolveParent(ns string, prior, next *Ticket) error {
	if next.Parent == "" {
		return nil
	}
	if next.Type == TypeEpic {
		return fmt.Errorf("epic %s cannot have parent %s: epics are top level. "+
			"Clear the parent, or change this ticket's type", next.ID, next.Parent)
	}
	parentID := qualifyRef(ns, next.Parent)
	parentNS, fragment := ParseNamespacedID(parentID)
	if parentNS != ns && !o.snap.crossProject {
		return fmt.Errorf("ticket %s: parent %s is in another project: an epic and its children must live in the same project until the catalog requires %s. "+
			"Repoint the parent at an epic in this project, or clear it", next.ID, next.Parent, FeatureCrossProjectParents)
	}
	parent, ok := o.snap.byID[parentID]
	if !ok {
		// An ID two files claim is absent from byID by design, and both
		// claimants would match the fragment: refused as the duplicate it is,
		// the way leafIssue reports it, not as a fragment the caller should
		// have spelled out. The remedy names the files, as the skips do.
		if o.snap.claims[parentID] > 1 {
			var files []string
			for _, skip := range o.snap.Skips {
				if skip.Kind == FileSkipDuplicateID && skip.Project == parentNS {
					files = append(files, skipLine(skip))
				}
			}
			return fmt.Errorf("ticket %s: parent %s does not resolve: %s (%s). "+
				"Decide which file is the epic and remove or re-id the other, then retry", next.ID, next.Parent, duplicateIssue(parentID), strings.Join(files, "; "))
		}
		matches := o.snap.partialMatches(parentNS, fragment)
		switch len(matches) {
		case 1:
			parent = matches[0]
		case 0:
			if reason, failed := o.snap.failed[parentNS]; failed {
				return fmt.Errorf("ticket %s: parent %s is in namespace %q, which could not be read: %s. "+
					"Repair that namespace, or clear the parent", next.ID, next.Parent, parentNS, reason)
			}
			return fmt.Errorf("ticket %s: parent %s does not resolve: ticket %s not found. "+
				"Repoint the parent at an existing epic, or clear it", next.ID, next.Parent, parentID)
		default:
			ids := make([]string, len(matches))
			for i, m := range matches {
				ids[i] = m.ID
			}
			return fmt.Errorf("ticket %s: parent %s is ambiguous, matching %s. "+
				"Name the epic in full", next.ID, next.Parent, strings.Join(ids, ", "))
		}
	}
	if parent.Type != TypeEpic {
		return fmt.Errorf("ticket %s: parent %s is type %s, not an epic: only epics hold children. "+
			"Repoint the parent at an epic, or clear it", next.ID, parent.ID, parent.Type)
	}
	// A reference the file already holds keeps its spelling: a legacy bare
	// parent is local by the reading rule and rewriting it would be a
	// migration nobody asked for. Anything else is stored resolved and
	// qualified, since every reader matches a parent by exact ID.
	if prior != nil && prior.Parent != "" && qualifyRef(ns, prior.Parent) == parent.ID {
		next.Parent = prior.Parent
		return nil
	}
	next.Parent = parent.ID
	return nil
}

// graphChanged reports whether the write moves an edge the cycle check has to
// look at again: a create, a type change, a different parent, or a different
// dep set.
func (o *op) graphChanged(ns string, prior, next *Ticket) bool {
	if prior == nil || prior.Type != next.Type {
		return true
	}
	if qualifyRef(ns, prior.Parent) != qualifyRef(ns, next.Parent) {
		return true
	}
	if len(prior.Deps) != len(next.Deps) {
		return true
	}
	for i := range prior.Deps {
		if qualifyRef(ns, prior.Deps[i]) != qualifyRef(ns, next.Deps[i]) {
			return true
		}
	}
	return false
}

// cycleCheck refuses next if it lies on a cycle of the combined "waits for"
// graph: a leaf waits for each dep, an epic waits for each child. Terminal
// tickets are nodes like any other — a cycle through a done ticket is one
// reopening it would activate. A child depending on its own epic is the
// canonical case: the epic waits for the child, the child waits for the epic.
func (o *op) cycleCheck(ns string, next *Ticket) error {
	nextID := FormatNamespacedID(ns, next.ID)
	waits := map[string][]string{}
	for _, t := range o.snap.Tickets {
		if t.ID == nextID {
			continue
		}
		tns := namespaceOf(t.ID)
		for _, d := range t.Deps {
			waits[t.ID] = append(waits[t.ID], qualifyRef(tns, d))
		}
		if t.Type == TypeEpic {
			for _, c := range o.snap.children[t.ID] {
				if c.ID != nextID {
					waits[t.ID] = append(waits[t.ID], c.ID)
				}
			}
		}
	}
	for _, d := range next.Deps {
		waits[nextID] = append(waits[nextID], qualifyRef(ns, d))
	}
	if next.Parent != "" {
		parentID := qualifyRef(ns, next.Parent)
		waits[parentID] = append(waits[parentID], nextID)
	}
	// An epic's children as the write leaves them, not as the snapshot placed
	// them: a leaf whose parent names an epic that does not exist yet, or one
	// that is still a leaf, has no child edge in the snapshot, and the create
	// or the promotion is exactly what makes it one. Matched on the parent as
	// stored, whatever issue the snapshot stamped, so no edge this write would
	// close is missed.
	if next.Type == TypeEpic {
		for _, t := range o.snap.Tickets {
			if t.ID != nextID && t.Parent != "" && qualifyRef(namespaceOf(t.ID), t.Parent) == nextID {
				waits[nextID] = append(waits[nextID], t.ID)
			}
		}
	}

	visited := map[string]bool{}
	var path []string
	var walk func(id string) bool
	walk = func(id string) bool {
		path = append(path, id)
		for _, succ := range waits[id] {
			if succ == nextID {
				path = append(path, succ)
				return true
			}
			if visited[succ] {
				continue
			}
			visited[succ] = true
			if walk(succ) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	visited[nextID] = true
	if walk(nextID) {
		return fmt.Errorf("ticket %s would wait for itself: %s. A leaf waits for its deps and an epic waits for its children, and this write closes the loop",
			next.ID, strings.Join(path, " -> "))
	}
	return nil
}

// leafConversion refuses turning an epic into a leaf while any ticket in any
// namespace still names it as parent, or while the snapshot cannot prove none
// does. Inbound rather than Children: a child the snapshot could not place —
// a foreign one while cross-project parents are not activated, one stamped
// with a duplicate-id issue — still names this epic, and converting it would
// leave that reference pointing at a leaf, which activating the feature later
// cannot repair.
func (o *op) leafConversion(ns string, next *Ticket) error {
	id := FormatNamespacedID(ns, next.ID)
	if !o.snap.Complete {
		return o.incompleteError(fmt.Sprintf("changing epic %s to a %s", next.ID, next.Type))
	}
	var ids []string
	for _, ref := range o.snap.Inbound(id) {
		if ref.Kind == InboundParent {
			ids = append(ids, ref.From)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return fmt.Errorf("epic %s cannot become a %s while it has children (%s). Reparent or close them first",
		next.ID, next.Type, groupByProject(ids))
}

// create is FileStore.Create's body with the boundary held: validation against
// the snapshot, then the collision-retrying write under the ticket lock.
func (o *op) create(s *FileStore, t *Ticket) error {
	if err := o.validate(s.Project, nil, t); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	// A new epic's status is the one its children imply — which for a ticket
	// nothing yet names as parent is backlog. Storing another value would be
	// inert rather than wrong, since nothing reads it back, but a status the
	// caller chose and no reader will ever see is worth refusing.
	if t.Type == TypeEpic {
		children := o.snap.Children(FormatNamespacedID(s.Project, t.ID))
		if derived := derivedEpicStatus(t.Abandoned, children, !o.snap.Complete); t.Status != derived {
			// closed is the one value changing the children cannot produce on a
			// childless epic: it is the abandon intent, and only an edit records
			// one — so it is refused with the remedy that does.
			remedy := "Create it, then change its children"
			if t.Status == StatusClosed {
				remedy = fmt.Sprintf("Create it, then run `tk edit %s --status closed` to abandon it", t.ID)
			}
			return fmt.Errorf("create: cannot create epic %s as %s: an epic's status is derived from its children, and it would read %s. %s",
				t.ID, t.Status, derived, remedy)
		}
	}
	if err := s.EnsureDir(); err != nil {
		return err
	}
	path, err := s.ticketFile(t.ID)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("ticket %s already exists", t.ID)
	}
	// Retry on hash collision (different title, same 4-char hash).
	const maxRetries = 5
	for i := 0; i < maxRetries; i++ {
		written, err := s.createLocked(t)
		if err != nil {
			return err
		}
		if written {
			return nil
		}
		t.ID = GenerateID(t.Title)
	}
	return fmt.Errorf("ticket ID collision after %d attempts", maxRetries)
}

// update is FileStore.Update's body with the boundary held: validation against
// the stored twin, then the compare-and-swap write under the ticket lock.
func (o *op) update(s *FileStore, t *Ticket) error {
	if err := o.validate(s.Project, o.prior(s, t.ID), t); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	release, err := s.lockTicket(t.ID)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	defer release()
	return s.updateLocked(t)
}

// prior is the ticket an update is written over, as its file holds it. The
// snapshot answers for an ID one file claims; an ID two files claim is
// nobody's there, and validating against no prior would judge the write as a
// create — the epic-to-leaf guard, the new-dep and promotion rules and the
// legacy spellings a prior preserves all key on it, and an epic its children
// still name could be written back as a leaf. So whenever the snapshot cannot
// resolve the identity, the file the write lands on — the path updateLocked
// reads — is read directly. A file that is not there, or does not parse,
// yields no prior, and updateLocked reports it.
func (o *op) prior(s *FileStore, id string) *Ticket {
	if prior, ok := o.snap.Get(FormatNamespacedID(s.Project, id)); ok {
		return prior
	}
	path, err := s.ticketFile(id)
	if err != nil {
		return nil
	}
	prior, err := s.readFile(path)
	if err != nil {
		return nil
	}
	return prior
}

// stamp gives a ticket read directly from its file what a snapshot read would
// have given it: a derived status and completion date for an epic, the
// relationship issue for a leaf. id is the ticket's qualified ID.
func (o *op) stamp(id string, t *Ticket) {
	twin, ok := o.snap.Get(id)
	switch {
	case t.Type == TypeEpic && ok:
		t.Status, t.Completed = twin.Status, twin.Completed
	case t.Type == TypeEpic:
		// Two files claim the ID, so the snapshot placed no children under it:
		// derived as it would be from an empty, incomplete set, and stamped as
		// the ambiguous identity the snapshot stamps every claimant with.
		t.Status, t.Completed = deriveEpicFrom(t.Abandoned, nil, true)
		t.relationshipIssue = duplicateIssue(id)
	case ok:
		t.relationshipIssue = twin.relationshipIssue
	case o.snap.claims[id] > 1:
		// Two files claim the ID, so the snapshot answers for neither: stamped
		// as the ambiguous identity every claimant carries, the way the epic
		// branch above is — a mutation would otherwise hand back one claimant
		// reading ready while Get and List exclude both.
		t.relationshipIssue = duplicateIssue(id)
	}
}

// delete removes the resolved ticket, refusing while anything still names it
// or while the snapshot cannot say nothing does: a deleted parent orphans
// children in every namespace, a deleted dep leaves a blocker that can never
// clear.
func (o *op) delete(s *FileStore, path string) error {
	resolved := strings.TrimSuffix(filepath.Base(path), ".md")
	if !o.snap.Complete {
		return o.incompleteError(fmt.Sprintf("deleting %s", resolved))
	}
	// References land on the ID the file stores, which is how the snapshot
	// keyed it — a file renamed to `renamed.md` while keeping `id: original`
	// is `original` to every ticket naming it, and checking the filename
	// would find no referrer and orphan them. A file that yields no ticket of
	// this project is indexed nowhere, and is deleted as the file it is.
	id := FormatNamespacedID(s.Project, resolved)
	label := resolved
	if stored, err := s.readFile(path); err == nil && stored.ID != resolved {
		id = FormatNamespacedID(s.Project, stored.ID)
		label = fmt.Sprintf("%s (stored as %s)", resolved, stored.ID)
	}
	if inbound := o.snap.Inbound(id); len(inbound) > 0 {
		refs := make([]string, len(inbound))
		for i, r := range inbound {
			refs[i] = fmt.Sprintf("%s (%s)", r.From, r.Kind)
		}
		sort.Strings(refs)
		return fmt.Errorf("cannot delete %s: it is still referenced by %s. Remove those references first, or close the ticket instead",
			label, strings.Join(refs, ", "))
	}
	// Under the ticket's lock, keyed on the resolved file's own name the way
	// mutate keys it: an unlocked delete can land between updateLocked's read
	// and its rename, and the rename then recreates the ticket the delete
	// removed.
	release, err := s.lockTicket(resolved)
	if err != nil {
		return err
	}
	defer release()
	if err := os.Remove(path); err != nil {
		return err
	}
	s.logMutation(resolved, MutationDelete, nil)
	return nil
}

// closeChildren closes every non-terminal child of an abandoned epic through
// the boundary, in sorted qualified-ID order — the fixed order every
// multi-ticket write takes its ticket locks in. The children are the ones the
// abandon was decided against, from the same snapshot, so a second listing
// cannot disagree with the decision.
//
// Every child is attempted rather than stopping at the first failure, so the
// error can name what was closed and what was not — a partial cascade leaves
// the epic reading as its children imply, not as closed, and the rest have to
// be closed by hand. The IDs it closed come back either way, bare within the
// epic's own namespace: the abandon refused every non-terminal foreign child
// before this ran, so a local ID is the only kind there is to report.
func (o *op) closeChildren(epic *Ticket, children []*Ticket) ([]string, error) {
	sorted := append([]*Ticket(nil), children...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var closed, failed []string
	for _, child := range sorted {
		if isTerminal(child) {
			continue
		}
		ns, bare := ParseNamespacedID(child.ID)
		child.ID = bare
		child.Status = StatusClosed
		if err := o.closeChild(ns, child); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", bare, err))
			continue
		}
		closed = append(closed, bare)
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

func (o *op) closeChild(ns string, child *Ticket) error {
	store, err := o.store(ns)
	if err != nil {
		return err
	}
	release, err := store.lockTicket(child.ID)
	if err != nil {
		return err
	}
	defer release()
	return store.updateLocked(child)
}
