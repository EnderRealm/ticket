package ticket

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The helper process protocol: the test binary is re-run with the environment
// below set, takes the store lock named by the path exclusively, reports
// "held" on stdout, sleeps for the hold, and exits. That is another tk process
// holding the store, which is what the cross-process cases have to wait on.
const (
	lockHelperEnv  = "TK_TEST_STORE_LOCK_HELPER"
	lockHelperPath = "TK_TEST_STORE_LOCK_PATH"
	lockHelperHold = 300 * time.Millisecond
)

func TestStoreLockHelperProcess(t *testing.T) {
	if os.Getenv(lockHelperEnv) != "1" {
		t.Skip("helper process entry point")
	}
	release, err := acquireStoreLock(os.Getenv(lockHelperPath), true)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(2)
	}
	fmt.Println("held")
	time.Sleep(lockHelperHold)
	release()
	os.Exit(0)
}

// holdStoreLockInAnotherProcess starts the helper against c's lock and returns
// once it reports the lock held, plus a wait for it to exit.
func holdStoreLockInAnotherProcess(t *testing.T, c *central) func() {
	t.Helper()
	path, err := c.lockPath()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreLockHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), lockHelperEnv+"=1", lockHelperPath+"="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "held" {
			break
		}
		if strings.HasPrefix(line, "error:") {
			t.Fatalf("helper: %s", line)
		}
	}
	return func() {
		if err := cmd.Wait(); err != nil {
			t.Errorf("helper exited: %v", err)
		}
	}
}

// ─── AC5: cross-process consistency and the nested-entry refusal ───────────

func TestStoreLockWaitsForAnotherProcessWithinBoundedDeadlines(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/child-0002", StatusOpen, "warp/epic-0001"),
	)
	c := centralForMulti(ms)
	lockPath, err := c.lockPath()
	if err != nil {
		t.Fatal(err)
	}
	cache, _ := os.UserCacheDir()
	if !strings.HasPrefix(lockPath, cache) {
		t.Errorf("store lock %s is not under the cache directory %s", lockPath, cache)
	}
	before := snapshotTree(t, root)

	// A snapshot waits for the other process's exclusive hold, then completes.
	wait := holdStoreLockInAnotherProcess(t, c)
	started := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := ms.Snapshot()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if waited := time.Since(started); waited < lockHelperHold/2 {
			t.Errorf("snapshot returned after %v while another process held the lock for %v", waited, lockHelperHold)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("snapshot did not complete after the other process released the lock")
	}
	wait()
	assertTreeUnchanged(t, before, snapshotTree(t, root))

	// A write waits the same way, then lands.
	wait = holdStoreLockInAnotherProcess(t, c)
	started = time.Now()
	go func() {
		_, err := Mutate(ms, "warp/child-0002", func(t *Ticket) error {
			t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "after the wait"})
			return nil
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Mutate: %v", err)
		}
		if waited := time.Since(started); waited < lockHelperHold/2 {
			t.Errorf("write returned after %v while another process held the lock for %v", waited, lockHelperHold)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write did not complete after the other process released the lock")
	}
	wait()
	if notes := mustGet(t, ms, "warp/child-0002").Notes; len(notes) != 1 {
		t.Errorf("notes = %v, want the write to have landed", notes)
	}
	// Nothing about the lock lives in the synced tree.
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(path, ".lock") {
			t.Errorf("lock file %s inside the store root", path)
		}
		return nil
	})
}

func TestStoreLockFileIsNoTicketLockFile(t *testing.T) {
	// A single store's lock hashes the directory its ticket locks hash, so the
	// name has to be one no ticket ID produces.
	s := NewFileStore(t.TempDir())
	storeLock, err := centralFor(s).lockPath()
	if err != nil {
		t.Fatal(err)
	}
	ticketLock, err := s.lockFile("store")
	if err != nil {
		t.Fatal(err)
	}
	if storeLock == ticketLock {
		t.Errorf("store lock %s is the lock of a ticket whose ID is \"store\"", storeLock)
	}
}

