package ticket

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Snapshot.Revision: the pagination token ───────────────────────────────

func TestRevisionIsStableAndTracksEveryChange(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/child-0002", StatusOpen, "warp/epic-0001"),
	)
	revision := func() string {
		t.Helper()
		snap, err := ms.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return snap.Revision()
	}

	unchanged := revision()
	if len(unchanged) != 64 {
		t.Errorf("Revision = %q, want a sha256 hex digest", unchanged)
	}
	if again := revision(); again != unchanged {
		t.Errorf("two snapshots of an unchanged store differ: %s vs %s", unchanged, again)
	}

	if _, err := Mutate(ms, "warp/child-0002", func(t *Ticket) error {
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "edited"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	edited := revision()
	if edited == unchanged {
		t.Error("a file change did not change the revision")
	}

	mustCreate(t, ms, mk("loom/l-0003", StatusOpen))
	added := revision()
	if added == edited {
		t.Error("an added file did not change the revision")
	}

	if err := os.Remove(filepath.Join(root, "tickets", "loom", "l-0003.md")); err != nil {
		t.Fatal(err)
	}
	removed := revision()
	if removed == added {
		t.Error("a removed file did not change the revision")
	}
	if removed != edited {
		t.Error("removing the added file should restore the revision the store had before it")
	}

	if err := os.WriteFile(filepath.Join(root, "tickets", "warp", "bad.md"), []byte("---\nid: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if skipped := revision(); skipped == removed {
		t.Error("a changed skip set did not change the revision")
	}
}

// Activating cross-project parents moves foreign children into their epics
// without touching a ticket file, so the catalog line alone has to move the
// token: a page cut before the flip must not be continued after it.
func TestRevisionTracksCrossProjectActivation(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("loom/child-0002", StatusOpen, "warp/epic-0001"),
	)
	revision := func() string {
		t.Helper()
		snap, err := ms.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		return snap.Revision()
	}

	activated := revision()
	writeCatalog(t, root, "required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n  warp: {kind: project}\n  loom: {kind: project}\n  ticket: {kind: project}\n")
	deactivated := revision()
	if deactivated == activated {
		t.Error("flipping the catalog's cross-project-parents activation alone did not change the revision")
	}
	if again := revision(); again != deactivated {
		t.Errorf("two snapshots of the unchanged deactivated store differ: %s vs %s", deactivated, again)
	}
}

// ─── Snapshot.Progress: counts off the same snapshot as the status ─────────

func TestProgressCountsChildrenAcrossNamespaces(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/done-0002", StatusDone, "warp/epic-0001"),
		mkWithParent("warp/open-0003", StatusOpen, "warp/epic-0001"),
		mkWithParent("loom/ready-0004", StatusReady, "warp/epic-0001"),
		mkWithParent("loom/closed-0005", StatusClosed, "warp/epic-0001"),
		mkWithParent("_root/backlog-0006", StatusBacklog, "warp/epic-0001"),
		mk("warp/stray-0007", StatusOpen),
	)
	snap, err := ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := EpicProgress{Total: 5, Done: 1, Closed: 1, Open: 1, Ready: 1, Backlog: 1, Complete: true}
	if got := snap.Progress("warp/epic-0001"); !reflect.DeepEqual(got, want) {
		t.Errorf("Progress = %+v, want %+v", got, want)
	}
	if epic, _ := snap.Get("warp/epic-0001"); epic.Status != StatusOpen {
		t.Errorf("epic status = %s, want open — the status the counts imply", epic.Status)
	}
	if got := snap.Progress("warp/stray-0007"); !reflect.DeepEqual(got, EpicProgress{Complete: true}) {
		t.Errorf("Progress of a leaf = %+v, want zero counts", got)
	}

	// An unreadable file anywhere demotes the epic and the counts say why.
	if err := os.WriteFile(filepath.Join(root, "tickets", "loom", "bad.md"), []byte("---\nid: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, ms,
		mkEpic("loom/epic-0008", StatusBacklog, ""),
		mkWithParent("loom/d-0009", StatusDone, "loom/epic-0008"),
	)
	snap, err = ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	got := snap.Progress("loom/epic-0008")
	if got.Total != 1 || got.Done != 1 || got.Complete || len(got.Diagnostics) == 0 {
		t.Errorf("Progress over an incomplete snapshot = %+v, want the counts, complete=false and a diagnostic", got)
	}
	if epic, _ := snap.Get("loom/epic-0008"); epic.Status != StatusBacklog {
		t.Errorf("epic status = %s, want backlog: an incomplete snapshot certifies no epic done", epic.Status)
	}
}

// ─── Snapshot.Resolve: a typed ID to the exact qualified one ───────────────

func TestResolveTypedIDsAgainstTheSnapshot(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mk("warp/task-0002", StatusOpen),
		mk("warp/task-0003", StatusOpen),
		mk("loom/task-0002", StatusOpen),
		mk("warp/dup-0004", StatusOpen),
	)
	// A second file claiming dup-0004, and a catalogued namespace with no
	// directory: the two lookups a typed ID can land on that are neither a
	// ticket nor nothing.
	impostor := mk("warp/dup-0004", StatusDone)
	data, err := Serialize(impostor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tickets", "warp", "zz-dup-0004.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "tickets", "ticket")); err != nil {
		t.Fatal(err)
	}
	snap, err := ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, ns, input, want string
		wantErr               []string
	}{
		{"qualified exact", "warp", "warp/epic-0001", "warp/epic-0001", nil},
		{"qualified from another namespace", "loom", "warp/epic-0001", "warp/epic-0001", nil},
		{"bare exact in ns", "warp", "epic-0001", "warp/epic-0001", nil},
		{"bare names its own namespace, never another", "loom", "task-0002", "loom/task-0002", nil},
		{"fragment within ns alone", "warp", "0002", "warp/task-0002", nil},
		{"fragment ambiguous", "warp", "task", "", []string{"ambiguous", "warp/task-0002", "warp/task-0003"}},
		{"bare not found is not searched elsewhere", "loom", "epic", "", []string{"ticket loom/epic not found"}},
		{"qualified must be exact", "warp", "warp/epic", "", []string{"ticket warp/epic not found"}},
		{"qualified not found", "warp", "loom/nope-0009", "", []string{"ticket loom/nope-0009 not found"}},
		{"qualified into a failed namespace", "warp", "ticket/x-0001", "", []string{`namespace "ticket"`, "could not be read"}},
		{"bare in a failed namespace", "ticket", "x-0001", "", []string{`namespace "ticket"`, "could not be read"}},
		{"duplicate bare", "warp", "dup-0004", "", []string{duplicateIssue("warp/dup-0004")}},
		{"duplicate qualified", "warp", "warp/dup-0004", "", []string{duplicateIssue("warp/dup-0004")}},
		{"duplicate by fragment", "warp", "dup", "", []string{duplicateIssue("warp/dup-0004")}},
		{"empty", "warp", " ", "", []string{"id is required"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := snap.Resolve(tc.ns, tc.input)
			if tc.wantErr == nil {
				if err != nil || got != tc.want {
					t.Fatalf("Resolve(%q, %q) = %q, %v; want %q", tc.ns, tc.input, got, err, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Resolve(%q, %q) = %q, want an error", tc.ns, tc.input, got)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}

	// A store with no namespace resolves a bare input as it is.
	single := NewFileStore(t.TempDir())
	mustCreate(t, single, mk("s-0001", StatusOpen))
	snap, err = single.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"s-0001", "0001"} {
		if got, err := snap.Resolve("", input); err != nil || got != "s-0001" {
			t.Errorf("single-store Resolve(%q) = %q, %v; want s-0001", input, got, err)
		}
	}
}

// ─── WithStoreLock: the boundary's lock, held from outside a write ─────────

func TestWithStoreLockExcludesAConcurrentWrite(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms, mk("warp/w-0001", StatusOpen))

	// The lock is the one every store entry point takes, so a caller holding
	// it holds the store: a write started meanwhile lands only once fn
	// returns.
	fromRoot, err := (&central{root: root, ticketsDir: filepath.Join(root, ticketsDirName)}).lockPath()
	if err != nil {
		t.Fatal(err)
	}
	if fromStore, _ := centralForMulti(ms).lockPath(); fromStore != fromRoot {
		t.Errorf("WithStoreLock keys on %s, the store on %s", fromRoot, fromStore)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var orderMu sync.Mutex
	var order []string
	record := func(step string) {
		orderMu.Lock()
		order = append(order, step)
		orderMu.Unlock()
	}
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- WithStoreLock(root, true, func() error {
			close(entered)
			<-release
			record("lock")
			return nil
		})
	}()
	<-entered

	writeDone := make(chan error, 1)
	go func() {
		_, err := Mutate(ms, "warp/w-0001", func(t *Ticket) error { t.Priority = 0; return nil })
		record("write")
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("the write landed (%v) while WithStoreLock held the store", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-lockDone; err != nil {
		t.Fatalf("WithStoreLock: %v", err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Mutate after the lock was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the write did not land after WithStoreLock returned")
	}
	if !reflect.DeepEqual(order, []string{"lock", "write"}) {
		t.Errorf("order = %v, want the write after fn returned", order)
	}
	if got := mustGet(t, ms, "warp/w-0001").Priority; got != 0 {
		t.Errorf("Priority = %d, want the write to have landed", got)
	}
}

// ─── ImportAll: a batch of legacy files through the boundary ───────────────

func TestImportAllValidatesTheBatchAsAWholeAndWritesOnce(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, warp, mk("kept-0001", StatusOpen))

	// The child precedes its epic in the batch, the epic carries a legacy
	// stored status nothing derives, and one ID is already the store's.
	batch := []*Ticket{
		mkWithParent("child-0002", StatusOpen, "epic-0003"),
		mkEpic("epic-0003", StatusDone, ""),
		mk("kept-0001", StatusDone),
	}
	imported, skipped, err := warp.ImportAll(batch)
	if err != nil {
		t.Fatalf("ImportAll: %v", err)
	}
	if !reflect.DeepEqual(imported, []string{"child-0002", "epic-0003"}) || !reflect.DeepEqual(skipped, []string{"kept-0001"}) {
		t.Errorf("imported = %v, skipped = %v", imported, skipped)
	}
	if got := mustGet(t, ms, "warp/child-0002").Parent; got != "warp/epic-0003" {
		t.Errorf("child parent = %q, want the epic beside it in the batch, canonicalized", got)
	}
	raw, err := os.ReadFile(filepath.Join(warp.Dir, "epic-0003.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "status: open") || strings.Contains(string(raw), "status: done") {
		t.Errorf("epic stored with a status other than the derived one:\n%s", raw)
	}
	if got := statusOf(t, ms, "warp/kept-0001"); got != StatusOpen {
		t.Errorf("kept-0001 = %s, want the central copy left as it was", got)
	}

	// One invalid ticket refuses the whole batch with nothing written: a
	// parent that does not resolve, and a child that waits for its own epic.
	for reason, bad := range map[string][]*Ticket{
		"nope-9999 not found":   {mk("ok-0004", StatusOpen), mkWithParent("bad-0005", StatusOpen, "nope-9999")},
		"would wait for itself": {mkEpic("e-0006", StatusBacklog, ""), mkWithParent("c-0007", StatusOpen, "e-0006", "e-0006")},
	} {
		before := snapshotTree(t, filepath.Join(root, "tickets"))
		imported, skipped, err := warp.ImportAll(bad)
		if err == nil {
			t.Fatalf("%s: ImportAll = %v, %v, nil; want a refusal", reason, imported, skipped)
		}
		for _, want := range []string{bad[1].ID, reason} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q does not contain %q", err, want)
			}
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	}
}

// Two legacy parents spelled as fragments place neither child in a graph built
// from the batch as written, and a cycle through both — each child waits for
// the other's epic — would pass a check made per ticket over that graph. The
// batch is canonicalized first and checked against the graph it then forms.
func TestImportAllRefusesACycleThroughFragmentParents(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")

	before := snapshotTree(t, filepath.Join(root, "tickets"))
	imported, skipped, err := warp.ImportAll([]*Ticket{
		mkEpic("epic-a111", StatusBacklog, ""),
		mkEpic("epic-b222", StatusBacklog, ""),
		mkWithParent("child-0001", StatusOpen, "a111", "epic-b222"),
		mkWithParent("child-0002", StatusOpen, "b222", "epic-a111"),
	})
	if err == nil {
		t.Fatalf("ImportAll = %v, %v, nil; want the cycle refused", imported, skipped)
	}
	if !strings.Contains(err.Error(), "would wait for itself") {
		t.Errorf("refusal %q does not name the cycle", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// The same fragments without the cycle import, stored as the exact epics
	// they resolved to.
	imported, _, err = warp.ImportAll([]*Ticket{
		mkEpic("epic-a111", StatusBacklog, ""),
		mkWithParent("child-0001", StatusOpen, "a111"),
	})
	if err != nil {
		t.Fatalf("ImportAll: %v", err)
	}
	if !reflect.DeepEqual(imported, []string{"epic-a111", "child-0001"}) {
		t.Errorf("imported = %v", imported)
	}
	if got := mustGet(t, ms, "warp/child-0001").Parent; got != "warp/epic-a111" {
		t.Errorf("child parent = %q, want the fragment canonicalized", got)
	}
}

// A symlinked namespace is left out of readSources, so an import into one
// would see no target and write through s.Dir to the link's target outside the
// store. The destination has to pass the same lstat every other write's does.
func TestImportAllRefusesASymlinkedNamespace(t *testing.T) {
	root, _ := centralFixture(t, true)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "tickets", "linked")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	captureWarnings(t)

	linked := nsStore(root, "linked")
	imported, skipped, err := linked.ImportAll([]*Ticket{mk("l-0001", StatusOpen)})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ImportAll = %v, %v, %v; want the not-a-directory refusal", imported, skipped, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused import wrote through the link: %v", entries)
	}
}

func TestImportAllIsRefusedByTheCatalogGuard(t *testing.T) {
	root := catalogRoot(t)
	writeCatalog(t, root, "required_features: [time-travel]\nnamespaces: {}\n")
	warp := nsStore(root, "warp")
	_, _, err := warp.ImportAll([]*Ticket{mk("w-0001", StatusOpen)})
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Fatalf("ImportAll = %v, want the catalog guard's refusal", err)
	}
	if _, statErr := os.Stat(warp.Dir); !os.IsNotExist(statErr) {
		t.Errorf("a refused import created %s", warp.Dir)
	}
}
