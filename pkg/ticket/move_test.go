package ticket

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// moveStores is a source and a destination project in one central store —
// the only shape a move runs in, since it holds the source's store lock across
// both ends. The destination directory is not made: a registered project that
// has never held a ticket has none, and the create makes it.
func moveStores(t *testing.T) (src, dst *FileStore) {
	t.Helper()
	root := catalogRoot(t)
	src = NewProjectFileStore(mkNamespaceDir(t, root, "mv-src"), "mv-src")
	dst = NewProjectFileStore(filepath.Join(root, "tickets", "mv-dst"), "mv-dst")
	return src, dst
}

func TestMoveTicketPreservesAllFields(t *testing.T) {
	src, dst := moveStores(t)

	original := &Ticket{
		ID:          "test-ticket-1234",
		Status:      StatusReady,
		Type:        TypeFeature,
		Priority:    1,
		Tags:        []string{"frontend", "urgent"},
		ExternalRef: "GH-42",
		Branch:      "feature/foo",
		Created:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:       "Test ticket with all fields",
		Body:        "Some body text.",
		Notes:       []Note{{Timestamp: time.Now().UTC(), Text: "initial note"}},
		Deps:        []string{},
		Links:       []string{},
	}

	if err := src.Create(original); err != nil {
		t.Fatalf("create source ticket: %v", err)
	}

	results, err := MoveTicket(src, dst, original.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	newID := results[0].NewID

	// Read the moved ticket from dst.
	moved, err := dst.Get(newID)
	if err != nil {
		t.Fatalf("get moved ticket: %v", err)
	}

	// Fields that should be preserved as-is.
	if moved.Type != TypeFeature {
		t.Errorf("Type: got %q, want %q", moved.Type, TypeFeature)
	}
	if moved.Priority != 1 {
		t.Errorf("Priority: got %d, want 1", moved.Priority)
	}
	if moved.ExternalRef != "GH-42" {
		t.Errorf("ExternalRef: got %q, want %q", moved.ExternalRef, "GH-42")
	}
	if moved.Branch != "feature/foo" {
		t.Errorf("Branch: got %q, want %q", moved.Branch, "feature/foo")
	}
	if len(moved.Tags) != 2 || moved.Tags[0] != "frontend" || moved.Tags[1] != "urgent" {
		t.Errorf("Tags: got %v, want [frontend urgent]", moved.Tags)
	}
	// Fields that should be reset.
	if moved.Status != StatusBacklog {
		t.Errorf("Status: got %q, want %q (should reset to backlog)", moved.Status, StatusBacklog)
	}

	// Should have provenance note.
	foundProvenance := false
	for _, n := range moved.Notes {
		if len(n.Text) > 10 && n.Text[:10] == "Moved from" {
			foundProvenance = true
		}
	}
	if !foundProvenance {
		t.Error("missing provenance note on moved ticket")
	}

	// Original should be closed — it left, it did not complete here.
	orig, err := src.Get(original.ID)
	if err != nil {
		t.Fatalf("get original: %v", err)
	}
	if orig.Status != StatusClosed {
		t.Errorf("original status: got %q, want %q", orig.Status, StatusClosed)
	}
}

func TestMoveTicketCreatesFileInBothDirs(t *testing.T) {
	src, dst := moveStores(t)

	original := &Ticket{
		ID:       "iso-test-abcd",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Created:  time.Now().UTC(),
		Title:    "Isolation test",
		Body:     "",
		Tags:     []string{"alpha"},
		Deps:     []string{},
		Links:    []string{},
		Notes:    []Note{{Timestamp: time.Now().UTC(), Text: "original note"}},
	}

	if err := src.Create(original); err != nil {
		t.Fatalf("create: %v", err)
	}

	results, err := MoveTicket(src, dst, original.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	// Verify one file exists in each directory.
	dstFiles, _ := filepath.Glob(filepath.Join(dst.Dir, "*.md"))
	srcFiles, _ := filepath.Glob(filepath.Join(src.Dir, "*.md"))
	if len(dstFiles) != 1 || len(srcFiles) != 1 {
		t.Errorf("expected 1 file in each dir, got dst=%d src=%d", len(dstFiles), len(srcFiles))
	}

	// Verify dst ticket has the tag from original.
	moved, err := dst.Get(results[0].NewID)
	if err != nil {
		t.Fatalf("get moved: %v", err)
	}
	if len(moved.Tags) != 1 || moved.Tags[0] != "alpha" {
		t.Errorf("Tags: got %v, want [alpha]", moved.Tags)
	}
}

// A ticket with a dep, and the cargo on it, does not move: the copy would
// either carry a dep on a ticket in another project's plan or drop the edge,
// and the original would close with the cargo unresolved. Refused before any
// write, naming the dep.
func TestMoveRefusesATicketWithDepsAndCargo(t *testing.T) {
	src, dst := moveStores(t)

	parent := &Ticket{
		ID:       "cargo-parent-0001",
		Status:   StatusBacklog,
		Type:     TypeEpic,
		Priority: 2,
		Title:    "Cargo parent",
		Deps:     []string{},
		Links:    []string{},
	}
	child := &Ticket{
		ID:       "cargo-child-0002",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Title:    "Cargo child",
		Deps:     []string{"outside-9999"},
		Links:    []string{},
		DepCargo: map[string]string{"outside-9999": "migration doc"},
	}
	if err := src.Create(parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if err := src.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}
	before := snapshotTree(t, filepath.Dir(src.Dir))

	results, err := MoveTicket(src, dst, child.ID, false)
	if err == nil || !strings.Contains(err.Error(), "dep mv-src/outside-9999") {
		t.Fatalf("MoveTicket = %v, want a refusal naming the dep", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))

	// The source ticket's map must not have been touched.
	orig, err := src.Get(child.ID)
	if err != nil {
		t.Fatalf("Get source child: %v", err)
	}
	if len(orig.DepCargo) != 1 || orig.DepCargo["mv-src/outside-9999"] != "migration doc" {
		t.Errorf("source DepCargo = %v, want the original entry under its qualified key", orig.DepCargo)
	}
}

func TestMoveLeavesVerdictsBehind(t *testing.T) {
	src, dst := moveStores(t)

	original := &Ticket{
		ID:       "verd-move-0001",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Title:    "Verdict move",
		Deps:     []string{},
		Links:    []string{},
	}
	if err := src.Create(original); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := RecordVerdict(src, original.ID, shaA, VerdictTestVerified, VerdictRoleWorker, "go test ./...", "worker-1"); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}

	results, err := MoveTicket(src, dst, original.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	moved, err := dst.Get(results[0].NewID)
	if err != nil {
		t.Fatalf("Get moved: %v", err)
	}
	if len(moved.Verdicts) != 0 {
		t.Errorf("moved verdicts = %+v, want none: the rows judged the source repo's commits", moved.Verdicts)
	}

	orig, err := src.Get(original.ID)
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if len(orig.Verdicts) != 1 || orig.Verdicts[0].SHA != shaA {
		t.Errorf("source verdicts = %+v, want the recorded row kept", orig.Verdicts)
	}
}

// A recursive move is refused as a whole, whatever the tree looks like: the
// move copies under new IDs and closes originals, and an epic's children would
// be left naming a closed copy. Nothing is written.
func TestMoveRefusesRecursive(t *testing.T) {
	src, dst := moveStores(t)

	mkMovable(t, src, "mv-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "mv-bare-0002", TypeFeature, StatusClosed, "mv-epic-0001")
	mkMovable(t, src, "mv-ns-0003", TypeFeature, StatusOpen, "mv-src/mv-epic-0001")
	before := snapshotTree(t, filepath.Dir(src.Dir))

	results, err := MoveTicket(src, dst, "mv-epic-0001", true)
	if err == nil || !strings.Contains(err.Error(), "recursive moves are refused") {
		t.Fatalf("MoveTicket = %v, want the recursive refusal", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))
	if _, err := os.Stat(dst.Dir); !os.IsNotExist(err) {
		t.Errorf("the refusal created the destination directory (stat err: %v)", err)
	}
}

// An epic does not move on its own either: its children would be left naming
// a closed copy in the source.
func TestMoveRefusesAnEpic(t *testing.T) {
	src, dst := moveStores(t)

	mkMovable(t, src, "nr-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "nr-open-0002", TypeFeature, StatusOpen, "nr-epic-0001")
	before := snapshotTree(t, filepath.Dir(src.Dir))

	_, err := MoveTicket(src, dst, "nr-epic-0001", false)
	if err == nil || !strings.Contains(err.Error(), "is an epic and cannot move") {
		t.Fatalf("MoveTicket = %v, want the epic refusal", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))

	// A childless epic is refused too: the rule is the type, not the tree.
	mkMovable(t, src, "lone-epic-0003", TypeEpic, StatusBacklog, "")
	if _, err := MoveTicket(src, dst, "lone-epic-0003", false); err == nil {
		t.Error("a childless epic moved")
	}
}

// A ticket related to any other — by its own parent, dep or link, or by another
// ticket naming it — is refused, naming the relationship, with nothing written.
func TestMoveRefusesARelatedTicket(t *testing.T) {
	src, dst := moveStores(t)

	mkMovable(t, src, "rel-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "rel-child-0002", TypeFeature, StatusOpen, "rel-epic-0001")
	mkMovable(t, src, "rel-blocker-0003", TypeFeature, StatusOpen, "")
	mkMovable(t, src, "rel-waiter-0004", TypeFeature, StatusOpen, "")
	if _, err := Mutate(src, "rel-waiter-0004", func(t *Ticket) error { return AddDep(t, "rel-blocker-0003") }); err != nil {
		t.Fatal(err)
	}
	mkMovable(t, src, "rel-link-0005", TypeFeature, StatusOpen, "")
	mkMovable(t, src, "rel-linked-0006", TypeFeature, StatusOpen, "")
	linked, _ := src.Get("rel-linked-0006")
	if _, err := Mutate(src, "rel-link-0005", func(t *Ticket) error { AddLink(t, linked); return nil }); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, filepath.Dir(src.Dir))

	cases := map[string]string{
		"rel-child-0002":   "parent mv-src/rel-epic-0001",
		"rel-waiter-0004":  "dep mv-src/rel-blocker-0003",
		"rel-blocker-0003": "dep of mv-src/rel-waiter-0004",
		"rel-link-0005":    "link mv-src/rel-linked-0006",
		"rel-linked-0006":  "link of mv-src/rel-link-0005",
	}
	for id, want := range cases {
		results, err := MoveTicket(src, dst, id, false)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("MoveTicket(%s) = %v, want a refusal naming %q", id, err, want)
		}
		if len(results) != 0 {
			t.Errorf("MoveTicket(%s) results = %v, want none", id, results)
		}
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))
}

// References land on the ID a file stores, not on its name, and so does the
// close that records the move: a file renamed while keeping its id is still
// that ticket to everything naming it, and moving it by the new name has to
// find those referrers — and, once nothing names it, is still refused, since
// the source cannot be closed under a name no file holds.
func TestMoveRefusesARenamedFile(t *testing.T) {
	src, dst := moveStores(t)
	mkMovable(t, src, "ren-blocker-0001", TypeFeature, StatusOpen, "")
	mkMovable(t, src, "ren-waiter-0002", TypeFeature, StatusOpen, "")
	if _, err := Mutate(src, "ren-waiter-0002", func(t *Ticket) error { return AddDep(t, "ren-blocker-0001") }); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(src.Dir, "ren-blocker-0001.md"), filepath.Join(src.Dir, "renamed-0003.md")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, filepath.Dir(src.Dir))

	results, err := MoveTicket(src, dst, "renamed-0003", false)
	if err == nil || !strings.Contains(err.Error(), "dep of mv-src/ren-waiter-0002") {
		t.Errorf("moving a renamed, referenced leaf: want a refusal naming the referrer of the stored id, got %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))

	// Unreferenced, the mismatch alone refuses it.
	if _, err := Mutate(src, "ren-waiter-0002", func(t *Ticket) error { RemoveDep(t, "mv-src/ren-blocker-0001"); return nil }); err != nil {
		t.Fatal(err)
	}
	before = snapshotTree(t, filepath.Dir(src.Dir))
	results, err = MoveTicket(src, dst, "renamed-0003", false)
	if err == nil || !strings.Contains(err.Error(), "stored as ren-blocker-0001") {
		t.Errorf("moving a renamed leaf: want a refusal naming the stored id, got %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", results)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))
}

// A move needs the whole store read: an unreadable file anywhere in it could
// be a ticket naming the one that is moving.
func TestMoveRefusesOnAnIncompleteSnapshot(t *testing.T) {
	src, dst := moveStores(t)
	mkMovable(t, src, "inc-move-0001", TypeFeature, StatusOpen, "")
	other := mkNamespaceDir(t, filepath.Dir(filepath.Dir(src.Dir)), "other")
	plantUnreadable(t, other, "broken-9999.md")
	before := snapshotTree(t, filepath.Dir(src.Dir))

	_, err := MoveTicket(src, dst, "inc-move-0001", false)
	if err == nil || !strings.Contains(err.Error(), "broken-9999.md") {
		t.Fatalf("MoveTicket = %v, want a refusal naming the unreadable file", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Dir(src.Dir)))
}

func TestMoveClosesTheTicketThatLeft(t *testing.T) {
	// A ticket that moves away is closed in the source, not done: it did not
	// complete here, it left.
	src, dst := moveStores(t)
	mkMovable(t, src, "stay-leaf-0002", TypeFeature, StatusOpen, "")

	if _, err := MoveTicket(src, dst, "stay-leaf-0002", false); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	left, err := src.Get("stay-leaf-0002")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if left.Status != StatusClosed {
		t.Errorf("ticket left behind = %q, want %q", left.Status, StatusClosed)
	}
}

func TestMovePartialFailureReportsWhatLanded(t *testing.T) {
	// The move is not atomic. When the source write fails after the target
	// write, the caller needs the name of the target copy whose source is still
	// open — that pair is what reconciling needs.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	src, dst := moveStores(t)
	mkMovable(t, src, "part-leaf-0002", TypeFeature, StatusOpen, "")

	file := filepath.Join(src.Dir, "part-leaf-0002.md")
	if err := os.Chmod(file, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(file, 0o644) })

	results, moveErr := MoveTicket(src, dst, "part-leaf-0002", false)
	if moveErr == nil {
		t.Fatal("MoveTicket succeeded, want a failure closing the read-only source")
	}
	if len(results) != 0 {
		t.Fatalf("completed = %v, want none — the source never closed", results)
	}

	left, err := src.Get("part-leaf-0002")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if left.Status != StatusOpen {
		t.Fatalf("source = %q, want %q — the write was supposed to fail", left.Status, StatusOpen)
	}

	dstTickets, err := dst.List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 1 {
		t.Fatalf("target holds %d tickets, want the orphaned copy: %v", len(dstTickets), ids(dstTickets))
	}
	orphan := qualifyForStore(dst, dstTickets[0].ID)
	if !strings.Contains(moveErr.Error(), orphan) {
		t.Errorf("error %q does not name %s, the target copy left behind", moveErr, orphan)
	}
	if !strings.Contains(moveErr.Error(), "part-leaf-0002") {
		t.Errorf("error %q does not name the source ticket left open", moveErr)
	}
}

// A destination that resolves to the source store is a rename, not a move: the
// ticket would lose the ID other tickets and commit messages reference, and land
// back where it started.
func TestMoveRefusesADestinationThatIsTheSourceStore(t *testing.T) {
	dir := t.TempDir()
	src := NewProjectFileStore(dir, "self")
	dst := NewProjectFileStore(dir, "self")

	mkMovable(t, src, "self-move-0001", TypeFeature, StatusOpen, "")

	results, err := MoveTicket(src, dst, "self-move-0001", false)
	if err == nil {
		t.Fatal("MoveTicket succeeded, want a refusal")
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none written", results)
	}
	for _, want := range []string{"project self", "no move performed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	orig, err := src.Get("self-move-0001")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if orig.Status != StatusOpen {
		t.Errorf("source status = %q, want %q — nothing was moved", orig.Status, StatusOpen)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("store holds %v, want only the original", files)
	}
}

// A move writes through the boundary's helpers rather than through Create and
// Update, so it makes the catalog guard those entry points make: Root before
// activation and a catalog requiring a feature this binary lacks both refuse
// the move, whichever end they are, and leave both stores as they were.
func TestMoveRefusesWhatTheCatalogGuardRefuses(t *testing.T) {
	t.Run("root not activated", func(t *testing.T) {
		src, _ := moveStores(t)
		mkMovable(t, src, "to-root-0001", TypeFeature, StatusOpen, "")
		rootNS := NewProjectFileStore(mkNamespaceDir(t, filepath.Dir(filepath.Dir(src.Dir)), project.RootNamespace), project.RootNamespace)
		writeLegacy(t, rootNS, movable("from-root-0002", TypeFeature, StatusOpen, ""))
		tickets := filepath.Dir(src.Dir)
		before := snapshotTree(t, tickets)

		results, err := MoveTicket(src, rootNS, "to-root-0001", false)
		if !errors.Is(err, ErrRootNotActivated) {
			t.Errorf("move into Root: want ErrRootNotActivated, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("move into Root: results = %v, want none", results)
		}
		results, err = MoveTicket(rootNS, src, "from-root-0002", false)
		if !errors.Is(err, ErrRootNotActivated) {
			t.Errorf("move out of Root: want ErrRootNotActivated, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("move out of Root: results = %v, want none", results)
		}
		assertTreeUnchanged(t, before, snapshotTree(t, tickets))
	})
	t.Run("unsupported feature", func(t *testing.T) {
		src, dst := moveStores(t)
		mkMovable(t, src, "stuck-0001", TypeFeature, StatusOpen, "")
		root := filepath.Dir(filepath.Dir(src.Dir))
		writeCatalog(t, root, "required_features: [root-namespace, time-travel]\nnamespaces:\n  mv-src: {kind: project}\n  mv-dst: {kind: project}\n")
		before := snapshotTree(t, filepath.Join(root, "tickets"))

		results, err := MoveTicket(src, dst, "stuck-0001", false)
		var unsupported *UnsupportedFeatureError
		if !errors.As(err, &unsupported) {
			t.Errorf("want UnsupportedFeatureError, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("results = %v, want none", results)
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	})
}

// Two stores that do not share a central store cannot be covered by one hold
// of the store lock, so the move is refused before anything is read.
func TestMoveRefusesADestinationInAnotherCentralStore(t *testing.T) {
	src := NewProjectFileStore(t.TempDir(), "alpha")
	dst := NewProjectFileStore(t.TempDir(), "beta")
	mkMovable(t, src, "apart-0001", TypeFeature, StatusOpen, "")

	_, err := MoveTicket(src, dst, "apart-0001", false)
	if err == nil || !strings.Contains(err.Error(), "not in the same central store") {
		t.Fatalf("MoveTicket = %v, want a refusal naming the store mismatch", err)
	}
	if orig, _ := src.Get("apart-0001"); orig.Status != StatusOpen {
		t.Errorf("source status = %q, want %q — nothing was moved", orig.Status, StatusOpen)
	}
}

// Directory identity, not the string the caller passed: /tmp and /var are
// symlinks on macOS, so the same store legitimately arrives spelled two ways.
func TestMoveRefusesTheSourceStoreReachedByAnotherSpelling(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	src := NewProjectFileStore(dir, "spelling")
	dst := NewProjectFileStore(link, "spelling")

	mkMovable(t, src, "spell-move-0001", TypeFeature, StatusOpen, "")

	if _, err := MoveTicket(src, dst, "spell-move-0001", false); err == nil {
		t.Fatal("MoveTicket succeeded, want a refusal — the link names the source store")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("store holds %v, want only the original", files)
	}
}

// A case variant is another spelling of the same directory on a case-insensitive
// filesystem — APFS by default, so this is the everyday macOS case. It is
// reachable: a repo-owned .tickets/ store's directory comes from walking the path
// the user typed, so `tk move <id> ../proj` from /Users/me/code/Proj arrives
// spelled differently from the store it names. EvalSymlinks resolves links but
// does not fold case, so only stat identity catches it.
func TestMoveRefusesTheSourceStoreSpelledInAnotherCase(t *testing.T) {
	parent := t.TempDir()
	upper := filepath.Join(parent, "Store")
	if err := os.MkdirAll(upper, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lower := filepath.Join(parent, "store")
	if !sameDirOnDisk(t, upper, lower) {
		t.Skip("case-sensitive filesystem: the two spellings name different directories")
	}

	src := NewProjectFileStore(upper, "casing")
	dst := NewProjectFileStore(lower, "casing")

	mkMovable(t, src, "case-move-0001", TypeFeature, StatusOpen, "")

	if _, err := MoveTicket(src, dst, "case-move-0001", false); err == nil {
		t.Fatal("MoveTicket succeeded, want a refusal — the two spellings name one directory")
	}
	orig, err := src.Get("case-move-0001")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if orig.Status != StatusOpen {
		t.Errorf("source status = %q, want %q — nothing was moved", orig.Status, StatusOpen)
	}
	files, err := filepath.Glob(filepath.Join(upper, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("store holds %v, want only the original", files)
	}
}

// sameDirOnDisk reports whether two paths name one directory, so a test that
// assumes a case-insensitive filesystem can skip rather than fail on one that
// is not.
func sameDirOnDisk(t *testing.T, a, b string) bool {
	t.Helper()
	fiA, err := os.Stat(a)
	if err != nil {
		t.Fatalf("Stat %s: %v", a, err)
	}
	fiB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fiA, fiB)
}

// The guard resolves symlinks best effort: a registered central project that has
// never held a ticket has no directory until the create makes one, and
// EvalSymlinks fails on a path that does not exist.
func TestMoveIntoAProjectThatHasNeverHeldATicket(t *testing.T) {
	src, dst := moveStores(t)
	mkMovable(t, src, "unused-move-0001", TypeFeature, StatusOpen, "")

	results, err := MoveTicket(src, dst, "unused-move-0001", false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %v, want the one move", results)
	}
	if _, err := dst.Get(results[0].NewID); err != nil {
		t.Fatalf("Get moved: %v", err)
	}
}

func movable(id string, typ TicketType, status Status, parent string) *Ticket {
	return &Ticket{
		ID: id, Status: status, Type: typ, Priority: 2, Parent: parent,
		Created: time.Now(), Title: "Item " + id, Body: "\n",
		Deps: []string{}, Links: []string{},
	}
}

func mkMovable(t *testing.T, store *FileStore, id string, typ TicketType, status Status, parent string) {
	t.Helper()
	if err := store.Create(movable(id, typ, status, parent)); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func TestMovePreservesCreated(t *testing.T) {
	src, dst := moveStores(t)

	original := &Ticket{
		ID:       "keep-created-0001",
		Status:   StatusBacklog,
		Type:     TypeFeature,
		Priority: 2,
		Title:    "Keep",
		Deps:     []string{},
		Links:    []string{},
	}
	if err := src.Create(original); err != nil {
		t.Fatalf("Create: %v", err)
	}
	orig, _ := src.Get("keep-created-0001")

	time.Sleep(10 * time.Millisecond)
	results, err := MoveTicket(src, dst, "keep-created-0001", false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	moved, err := dst.Get(results[0].NewID)
	if err != nil {
		t.Fatalf("Get moved: %v", err)
	}
	if !moved.Created.Equal(orig.Created) {
		t.Errorf("Created not preserved on move: was %v, now %v", orig.Created, moved.Created)
	}
}

// The destination file is new and the body was stripped at parse time, so the
// copy's write drops nothing — but the drop count rides the shallow copy unless
// it is reset, and a warning asserting content loss where none occurred is the
// failure the warning exists to fix.
func TestMoveWarnsOnlyForTheSourceReviewLogDrop(t *testing.T) {
	src, dst := moveStores(t)

	legacy := &Ticket{
		ID:       "rlog-move-0001",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Title:    "Review log move",
		Body:     "\nDescription.\n\n## Review Log\n\n**2026-02-25T12:00:00Z [agent:design-reviewer]**\nAPPROVED\n",
		Deps:     []string{},
		Links:    []string{},
	}
	plantTicketFile(t, src.Dir, legacy.ID+".md", legacy)

	warnings := captureWarnings(t)

	results, err := MoveTicket(src, dst, legacy.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	if len(*warnings) != 1 {
		t.Fatalf("the move produced %d warning(s), want 1: %v", len(*warnings), *warnings)
	}
	warning := (*warnings)[0]
	if !strings.Contains(warning, "mv-src/"+legacy.ID) {
		t.Errorf("the warning does not name the source ticket the section left: %q", warning)
	}
	_, bareNew := ParseNamespacedID(results[0].NewID)
	if strings.Contains(warning, bareNew) {
		t.Errorf("the warning names the destination copy, which dropped nothing: %q", warning)
	}
}