func TestNestedEntryPointsInsideAMutationAreRefused(t *testing.T) {
	orig := storeLockTimeout
	storeLockTimeout = 150 * time.Millisecond
	t.Cleanup(func() { storeLockTimeout = orig })

	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/child-0002", StatusOpen, "warp/epic-0001"),
		mk("warp/other-0003", StatusOpen),
	)
	warp := nsStore(root, "warp")
	other := mustGet(t, ms, "warp/other-0003")
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	nested := map[string]func() error{
		"Update": func() error {
			other.Title = "changed inside a callback"
			return ms.Update(other)
		},
		"Mutate": func() error {
			_, err := Mutate(warp, "other-0003", func(t *Ticket) error { t.Priority = 0; return nil })
			return err
		},
		"Get of an epic": func() error {
			_, err := ms.Get("warp/epic-0001")
			return err
		},
		"List": func() error {
			_, err := warp.List()
			return err
		},
	}
	for name, call := range nested {
		started := time.Now()
		var inner error
		_, outer := Mutate(ms, "warp/child-0002", func(t *Ticket) error {
			inner = call()
			return inner
		})
		if !errors.Is(inner, ErrStoreLockTimeout) {
			t.Errorf("%s inside a Mutate callback = %v, want ErrStoreLockTimeout", name, inner)
		}
		if outer == nil {
			t.Errorf("%s: the outer Mutate wrote despite the callback failing", name)
		}
		if waited := time.Since(started); waited > 5*time.Second {
			t.Errorf("%s: the nested call took %v to fail — it deadlocked until something else gave up", name, waited)
		}
		if inner != nil && !strings.Contains(inner.Error(), "nested inside a mutation callback") {
			t.Errorf("%s: the refusal should say a nested call is refused, got %v", name, inner)
		}
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// A Get of a leaf is a single file read and stays safe inside a callback.
	if _, err := Mutate(ms, "warp/child-0002", func(t *Ticket) error {
		leaf, err := ms.Get("warp/other-0003")
		if err != nil {
			return err
		}
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "saw " + leaf.ID})
		return nil
	}); err != nil {
		t.Errorf("a leaf read inside a callback should succeed: %v", err)
	}
}

func TestConflictsSurviveTheBoundary(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms, mk("warp/conf-0001", StatusOpen))

	stale := mustGet(t, ms, "warp/conf-0001")
	winner := mustGet(t, ms, "warp/conf-0001")
	winner.Title = "Written first"
	if err := ms.Update(winner); err != nil {
		t.Fatalf("Update: %v", err)
	}
	stale.Title = "Computed from what the winner replaced"
	if err := ms.Update(stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("Update of a stale read = %v, want ErrConflict", err)
	}
	if got := mustGet(t, ms, "warp/conf-0001").Title; got != "Written first" {
		t.Errorf("Title = %q, want the winner's", got)
	}
}

