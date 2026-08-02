package ticket

import (
	"path/filepath"
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

	// Original should be done.
	orig, err := src.Get(original.ID)
	if err != nil {
		t.Fatalf("get original: %v", err)
	}
	if orig.Status != StatusDone {
		t.Errorf("original status: got %q, want %q", orig.Status, StatusDone)
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
		Status:   StatusReady,
		Type:     TypeFeature,
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
	// carry both forms, and the levels below them.
	src := &FileStore{Dir: t.TempDir(), Project: "proj"}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "mv-epic-0001", TypeEpic, StatusOpen, "")
	mkMovable(t, src, "mv-bare-0002", TypeFeature, StatusClosed, "mv-epic-0001")
	mkMovable(t, src, "mv-ns-0003", TypeFeature, StatusClosed, "proj/mv-epic-0001")
	mkMovable(t, src, "mv-grand-0004", TypeFeature, StatusClosed, "proj/mv-ns-0003")

	results, err := MoveTicket(src, dst, "mv-epic-0001", true)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	moved := map[string]string{}
	for _, r := range results {
		moved[r.OldID] = r.NewID
	}
	for _, id := range []string{"mv-epic-0001", "mv-bare-0002", "mv-ns-0003", "mv-grand-0004"} {
		if moved[id] == "" {
			t.Fatalf("%s was left behind, moved set = %v", id, moved)
		}
	}

	// Carrying the tickets is only half the move — a namespaced parent must
	// remap to the moving parent's new ID, not be dropped as if it stayed.
	wantParent := map[string]string{
		"mv-epic-0001":  "",
		"mv-bare-0002":  moved["mv-epic-0001"],
		"mv-ns-0003":    moved["mv-epic-0001"],
		"mv-grand-0004": moved["mv-ns-0003"],
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
	if len(dstTickets) != 4 {
		t.Errorf("target holds %d tickets, want 4", len(dstTickets))
	}
}

func TestMoveRecursiveSkipsForeignProjectChild(t *testing.T) {
	// A child whose parent names a different project is not this store's child,
	// even when the bare IDs match. Sweeping it into the move would set the
	// wrong ticket done and copy it into the target repo — the mis-resolution
	// FileStore.Resolve rejects for the same reason.
	src := &FileStore{Dir: t.TempDir(), Project: "proj"}
	dst := &FileStore{Dir: t.TempDir()}

	mkMovable(t, src, "mv-epic-0001", TypeEpic, StatusOpen, "")
	mkMovable(t, src, "mv-own-0002", TypeFeature, StatusClosed, "proj/mv-epic-0001")
	mkMovable(t, src, "mv-foreign-0003", TypeFeature, StatusClosed, "otherproj/mv-epic-0001")

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
		ID: "nsdep-parent-0001", Status: StatusReady, Type: TypeFeature, Priority: 2,
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
	src := &FileStore{Dir: t.TempDir()}
	mkMovable(t, src, "cyc-a-0001", TypeFeature, StatusOpen, "cyc-b-0002")
	mkMovable(t, src, "cyc-b-0002", TypeFeature, StatusOpen, "cyc-a-0001")

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

func mkMovable(t *testing.T, store *FileStore, id string, typ TicketType, status Status, parent string) {
	t.Helper()
	tk := &Ticket{
		ID: id, Status: status, Type: typ, Priority: 2, Parent: parent,
		Created: time.Now(), Title: "Item " + id, Body: "\n",
		Deps: []string{}, Links: []string{},
	}
	if err := store.Create(tk); err != nil {
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
