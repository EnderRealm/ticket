package journal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/internal/state"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// fakeLoom puts an executable `loom` on PATH ahead of anything else, so the
// machine's own loom is never the thing a test runs, and returns the file the
// fake appends its argv to. exitCode is what it exits with.
func fakeLoom(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv.txt")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexit %d\n", argv, exitCode)
	if err := os.WriteFile(filepath.Join(dir, loomBinary), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argv
}

// loomInvocations returns the fake's recorded argv lines, waiting for at least
// want of them: the runs are started and reaped off the cycle, so the child has
// not necessarily written its line by the time RunWatchCycle returns.
func loomInvocations(t *testing.T, argv string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(argv)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read fake loom argv: %v", err)
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
		if len(lines) >= want || time.Now().After(deadline) {
			return lines
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func retrospectMarkers(t *testing.T, projectName string) string {
	t.Helper()
	path, err := state.RetrospectLogPath(projectName)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read markers: %v", err)
	}
	return string(raw)
}

func markerFileExists(t *testing.T, projectName string) bool {
	t.Helper()
	path, err := state.RetrospectLogPath(projectName)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(path)
	return err == nil
}

func mkTicket(t *testing.T, store ticket.Store, title string, status ticket.Status) *ticket.Ticket {
	t.Helper()
	tk := &ticket.Ticket{
		ID:       ticket.GenerateID(title),
		Title:    title,
		Type:     ticket.TypeFeature,
		Status:   status,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

// The flag is the whole gate: with it off nothing is fired and no marker file is
// written at all, so tk stays free of loom on every machine that never asked.
func TestWatchCycle_RetrospectFlagOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-off")
	mkTicket(t, store, "Already done", ticket.StatusDone)
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("retro-off", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 0 || result.RetrospectSeeded != 0 {
		t.Errorf("fired = %d, seeded = %d, want 0 and 0", result.RetrospectFired, result.RetrospectSeeded)
	}
	if markerFileExists(t, "retro-off") {
		t.Error("marker file written with the flag off")
	}
	if _, err := os.Stat(argv); err == nil {
		t.Error("loom was invoked with the flag off")
	}
}

// Enabling the flag must not blast a retrospect across the store's whole
// history: the first cycle records what is already closed and fires nothing.
func TestWatchCycle_RetrospectSeedsWithoutFiring(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-seed")
	done := mkTicket(t, store, "Already done", ticket.StatusDone)
	closed := mkTicket(t, store, "Already closed", ticket.StatusClosed)
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	result, err := RunWatchCycle("retro-seed", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectSeeded != 2 || result.RetrospectFired != 0 {
		t.Fatalf("seeded = %d, fired = %d, want 2 and 0", result.RetrospectSeeded, result.RetrospectFired)
	}
	markers := retrospectMarkers(t, "retro-seed")
	for _, id := range []string{done.ID, closed.ID} {
		if !strings.Contains(markers, id) {
			t.Errorf("markers %q do not record %s", markers, id)
		}
	}
	if _, err := os.Stat(argv); err == nil {
		t.Error("seeding invoked loom")
	}
}

// The close path the trigger exists for, end to end: a `Closes:` commit
// auto-closes the ticket and the same cycle hands it to loom under its
// namespaced ID, exactly once ever.
func TestWatchCycle_RetrospectFiresOnceOnClose(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-fire")
	tk := mkTicket(t, store, "Work to land", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "["+tk.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-fire", cfg, store); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("retro-fire", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 1 {
		t.Fatalf("Closed = %d, want 1", result.Closed)
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want 1: %v", result.RetrospectFired, result.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-fire/" + tk.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}

	again, err := RunWatchCycle("retro-fire", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if again.RetrospectFired != 0 {
		t.Errorf("fired = %d on the second cycle, want 0", again.RetrospectFired)
	}
	if got := loomInvocations(t, argv, 1); len(got) != 1 {
		t.Errorf("loom invocations = %v, want the one", got)
	}
}

// A ticket closed by hand between cycles — `tk edit`, the TUI, an MCP write —
// is why the trigger scans the store instead of watching the close loop.
func TestWatchCycle_RetrospectFiresOnExternalClose(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-ext")
	tk := mkTicket(t, store, "Closed elsewhere", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-ext", cfg, store); err != nil {
		t.Fatal(err)
	}

	if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Status = ticket.StatusDone
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result, err := RunWatchCycle("retro-ext", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want 1: %v", result.RetrospectFired, result.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-ext/" + tk.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
}

// A loom that exits non-zero costs a log line and nothing else: the cycle
// reports its close, and the marker stands so the failure is not retried on
// every tick for the life of the daemon.
func TestWatchCycle_RetrospectFailingLoomIsNotFatal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 1)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-fail")
	tk := mkTicket(t, store, "Work to land", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "["+tk.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-fail", cfg, store); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("retro-fail", cfg, store)
	if err != nil {
		t.Fatalf("a failing loom failed the cycle: %v", err)
	}
	if result.Closed != 1 || result.Appended != 1 {
		t.Errorf("Closed = %d, Appended = %d, want 1 and 1", result.Closed, result.Appended)
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want 1: %v", result.RetrospectFired, result.Warnings)
	}
	if got := loomInvocations(t, argv, 1); len(got) != 1 {
		t.Errorf("loom invocations = %v, want the one", got)
	}
	if markers := retrospectMarkers(t, "retro-fail"); !strings.Contains(markers, tk.ID) {
		t.Errorf("markers %q do not record the failed run", markers)
	}
}

// With no loom on PATH nothing is recorded, so the close is still pending and
// fires on the first cycle after loom is installed.
func TestWatchCycle_RetrospectMissingLoomRecordsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)

	// A PATH with git and nothing else: the cycle shells out to git, and the
	// machine's real loom must not be reachable.
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(gitPath))
	if _, err := exec.LookPath(loomBinary); err == nil {
		t.Skipf("%s lives beside git, so it cannot be made missing", loomBinary)
	}

	store := ticket.NewProjectFileStore(t.TempDir(), "retro-noloom")
	tk := mkTicket(t, store, "Work to land", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "["+tk.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-noloom", cfg, store); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("retro-noloom", cfg, store)
	if err != nil {
		t.Fatalf("a missing loom failed the cycle: %v", err)
	}
	if result.RetrospectFired != 0 {
		t.Errorf("fired = %d with no loom on PATH, want 0", result.RetrospectFired)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not found") {
		t.Errorf("warnings = %v, want the missing loom named once", result.Warnings)
	}
	if markers := retrospectMarkers(t, "retro-noloom"); strings.Contains(markers, tk.ID) {
		t.Fatalf("markers %q record a run that never happened", markers)
	}

	argv := fakeLoom(t, 0)
	later, err := RunWatchCycle("retro-noloom", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if later.RetrospectFired != 1 {
		t.Fatalf("fired = %d once loom is installed, want the pending close: %v", later.RetrospectFired, later.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-noloom/" + tk.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
}

// An epic is done because its children are, and each of those fires a
// retrospect of its own, so the epic itself never does.
func TestWatchCycle_RetrospectSkipsEpics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-epic")

	epic := &ticket.Ticket{
		ID:       ticket.GenerateID("Parent epic"),
		Title:    "Parent epic",
		Type:     ticket.TypeEpic,
		Status:   ticket.StatusBacklog,
		Priority: 2,
	}
	if err := store.Create(epic); err != nil {
		t.Fatal(err)
	}
	child := &ticket.Ticket{
		ID:       ticket.GenerateID("Only child"),
		Title:    "Only child",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
		Parent:   "retro-epic/" + epic.ID,
	}
	if err := store.Create(child); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repoDir, "a.go", "package a\n", "["+child.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	seed, err := RunWatchCycle("retro-epic", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if seed.RetrospectSeeded != 0 {
		t.Fatalf("seeded = %d, want 0 — nothing is closed yet", seed.RetrospectSeeded)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+child.ID+"] Land the last child")
	result, err := RunWatchCycle("retro-epic", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want 1 — the child only: %v", result.RetrospectFired, result.Warnings)
	}

	derived, err := store.Get(epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if derived.Status != ticket.StatusDone {
		t.Fatalf("epic status = %q, want done — the skip is untested otherwise", derived.Status)
	}

	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-epic/" + child.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
	if markers := retrospectMarkers(t, "retro-epic"); strings.Contains(markers, epic.ID) {
		t.Errorf("markers %q record the epic", markers)
	}
}

// A batch of closes — a store that synced them in, or the backlog the
// missing-loom path keeps pending — starts at most maxRetrospectsPerCycle
// extractions at once. The rest carry no marker and fire on the next cycle.
func TestWatchCycle_RetrospectCapsFiresPerCycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-cap")
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-cap", cfg, store); err != nil {
		t.Fatal(err)
	}

	total := maxRetrospectsPerCycle + 2
	for i := 0; i < total; i++ {
		mkTicket(t, store, fmt.Sprintf("Batch member %d", i), ticket.StatusDone)
	}

	result, err := RunWatchCycle("retro-cap", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != maxRetrospectsPerCycle {
		t.Fatalf("fired = %d, want the cap %d: %v", result.RetrospectFired, maxRetrospectsPerCycle, result.Warnings)
	}
	deferred := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "deferred") {
			deferred = true
		}
	}
	if !deferred {
		t.Errorf("warnings = %v, want the truncation reported", result.Warnings)
	}
	if got := loomInvocations(t, argv, maxRetrospectsPerCycle); len(got) != maxRetrospectsPerCycle {
		t.Fatalf("loom invocations = %d, want the cap %d", len(got), maxRetrospectsPerCycle)
	}

	next, err := RunWatchCycle("retro-cap", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if next.RetrospectFired != total-maxRetrospectsPerCycle {
		t.Fatalf("fired = %d on the next cycle, want the remaining %d: %v", next.RetrospectFired, total-maxRetrospectsPerCycle, next.Warnings)
	}
	if got := loomInvocations(t, argv, total); len(got) != total {
		t.Errorf("loom invocations = %d, want %d in total", len(got), total)
	}
}

// A ticket ID reaches a child's argv from YAML that replicates over the store's
// git remote, so a file written into the store by hand is exactly the shape that
// arrives. Nothing but a plain ID is handed over, and the clean ticket beside it
// still fires.
func TestWatchCycle_RetrospectSkipsHostileTicketIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	storeDir := t.TempDir()
	store := ticket.NewProjectFileStore(storeDir, "retro-hostile")
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-hostile", cfg, store); err != nil {
		t.Fatal(err)
	}

	// Written as files rather than through Create: the store refuses an ID that
	// is not its own file name, and these are what a malicious or corrupt sync
	// delivers regardless. The first never reaches the guard below any more —
	// its slash makes the ID name a project other than the directory's, and the
	// store no longer reads such a file as one of this project's tickets — so
	// two of the three are what the guard itself has to catch.
	for i, id := range []string{"../escape-0001", "-flagged-0002", ".."} {
		body := fmt.Sprintf("---\nid: %s\nstatus: done\ntype: feature\npriority: 2\n---\n# Hostile %d\n", id, i)
		if err := os.WriteFile(filepath.Join(storeDir, fmt.Sprintf("hostile-%d.md", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clean := mkTicket(t, store, "Clean close", ticket.StatusDone)

	result, err := RunWatchCycle("retro-hostile", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want the clean ticket only: %v", result.RetrospectFired, result.Warnings)
	}
	skipped := 0
	for _, w := range result.Warnings {
		if strings.Contains(w, "not a plain ticket ID") {
			skipped++
		}
	}
	if skipped != 2 {
		t.Errorf("warnings = %v, want every hostile ID the listing still yields named", result.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-hostile/" + clean.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
	if markers := retrospectMarkers(t, "retro-hostile"); strings.Contains(markers, "escape-0001") || strings.Contains(markers, "flagged-0002") || strings.Contains(markers, `".."`) {
		t.Errorf("markers %q record a hostile ID, so a repaired store would never fire it", markers)
	}
}

// The project half of the namespaced ID reaches argv too, out of a config.yaml
// that replicates over its own git remote, and registration admits names the
// child must not see — a leading dash is a flag to whatever parses loom's
// arguments. Nothing is scanned and no marker is written, so a config repaired
// later still fires the project's closes.
func TestWatchCycle_RetrospectSkipsHostileProjectName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "-retro-flagged")
	tk := mkTicket(t, store, "Closed elsewhere", ticket.StatusDone)
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	// Seeded by hand: a project whose scan is refused never gets past the seed
	// pass on its own, and what the check has to stop is a fire.
	markerPath, err := state.RetrospectLogPath("-retro-flagged")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	result, err := RunWatchCycle("-retro-flagged", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 0 || result.RetrospectSeeded != 0 {
		t.Fatalf("fired = %d, seeded = %d, want 0 and 0", result.RetrospectFired, result.RetrospectSeeded)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "not a plain project name") {
		t.Errorf("warnings = %v, want the project named once", result.Warnings)
	}
	if markers := retrospectMarkers(t, "-retro-flagged"); strings.Contains(markers, tk.ID) {
		t.Errorf("markers %q record a run, so a repaired config would never fire it", markers)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Error("loom was invoked under a hostile project name")
	}
}

// The scan depends on the store and the marker file alone, so a project whose
// repo is unreachable — the cycle fails on git — still has its closes mined.
func TestWatchCycle_RetrospectRunsWhenGitFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	noRepo := t.TempDir()
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-nogit")

	cfg := project.ProjectConfig{Path: noRepo, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-nogit", cfg, store); err == nil {
		t.Fatal("a directory with no .git did not fail the cycle")
	}

	tk := mkTicket(t, store, "Closed elsewhere", ticket.StatusDone)
	result, err := RunWatchCycle("retro-nogit", cfg, store)
	if err == nil {
		t.Fatal("a directory with no .git did not fail the cycle")
	}
	if result.RetrospectFired != 1 {
		t.Fatalf("fired = %d, want 1 despite the git failure: %v", result.RetrospectFired, result.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-nogit/" + tk.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
}

// Two journal loops run on a machine in practice — `tk watch run` beside a
// `tk serve` per MCP client — and both would read the same close as pending. The
// loser of the race scans nothing at all and retries on its next cycle.
func TestWatchCycle_RetrospectSkipsWhileLockIsHeld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	argv := fakeLoom(t, 0)

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "retro-lock")
	commitFile(t, repoDir, "a.go", "package a\n", "No refs here")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true, AutoRetrospect: true}
	if _, err := RunWatchCycle("retro-lock", cfg, store); err != nil {
		t.Fatal(err)
	}
	tk := mkTicket(t, store, "Closed elsewhere", ticket.StatusDone)

	markerPath, err := state.RetrospectLogPath("retro-lock")
	if err != nil {
		t.Fatal(err)
	}
	// A second descriptor of the lock file, which is what another process holds:
	// an flock belongs to the open file description, so this excludes the cycle
	// even from inside the same process.
	held, err := os.OpenFile(markerPath+".lock", os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}

	result, err := RunWatchCycle("retro-lock", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetrospectFired != 0 {
		t.Fatalf("fired = %d while another process held the lock, want 0", result.RetrospectFired)
	}
	if markers := retrospectMarkers(t, "retro-lock"); strings.Contains(markers, tk.ID) {
		t.Fatalf("markers %q record a run the lock should have deferred", markers)
	}
	if _, err := os.Stat(argv); err == nil {
		t.Fatal("loom was invoked while another process held the lock")
	}

	held.Close()

	after, err := RunWatchCycle("retro-lock", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if after.RetrospectFired != 1 {
		t.Fatalf("fired = %d once the lock was released, want 1: %v", after.RetrospectFired, after.Warnings)
	}
	got := loomInvocations(t, argv, 1)
	want := "retrospect retro-lock/" + tk.ID
	if len(got) != 1 || got[0] != want {
		t.Fatalf("loom invocations = %v, want exactly [%q]", got, want)
	}
}

// A child's stderr is held for the life of the run and logged on failure, so
// what it writes cannot size the daemon's memory or its log line.
func TestCappedBufferTruncates(t *testing.T) {
	var b cappedBuffer
	chunk := strings.Repeat("x", 1024)
	for i := 0; i < 8; i++ {
		n, err := b.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write = %d, %v, want %d and no error — a short write stops the child", n, err, len(chunk))
		}
	}
	if len(b.String()) != retrospectStderrLimit {
		t.Errorf("captured %d bytes, want the limit %d", len(b.String()), retrospectStderrLimit)
	}
}