func TestAbandonCascadesLocalChildrenUnderOneHold(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/open-0002", StatusOpen, "warp/epic-0001"),
		mkWithParent("warp/ready-0003", StatusReady, "warp/epic-0001"),
		mkWithParent("warp/done-0004", StatusDone, "warp/epic-0001"),
		mkWithParent("loom/done-0005", StatusDone, "warp/epic-0001"),
	)
	epic := mustGet(t, ms, "warp/epic-0001")
	epic.Status = StatusClosed
	closed, err := SaveEdit(ms, epic, true)
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if strings.Join(closed, ",") != "warp/open-0002,warp/ready-0003" {
		t.Errorf("closed = %v, want the two live local children in sorted order", closed)
	}
	for id, want := range map[string]Status{
		"warp/open-0002":  StatusClosed,
		"warp/ready-0003": StatusClosed,
		"warp/done-0004":  StatusDone,
		"loom/done-0005":  StatusDone,
		"warp/epic-0001":  StatusClosed,
	} {
		if got := statusOf(t, ms, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

// ─── AC6: destructive operations need a complete inbound snapshot ──────────

func TestDeleteRefusesWhileReferencedOrUnproven(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("loom/child-0002", StatusOpen, "warp/epic-0001"),
		mk("warp/blocker-0003", StatusOpen),
		mk("ticket/waiter-0004", StatusOpen, "warp/blocker-0003"),
		mk("warp/lone-0005", StatusOpen),
	)
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	err := ms.Delete("warp/epic-0001")
	if err == nil || !strings.Contains(err.Error(), "loom/child-0002 (parent)") {
		t.Errorf("deleting a parent: want a refusal naming the child, got %v", err)
	}
	err = nsStore(root, "warp").Delete("blocker-0003")
	if err == nil || !strings.Contains(err.Error(), "ticket/waiter-0004 (dep)") {
		t.Errorf("deleting a dep target: want a refusal naming the dependant, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// Unreferenced, but the snapshot cannot prove it while a file is unreadable.
	plantUnreadable(t, filepath.Join(root, "tickets", "ticket"), "broken-9999.md")
	before = snapshotTree(t, filepath.Join(root, "tickets"))
	err = ms.Delete("warp/lone-0005")
	if err == nil || !strings.Contains(err.Error(), "complete snapshot") || !strings.Contains(err.Error(), "broken-9999.md") {
		t.Errorf("deleting on an incomplete snapshot: want a refusal naming the evidence, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	os.Remove(filepath.Join(root, "tickets", "ticket", "broken-9999.md"))
	if err := ms.Delete("warp/lone-0005"); err != nil {
		t.Errorf("deleting an unreferenced ticket on a complete snapshot: %v", err)
	}
}

// References land on the ID a file stores, not on its name: a file renamed
// while keeping its id is still that ticket to everything naming it, and a
// delete by the new name has to find those referrers.
func TestDeleteChecksReferencesToTheStoredID(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("loom/child-0002", StatusOpen, "warp/epic-0001"),
	)
	warpDir := filepath.Join(root, "tickets", "warp")
	if err := os.Rename(filepath.Join(warpDir, "epic-0001.md"), filepath.Join(warpDir, "renamed.md")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	err := ms.Delete("warp/renamed")
	if err == nil || !strings.Contains(err.Error(), "loom/child-0002 (parent)") || !strings.Contains(err.Error(), "stored as epic-0001") {
		t.Errorf("deleting a renamed parent: want a refusal naming the child and the stored id, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// Unreferenced, it deletes by its filename.
	if err := ms.Delete("loom/child-0002"); err != nil {
		t.Fatal(err)
	}
	if err := ms.Delete("warp/renamed"); err != nil {
		t.Errorf("deleting the renamed file once nothing names its ticket: %v", err)
	}
}

func TestEpicToLeafRefusedWhileChildrenExistAnywhere(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/w-0002", StatusOpen, "_root/epic-0001"),
		mkWithParent("loom/l-0003", StatusDone, "_root/epic-0001"),
		mkEpic("_root/empty-0004", StatusBacklog, ""),
	)
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	epic := mustGet(t, ms, "_root/epic-0001")
	epic.Type = TypeFeature
	_, err := SaveEdit(ms, epic, false)
	if err == nil || !strings.Contains(err.Error(), "loom: l-0003") || !strings.Contains(err.Error(), "warp: w-0002") {
		t.Errorf("want a refusal naming the children grouped by project, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

	// A childless epic converts — unless the snapshot cannot prove it childless.
	plantUnreadable(t, filepath.Join(root, "tickets", "ticket"), "broken-9999.md")
	empty := mustGet(t, ms, "_root/empty-0004")
	empty.Type = TypeFeature
	if _, err := SaveEdit(ms, empty, false); err == nil || !strings.Contains(err.Error(), "broken-9999.md") {
		t.Errorf("converting on an incomplete snapshot: want a refusal naming the evidence, got %v", err)
	}
	os.Remove(filepath.Join(root, "tickets", "ticket", "broken-9999.md"))
	empty = mustGet(t, ms, "_root/empty-0004")
	empty.Type = TypeFeature
	if _, err := SaveEdit(ms, empty, false); err != nil {
		t.Errorf("converting a childless epic: %v", err)
	}
}

// A synced or legacy child naming an epic in another project is no child in
// the snapshot while cross-project parents are not activated, but it still
// names the epic: converted to a leaf, the epic could never take that child
// back once the feature is activated. The guard reads the inbound references,
// not the placed children.
func TestEpicToLeafRefusedWhileAForeignChildNamesItUnactivated(t *testing.T) {
	root, ms := centralFixture(t, false)
	mustCreate(t, ms, mkEpic("warp/epic-0001", StatusBacklog, ""))
	plantTicketFile(t, filepath.Join(root, "tickets", "loom"), "child-0002.md", mkWithParent("child-0002", StatusOpen, "warp/epic-0001"))
	snap, err := ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if children := snap.Children("warp/epic-0001"); len(children) != 0 {
		t.Fatalf("unactivated: the foreign child was placed under the epic (%d children)", len(children))
	}
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	epic := mustGet(t, ms, "warp/epic-0001")
	epic.Type = TypeFeature
	_, err = SaveEdit(ms, epic, false)
	if err == nil || !strings.Contains(err.Error(), "loom: child-0002") {
		t.Errorf("converting an epic a foreign child names: want a refusal naming the child, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	if stored, err := nsStore(root, "warp").getStored("epic-0001"); err != nil || stored.Type != TypeEpic {
		t.Errorf("epic-0001 stored as %v (%v), want it still an epic", stored, err)
	}
}

// An epic whose ID a second file claims is nobody's in the snapshot, so the
// prior an update validates against has to come from the file the write lands
// on: validated against no prior, the conversion would pass as a create and
// the epic-to-leaf guard would never run over the children still naming it.
func TestEpicToLeafRefusedWhenEpicIDIsDuplicated(t *testing.T) {
	for _, via := range []string{"Update", "SaveEdit"} {
		t.Run(via, func(t *testing.T) {
			root, ms := centralFixture(t, true)
			mustCreate(t, ms,
				mkEpic("warp/epic-0001", StatusBacklog, ""),
				mkWithParent("loom/child-0002", StatusOpen, "warp/epic-0001"),
			)
			plantTicketFile(t, filepath.Join(root, "tickets", "warp"), "zz-epic-0001.md", mkEpic("warp/epic-0001", StatusBacklog, ""))
			captureWarnings(t)
			before := snapshotTree(t, filepath.Join(root, "tickets"))

			epic := mustGet(t, ms, "warp/epic-0001")
			epic.Type = TypeFeature
			var err error
			if via == "Update" {
				err = ms.Update(epic)
			} else {
				_, err = SaveEdit(ms, epic, false)
			}
			if err == nil || !strings.Contains(err.Error(), "complete snapshot") || !strings.Contains(err.Error(), "zz-epic-0001.md") {
				t.Errorf("converting a duplicated epic: want a refusal naming the duplicate, got %v", err)
			}
			assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
			if stored, err := nsStore(root, "warp").getStored("epic-0001"); err != nil || stored.Type != TypeEpic {
				t.Errorf("epic-0001 stored as %v (%v), want it still an epic", stored, err)
			}
		})
	}
}

func TestAbandonRefusesUnfinishedForeignChildrenWithoutWriting(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/epic-0001", StatusBacklog, ""),
		mkWithParent("_root/local-0002", StatusOpen, "_root/epic-0001"),
		mkWithParent("warp/w-0003", StatusOpen, "_root/epic-0001"),
		mkWithParent("warp/w-0004", StatusReady, "_root/epic-0001"),
		mkWithParent("loom/l-0005", StatusBacklog, "_root/epic-0001"),
		mkWithParent("loom/l-0006", StatusDone, "_root/epic-0001"),
	)
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	epic := mustGet(t, ms, "_root/epic-0001")
	epic.Status = StatusClosed
	closed, err := SaveEdit(ms, epic, true)
	if err == nil {
		t.Fatal("abandoning an epic with unfinished foreign children succeeded")
	}
	if !strings.Contains(err.Error(), "loom: l-0005; warp: w-0003, w-0004") {
		t.Errorf("want the affected IDs grouped by project, got %v", err)
	}
	if strings.Contains(err.Error(), "l-0006") || strings.Contains(err.Error(), "local-0002") {
		t.Errorf("the refusal names a finished or local child: %v", err)
	}
	if len(closed) != 0 {
		t.Errorf("closed = %v, want nothing", closed)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	if stored, _ := nsStore(root, "_root").getStored("epic-0001"); stored.Abandoned {
		t.Error("the refused abandon recorded its intent")
	}

	// The epic's own project store refuses the same way.
	local := mustGet(t, nsStore(root, "_root"), "epic-0001")
	local.Status = StatusClosed
	if _, err := SaveEdit(nsStore(root, "_root"), local, true); err == nil || !strings.Contains(err.Error(), "warp: w-0003") {
		t.Errorf("abandon via the project store: want the same refusal, got %v", err)
	}

	// An incomplete snapshot refuses too, even with every child local.
	mustCreate(t, ms, mkEpic("_root/lone-0007", StatusBacklog, ""), mkWithParent("_root/kid-0008", StatusOpen, "_root/lone-0007"))
	plantUnreadable(t, filepath.Join(root, "tickets", "ticket"), "broken-9999.md")
	lone := mustGet(t, ms, "_root/lone-0007")
	lone.Status = StatusClosed
	if _, err := SaveEdit(ms, lone, true); err == nil || !strings.Contains(err.Error(), "broken-9999.md") {
		t.Errorf("abandon on an incomplete snapshot: want a refusal naming the evidence, got %v", err)
	}
	if got := statusOf(t, ms, "_root/kid-0008"); got != StatusOpen {
		t.Errorf("kid-0008 = %q after a refused abandon, want %q", got, StatusOpen)
	}
}

func TestRepairsSucceedOnAnIncompleteSnapshot(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/child-0002", StatusOpen, "warp/epic-0001", "loom/gone-9999"),
		mk("loom/leaf-0003", StatusReady),
	)
	plantUnreadable(t, filepath.Join(root, "tickets", "ticket"), "broken-9999.md")

	// Clearing a parent, removing a dep, editing text and moving a leaf's
	// status prove no absence, so none of them needs the store read in full.
	if _, err := Mutate(ms, "warp/child-0002", func(t *Ticket) error {
		t.Parent = ""
		RemoveDep(t, "loom/gone-9999")
		t.Title = "Repaired"
		return nil
	}); err != nil {
		t.Errorf("repairing a leaf on an incomplete snapshot: %v", err)
	}
	child := mustGet(t, ms, "warp/child-0002")
	if child.Parent != "" || len(child.Deps) != 0 || child.Title != "Repaired" {
		t.Errorf("repair did not land: %+v", child)
	}
	if err := setStatus(t, ms, "loom/leaf-0003", StatusDone); err != nil {
		t.Errorf("changing a leaf's status on an incomplete snapshot: %v", err)
	}
	// An unrelated edit of the epic lands too; only the absence proofs refuse.
	epic := mustGet(t, ms, "warp/epic-0001")
	epic.Notes = append(epic.Notes, Note{Timestamp: time.Now().UTC(), Text: "still editable"})
	if _, err := SaveEdit(ms, epic, false); err != nil {
		t.Errorf("editing an epic on an incomplete snapshot: %v", err)
	}
}

func TestSnapshotExposesTheGraph(t *testing.T) {
	_, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("_root/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/w-0002", StatusOpen, "_root/epic-0001"),
		mk("loom/l-0003", StatusOpen, "warp/w-0002"),
	)
	link := mustGet(t, ms, "warp/w-0002")
	if _, err := Mutate(ms, "loom/l-0003", func(t *Ticket) error { AddLink(t, link); return nil }); err != nil {
		t.Fatal(err)
	}
	snap, err := ms.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || len(snap.Skips) != 0 {
		t.Errorf("complete=%v skips=%v, want a clean snapshot", snap.Complete, snap.Skips)
	}
	if epic, ok := snap.Get("_root/epic-0001"); !ok || epic.Status != StatusOpen {
		t.Errorf("Get epic = %+v %v, want the derived epic", epic, ok)
	}
	if children := snap.Children("_root/epic-0001"); len(children) != 1 || children[0].ID != "warp/w-0002" {
		t.Errorf("Children = %v", ids2(children))
	}
	inbound := snap.Inbound("warp/w-0002")
	kinds := map[InboundKind]string{}
	for _, in := range inbound {
		kinds[in.Kind] = in.From
	}
	if kinds[InboundDep] != "loom/l-0003" || kinds[InboundLink] != "loom/l-0003" || len(inbound) != 2 {
		t.Errorf("Inbound = %+v, want the dep and the link from loom/l-0003", inbound)
	}
	if inbound := snap.Inbound("_root/epic-0001"); len(inbound) != 1 || inbound[0].Kind != InboundParent {
		t.Errorf("Inbound epic = %+v, want the parent reference", inbound)
	}
	// The project store's snapshot is the same graph.
	fromProject, err := nsStore(filepath.Dir(ms.rootDir), "warp").Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fromProject.Tickets) != len(snap.Tickets) {
		t.Errorf("project snapshot holds %d tickets, central %d", len(fromProject.Tickets), len(snap.Tickets))
	}
}

// ─── The boundary is the supplied store unless the layout is central ───────

func TestIsolatedStoreNamedForItsProjectIsNoCentralStore(t *testing.T) {
	// /work/app for project app: the directory is named for its project and
	// nothing else about the layout is central. Its parent is not the tickets
	// directory, so its siblings are not namespaces and the catalog two levels
	// up is not its catalog.
	base := t.TempDir()
	work := filepath.Join(base, "work")
	app := NewProjectFileStore(filepath.Join(work, "app"), "app")
	sibling := NewProjectFileStore(filepath.Join(work, "other"), "other")
	mustCreate(t, sibling, mk("other-0001", StatusOpen))
	writeCatalog(t, base, "required_features: [time-travel]\n")

	if !centralFor(app).isSingle() {
		t.Fatalf("%s is a boundary over %s, want one over the store alone", app.Dir, centralFor(app).ticketsDir)
	}
	// The catalog at base would refuse this write if the store were read as
	// central; an isolated store has no catalog to consult.
	mustCreate(t, app, mk("app-0001", StatusOpen))
	snap, err := app.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || len(snap.Skips) != 0 {
		t.Errorf("complete=%v skips=%v, want a clean snapshot of the one directory", snap.Complete, snap.Skips)
	}
	if len(snap.Tickets) != 1 || snap.Tickets[0].ID != "app/app-0001" {
		t.Errorf("snapshot tickets = %v, want the store's own ticket alone", ids2(snap.Tickets))
	}
	if _, ok := snap.Get("other/other-0001"); ok {
		t.Error("the sibling directory's ticket was read into the snapshot")
	}

	// Both halves present — <root>/tickets/<project> — is the central layout.
	central := NewProjectFileStore(filepath.Join(base, ticketsDirName, "app"), "app")
	if c := centralFor(central); c.isSingle() || c.root != base {
		t.Errorf("%s: boundary = %+v, want the central store at %s", central.Dir, c, base)
	}
}

// The layout is judged on the cleaned path: a trailing slash left Base and Dir
// one level off, so `<root>/tickets/warp/` was read as an isolated store whose
// snapshot omitted every other namespace and whose lock was keyed on the
// project directory — and a warp ticket still named from loom could be deleted
// through it while the same spelling without the slash refused.
func TestTrailingSlashStoreIsTheSameCentralStore(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mk("warp/blocker-0001", StatusOpen),
		mk("loom/waiter-0002", StatusOpen, "warp/blocker-0001"),
	)
	clean := nsStore(root, "warp")
	slashed := NewProjectFileStore(clean.Dir+string(filepath.Separator), "warp")

	c := centralFor(slashed)
	if c.isSingle() || c.root != root || c.ticketsDir != filepath.Join(root, "tickets") {
		t.Fatalf("%s: boundary = %+v, want the central store at %s", slashed.Dir, c, root)
	}
	same, err := c.sameCentral(centralFor(clean))
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Errorf("%s and %s take different store locks, want one", slashed.Dir, clean.Dir)
	}

	before := snapshotTree(t, filepath.Join(root, "tickets"))
	err = slashed.Delete("blocker-0001")
	if err == nil || !strings.Contains(err.Error(), "loom/waiter-0002 (dep)") {
		t.Errorf("deleting through the trailing-slash store: want a refusal naming the dependant, got %v", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
}

// ─── The catalog guard is decided under the lock the write lands under ─────

func TestQueuedWriteIsRefusedByTheCatalogThatArrivedWhileItWaited(t *testing.T) {
	// The catalog is rewritten while the write waits for the store lock — an
	// activation on a newer tk synced in mid-wait — so the check made before
	// the wait passed and only the one under the lock can refuse.
	cases := []struct {
		name  string
		write func(store *FileStore) error
	}{
		{"Create", func(store *FileStore) error { return store.Create(sampleTicket("late-0002")) }},
		{"Update", func(store *FileStore) error {
			tk := mustGet(t, store, "keep-0001")
			tk.Title = "queued"
			return store.Update(tk)
		}},
		{"Delete", func(store *FileStore) error { return store.Delete("keep-0001") }},
		{"Mutate", func(store *FileStore) error {
			_, err := Mutate(store, "keep-0001", func(t *Ticket) error { t.Title = "queued"; return nil })
			return err
		}},
		{"SaveEdit", func(store *FileStore) error {
			tk := mustGet(t, store, "keep-0001")
			tk.Title = "queued"
			_, err := SaveEdit(store, tk, false)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := catalogRoot(t)
			store := NewProjectFileStore(mkNamespaceDir(t, root, "proj"), "proj")
			mustCreate(t, store, sampleTicket("keep-0001"))
			tickets := filepath.Join(root, ticketsDirName)
			before := snapshotTree(t, tickets)

			lockPath, err := centralFor(store).lockPath()
			if err != nil {
				t.Fatal(err)
			}
			release, err := acquireStoreLock(lockPath, true)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- c.write(store) }()
			// Long enough for the write to pass its pre-lock check and queue on
			// the lock; the assertion below tells if it was not.
			time.Sleep(100 * time.Millisecond)
			writeCatalog(t, root, "required_features: [root-namespace, time-travel]\nnamespaces:\n  proj: {kind: project}\n")
			release()

			var err2 error
			select {
			case err2 = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the queued write did not return after the lock was released")
			}
			var unsupported *UnsupportedFeatureError
			if !errors.As(err2, &unsupported) {
				t.Fatalf("want UnsupportedFeatureError, got %v", err2)
			}
			// The pre-lock check wraps its refusal with the operation; the one
			// under the lock returns it as it is. The refusal has to be the
			// latter's, or the write was never queued and nothing was tested.
			if err2 != error(unsupported) {
				t.Errorf("refused before the lock (%v), want the guard under it", err2)
			}
			assertTreeUnchanged(t, before, snapshotTree(t, tickets))
		})
	}
}

// ─── A single store that cannot be listed fails, as it did before ──────────

func TestUnlistableSingleStoreIsAnErrorNotACatalogSkip(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lists every directory")
	}
	// A store with no project: a namespace skip in its name would be the ""
	// skip every reader takes for the catalog. Search-only rather than 000,
	// so the exact-path read a Get makes first still succeeds and the listing
	// the snapshot takes is what fails.
	s := NewFileStore(t.TempDir())
	mustCreate(t, s,
		mkEpic("epic-0001", StatusBacklog, ""),
		mkWithParent("leaf-0002", StatusDone, "epic-0001"),
	)
	if err := os.Chmod(s.Dir, 0o100); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.Dir, 0o755) })
	warnings := captureWarnings(t)

	if tickets, err := s.List(); err == nil || !strings.Contains(err.Error(), "listing "+s.Dir) {
		t.Errorf("List = %v, %v, want the listing error", ids2(tickets), err)
	}
	if tickets, skips, err := s.ListWithSkips(); err == nil || !strings.Contains(err.Error(), "listing "+s.Dir) {
		t.Errorf("ListWithSkips = %v, %v, %v, want the listing error", ids2(tickets), skips, err)
	}
	if epic, err := s.Get("epic-0001"); err == nil || !strings.Contains(err.Error(), "listing "+s.Dir) {
		t.Errorf("Get epic = %v, %v, want the listing error, not a demoted epic", epic, err)
	}
	if len(*warnings) != 0 {
		t.Errorf("warnings = %q, want none: a store with no catalog has none to warn about", *warnings)
	}
}

// ─── A parent two files claim is refused as the duplicate it is ────────────

func TestParentClaimedByTwoFilesIsRefusedAsADuplicate(t *testing.T) {
	for _, via := range []string{"Create", "Update"} {
		t.Run(via, func(t *testing.T) {
			root, ms := centralFixture(t, true)
			mustCreate(t, ms,
				mkEpic("warp/epic-0001", StatusBacklog, ""),
				mk("warp/leaf-0002", StatusOpen),
			)
			plantTicketFile(t, filepath.Join(root, "tickets", "warp"), "zz-epic-0001.md", mkEpic("warp/epic-0001", StatusBacklog, ""))
			captureWarnings(t)
			before := snapshotTree(t, filepath.Join(root, "tickets"))

			var err error
			if via == "Create" {
				err = ms.Create(mkWithParent("warp/leaf-0003", StatusOpen, "warp/epic-0001"))
			} else {
				leaf := mustGet(t, ms, "warp/leaf-0002")
				leaf.Parent = "warp/epic-0001"
				err = ms.Update(leaf)
			}
			// The caller named the epic in full; the fragment remedy is wrong.
			// The refusal is the read path's wording, naming both files.
			if err == nil || !strings.Contains(err.Error(), duplicateIssue("warp/epic-0001")) ||
				!strings.Contains(err.Error(), "zz-epic-0001.md") || !strings.Contains(err.Error(), `"warp/epic-0001.md"`) {
				t.Errorf("parenting under a duplicated epic: want the duplicate-claim refusal naming both files, got %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "ambiguous") {
				t.Errorf("parenting under a duplicated epic: refused as a fragment, got %v", err)
			}
			assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
		})
	}
}

// ─── A symlinked namespace is refused by every path through the boundary ───

// The central store is a git repo and git tracks symlinks, so a synced
// `tickets/foreign` can point outside the store. readSources leaves it out of
// the snapshot the way MultiStore.projects always did, but a leaf's dep on
// `foreign/x` falls through the listing to claimedStored, which lists the
// namespace through central.store — and a done ticket at the link's target
// would clear the dep the snapshot never saw. The refusal has to sit in
// central.store, so the single reads exclude the link exactly as the listing
// does. The external directory holds the one ticket that would change the
// answer: if any path reads through the link, the leaf comes back runnable.
func TestSymlinkedNamespaceIsRefusedByTheSingleReads(t *testing.T) {
	root, ms := centralFixture(t, true)
	loom := nsStore(root, "loom")
	outside := t.TempDir()
	plantTicketFile(t, outside, "x-0001.md", mk("x-0001", StatusDone))
	if err := os.Symlink(outside, filepath.Join(root, "tickets", "foreign")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	mustCreate(t, ms, mk("loom/waiter-0002", StatusReady, "foreign/x-0001"))
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
				t.Errorf("%s view offered loom/waiter-0002 as runnable off a done ticket behind a symlinked namespace", name)
			}
		}
	}
	waiter := mustGet(t, loom, "waiter-0002")
	if !IsBlocked(loom, waiter) || !IsBlocked(ms, mustGet(t, ms, "loom/waiter-0002")) {
		t.Error("the single-ticket check read loom/waiter-0002 unblocked through the symlinked namespace")
	}
	if got := BlockingDeps(loom, waiter); len(got) != 1 || got[0] != "foreign/x-0001" {
		t.Errorf("BlockingDeps = %v, want [foreign/x-0001]", got)
	}
	if _, err := ms.central().claimedStored("foreign/x-0001"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("claimedStored through a symlinked namespace: want the not-a-directory refusal, got %v", err)
	}
}
