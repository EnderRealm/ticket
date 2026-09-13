package ticket

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// centralFixture is the central store every cross-project case runs against:
// namespaces _root, warp, loom and ticket, catalogued, with root-namespace
// required and cross-project-parents required when crossProject is set. The
// two variants differ in that one line of the catalog alone, so a case that
// passes activated and fails unactivated is pinned to the feature gate and
// nothing else.
func centralFixture(t *testing.T, crossProject bool) (root string, ms *MultiStore) {
	t.Helper()
	root = catalogRoot(t)
	features := "required_features: [root-namespace]\n"
	if crossProject {
		features = "required_features: [root-namespace, cross-project-parents]\n"
	}
	writeCatalog(t, root, features+"namespaces:\n  _root: {kind: root}\n  warp: {kind: project}\n  loom: {kind: project}\n  ticket: {kind: project}\n")
	for _, ns := range []string{project.RootNamespace, "warp", "loom", "ticket"} {
		mkNamespaceDir(t, root, ns)
	}
	return root, NewMultiStore(filepath.Join(root, "tickets"))
}

// nsStore is the project store for one namespace of the fixture — the shape
// the CLI and `ticket_create(repo=)` write through.
func nsStore(root, ns string) *FileStore {
	return NewProjectFileStore(filepath.Join(root, "tickets", ns), ns)
}

func mustCreate(t *testing.T, s Store, tickets ...*Ticket) {
	t.Helper()
	for _, tk := range tickets {
		if err := s.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", tk.ID, err)
		}
	}
}

func mustGet(t *testing.T, s Store, id string) *Ticket {
	t.Helper()
	tk, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return tk
}

func statusOf(t *testing.T, s Store, id string) Status {
	t.Helper()
	return mustGet(t, s, id).Status
}

// ─── AC1: identity, the feature gate and qualified references ───────────────

func TestForeignParentAcceptedOnlyWhenActivated(t *testing.T) {
	for _, activated := range []bool{true, false} {
		root, ms := centralFixture(t, activated)
		mustCreate(t, ms, mkEpic("_root/unified-0001", StatusBacklog, ""))
		before := snapshotTree(t, filepath.Join(root, "tickets"))

		// Through the MultiStore, as MCP writes, and through the project
		// store, as the CLI and ticket_create(repo=) write.
		err := ms.Create(mkWithParent("warp/leaf-0002", StatusOpen, "_root/unified-0001"))
		fsErr := nsStore(root, "loom").Create(mkWithParent("leaf-0003", StatusOpen, "_root/unified-0001"))
		if activated {
			if err != nil || fsErr != nil {
				t.Fatalf("activated: foreign parent refused: %v / %v", err, fsErr)
			}
			if got := statusOf(t, ms, "_root/unified-0001"); got != StatusOpen {
				t.Errorf("activated: epic reads %q, want %q from its foreign children", got, StatusOpen)
			}
			continue
		}
		for name, e := range map[string]error{"MultiStore": err, "FileStore": fsErr} {
			if e == nil || !strings.Contains(e.Error(), "another project") || !strings.Contains(e.Error(), FeatureCrossProjectParents) {
				t.Errorf("unactivated %s: want a refusal naming the feature that activates it, got %v", name, e)
			}
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	}
}

func TestParentRulesHoldAcrossNamespaces(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/epic-0001", StatusBacklog, ""),
		mk("warp/feat-0002", StatusOpen),
		mkEpic("_root/epic-0007", StatusBacklog, ""),
	)
	cases := map[string]*Ticket{
		"epic child":      mkEpic("loom/sub-0003", StatusBacklog, "_root/epic-0001"),
		"non-epic parent": mkWithParent("loom/leaf-0004", StatusOpen, "warp/feat-0002"),
		"missing parent":  mkWithParent("loom/leaf-0005", StatusOpen, "warp/gone-9999"),
		"ambiguous":       mkWithParent("loom/leaf-0006", StatusOpen, "_root/000"),
	}
	before := snapshotTree(t, filepath.Join(root, "tickets"))
	for name, tk := range cases {
		if err := ms.Create(tk); err == nil {
			t.Errorf("%s: accepted %s with parent %s", name, tk.ID, tk.Parent)
		}
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
}

func TestIdenticalBareIDsNeverJoinAcrossNamespaces(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-1", StatusBacklog, ""),
		mkEpic("loom/epic-1", StatusBacklog, ""),
		mkWithParent("warp/open-0002", StatusOpen, "warp/epic-1"),
	)
	// A bare parent typed from loom names loom's epic-1, whatever warp holds.
	mustCreate(t, nsStore(root, "loom"), mkWithParent("done-0003", StatusDone, "epic-1"))

	if got := statusOf(t, ms, "warp/epic-1"); got != StatusOpen {
		t.Errorf("warp/epic-1 = %q, want %q from its own child", got, StatusOpen)
	}
	if got := statusOf(t, ms, "loom/epic-1"); got != StatusDone {
		t.Errorf("loom/epic-1 = %q, want %q from its own child", got, StatusDone)
	}
	if parent := mustGet(t, ms, "loom/done-0003").Parent; parent != "loom/epic-1" {
		t.Errorf("bare parent stored as %q, want the local epic qualified", parent)
	}

	// A bare dep from loom names loom's epic-1, which is done: the dependant
	// is unblocked. Resolved by luck across projects, warp's open epic-1 would
	// block it.
	mustCreate(t, nsStore(root, "loom"), mk("waiter-0004", StatusReady, "epic-1"))
	waiter := mustGet(t, ms, "loom/waiter-0004")
	if len(waiter.Deps) != 1 || waiter.Deps[0] != "loom/epic-1" {
		t.Errorf("bare dep stored as %v, want [loom/epic-1]", waiter.Deps)
	}
	if IsBlocked(ms, waiter) {
		t.Error("loom/waiter-0004 reads blocked: its bare dep resolved to another project's epic-1")
	}
	all, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	if BlockedFunc(ms, all)(waiter) {
		t.Error("BlockedFunc resolved the bare dep across projects")
	}
	// The legacy shape: a bare dep written before qualification is local too.
	writeLegacy(t, nsStore(root, "warp"), mk("legacy-0005", StatusReady, "epic-1"))
	if !IsBlocked(ms, mustGet(t, ms, "warp/legacy-0005")) {
		t.Error("warp/legacy-0005 reads unblocked: its bare dep should name warp's open epic-1")
	}
}

