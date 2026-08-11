package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMoveTicketPreservesAllFields(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := &FileStore{Dir: srcDir}
	dst := &FileStore{Dir: dstDir}

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
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := &FileStore{Dir: srcDir}
	dst := &FileStore{Dir: dstDir}

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
	dstFiles, _ := filepath.Glob(filepath.Join(dstDir, "*.md"))
	srcFiles, _ := filepath.Glob(filepath.Join(srcDir, "*.md"))
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

func TestMoveRemapsDepCargo(t *testing.T) {
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

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
		Parent:   parent.ID,
		Title:    "Cargo child",
		Deps:     []string{parent.ID, "outside-9999"},
		Links:    []string{},
		DepCargo: map[string]string{
			parent.ID:      "event schema",
			"outside-9999": "migration doc",
		},
	}
	if err := src.Create(parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if err := src.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	results, err := MoveTicket(src, dst, parent.ID, true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	newParent, newChild := results[0].NewID, results[1].NewID

	moved, err := dst.Get(newChild)
	if err != nil {
		t.Fatalf("Get moved child: %v", err)
	}
	if len(moved.DepCargo) != 1 {
		t.Fatalf("DepCargo = %v, want only the surviving dep", moved.DepCargo)
	}
	if moved.DepCargo[newParent] != "event schema" {
		t.Errorf("DepCargo[%s] = %q, want event schema", newParent, moved.DepCargo[newParent])
	}

	// The source ticket's map must not have been aliased and mutated.
	orig, err := src.Get(child.ID)
	if err != nil {
		t.Fatalf("Get source child: %v", err)
	}
	if len(orig.DepCargo) != 2 || orig.DepCargo[parent.ID] != "event schema" || orig.DepCargo["outside-9999"] != "migration doc" {
		t.Errorf("source DepCargo = %v, want the original two entries", orig.DepCargo)
	}
}

func TestMoveRecursiveCollectsNamespacedParentDescendants(t *testing.T) {
	// The central store records a child's parent namespaced; tickets written
	// before the namespacing rollout record it bare. A recursive move must
	// carry both forms.
	src := &FileStore{Dir: t.TempDir(), Project: "proj"}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "mv-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "mv-bare-0002", TypeFeature, StatusClosed, "mv-epic-0001")
	mkMovable(t, src, "mv-ns-0003", TypeFeature, StatusClosed, "proj/mv-epic-0001")

	results, err := MoveTicket(src, dst, "mv-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	moved := map[string]string{}
	for _, r := range results {
		moved[r.OldID] = r.NewID
	}
	for _, id := range []string{"mv-epic-0001", "mv-bare-0002", "mv-ns-0003"} {
		if moved[id] == "" {
			t.Fatalf("%s was left behind, moved set = %v", id, moved)
		}
	}

	// Carrying the tickets is only half the move — a namespaced parent must
	// remap to the moving epic's new ID, not be dropped as if it stayed.
	wantParent := map[string]string{
		"mv-epic-0001": "",
		"mv-bare-0002": moved["mv-epic-0001"],
		"mv-ns-0003":   moved["mv-epic-0001"],
	}
	for oldID, want := range wantParent {
		got, err := dst.Get(moved[oldID])
		if err != nil {
			t.Fatalf("Get moved %s: %v", oldID, err)
		}
		if got.Parent != want {
			t.Errorf("%s moved with parent %q, want %q", oldID, got.Parent, want)
		}
	}

	dstTickets, err := dst.List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 3 {
		t.Errorf("target holds %d tickets, want 3", len(dstTickets))
	}
}

func TestMoveRecursiveSkipsForeignProjectChild(t *testing.T) {
	// A child whose parent names a different project is not this store's child,
	// even when the bare IDs match. Sweeping it into the move would set the
	// wrong ticket done and copy it into the target repo — the mis-resolution
	// FileStore.Resolve rejects for the same reason.
	src := &FileStore{Dir: t.TempDir(), Project: "proj"}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "mv-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "mv-own-0002", TypeFeature, StatusClosed, "proj/mv-epic-0001")
	// A parent in another project is refused on write; this one predates that
	// rule, and the walk still has to leave it alone.
	writeLegacy(t, src, movable("mv-foreign-0003", TypeFeature, StatusClosed, "otherproj/mv-epic-0001"))

	results, err := MoveTicket(src, dst, "mv-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	var movedIDs []string
	for _, r := range results {
		movedIDs = append(movedIDs, r.OldID)
	}
	if len(movedIDs) != 2 || movedIDs[0] != "mv-epic-0001" || movedIDs[1] != "mv-own-0002" {
		t.Fatalf("moved %v, want [mv-epic-0001 mv-own-0002] — the foreign child is not this epic's", movedIDs)
	}

	// The foreign child must be untouched: the move would flip it to done.
	foreign, err := src.Get("mv-foreign-0003")
	if err != nil {
		t.Fatalf("Get foreign child: %v", err)
	}
	if foreign.Status != StatusClosed {
		t.Errorf("foreign child status = %q, want %q — it was swept into the move", foreign.Status, StatusClosed)
	}

	dstTickets, err := dst.List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 2 {
		t.Errorf("target holds %d tickets, want 2: %v", len(dstTickets), ids(dstTickets))
	}
}

func TestMoveRemapsNamespacedDepsAndLinks(t *testing.T) {
	// A dep or link on a ticket that is also moving must be remapped, not
	// reported as stripped, when it is recorded namespaced.
	src := &FileStore{Dir: t.TempDir(), Project: "proj"}
	dst := &FileStore{Dir: t.TempDir()}

	parent := &Ticket{
		ID: "nsdep-parent-0001", Status: StatusBacklog, Type: TypeEpic, Priority: 2,
		Title: "Namespaced dep parent", Deps: []string{}, Links: []string{},
	}
	child := &Ticket{
		ID: "nsdep-child-0002", Status: StatusReady, Type: TypeFeature, Priority: 2,
		Parent: "proj/nsdep-parent-0001", Title: "Namespaced dep child",
		Deps:     []string{"proj/nsdep-parent-0001"},
		Links:    []string{"proj/nsdep-parent-0001"},
		DepCargo: map[string]string{"proj/nsdep-parent-0001": "event schema"},
	}
	if err := src.Create(parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if err := src.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	results, err := MoveTicket(src, dst, parent.ID, true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	newParent, childResult := results[0].NewID, results[1]

	if len(childResult.StrippedDeps) > 0 || len(childResult.StrippedLinks) > 0 {
		t.Errorf("stripped deps %v links %v, want none — both name a moving ticket",
			childResult.StrippedDeps, childResult.StrippedLinks)
	}

	movedChild, err := dst.Get(childResult.NewID)
	if err != nil {
		t.Fatalf("Get moved child: %v", err)
	}
	if len(movedChild.Deps) != 1 || movedChild.Deps[0] != newParent {
		t.Errorf("Deps = %v, want [%s]", movedChild.Deps, newParent)
	}
	if len(movedChild.Links) != 1 || movedChild.Links[0] != newParent {
		t.Errorf("Links = %v, want [%s]", movedChild.Links, newParent)
	}
	if movedChild.DepCargo[newParent] != "event schema" {
		t.Errorf("DepCargo[%s] = %q, want event schema", newParent, movedChild.DepCargo[newParent])
	}
}

func TestCollectDescendantsTerminatesOnParentCycle(t *testing.T) {
	// Two tickets naming each other as parent must not spin the BFS forever.
	// A cycle is only writable in a store that predates the one-level rule.
	src := &FileStore{Dir: t.TempDir()}
	writeLegacy(t, src, movable("cyc-a-0001", TypeFeature, StatusOpen, "cyc-b-0002"))
	writeLegacy(t, src, movable("cyc-b-0002", TypeFeature, StatusOpen, "cyc-a-0001"))

	type walk struct {
		descendants []*Ticket
		err         error
	}
	done := make(chan walk, 1)
	go func() {
		got, err := collectDescendants(src, "cyc-a-0001")
		done <- walk{descendants: got, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("collectDescendants: %v", res.err)
		}
		if len(res.descendants) != 1 || res.descendants[0].ID != "cyc-b-0002" {
			t.Errorf("descendants = %v, want just cyc-b-0002", ids(res.descendants))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("collectDescendants did not terminate on a parent cycle")
	}
}

func TestMoveRecursiveEpicWithNonTerminalChildren(t *testing.T) {
	// An epic worth moving to another repo is one that still has open work in
	// it. The whole tree must land.
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "live-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "live-open-0002", TypeFeature, StatusOpen, "live-epic-0001")
	mkMovable(t, src, "live-ready-0003", TypeFeature, StatusReady, "live-epic-0001")
	mkMovable(t, src, "live-backlog-0004", TypeFeature, StatusBacklog, "live-epic-0001")

	results, err := MoveTicket(src, dst, "live-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("moved %d tickets, want 4", len(results))
	}

	dstTickets, err := dst.List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 4 {
		t.Errorf("target holds %d tickets, want 4: %v", len(dstTickets), ids(dstTickets))
	}
	for _, r := range results {
		orig, err := src.Get(r.OldID)
		if err != nil {
			t.Fatalf("Get source %s: %v", r.OldID, err)
		}
		if orig.Status != StatusClosed {
			t.Errorf("source %s = %q, want %q — it left, it did not complete", r.OldID, orig.Status, StatusClosed)
		}
	}
}

func TestMoveRecursiveFullyClosedEpic(t *testing.T) {
	// The case that already worked keeps working, modulo the marker status.
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

	// The epic reads closed off the child rather than being stored that way.
	mkMovable(t, src, "shut-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "shut-child-0002", TypeFeature, StatusClosed, "shut-epic-0001")

	results, err := MoveTicket(src, dst, "shut-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("moved %d tickets, want 2", len(results))
	}
	for _, r := range results {
		moved, err := dst.Get(r.NewID)
		if err != nil {
			t.Fatalf("Get moved %s: %v", r.NewID, err)
		}
		if moved.Status != StatusBacklog {
			t.Errorf("target %s = %q, want %q", r.NewID, moved.Status, StatusBacklog)
		}
		orig, err := src.Get(r.OldID)
		if err != nil {
			t.Fatalf("Get source %s: %v", r.OldID, err)
		}
		if orig.Status != StatusClosed {
			t.Errorf("source %s = %q, want %q", r.OldID, orig.Status, StatusClosed)
		}
	}
}

func TestMoveClosesTheTicketThatLeft(t *testing.T) {
	// A ticket that moves away is closed in the source, not done: it did not
	// complete here, it left. The epic staying behind reads that off its
	// children — every one of them terminal, one of them closed — rather than
	// off a status of its own, so it does not claim to have finished either.
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "stay-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "stay-child-0002", TypeFeature, StatusOpen, "stay-epic-0001")

	if _, err := MoveTicket(src, dst, "stay-child-0002", false); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	child, err := src.Get("stay-child-0002")
	if err != nil {
		t.Fatalf("Get child: %v", err)
	}
	if child.Status != StatusClosed {
		t.Errorf("child left behind = %q, want %q", child.Status, StatusClosed)
	}
	epic, err := src.Get("stay-epic-0001")
	if err != nil {
		t.Fatalf("Get epic: %v", err)
	}
	if epic.Status != StatusClosed {
		t.Errorf("epic left behind = %q, want %q — its only child left rather than finished", epic.Status, StatusClosed)
	}
}

func TestMoveNonRecursiveLeavesTheChildrenAlone(t *testing.T) {
	// Moving an epic without -r moves only the epic. Closing it in the source
	// records that it left, which is not a decision to abandon the children that
	// stayed — nobody asked for those to be closed.
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "nr-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "nr-open-0002", TypeFeature, StatusOpen, "nr-epic-0001")
	mkMovable(t, src, "nr-ready-0003", TypeFeature, StatusReady, "nr-epic-0001")

	if _, err := MoveTicket(src, dst, "nr-epic-0001", false); err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	for id, want := range map[string]Status{"nr-open-0002": StatusOpen, "nr-ready-0003": StatusReady} {
		child, err := src.Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if child.Status != want {
			t.Errorf("child %s = %q, want %q — a move is not an abandon", id, child.Status, want)
		}
	}
	// The closed the move wrote on the epic is inert: an epic's status is
	// derived, and a move records no abandon intent, so what stayed behind reads
	// as the children that stayed with it.
	epic, err := src.Get("nr-epic-0001")
	if err != nil {
		t.Fatalf("Get nr-epic-0001: %v", err)
	}
	if epic.Status != StatusOpen {
		t.Errorf("source epic = %q, want %q — its open child stayed put", epic.Status, StatusOpen)
	}
}

func TestMovePartialFailureReportsWhatLanded(t *testing.T) {
	// The move is not atomic. When the source write fails after the target
	// write, the caller needs the completed moves and the name of the target
	// copy whose source is still open — that pair is what reconciling needs.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "part-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "part-child-0002", TypeFeature, StatusOpen, "part-epic-0001")

	childFile := filepath.Join(src.Dir, "part-child-0002.md")
	if err := os.Chmod(childFile, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(childFile, 0o644) })

	results, moveErr := MoveTicket(src, dst, "part-epic-0001", true)
	if moveErr == nil {
		t.Fatal("MoveTicket succeeded, want a failure closing the read-only child")
	}
	if len(results) != 1 || results[0].OldID != "part-epic-0001" {
		t.Fatalf("completed = %v, want only the epic — the child never closed", results)
	}

	child, err := src.Get("part-child-0002")
	if err != nil {
		t.Fatalf("Get source child: %v", err)
	}
	if child.Status != StatusOpen {
		t.Fatalf("source child = %q, want %q — the write was supposed to fail", child.Status, StatusOpen)
	}

	dstTickets, err := dst.List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 2 {
		t.Fatalf("target holds %d tickets, want 2: %v", len(dstTickets), ids(dstTickets))
	}
	var orphan string
	for _, dt := range dstTickets {
		if dt.ID != results[0].NewID {
			orphan = dt.ID
		}
	}
	if !strings.Contains(moveErr.Error(), orphan) {
		t.Errorf("error %q does not name %s, the target copy left behind", moveErr, orphan)
	}
	if !strings.Contains(moveErr.Error(), "part-child-0002") {
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

// The refusal lands before anything is written. MoveTicket is not atomic, so a
// recursive self-move discovered partway would leave some descendants renamed
// and some not.
func TestMoveRefusesARecursiveSelfMoveAsAWhole(t *testing.T) {
	dir := t.TempDir()
	src := NewProjectFileStore(dir, "self-rec")
	dst := NewProjectFileStore(dir, "self-rec")

	mkMovable(t, src, "rec-epic-0001", TypeEpic, StatusBacklog, "")
	mkMovable(t, src, "rec-child-0002", TypeFeature, StatusOpen, "rec-epic-0001")
	mkMovable(t, src, "rec-child-0003", TypeFeature, StatusReady, "rec-epic-0001")

	if _, err := MoveTicket(src, dst, "rec-epic-0001", true); err == nil {
		t.Fatal("MoveTicket succeeded, want a refusal")
	}

	for id, want := range map[string]Status{"rec-child-0002": StatusOpen, "rec-child-0003": StatusReady} {
		got, err := src.Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if got.Status != want {
			t.Errorf("descendant %s = %q, want %q — the refusal moved part of the tree", id, got.Status, want)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("store holds %v, want the three originals", files)
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
// never held a ticket has no directory until dst.Create makes one, and
// EvalSymlinks fails on a path that does not exist.
func TestMoveIntoAProjectThatHasNeverHeldATicket(t *testing.T) {
	src := NewProjectFileStore(t.TempDir(), "unused-from")
	dstDir := filepath.Join(t.TempDir(), "never-used")
	dst := NewProjectFileStore(dstDir, "unused-to")

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
	src := &FileStore{Dir: t.TempDir()}
	dst := &FileStore{Dir: t.TempDir()}

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