func TestReferenceEditsKeepIdenticalBareIDsApart(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-1", StatusBacklog, ""),
		mkEpic("loom/epic-1", StatusBacklog, ""),
		mk("warp/subject-0002", StatusOpen, "warp/epic-1"),
	)
	subject, err := Mutate(ms, "warp/subject-0002", func(t *Ticket) error {
		if err := AddDep(t, "loom/epic-1"); err != nil {
			return err
		}
		return SetDepCargo(t, "loom/epic-1", "loom's schema")
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Deps; len(got) != 2 || got[0] != "warp/epic-1" || got[1] != "loom/epic-1" {
		t.Fatalf("Deps = %v, want both epics: the bare halves match, the tickets do not", got)
	}
	if CargoFor(subject, "loom/epic-1") != "loom's schema" || CargoFor(subject, "warp/epic-1") != "" {
		t.Errorf("DepCargo = %v, want the cargo on loom's edge alone", subject.DepCargo)
	}
	// Links the same way: warp's epic is linked, loom's is a second link.
	warpEpic, loomEpic := mustGet(t, ms, "warp/epic-1"), mustGet(t, ms, "loom/epic-1")
	AddLink(subject, warpEpic)
	AddLink(subject, loomEpic)
	if got := subject.Links; len(got) != 2 {
		t.Errorf("Links = %v, want both epics", got)
	}
	RemoveLink(subject, loomEpic)
	if got := subject.Links; len(got) != 1 || got[0] != "warp/epic-1" {
		t.Errorf("Links after removing loom's = %v, want warp's alone", got)
	}

	subject, err = Mutate(ms, "warp/subject-0002", func(t *Ticket) error {
		RemoveDep(t, "loom/epic-1")
		return nil
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Deps; len(got) != 1 || got[0] != "warp/epic-1" {
		t.Errorf("Deps after removing loom's = %v, want warp's alone", got)
	}
	if len(subject.DepCargo) != 0 {
		t.Errorf("DepCargo after removing loom's edge = %v, want none", subject.DepCargo)
	}
}

// The same rule from a project store, where the owner's ID and the typed
// argument are both bare: `tk dep <id> epic-1` in warp names warp's epic-1,
// and a loom edge already stored is neither the edge being added nor the one
// being removed.
func TestBareReferenceEditsFromAProjectStoreNameItsOwnProject(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, ms,
		mkEpic("warp/epic-1", StatusBacklog, ""),
		mkEpic("loom/epic-1", StatusBacklog, ""),
		mk("warp/subject-0002", StatusOpen, "loom/epic-1"),
	)
	subject, err := Mutate(warp, "subject-0002", func(t *Ticket) error {
		if err := AddDep(t, "epic-1"); err != nil {
			return err
		}
		return SetDepCargo(t, "epic-1", "warp's schema")
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Deps; len(got) != 2 || got[0] != "loom/epic-1" || got[1] != "warp/epic-1" {
		t.Fatalf("Deps = %v, want loom's edge kept and warp's added", got)
	}
	if CargoFor(subject, "epic-1") != "warp's schema" || CargoFor(subject, "loom/epic-1") != "" {
		t.Errorf("DepCargo = %v, want the cargo on warp's edge alone", subject.DepCargo)
	}
	// Cargo on loom's edge does not answer a bare read from warp.
	subject, err = Mutate(warp, "subject-0002", func(t *Ticket) error {
		return SetDepCargo(t, "loom/epic-1", "loom's schema")
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if CargoFor(subject, "epic-1") != "warp's schema" || CargoFor(subject, "loom/epic-1") != "loom's schema" {
		t.Errorf("DepCargo = %v, want one cargo per edge", subject.DepCargo)
	}

	// Links the same way: loom's epic linked through the central store, warp's
	// added and removed under the bare ID the project store hands back.
	loomEpic := mustGet(t, ms, "loom/epic-1")
	if _, err := Mutate(ms, "warp/subject-0002", func(t *Ticket) error { AddLink(t, loomEpic); return nil }); err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	warpEpic := mustGet(t, warp, "epic-1")
	subject, err = Mutate(warp, "subject-0002", func(t *Ticket) error { AddLink(t, warpEpic); return nil })
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Links; len(got) != 2 || got[0] != "loom/epic-1" || got[1] != "warp/epic-1" {
		t.Errorf("Links = %v, want loom's kept and warp's added", got)
	}
	subject, err = Mutate(warp, "subject-0002", func(t *Ticket) error { RemoveLink(t, warpEpic); return nil })
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Links; len(got) != 1 || got[0] != "loom/epic-1" {
		t.Errorf("Links after removing warp's = %v, want loom's alone", got)
	}

	subject, err = Mutate(warp, "subject-0002", func(t *Ticket) error { RemoveDep(t, "epic-1"); return nil })
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got := subject.Deps; len(got) != 1 || got[0] != "loom/epic-1" {
		t.Errorf("Deps after removing warp's = %v, want loom's alone", got)
	}
	if len(subject.DepCargo) != 1 || CargoFor(subject, "loom/epic-1") != "loom's schema" {
		t.Errorf("DepCargo after removing warp's edge = %v, want loom's cargo alone", subject.DepCargo)
	}
}

func TestNewReferencesAreQualifiedAndLegacyOnesStayBare(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, warp,
		mkEpic("epic-0001", StatusBacklog, ""),
		mk("dep-0002", StatusOpen),
		mk("link-0003", StatusOpen),
		mk("subject-0004", StatusOpen),
	)
	// A legacy ticket holding bare references, written before qualification.
	writeLegacy(t, warp, mkWithParent("legacy-0005", StatusOpen, "epic-0001", "dep-0002"))

	// A new dep, link and cargo key from the project store are stored
	// qualified; the cargo value survives the rekey.
	link := mustGet(t, warp, "link-0003")
	subject, err := Mutate(warp, "subject-0004", func(t *Ticket) error {
		if err := AddDep(t, "dep-0002"); err != nil {
			return err
		}
		AddLink(t, link)
		return SetDepCargo(t, "dep-0002", "event schema")
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(subject.Deps) != 1 || subject.Deps[0] != "warp/dep-0002" {
		t.Errorf("Deps = %v, want [warp/dep-0002]", subject.Deps)
	}
	if len(subject.Links) != 1 || subject.Links[0] != "warp/link-0003" {
		t.Errorf("Links = %v, want [warp/link-0003]", subject.Links)
	}
	if subject.DepCargo["warp/dep-0002"] != "event schema" || len(subject.DepCargo) != 1 {
		t.Errorf("DepCargo = %v, want the cargo under the qualified key alone", subject.DepCargo)
	}
	stored, _ := warp.getStored("subject-0004")
	if stored.DepCargo["warp/dep-0002"] != "event schema" {
		t.Errorf("stored DepCargo = %v, want the cargo persisted under the qualified key", stored.DepCargo)
	}

	// An unrelated edit of the legacy ticket leaves its bare references as
	// they are: a bare reference is local by the reading rule, and rewriting
	// it would be a migration nobody asked for.
	if _, err := Mutate(warp, "legacy-0005", func(t *Ticket) error {
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "unrelated"})
		return nil
	}); err != nil {
		t.Fatalf("Mutate legacy: %v", err)
	}
	legacy, _ := warp.getStored("legacy-0005")
	if legacy.Parent != "epic-0001" || len(legacy.Deps) != 1 || legacy.Deps[0] != "dep-0002" {
		t.Errorf("legacy references rewritten: parent %q deps %v", legacy.Parent, legacy.Deps)
	}
	// And the same edit through the MultiStore, where the ID is qualified.
	if _, err := Mutate(ms, "warp/legacy-0005", func(t *Ticket) error {
		t.Priority = 1
		return nil
	}); err != nil {
		t.Fatalf("Mutate legacy via MultiStore: %v", err)
	}
	legacy, _ = warp.getStored("legacy-0005")
	if legacy.Parent != "epic-0001" || legacy.Deps[0] != "dep-0002" {
		t.Errorf("legacy references rewritten by a MultiStore edit: parent %q deps %v", legacy.Parent, legacy.Deps)
	}
	// The legacy child still counts: its bare parent names warp's epic.
	if got := statusOf(t, ms, "warp/epic-0001"); got != StatusOpen {
		t.Errorf("warp/epic-0001 = %q, want %q from its legacy child", got, StatusOpen)
	}
}

// ─── AC2: global derivation ────────────────────────────────────────────────

func TestEpicDerivesFromChildrenInEveryNamespace(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/unified-0001", StatusBacklog, ""),
		mkWithParent("warp/w-0002", StatusBacklog, "_root/unified-0001"),
		mkWithParent("loom/l-0003", StatusBacklog, "_root/unified-0001"),
	)
	warp := nsStore(root, "warp")

	if got := statusOf(t, ms, "_root/unified-0001"); got != StatusBacklog {
		t.Fatalf("epic = %q, want %q with backlog children", got, StatusBacklog)
	}
	if err := setStatus(t, ms, "warp/w-0002", StatusOpen); err != nil {
		t.Fatal(err)
	}
	// Read through the central store and through the epic's own project store:
	// the derivation is the same whichever view asked.
	if got := statusOf(t, ms, "_root/unified-0001"); got != StatusOpen {
		t.Errorf("epic = %q, want %q from a foreign open child", got, StatusOpen)
	}
	if got := statusOf(t, nsStore(root, project.RootNamespace), "unified-0001"); got != StatusOpen {
		t.Errorf("epic via its project store = %q, want %q", got, StatusOpen)
	}
	// A project-filtered listing of warp still derives warp's view of nothing
	// here, but the epic in _root is what it is: List on warp shows only warp.
	warpOnly, err := warp.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range warpOnly {
		if namespaceOf(tk.ID) != "" {
			t.Errorf("project listing yielded a qualified ID %q", tk.ID)
		}
	}

	if err := setStatus(t, ms, "warp/w-0002", StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(t, ms, "loom/l-0003", StatusDone); err != nil {
		t.Fatal(err)
	}
	epic := mustGet(t, ms, "_root/unified-0001")
	if epic.Status != StatusDone {
		t.Fatalf("epic = %q, want %q once every child in every namespace is done", epic.Status, StatusDone)
	}
	// Completed is the latest child completion across namespaces.
	loomChild := mustGet(t, ms, "loom/l-0003")
	if !epic.Completed.Equal(loomChild.Completed) || epic.Completed.IsZero() {
		t.Errorf("epic completed = %v, want the last child's %v", epic.Completed, loomChild.Completed)
	}

	// A closed child among done ones: closed, not done.
	if err := setStatus(t, ms, "loom/l-0003", StatusClosed); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, ms, "_root/unified-0001"); got != StatusClosed {
		t.Errorf("epic = %q, want %q when a child was closed rather than done", got, StatusClosed)
	}

	// A foreign child reopening clears the status and the date.
	if err := setStatus(t, ms, "loom/l-0003", StatusOpen); err != nil {
		t.Fatal(err)
	}
	epic = mustGet(t, ms, "_root/unified-0001")
	if epic.Status != StatusOpen || !epic.Completed.IsZero() {
		t.Errorf("epic = %q completed %v after a foreign child reopened, want %q and no date", epic.Status, epic.Completed, StatusOpen)
	}
}

func TestAbandonOverridesAllTerminalAndChildless(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/done-0001", StatusBacklog, ""),
		mkWithParent("warp/w-0002", StatusDone, "_root/done-0001"),
		mkWithParent("loom/l-0003", StatusDone, "_root/done-0001"),
		mkEpic("_root/empty-0004", StatusBacklog, ""),
	)
	if got := statusOf(t, ms, "_root/done-0001"); got != StatusDone {
		t.Fatalf("epic = %q, want %q", got, StatusDone)
	}
	if err := setStatus(t, ms, "_root/done-0001", StatusClosed); err != nil {
		t.Fatalf("abandoning an all-done epic: %v", err)
	}
	if got := statusOf(t, ms, "_root/done-0001"); got != StatusClosed {
		t.Errorf("abandoned all-done epic = %q, want %q", got, StatusClosed)
	}
	if err := setStatus(t, ms, "_root/empty-0004", StatusClosed); err != nil {
		t.Fatalf("abandoning a childless epic: %v", err)
	}
	empty := mustGet(t, ms, "_root/empty-0004")
	if empty.Status != StatusClosed || !empty.Completed.IsZero() {
		t.Errorf("abandoned childless epic = %q completed %v, want %q with no invented date", empty.Status, empty.Completed, StatusClosed)
	}
}

// ─── AC3: incompleteness and diagnostics ───────────────────────────────────

func TestIncompleteSnapshotCertifiesNoEpicAnywhere(t *testing.T) {
	type plant func(t *testing.T, root string)
	cases := map[string]struct {
		plant   plant
		kind    FileSkipKind
		mention string
	}{
		"unreadable file in a foreign namespace": {
			plant: func(t *testing.T, root string) {
				plantUnreadable(t, filepath.Join(root, "tickets", "ticket"), "broken-9999.md")
			},
			kind:    FileSkipUnreadable,
			mention: "broken-9999.md",
		},
		"duplicate id": {
			plant: func(t *testing.T, root string) {
				plantTicketFile(t, filepath.Join(root, "tickets", "ticket"), "zz-twin-0001.md", mk("ticket/twin-0001", StatusDone))
				plantTicketFile(t, filepath.Join(root, "tickets", "ticket"), "twin-0001.md", mk("twin-0001", StatusOpen))
			},
			kind:    FileSkipDuplicateID,
			mention: "twin-0001",
		},
		"unlistable namespace": {
			plant: func(t *testing.T, root string) {
				if os.Geteuid() == 0 {
					t.Skip("root lists every directory")
				}
				dir := filepath.Join(root, "tickets", "ticket")
				if err := os.Chmod(dir, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(dir, 0o755) })
			},
			kind:    FileSkipNamespace,
			mention: `"ticket"`,
		},
		"catalogued namespace with no directory": {
			plant: func(t *testing.T, root string) {
				writeCatalog(t, root, "required_features: [root-namespace, cross-project-parents]\nnamespaces:\n  _root: {kind: root}\n  warp: {kind: project}\n  loom: {kind: project}\n  ticket: {kind: project}\n  ghost: {kind: project}\n")
			},
			kind:    FileSkipNamespace,
			mention: `"ghost"`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root, ms := centralFixture(t, true)
			mustCreate(t, ms,
				mkEpic("_root/done-0001", StatusBacklog, ""),
				mkWithParent("warp/w-0002", StatusDone, "_root/done-0001"),
				mkEpic("loom/shut-0003", StatusBacklog, ""),
				mkWithParent("loom/l-0004", StatusClosed, "loom/shut-0003"),
			)
			for id, want := range map[string]Status{"_root/done-0001": StatusDone, "loom/shut-0003": StatusClosed} {
				if got := statusOf(t, ms, id); got != want {
					t.Fatalf("%s = %q before the plant, want %q", id, got, want)
				}
			}
			c.plant(t, root)

			snap, err := ms.Snapshot()
			if err != nil {
				t.Fatalf("Snapshot: %v", err)
			}
			if snap.Complete {
				t.Error("snapshot reports complete")
			}
			found := false
			for _, skip := range snap.Skips {
				if skip.Kind == c.kind {
					found = true
				}
			}
			if !found {
				t.Errorf("skips = %+v, want one of kind %s", snap.Skips, c.kind)
			}
			if diag := strings.Join(snap.Diagnostics(), "\n"); !strings.Contains(diag, c.mention) {
				t.Errorf("diagnostics = %q, want a mention of %s", diag, c.mention)
			}
			captureWarnings(t)
			for _, id := range []string{"_root/done-0001", "loom/shut-0003"} {
				epic := mustGet(t, ms, id)
				if epic.Status != StatusBacklog || !epic.Completed.IsZero() {
					t.Errorf("%s = %q completed %v beside an incomplete snapshot, want %q and no date", id, epic.Status, epic.Completed, StatusBacklog)
				}
				if got, _ := snap.Get(id); got.Status != StatusBacklog {
					t.Errorf("snapshot reads %s as %q, want %q", id, got.Status, StatusBacklog)
				}
			}
			// The project view of warp — which holds nothing wrong — still
			// carries the foreign failure.
			_, skips, err := nsStore(root, "warp").ListWithSkips()
			if err != nil {
				t.Fatal(err)
			}
			if len(skips) == 0 {
				t.Error("a project view hid the foreign failure that degrades its epics")
			}
		})
	}
}

func TestInvalidRelationshipsExcludeLeavesFromAutomaticRuns(t *testing.T) {
	root, ms := centralFixture(t, false)
	mustCreate(t, ms,
		mkEpic("_root/epic-0001", StatusBacklog, ""),
		mkEpic("warp/epic-0002", StatusBacklog, ""),
		mk("warp/feat-0003", StatusOpen),
		mkWithParent("warp/fine-0004", StatusReady, "warp/epic-0002"),
	)
	warp := nsStore(root, "warp")
	writeLegacy(t, warp, mkWithParent("missing-0005", StatusReady, "gone-9999"))
	writeLegacy(t, warp, mkWithParent("notepic-0006", StatusReady, "feat-0003"))
	writeLegacy(t, warp, mkWithParent("foreign-0007", StatusReady, "_root/epic-0001"))
	// A parent stored as a fragment: FileStore.Resolve would substring-match
	// it to epic-0002, and the snapshot's exact lookup does not.
	writeLegacy(t, warp, mkWithParent("fragment-0008", StatusReady, "0002"))
	// A parent two files claim: the snapshot places the leaf under neither,
	// and a single read of the leaf has to say the same.
	mustCreate(t, ms, mkEpic("warp/twin-0009", StatusBacklog, ""))
	twin := filepath.Join(root, "tickets", "warp", "zz-twin-0009.md")
	plantTicketFile(t, filepath.Dir(twin), filepath.Base(twin), mkEpic("warp/twin-0009", StatusBacklog, ""))
	writeLegacy(t, warp, mkWithParent("claimed-0010", StatusReady, "twin-0009"))
	captureWarnings(t)

	frontier, err := FrontierTickets(ms)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := ReadyTickets(ms)
	if err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for _, tk := range append(frontier, ready...) {
		offered[tk.ID] = true
	}
	if !offered["warp/fine-0004"] {
		t.Errorf("the valid leaf is missing from the frontier/ready set: %v %v", ids2(frontier), ids2(ready))
	}
	want := map[string]string{
		"warp/missing-0005":  "does not resolve",
		"warp/notepic-0006":  "not an epic",
		"warp/foreign-0007":  "not activated",
		"warp/fragment-0008": "does not resolve",
		"warp/claimed-0010":  "does not resolve",
	}
	for id, reason := range want {
		if offered[id] {
			t.Errorf("%s was offered as runnable despite an invalid parent", id)
		}
		// Named on the ticket, through a listing and through a single read.
		listed := mustGet(t, ms, id)
		if issue := RelationshipIssue(listed); !strings.Contains(issue, reason) {
			t.Errorf("RelationshipIssue(%s) via Get = %q, want it to say %q", id, issue, reason)
		}
		if IsReady(ms, listed) || IsReadyOpen(ms, listed) {
			t.Errorf("%s reads ready through the single-ticket checks", id)
		}
	}
	all, err := ms.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range all {
		if reason, ok := want[tk.ID]; ok && !strings.Contains(RelationshipIssue(tk), reason) {
			t.Errorf("RelationshipIssue(%s) via List = %q, want it to say %q", tk.ID, RelationshipIssue(tk), reason)
		}
	}
	// Once activated, the foreign parent is a valid relationship. The twin is
	// gone first: the duplicate degrades every epic, and the derivation below
	// is the point.
	if err := os.Remove(twin); err != nil {
		t.Fatal(err)
	}
	writeCatalog(t, root, "required_features: [root-namespace, cross-project-parents]\nnamespaces:\n  _root: {kind: root}\n  warp: {kind: project}\n  loom: {kind: project}\n  ticket: {kind: project}\n")
	if issue := RelationshipIssue(mustGet(t, ms, "warp/foreign-0007")); issue != "" {
		t.Errorf("activated: foreign parent still carries issue %q", issue)
	}
	if got := statusOf(t, ms, "_root/epic-0001"); got != StatusBacklog {
		t.Errorf("_root/epic-0001 = %q, want %q from its ready child", got, StatusBacklog)
	}
}

// A foreign dep two files claim — done under the dep's own filename, open
// beside it — blocks from the project view the way it blocks from the central
// one. A project listing holds no foreign ticket, so the dep falls through to
// the store, and that read has to see the namespace's claims rather than the
// one file whose name matches.
func TestDuplicateForeignDepBlocksFromTheProjectView(t *testing.T) {
	root, ms := centralFixture(t, true)
	loom := nsStore(root, "loom")
	mustCreate(t, ms,
		mk("warp/x-0001", StatusDone),
		mk("loom/waiter-0002", StatusReady, "warp/x-0001"),
	)
	if IsBlocked(loom, mustGet(t, loom, "waiter-0002")) {
		t.Fatal("loom/waiter-0002 reads blocked over a done dep before the duplicate is planted")
	}
	plantTicketFile(t, filepath.Join(root, "tickets", "warp"), "zz-x-0001.md", mk("warp/x-0001", StatusOpen))
	captureWarnings(t)

	for name, s := range map[string]Store{"project": loom, "central": ms} {
		frontier, err := FrontierTickets(s)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := ReadyTickets(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, tk := range append(frontier, ready...) {
			if _, bare := ParseNamespacedID(tk.ID); bare == "waiter-0002" {
				t.Errorf("%s view offered loom/waiter-0002 as runnable over a dep another file claims open", name)
			}
		}
	}
	waiter := mustGet(t, loom, "waiter-0002")
	if !IsBlocked(loom, waiter) || IsReady(loom, waiter) || IsReadyOpen(loom, waiter) {
		t.Error("the single-ticket checks read loom/waiter-0002 unblocked off the done claimant alone")
	}
	if got := BlockingDeps(loom, waiter); len(got) != 1 || got[0] != "warp/x-0001" {
		t.Errorf("BlockingDeps = %v, want [warp/x-0001]", got)
	}
}

// An ID two files claim is nobody's, and that includes the claimants
// themselves: `x.md` holding a ready leaf nothing blocks, beside `zz-x.md`
// holding the same ID behind an unfinished dep, must not offer x for
// automatic execution off the one file whose name matches. Every claimant
// carries the ambiguous identity as its relationship issue, from a listing
// and from a single read alike, until the duplicate is repaired.
func TestDuplicateClaimantsAreNeverRunnable(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, ms,
		mk("warp/x-0001", StatusReady),
		mk("warp/blocker-0002", StatusOpen),
		mk("warp/fine-0003", StatusReady),
	)
	plantTicketFile(t, filepath.Join(root, "tickets", "warp"), "zz-x-0001.md", mk("warp/x-0001", StatusOpen, "warp/blocker-0002"))
	captureWarnings(t)

	for name, s := range map[string]Store{"project": warp, "central": ms} {
		frontier, err := FrontierTickets(s)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := ReadyTickets(s)
		if err != nil {
			t.Fatal(err)
		}
		open, err := ReadyTicketsOpen(s)
		if err != nil {
			t.Fatal(err)
		}
		offered := map[string]bool{}
		for _, tk := range append(append(frontier, ready...), open...) {
			_, bare := ParseNamespacedID(tk.ID)
			offered[bare] = true
		}
		if offered["x-0001"] {
			t.Errorf("%s view offered x-0001 as runnable while another file claims its ID", name)
		}
		if !offered["fine-0003"] {
			t.Errorf("%s view dropped the unrelated ready leaf", name)
		}
		listed, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		claimants := 0
		for _, tk := range listed {
			if _, bare := ParseNamespacedID(tk.ID); bare != "x-0001" {
				continue
			}
			claimants++
			if issue := RelationshipIssue(tk); !strings.Contains(issue, "warp/x-0001") || !strings.Contains(issue, "more than one file") {
				t.Errorf("%s view: claimant %q (%s) carries issue %q, want the ambiguous identity named", name, tk.ID, tk.Status, issue)
			}
		}
		if claimants != 2 {
			t.Errorf("%s view listed %d claimants of x-0001, want both", name, claimants)
		}
	}

	for name, s := range map[string]Store{"project": warp, "central": ms} {
		for _, id := range []string{"x-0001", "zz-x-0001"} {
			x := mustGet(t, s, qualifyRef(storeProject(s), id))
			if issue := RelationshipIssue(x); !strings.Contains(issue, "warp/x-0001") || !strings.Contains(issue, "more than one file") {
				t.Errorf("%s Get %s: issue %q, want the ambiguous identity named", name, id, issue)
			}
			if IsReady(s, x) || IsReadyOpen(s, x) {
				t.Errorf("%s Get %s: the single-ticket checks read a duplicate claimant as ready", name, id)
			}
		}
	}
}

// A mutation reads its ticket through the snapshot the write holds, and a
// claimed-twice ID is absent from that index by design. The ticket Mutate
// hands back has to carry the ambiguous identity all the same: with no deps
// and no parent, IsReady and IsReadyOpen would otherwise accept it while Get
// and List exclude both files.
func TestMutateStampsADuplicateClaimant(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, ms, mk("warp/x-0001", StatusReady))
	plantTicketFile(t, filepath.Join(root, "tickets", "warp"), "zz-x-0001.md", mk("warp/x-0001", StatusOpen))
	captureWarnings(t)

	for name, s := range map[string]Store{"project": warp, "central": ms} {
		got, err := Mutate(s, qualifyRef(storeProject(s), "x-0001"), func(tk *Ticket) error {
			tk.Notes = append(tk.Notes, Note{Timestamp: time.Now().UTC(), Text: "via " + name})
			return nil
		})
		if err != nil {
			t.Fatalf("%s Mutate: %v", name, err)
		}
		if issue := RelationshipIssue(got); !strings.Contains(issue, "warp/x-0001") || !strings.Contains(issue, "more than one file") {
			t.Errorf("%s Mutate: issue %q, want the ambiguous identity named", name, issue)
		}
		if IsReady(s, got) || IsReadyOpen(s, got) {
			t.Errorf("%s Mutate: the single-ticket checks read the returned duplicate claimant as ready", name)
		}
	}
}

// ─── AC4: structural cycles and epic dependencies ──────────────────────────

func TestCycleCheckCoversDepsChildrenAndTerminalNodes(t *testing.T) {
	root, ms := centralFixture(t, true)
	warp := nsStore(root, "warp")
	mustCreate(t, ms,
		mk("warp/a-0001", StatusOpen),
		mk("warp/b-0002", StatusOpen, "warp/a-0001"),
		mkEpic("warp/epic-0003", StatusBacklog, ""),
		mkWithParent("warp/child-0004", StatusOpen, "warp/epic-0003"),
		mk("warp/done-0005", StatusDone, "warp/a-0001"),
	)
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	// a -> b -> a, explicit deps.
	_, err := Mutate(ms, "warp/a-0001", func(t *Ticket) error { return AddDep(t, "warp/b-0002") })
	if err == nil || !strings.Contains(err.Error(), "warp/a-0001 -> warp/b-0002 -> warp/a-0001") {
		t.Errorf("explicit cycle: want a refusal naming the path, got %v", err)
	}
	// A child depending on its own epic: the epic waits for the child.
	_, err = Mutate(warp, "child-0004", func(t *Ticket) error { return AddDep(t, "epic-0003") })
	if err == nil || !strings.Contains(err.Error(), "wait for itself") {
		t.Errorf("child depending on its own epic: want a refusal, got %v", err)
	}
	// Through the parent edge too: a leaf a's epic... reparenting b under an
	// epic that depends on nothing is fine, but making a's dependant the
	// parent's child closes a loop through the epic.
	_, err = Mutate(ms, "warp/epic-0003", func(t *Ticket) error { return AddDep(t, "warp/a-0001") })
	if err == nil || !strings.Contains(err.Error(), "acceptance leaf") {
		t.Errorf("epic adding a dep: want the acceptance-leaf refusal, got %v", err)
	}
	// A cycle through a terminal ticket: done-0005 waits for a; a waiting for
	// done-0005 would activate the loop the moment done-0005 reopened.
	_, err = Mutate(ms, "warp/a-0001", func(t *Ticket) error { return AddDep(t, "warp/done-0005") })
	if err == nil || !strings.Contains(err.Error(), "warp/done-0005") {
		t.Errorf("cycle through a terminal ticket: want a refusal, got %v", err)
	}
	// Promoting a leaf that carries deps to an epic.
	b := mustGet(t, ms, "warp/b-0002")
	b.Type = TypeEpic
	if _, err := SaveEdit(ms, b, false); err == nil || !strings.Contains(err.Error(), "acceptance leaf") {
		t.Errorf("promoting a leaf with deps: want the acceptance-leaf refusal, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// A leaf may still depend on an epic as a target.
	if _, err := Mutate(ms, "warp/a-0001", func(t *Ticket) error { return AddDep(t, "warp/epic-0003") }); err != nil {
		t.Errorf("a leaf depending on an epic it is not a child of should be accepted: %v", err)
	}
}

// The cycle check sees an epic's children as the write leaves them. A synced
// leaf naming a parent that does not exist yet, or one that is still a leaf,
// is no child in the snapshot — so a leaf that also depends on that parent
// carries no epic-to-child edge the snapshot can show, and the create or the
// promotion is what closes the loop.
func TestCycleCheckSeesChildrenACreateOrPromotionWouldAdopt(t *testing.T) {
	root, ms := centralFixture(t, true)
	warpDir := filepath.Join(root, "tickets", "warp")
	mustCreate(t, ms, mk("warp/leaf-0003", StatusOpen))
	plantTicketFile(t, warpDir, "child-0002.md", mkWithParent("child-0002", StatusOpen, "epic-0001", "epic-0001"))
	plantTicketFile(t, warpDir, "child-0004.md", mkWithParent("child-0004", StatusOpen, "leaf-0003", "leaf-0003"))
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	// Creating the missing epic: epic -> child -> epic.
	err := ms.Create(mkEpic("warp/epic-0001", StatusBacklog, ""))
	if err == nil || !strings.Contains(err.Error(), "warp/epic-0001 -> warp/child-0002 -> warp/epic-0001") {
		t.Errorf("creating an epic its future child depends on: want a refusal naming the path, got %v", err)
	}
	// Promoting a leaf such children name: leaf -> child -> leaf.
	leaf := mustGet(t, ms, "warp/leaf-0003")
	leaf.Type = TypeEpic
	_, err = SaveEdit(ms, leaf, false)
	if err == nil || !strings.Contains(err.Error(), "warp/leaf-0003 -> warp/child-0004 -> warp/leaf-0003") {
		t.Errorf("promoting a leaf its future child depends on: want a refusal naming the path, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
}

func TestAcceptanceLeafGatesASecondInitiativeOnSharedWork(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/init-a-0001", StatusBacklog, ""),
		mkWithParent("warp/impl-0002", StatusOpen, "warp/init-a-0001"),
		mkEpic("loom/init-b-0003", StatusBacklog, ""),
		mkWithParent("loom/accept-0004", StatusReady, "loom/init-b-0003", "warp/impl-0002"),
	)
	accept := mustGet(t, ms, "loom/accept-0004")
	if !IsBlocked(ms, accept) {
		t.Fatal("the acceptance leaf reads unblocked while the shared implementation is open")
	}
	if got := statusOf(t, ms, "loom/init-b-0003"); got == StatusDone {
		t.Fatal("initiative B reads done while its acceptance leaf waits")
	}
	frontier, err := FrontierTickets(ms)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range frontier {
		if tk.ID == "loom/accept-0004" {
			t.Error("the blocked acceptance leaf is on the frontier")
		}
	}

	if err := setStatus(t, ms, "warp/impl-0002", StatusDone); err != nil {
		t.Fatal(err)
	}
	if IsBlocked(ms, mustGet(t, ms, "loom/accept-0004")) {
		t.Error("the acceptance leaf stays blocked after the implementation finished")
	}
	if got := statusOf(t, ms, "warp/init-a-0001"); got != StatusDone {
		t.Errorf("initiative A = %q, want %q", got, StatusDone)
	}
	if err := setStatus(t, ms, "loom/accept-0004", StatusDone); err != nil {
		t.Fatal(err)
	}
	if got := statusOf(t, ms, "loom/init-b-0003"); got != StatusDone {
		t.Errorf("initiative B = %q, want %q once its acceptance leaf is done", got, StatusDone)
	}
}

// readyAnswers is what every readiness surface says about one ticket through
// one store: the single-ticket checks `tk show` and `tk blocked` run, and the
// listings `tk ready`, `tk frontier` and `tk blocked` run.
type readyAnswers struct {
	blocked, ready          bool
	blockingDeps            []string
	listedReady, onFrontier bool
	listedBlocked           bool
}

func readyAnswersFor(t *testing.T, s Store, id string) readyAnswers {
	t.Helper()
	tk := mustGet(t, s, id)
	a := readyAnswers{
		blocked:      IsBlocked(s, tk),
		ready:        IsReady(s, tk),
		blockingDeps: BlockingDeps(s, tk),
	}
	listed := func(run func(Store) ([]*Ticket, error)) bool {
		tickets, err := run(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range tickets {
			if SameTicketID(l.ID, id) {
				return true
			}
		}
		return false
	}
	a.listedReady = listed(ReadyTickets)
	a.onFrontier = listed(FrontierTickets)
	a.listedBlocked = listed(BlockedTickets)
	return a
}

func TestProjectStoreResolvesForeignReferencesLikeTheMultiStore(t *testing.T) {
	// The CLI's default store is the project store, and FileStore.Resolve
	// refuses a prefix naming another project — so a reference into another
	// namespace has to be resolved through the boundary, or `tk ready` from
	// the repo disagrees with `tk frontier` and MCP about the same ticket.
	root, ms := centralFixture(t, true)
	loom := nsStore(root, "loom")
	mustCreate(t, ms,
		mkEpic("_root/unified-0001", StatusBacklog, ""),
		mkEpic("warp/init-a-0002", StatusBacklog, ""),
		mkWithParent("warp/impl-0003", StatusOpen, "warp/init-a-0002"),
		mkEpic("loom/init-b-0004", StatusBacklog, ""),
	)
	mustCreate(t, loom,
		// The acceptance-leaf shape: a dep on another project's work.
		mkWithParent("accept-0005", StatusReady, "loom/init-b-0004", "warp/impl-0003"),
		// The activated foreign-parent shape: a child of an epic elsewhere.
		mkWithParent("foreign-child-0006", StatusReady, "_root/unified-0001"),
	)

	want := readyAnswers{blocked: true, blockingDeps: []string{"warp/impl-0003"}, listedBlocked: true}
	for _, s := range []Store{loom, ms} {
		if got := readyAnswersFor(t, s, "loom/accept-0005"); !reflect.DeepEqual(got, want) {
			t.Errorf("%T: acceptance leaf while its dep is open = %+v, want %+v", s, got, want)
		}
	}
	if err := setStatus(t, ms, "warp/impl-0003", StatusDone); err != nil {
		t.Fatal(err)
	}
	want = readyAnswers{ready: true, listedReady: true, onFrontier: true}
	for _, s := range []Store{loom, ms} {
		if got := readyAnswersFor(t, s, "loom/accept-0005"); !reflect.DeepEqual(got, want) {
			t.Errorf("%T: acceptance leaf once its dep is done = %+v, want %+v", s, got, want)
		}
		if got := readyAnswersFor(t, s, "loom/foreign-child-0006"); !reflect.DeepEqual(got, want) {
			t.Errorf("%T: child of a foreign epic = %+v, want %+v", s, got, want)
		}
	}
}

func TestMissingDepIsNotAnsweredByATicketContainingItsID(t *testing.T) {
	// The index misses a dep that does not exist, and the fallback read would
	// substring-match it: `warp/task-0001` reading done off
	// `warp/other-task-0001` clears a blocker nothing satisfied. Exact identity
	// is the rule in the snapshot and the cycle check, and in the fallback too,
	// whichever namespace the read goes to.
	root, ms := centralFixture(t, true)
	loom := nsStore(root, "loom")
	mustCreate(t, ms,
		mk("warp/other-task-0001", StatusDone),
		mk("loom/other-task-0002", StatusDone),
		mk("loom/wait-foreign-0003", StatusReady, "warp/task-0001"),
		mk("loom/wait-local-0004", StatusReady, "loom/task-0002"),
	)
	for _, c := range []struct{ id, dep string }{
		{"loom/wait-foreign-0003", "warp/task-0001"},
		{"loom/wait-local-0004", "loom/task-0002"},
	} {
		want := readyAnswers{blocked: true, blockingDeps: []string{c.dep}, listedBlocked: true}
		for _, s := range []Store{loom, ms} {
			if got := readyAnswersFor(t, s, c.id); !reflect.DeepEqual(got, want) {
				t.Errorf("%T: %s = %+v, want %+v — its dep is missing, not the done ticket whose ID contains it", s, c.id, got, want)
			}
		}
	}
}

func TestSupportedFeaturesIncludeCrossProjectParents(t *testing.T) {
	cat := &Catalog{RequiredFeatures: []string{FeatureRootNamespace, FeatureCrossProjectParents}}
	if !cat.CrossProjectParentsActivated() || cat.UnsupportedFeatures() != nil {
		t.Error("a catalog requiring cross-project-parents should be supported and read as activated")
	}
	var none *Catalog
	if none.CrossProjectParentsActivated() {
		t.Error("a nil catalog activates nothing")
	}
	if err := (&Catalog{RequiredFeatures: []string{"time-travel"}}).CheckFeatures(""); err == nil {
		t.Error("an unknown feature should still be refused")
	}
	var unsupported *UnsupportedFeatureError
	if err := (&Catalog{RequiredFeatures: []string{"time-travel"}}).CheckFeatures(""); !errors.As(err, &unsupported) {
		t.Errorf("want UnsupportedFeatureError, got %v", err)
	}
}
