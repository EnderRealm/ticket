package journal

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestWatchCycle(t *testing.T) {
	repoDir := initTestRepo(t)
	commitFile(t, repoDir, "a.go", "package a\n", "[watch-test-1] Add package")
	commitFile(t, repoDir, "b.go", "package b\n", "[watch-test-2] Add another")

	// Override home for journal path — use a temp state dir
	stateDir := t.TempDir()
	jPath := filepath.Join(stateDir, "commits.jsonl")

	cfg := project.ProjectConfig{
		Path:     repoDir,
		AutoLink: true,
	}

	// Run cycle directly with the journal path
	knownSHAs, lastSHA, err := LoadJournalState(jPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(knownSHAs) != 0 || lastSHA != "" {
		t.Fatal("expected empty journal state")
	}

	// Use RunWatchCycle via the full API by setting up the project config
	result, err := runTestWatchCycle(t, repoDir, jPath, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Appended != 2 {
		t.Errorf("Appended = %d, want 2", result.Appended)
	}

	// Run again — should not duplicate
	result2, err := runTestWatchCycle(t, repoDir, jPath, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result2.Appended != 0 {
		t.Errorf("second run Appended = %d, want 0 (no new commits)", result2.Appended)
	}

	// Add a third commit — should pick up only the new one
	commitFile(t, repoDir, "c.go", "package c\n", "[watch-test-3] Third")
	result3, err := runTestWatchCycle(t, repoDir, jPath, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result3.Appended != 1 {
		t.Errorf("third run Appended = %d, want 1", result3.Appended)
	}

	// Verify total entries
	entries, _ := ReadEntriesFromPath(jPath)
	if len(entries) != 3 {
		t.Errorf("total entries = %d, want 3", len(entries))
	}
}

func TestWatchCycle_AutoClose(t *testing.T) {
	repoDir := initTestRepo(t)

	// Create a ticket store with a ticket to auto-close
	ticketDir := t.TempDir()
	store := ticket.NewFileStore(ticketDir)
	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Test ticket"),
		Title:    "Test ticket",
		Type:     "feature",
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
	ticketID := tk.ID

	commitFile(t, repoDir, "fix.go", "package fix\n", "Closes: ["+ticketID+"] Fixed it")

	cfg := project.ProjectConfig{
		Path:      repoDir,
		AutoLink:  true,
		AutoClose: true,
	}

	stateDir := t.TempDir()
	jPath := filepath.Join(stateDir, "commits.jsonl")

	result, err := runTestWatchCycle(t, repoDir, jPath, cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range result.Warnings {
		t.Logf("warning: %s", w)
	}
	if result.Closed != 1 {
		t.Errorf("Closed = %d, want 1", result.Closed)
	}

	// Verify ticket is now done
	updated, err := store.Get(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
}

func TestWatchCycle_AutoCloseOutputs(t *testing.T) {
	// RunWatchCycle resolves the journal path under $HOME.
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	ticketDir := t.TempDir()
	store := ticket.NewFileStore(ticketDir)

	landed := &ticket.Ticket{
		ID:       ticket.GenerateID("Landed ticket"),
		Title:    "Landed ticket",
		Type:     "feature",
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(landed); err != nil {
		t.Fatal(err)
	}
	preset := &ticket.Ticket{
		ID:       ticket.GenerateID("Preset ticket"),
		Title:    "Preset ticket",
		Type:     "feature",
		Status:   ticket.StatusOpen,
		Priority: 2,
		Outputs:  map[string]string{ticket.OutputKeyCommit: "handwritten"},
	}
	if err := store.Create(preset); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "fix.go", "package fix\n",
		"Closes: ["+landed.ID+"] ["+preset.ID+"] Landed both")

	cfg := project.ProjectConfig{
		Path:      repoDir,
		AutoLink:  true,
		AutoClose: true,
	}
	result, err := RunWatchCycle("outputs-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range result.Warnings {
		t.Logf("warning: %s", w)
	}
	if result.Closed != 2 {
		t.Fatalf("Closed = %d, want 2", result.Closed)
	}

	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	sha := commits[0].SHA
	branch := ResolveStableBranchName(repoDir, sha)
	if branch == "" {
		t.Fatal("test repo has no stable branch name")
	}

	updated, err := store.Get(landed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Outputs[ticket.OutputKeyCommit] != sha {
		t.Errorf("Outputs[commit] = %q, want %q", updated.Outputs[ticket.OutputKeyCommit], sha)
	}
	if updated.Outputs[ticket.OutputKeyBranch] != branch {
		t.Errorf("Outputs[branch] = %q, want %q", updated.Outputs[ticket.OutputKeyBranch], branch)
	}

	// A commit value recorded by hand is never overwritten.
	kept, err := store.Get(preset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Outputs[ticket.OutputKeyCommit] != "handwritten" {
		t.Errorf("Outputs[commit] = %q, want handwritten", kept.Outputs[ticket.OutputKeyCommit])
	}
	if kept.Outputs[ticket.OutputKeyBranch] != branch {
		t.Errorf("Outputs[branch] = %q, want %q", kept.Outputs[ticket.OutputKeyBranch], branch)
	}
}

func TestWatchCycle_AutoCloseCountsTowardsNamespacedParent(t *testing.T) {
	// The watcher builds a project-scoped store for central-store projects, so
	// an auto-closed child must count towards its parent epic even though the
	// central store records the parent namespaced. Nothing writes the epic —
	// its status is derived from what the watcher wrote to the child.
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	ticketDir := t.TempDir()
	store := ticket.NewProjectFileStore(ticketDir, "propagate-test")

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
		ID:       ticket.GenerateID("Child ticket"),
		Title:    "Child ticket",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
		Parent:   "propagate-test/" + epic.ID,
	}
	if err := store.Create(child); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "fix.go", "package fix\n", "Closes: ["+child.ID+"] Landed the last child")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("propagate-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range result.Warnings {
		t.Logf("warning: %s", w)
	}
	if result.Closed != 1 {
		t.Fatalf("Closed = %d, want 1", result.Closed)
	}

	updated, err := store.Get(epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("epic status = %q, want done (a namespaced child has to count towards its epic)", updated.Status)
	}
}

// TestWatchCycle_NamespacedRef covers the refs central-store projects actually
// carry: `[project/slug-hash]`, the form every ID an agent is handed takes.
// They matched nothing at all before, so a project committing that way
// journalled nothing and auto-closed nothing. The entry is keyed by the bare
// ID — one project's journal file and its store are both scoped to that
// project, and every entry written before namespaced IDs existed is bare.
func TestWatchCycle_NamespacedRef(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "ns-test")

	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Namespaced ref ticket"),
		Title:    "Namespaced ref ticket",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "a.go", "package a\n", "[ns-test/"+tk.ID+"] Start the work")
	commitFile(t, repoDir, "b.go", "package b\n", "Closes: [ns-test/"+tk.ID+"] Land the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("ns-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range result.Warnings {
		t.Logf("warning: %s", w)
	}
	if result.Appended != 2 {
		t.Errorf("Appended = %d, want 2", result.Appended)
	}
	if result.Closed != 1 {
		t.Errorf("Closed = %d, want 1", result.Closed)
	}

	entries, err := ReadEntries("ns-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.Ticket != tk.ID {
			t.Errorf("entry ticket = %q, want the bare %q", entry.Ticket, tk.ID)
		}
	}

	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
}

// TestWatchCycle_DualFormRefIsOneEntry covers a commit naming one ticket in both
// forms — the subject ref namespaced, the trailer bare. They resolve to the same
// ID, so the commit gets one entry carrying the close rather than two that
// disagree about what the commit did to the ticket.
func TestWatchCycle_DualFormRefIsOneEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "dual-test")

	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Dual form ticket"),
		Title:    "Dual form ticket",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "a.go", "package a\n", "[dual-test/"+tk.ID+"] Land it\n\nCloses: ["+tk.ID+"]")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("dual-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Appended != 1 {
		t.Errorf("Appended = %d, want 1 — one commit, one ticket", result.Appended)
	}
	if result.Closed != 1 {
		t.Errorf("Closed = %d, want 1", result.Closed)
	}

	entries, err := ReadEntries("dual-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Ticket != tk.ID {
		t.Errorf("entry ticket = %q, want the bare %q", entries[0].Ticket, tk.ID)
	}
	if entries[0].Action != "close" {
		t.Errorf("entry action = %q, want close — a close outranks the plain ref", entries[0].Action)
	}
}

// TestWatchCycle_ForeignNamespacedRef holds a ref naming another project to the
// warning path: this store is scoped to one project and cannot resolve it, and
// guessing that the bare half names a local ticket would close the wrong one.
func TestWatchCycle_ForeignNamespacedRef(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "home-test")

	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Local ticket"),
		Title:    "Local ticket",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "a.go", "package a\n", "Closes: [other-project/"+tk.ID+"] Landed elsewhere")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("home-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 0 {
		t.Errorf("Closed = %d, want 0 — the ref names another project", result.Closed)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "a commit closes tickets in its own project only (other-project/"+tk.ID+" is in other-project)") {
		t.Fatalf("warnings = %v, want the foreign ref refused by name", result.Warnings)
	}

	// The commit is still journalled, under the namespaced form it named: the
	// entry records what the commit said, and only the close needs a store that
	// can resolve the ref.
	if result.Appended != 1 {
		t.Errorf("Appended = %d, want 1 — the commit is journalled either way", result.Appended)
	}
	entries, err := ReadEntries("home-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Ticket != "other-project/"+tk.ID {
		t.Errorf("entries = %+v, want one keyed %q", entries, "other-project/"+tk.ID)
	}

	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusOpen {
		t.Errorf("ticket status = %q, want open", updated.Status)
	}
}

// TestWatchCycle_RootRefNeverCloses holds a ref naming a Root ticket to the
// warning path: Root has no repository, so no commit is the one that closes an
// idea there, and the ref is refused by name rather than resolved against a
// store that could not answer it anyway.
func TestWatchCycle_RootRefNeverCloses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "root-ref-test")
	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+project.RootNamespace+"/idea-1234] Landed an idea")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("root-ref-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 0 {
		t.Errorf("Closed = %d, want 0 — a commit never closes a Root ticket", result.Closed)
	}
	want := "auto-close " + project.RootNamespace + "/idea-1234 skipped: Root tickets have no repository and are never closed by a commit"
	if len(result.Warnings) != 1 || result.Warnings[0] != want {
		t.Fatalf("warnings = %v, want [%q]", result.Warnings, want)
	}
	if result.Appended != 1 {
		t.Errorf("Appended = %d, want 1 — the commit is journalled either way", result.Appended)
	}
	entries, err := ReadEntries("root-ref-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Ticket != project.RootNamespace+"/idea-1234" {
		t.Errorf("entries = %+v, want one keyed under the Root ref as named", entries)
	}
}

// TestWatchCycle_LocalCloseLandsOnce is the local close across cycles: the
// bare ref reaches the project-scoped store, the close lands on the first
// cycle, the journal keys it bare, and the second cycle — the commit now in
// knownSHAs — neither re-closes nor re-journals it.
func TestWatchCycle_LocalCloseLandsOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "once-test")
	tk := mkTicket(t, store, "Local leaf", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] Land the leaf")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	first, err := RunWatchCycle("once-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Closed != 1 || first.Appended != 1 {
		t.Fatalf("first cycle closed %d, appended %d, want 1 and 1: %v", first.Closed, first.Appended, first.Warnings)
	}
	second, err := RunWatchCycle("once-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Closed != 0 || second.Appended != 0 || len(second.Warnings) != 0 {
		t.Errorf("second cycle closed %d, appended %d, warnings %v, want nothing", second.Closed, second.Appended, second.Warnings)
	}

	entries, err := ReadEntries("once-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Ticket != tk.ID || entries[0].Action != "close" {
		t.Errorf("entries = %+v, want one close keyed by the bare %q", entries, tk.ID)
	}
	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
	if len(updated.Notes) != 1 {
		t.Errorf("notes = %v, want the one auto-close note", updated.Notes)
	}
}

// removeWatchState deletes the machine-local watcher state for project, as a
// fresh machine, a wiped state directory or a new home would have none.
func removeWatchState(t *testing.T, project string) {
	t.Helper()
	jPath, err := JournalPath(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(jPath)); err != nil {
		t.Fatal(err)
	}
}

// TestWatchCycle_FreshStateKeepsReopenedTicket is a machine with no journal
// replaying a close a human has since undone: the ticket records that the
// commit already closed it, so the cycle leaves its reopened status alone.
func TestWatchCycle_FreshStateKeepsReopenedTicket(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "reopen-test")
	tk := mkTicket(t, store, "Reopened leaf", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] Land the leaf")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	first, err := RunWatchCycle("reopen-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Closed != 1 {
		t.Fatalf("first cycle closed %d, want 1: %v", first.Closed, first.Warnings)
	}
	if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Status = ticket.StatusReady
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	removeWatchState(t, "reopen-test")
	replay, err := RunWatchCycle("reopen-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Closed != 0 || len(replay.Warnings) != 0 {
		t.Errorf("replay closed %d with warnings %v, want 0 and none", replay.Closed, replay.Warnings)
	}

	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusReady {
		t.Errorf("ticket status = %q, want ready — the human reopened it", updated.Status)
	}
	if len(updated.Notes) != 1 {
		t.Errorf("notes = %v, want only the first auto-close note", updated.Notes)
	}
}

// TestWatchCycle_FreshStateLeavesClosedTicket is a machine with no journal
// replaying a close that already landed, under both records of it: the
// SHA-bearing note, and a ticket closed before the note carried a SHA, where
// outputs.commit is the only record: its SHA-less note is dated further
// before the commit than legacyNoteSkew, so the note alone would not match.
func TestWatchCycle_FreshStateLeavesClosedTicket(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy bool
	}{
		{name: "note", legacy: false},
		{name: "legacy", legacy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			repoDir := initTestRepo(t)
			store := ticket.NewProjectFileStore(t.TempDir(), "closed-test")
			tk := mkTicket(t, store, "Closed leaf", ticket.StatusOpen)
			commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] Land the leaf")

			cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
			if tc.legacy {
				commits, err := CollectCommits(repoDir, "", "")
				if err != nil {
					t.Fatal(err)
				}
				if len(commits) != 1 {
					t.Fatalf("got %d commits, want 1", len(commits))
				}
				if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
					t.Status = ticket.StatusDone
					t.Notes = append(t.Notes, ticket.Note{Timestamp: time.Now().UTC().Add(-legacyNoteSkew - time.Hour), Text: legacyAutoCloseNote})
					t.Outputs = map[string]string{ticket.OutputKeyCommit: commits[0].SHA}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				first, err := RunWatchCycle("closed-test", cfg, store)
				if err != nil {
					t.Fatal(err)
				}
				if first.Closed != 1 {
					t.Fatalf("first cycle closed %d, want 1: %v", first.Closed, first.Warnings)
				}
			}
			before, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}

			removeWatchState(t, "closed-test")
			replay, err := RunWatchCycle("closed-test", cfg, store)
			if err != nil {
				t.Fatal(err)
			}
			if replay.Closed != 0 || len(replay.Warnings) != 0 {
				t.Errorf("replay closed %d with warnings %v, want 0 and none", replay.Closed, replay.Warnings)
			}

			after, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != ticket.StatusDone {
				t.Errorf("ticket status = %q, want done", after.Status)
			}
			if len(after.Notes) != len(before.Notes) {
				t.Errorf("notes = %v, want the %d already there", after.Notes, len(before.Notes))
			}
			if !maps.Equal(after.Outputs, before.Outputs) {
				t.Errorf("outputs = %v, want %v unchanged", after.Outputs, before.Outputs)
			}
		})
	}
}

// TestWatchCycle_NewCloseWithStateLandsOnce pins the ordinary path the replay
// check must not block: on a machine with journal state, a Closes: commit it
// has not seen closes its open ticket once, under one note naming the commit.
func TestWatchCycle_NewCloseWithStateLandsOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "new-close-test")
	tk := mkTicket(t, store, "Open leaf", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "["+tk.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	if _, err := RunWatchCycle("new-close-test", cfg, store); err != nil {
		t.Fatal(err)
	}
	jPath, err := JournalPath("new-close-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jPath); err != nil {
		t.Fatalf("no journal state after the first cycle: %v", err)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("new-close-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 1 || len(result.Warnings) != 0 {
		t.Fatalf("closed %d with warnings %v, want 1 and none", result.Closed, result.Warnings)
	}

	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	sha := commits[len(commits)-1].SHA
	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
	if len(updated.Notes) != 1 || updated.Notes[0].Text != autoCloseNote(sha) {
		t.Errorf("notes = %v, want one %q", updated.Notes, autoCloseNote(sha))
	}
}

// TestWatchCycle_FreshStateNoteIsOnlyRecord replays a close whose only record
// is the note naming its commit: the ticket was closed by A, reopened and
// closed by B, so outputs.commit still names A.
func TestWatchCycle_FreshStateNoteIsOnlyRecord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reopen bool
	}{
		{name: "done", reopen: false},
		{name: "reopened", reopen: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			repoDir := initTestRepo(t)
			store := ticket.NewProjectFileStore(t.TempDir(), "note-only-test")
			tk := mkTicket(t, store, "Twice closed leaf", ticket.StatusOpen)
			cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}

			commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] First landing")
			if first, err := RunWatchCycle("note-only-test", cfg, store); err != nil {
				t.Fatal(err)
			} else if first.Closed != 1 {
				t.Fatalf("first cycle closed %d, want 1: %v", first.Closed, first.Warnings)
			}
			if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
				t.Status = ticket.StatusReady
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Second landing")
			if second, err := RunWatchCycle("note-only-test", cfg, store); err != nil {
				t.Fatal(err)
			} else if second.Closed != 1 {
				t.Fatalf("second cycle closed %d, want 1: %v", second.Closed, second.Warnings)
			}

			commits, err := CollectCommits(repoDir, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(commits) != 2 {
				t.Fatalf("got %d commits, want 2", len(commits))
			}
			shaA, shaB := commits[0].SHA, commits[1].SHA
			if tc.reopen {
				if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
					t.Status = ticket.StatusReady
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if before.Outputs[ticket.OutputKeyCommit] != shaA {
				t.Fatalf("outputs.commit = %q, want the first close's %q", before.Outputs[ticket.OutputKeyCommit], shaA)
			}
			if len(before.Notes) != 2 || before.Notes[1].Text != autoCloseNote(shaB) {
				t.Fatalf("notes = %v, want the second one naming %s", before.Notes, shaB)
			}

			removeWatchState(t, "note-only-test")
			replay, err := RunWatchCycle("note-only-test", cfg, store)
			if err != nil {
				t.Fatal(err)
			}
			if replay.Closed != 0 || len(replay.Warnings) != 0 {
				t.Errorf("replay closed %d with warnings %v, want 0 and none", replay.Closed, replay.Warnings)
			}

			after, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != before.Status {
				t.Errorf("ticket status = %q, want %q", after.Status, before.Status)
			}
			if len(after.Notes) != len(before.Notes) {
				t.Errorf("notes = %v, want the %d already there", after.Notes, len(before.Notes))
			}
			if !maps.Equal(after.Outputs, before.Outputs) {
				t.Errorf("outputs = %v, want %v unchanged", after.Outputs, before.Outputs)
			}
		})
	}
}

// TestWatchCycle_FreshStateLegacyMultiClose replays two closes made before the
// note named its commit: outputs.commit holds only the first, and the second
// is recorded by nothing but a SHA-less note written after it.
func TestWatchCycle_FreshStateLegacyMultiClose(t *testing.T) {
	for _, status := range []ticket.Status{ticket.StatusReady, ticket.StatusDone} {
		t.Run(string(status), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			repoDir := initTestRepo(t)
			store := ticket.NewProjectFileStore(t.TempDir(), "legacy-multi-test")
			tk := mkTicket(t, store, "Legacy twice closed leaf", ticket.StatusOpen)
			commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] First landing")
			commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Second landing")

			commits, err := CollectCommits(repoDir, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(commits) != 2 {
				t.Fatalf("got %d commits, want 2", len(commits))
			}
			if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
				now := time.Now().UTC()
				t.Status = status
				t.Notes = append(t.Notes,
					ticket.Note{Timestamp: now, Text: legacyAutoCloseNote},
					ticket.Note{Timestamp: now, Text: legacyAutoCloseNote})
				t.Outputs = map[string]string{ticket.OutputKeyCommit: commits[0].SHA}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}

			cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
			removeWatchState(t, "legacy-multi-test")
			replay, err := RunWatchCycle("legacy-multi-test", cfg, store)
			if err != nil {
				t.Fatal(err)
			}
			if replay.Closed != 0 || len(replay.Warnings) != 0 {
				t.Errorf("replay closed %d with warnings %v, want 0 and none", replay.Closed, replay.Warnings)
			}

			after, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != status {
				t.Errorf("ticket status = %q, want %q", after.Status, status)
			}
			if len(after.Notes) != len(before.Notes) {
				t.Errorf("notes = %v, want the %d already there", after.Notes, len(before.Notes))
			}
			if !maps.Equal(after.Outputs, before.Outputs) {
				t.Errorf("outputs = %v, want %v unchanged", after.Outputs, before.Outputs)
			}
		})
	}
}

// TestWatchCycle_FreshStateLegacyCloseDatedAhead replays a legacy second close
// whose committer date runs ahead of the note the watcher wrote for it, as a
// committer's fast clock or a rebase-carried date leaves it: inside
// legacyNoteSkew the note still records the close, so the reopened ticket
// stays reopened.
func TestWatchCycle_FreshStateLegacyCloseDatedAhead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "legacy-ahead-test")
	tk := mkTicket(t, store, "Legacy leaf dated ahead", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] First landing")
	noted := time.Now().UTC()
	t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("@%d +0000", noted.Add(2*time.Hour).Unix()))
	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Second landing")

	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	dated, err := time.Parse(time.RFC3339, commits[1].TS)
	if err != nil {
		t.Fatal(err)
	}
	if !dated.After(noted.Add(time.Hour)) {
		t.Fatalf("second commit dated %s, want it well after the note at %s", commits[1].TS, noted)
	}
	if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Status = ticket.StatusReady
		t.Notes = append(t.Notes,
			ticket.Note{Timestamp: noted, Text: legacyAutoCloseNote},
			ticket.Note{Timestamp: noted, Text: legacyAutoCloseNote})
		t.Outputs = map[string]string{ticket.OutputKeyCommit: commits[0].SHA}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	removeWatchState(t, "legacy-ahead-test")
	replay, err := RunWatchCycle("legacy-ahead-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Closed != 0 || len(replay.Warnings) != 0 {
		t.Errorf("replay closed %d with warnings %v, want 0 and none", replay.Closed, replay.Warnings)
	}

	after, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != ticket.StatusReady {
		t.Errorf("ticket status = %q, want ready — the human reopened it", after.Status)
	}
	if len(after.Notes) != len(before.Notes) {
		t.Errorf("notes = %v, want the %d already there", after.Notes, len(before.Notes))
	}
	if !maps.Equal(after.Outputs, before.Outputs) {
		t.Errorf("outputs = %v, want %v unchanged", after.Outputs, before.Outputs)
	}
}

// TestWatchCycle_FreshStateOlderLegacyNoteDoesNotBlockClose holds the
// legacy-note rule to its lower bound on the cycle it applies to: with no
// journal state, a SHA-less note older than a commit by more than
// legacyNoteSkew is not that commit's close, so the commit still closes the
// open ticket. The ticket's outputs.commit names an earlier, unrelated commit,
// which does not block the close either.
func TestWatchCycle_FreshStateOlderLegacyNoteDoesNotBlockClose(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "older-legacy-test")
	tk := mkTicket(t, store, "Reopened legacy leaf", ticket.StatusOpen)
	if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Notes = append(t.Notes, ticket.Note{Timestamp: time.Now().UTC().Add(-legacyNoteSkew - time.Hour), Text: legacyAutoCloseNote})
		t.Outputs = map[string]string{ticket.OutputKeyCommit: "1111111111111111111111111111111111111111"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("older-legacy-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 1 || len(result.Warnings) != 0 {
		t.Fatalf("closed %d with warnings %v, want 1 and none", result.Closed, result.Warnings)
	}

	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	sha := commits[len(commits)-1].SHA
	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
	if len(updated.Notes) != 2 || updated.Notes[1].Text != autoCloseNote(sha) {
		t.Errorf("notes = %v, want the legacy note and one %q", updated.Notes, autoCloseNote(sha))
	}
}

// TestWatchCycle_LegacyNoteIgnoredWithState holds the legacy-note match to the
// cycle it is for: with journal state the machine is past any replay, so a
// first-seen Closes: commit closes a reopened ticket even though it carries a
// SHA-less note inside legacyNoteSkew of the commit.
func TestWatchCycle_LegacyNoteIgnoredWithState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "legacy-state-test")
	tk := mkTicket(t, store, "Reopened leaf with legacy note", ticket.StatusReady)
	if _, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Notes = append(t.Notes, ticket.Note{Timestamp: time.Now().UTC(), Text: legacyAutoCloseNote})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repoDir, "a.go", "package a\n", "["+tk.ID+"] Start the work")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	if _, err := RunWatchCycle("legacy-state-test", cfg, store); err != nil {
		t.Fatal(err)
	}
	jPath, err := JournalPath("legacy-state-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jPath); err != nil {
		t.Fatalf("no journal state after the first cycle: %v", err)
	}

	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land the work")
	result, err := RunWatchCycle("legacy-state-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 1 || len(result.Warnings) != 0 {
		t.Fatalf("closed %d with warnings %v, want 1 and none", result.Closed, result.Warnings)
	}

	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	sha := commits[len(commits)-1].SHA
	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusDone {
		t.Errorf("ticket status = %q, want done", updated.Status)
	}
	if len(updated.Notes) != 2 || updated.Notes[1].Text != autoCloseNote(sha) {
		t.Errorf("notes = %v, want the legacy note and one %q", updated.Notes, autoCloseNote(sha))
	}
}

// TestWatchCycle_PartialRefDoesNotClose holds a ref that is only a substring of
// a ticket's ID to the warning path: the store's lookup falls back to substring
// matching, which would let any bracketed word in a commit subject close an
// unrelated ticket.
func TestWatchCycle_PartialRefDoesNotClose(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "partial-test")

	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Partial ref ticket"),
		Title:    "Partial ref ticket",
		Type:     ticket.TypeFeature,
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
	fragment := strings.Split(tk.ID, "-")[0]

	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+fragment+"] Landed something")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("partial-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 0 {
		t.Errorf("Closed = %d, want 0 — the ref does not name the ticket exactly", result.Closed)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], fragment) {
		t.Fatalf("warnings = %v, want the unresolved ref %q named", result.Warnings, fragment)
	}

	// The commit is still journalled, under the fragment it named: the entry
	// records what the commit said, and only the close needs an exact ref.
	if result.Appended != 1 {
		t.Errorf("Appended = %d, want 1 — the commit is journalled either way", result.Appended)
	}
	entries, err := ReadEntries("partial-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Ticket != fragment || entries[0].Action != "close" {
		t.Errorf("entries = %+v, want one keyed %q with action close", entries, fragment)
	}

	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != ticket.StatusOpen {
		t.Errorf("ticket status = %q, want open", updated.Status)
	}
	if len(updated.Notes) != 0 {
		t.Errorf("notes = %v, want none — nothing was closed", updated.Notes)
	}
}

func TestWatchCycle_AutoCloseSkipsEpic(t *testing.T) {
	// An epic is closed by its children, not by a commit. Writing one would be
	// inert while still appending a note and counting a close that never
	// happened, so the watcher names it as a warning instead.
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "epic-test")

	epic := &ticket.Ticket{
		ID:       ticket.GenerateID("Epic closed by commit"),
		Title:    "Epic closed by commit",
		Type:     ticket.TypeEpic,
		Status:   ticket.StatusBacklog,
		Priority: 2,
	}
	if err := store.Create(epic); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "fix.go", "package fix\n", "Closes: ["+epic.ID+"] Landed it")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle("epic-test", cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != 0 {
		t.Errorf("Closed = %d, want 0 — a commit cannot close an epic", result.Closed)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], epic.ID) {
		t.Fatalf("warnings = %v, want the skipped epic named", result.Warnings)
	}

	updated, err := store.Get(epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Notes) != 0 {
		t.Errorf("notes = %v, want none — nothing was closed", updated.Notes)
	}
}

func TestWatchCycle_AutoCloseSkipsUnserializableBranch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	// `%wip` is a legal git ref but not a safe unquoted YAML scalar.
	checkout := exec.Command("git", "-C", repoDir, "checkout", "-b", "%wip")
	if out, err := checkout.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	ticketDir := t.TempDir()
	store := ticket.NewFileStore(ticketDir)
	tk := &ticket.Ticket{
		ID:       ticket.GenerateID("Odd branch ticket"),
		Title:    "Odd branch ticket",
		Type:     "feature",
		Status:   ticket.StatusOpen,
		Priority: 2,
	}
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}

	commitFile(t, repoDir, "fix.go", "package fix\n", "Closes: ["+tk.ID+"] Landed on an odd branch")

	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	if _, err := RunWatchCycle("odd-branch-test", cfg, store); err != nil {
		t.Fatal(err)
	}

	// The ticket must still parse — a dropped output beats a corrupt file.
	all, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("List returned %d tickets, want 1 (ticket file no longer parses)", len(all))
	}
	updated, err := store.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Outputs[ticket.OutputKeyBranch]; ok {
		t.Errorf("Outputs[branch] = %q, want absent", updated.Outputs[ticket.OutputKeyBranch])
	}
	if updated.Outputs[ticket.OutputKeyCommit] == "" {
		t.Errorf("Outputs[commit] not set: %v", updated.Outputs)
	}
}

// TestWatchCycle_TraversingProjectName covers the config map key the watch loops
// iterate: an entry keyed with a traversing name reaches JournalPath, and the
// cycle creates the journal's parent directory before it checks the repo, so an
// unbounded join would build a state tree outside the state root every tick.
func TestWatchCycle_TraversingProjectName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Where the join would have landed: the two ".." segments eat "state" and
	// ".ticket", so the cleaned path lands directly under HOME, beside .ticket.
	escaped := filepath.Join(home, ".ticket", "state", "../../elsewhere")

	cfg := project.ProjectConfig{Path: t.TempDir(), AutoLink: true}
	_, err := RunWatchCycle("../../elsewhere", cfg, nil)
	if err == nil {
		t.Fatal("RunWatchCycle accepted a traversing project name")
	}
	if !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("error = %v, want it to name the invalid project", err)
	}

	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Errorf("%s exists: the cycle wrote outside the state root", escaped)
	}
	stateRoot := filepath.Join(home, ".ticket", "state")
	entries, err := os.ReadDir(stateRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Errorf("%s has %d entries, want none", stateRoot, len(entries))
	}
}

// runTestWatchCycle is a helper that runs a watch cycle against a specific
// journal path rather than using the project-name-based path resolution.
func runTestWatchCycle(t *testing.T, repoDir, jPath string, cfg project.ProjectConfig, store ticket.Store) (WatchCycleResult, error) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(jPath), 0o755); err != nil {
		return WatchCycleResult{}, err
	}

	knownSHAs, lastSHA, err := LoadJournalState(jPath)
	if err != nil {
		return WatchCycleResult{}, err
	}

	commits, err := CollectCommits(repoDir, lastSHA, cfg.RegisteredAt)
	if err != nil {
		return WatchCycleResult{}, err
	}

	var result WatchCycleResult
	var toAppend []Entry

	for _, commit := range commits {
		if _, seen := knownSHAs[commit.SHA]; seen {
			continue
		}
		actions := ExtractTicketActions(commit.Msg)
		if len(actions) == 0 {
			continue
		}

		files, added, removed, branch, _ := GetDiffStats(repoDir, commit.SHA)

		for ticketID, action := range actions {
			if action == "close" && cfg.AutoClose && store != nil {
				tk, err := store.Get(ticketID)
				if err != nil {
					result.Warnings = append(result.Warnings, err.Error())
				} else {
					tk.Status = ticket.StatusDone
					tk.Notes = append(tk.Notes, ticket.Note{
						Timestamp: time.Now().UTC(),
						Text:      autoCloseNote(commit.SHA),
					})
					if err := store.Update(tk); err != nil {
						result.Warnings = append(result.Warnings, err.Error())
					} else {
						result.Closed++
					}
				}
			}
			if cfg.AutoLink {
				toAppend = append(toAppend, Entry{
					SHA:          commit.SHA,
					Ticket:       ticketID,
					Repo:         repoDir,
					TS:           commit.TS,
					Msg:          commit.Msg,
					Author:       commit.Author,
					Action:       action,
					FilesChanged: files,
					LinesAdded:   added,
					LinesRemoved: removed,
					Branch:       branch,
				})
			}
		}
		knownSHAs[commit.SHA] = struct{}{}
	}

	if err := AppendEntriesToPath(jPath, toAppend); err != nil {
		return result, err
	}
	result.Appended = len(toAppend)
	return result, nil
}

// testedTicket is an open ticket with a criterion a person checks.
func testedTicket(t *testing.T, store ticket.Store, title string) *ticket.Ticket {
	t.Helper()
	tk := mkTicket(t, store, title, ticket.StatusOpen)
	tk, err := ticket.Mutate(store, tk.ID, func(t *ticket.Ticket) error {
		t.Body = "\nDescription.\n\n## Acceptance Criteria\n\n- Builds.\n  verify: go build ./...\n- Reads well.\n  test: look at it\n"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func applyTransition(t *testing.T, store ticket.Store, id, action string, req ticket.ActionRequest) *ticket.Ticket {
	t.Helper()
	req.ID, req.ActionID, req.Note = id, action, "note "+action
	res, err := ticket.ApplyAction(store, req)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return res.Ticket
}

func headSHA(t *testing.T, repoDir string) string {
	t.Helper()
	commits, err := CollectCommits(repoDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return commits[len(commits)-1].SHA
}

func watchCycle(t *testing.T, name string, repoDir string, store ticket.Store, closed int) {
	t.Helper()
	cfg := project.ProjectConfig{Path: repoDir, AutoLink: true, AutoClose: true}
	result, err := RunWatchCycle(name, cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Closed != closed || len(result.Warnings) != 0 {
		t.Fatalf("closed %d with warnings %v, want %d and none", result.Closed, result.Warnings, closed)
	}
}

func TestWatcherLandsIntoTesting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := initTestRepo(t)
	store := ticket.NewProjectFileStore(t.TempDir(), "landing-test")
	tested := testedTicket(t, store, "Tested leaf")
	untested := mkTicket(t, store, "Untested leaf", ticket.StatusOpen)
	commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tested.ID+"] ["+untested.ID+"] Land both")
	first := headSHA(t, repoDir)
	watchCycle(t, "landing-test", repoDir, store, 2)

	for id, want := range map[string]ticket.Status{tested.ID: ticket.StatusTesting, untested.ID: ticket.StatusDone} {
		got, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != want || got.Outputs[ticket.OutputKeyCommit] != first || got.ClosedAt().IsZero() != (want == ticket.StatusTesting) {
			t.Errorf("%s landed %s at %v, closed %v; want %s at %s", id, got.Status, got.Outputs, got.ClosedAt(), want, first)
		}
	}

	// Failed and landed again: testing, and the commit is the one to test now.
	applyTransition(t, store, tested.ID, "fail", ticket.ActionRequest{Transition: ticket.TransitionFail, Checks: []int{1}})
	commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tested.ID+"] Land it again")
	second := headSHA(t, repoDir)
	watchCycle(t, "landing-test", repoDir, store, 1)
	got, err := store.Get(tested.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ticket.StatusTesting || got.Outputs[ticket.OutputKeyCommit] != second {
		t.Errorf("re-landed %s at %v, want testing at %s", got.Status, got.Outputs, second)
	}

	// Neither landing is replayed on a machine with no journal.
	applyTransition(t, store, tested.ID, "pass", ticket.ActionRequest{Transition: ticket.TransitionPass})
	removeWatchState(t, "landing-test")
	watchCycle(t, "landing-test", repoDir, store, 0)
	if got, err = store.Get(tested.ID); err != nil || got.Status != ticket.StatusDone || got.Outputs[ticket.OutputKeyCommit] != second {
		t.Errorf("after the replay: %v at %v, %v", got.Status, got.Outputs, err)
	}
}

// TestWatcherLeavesEarlierLandActionAlone is a machine with no journal walking
// the commit of a landing a later one has replaced in outputs.commit, whose
// only records are the land receipt or the test of it.
func TestWatcherLeavesEarlierLandActionAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		land func(t *testing.T, store ticket.Store, id, sha string)
	}{
		{"land receipt", func(t *testing.T, store ticket.Store, id, sha string) {
			applyTransition(t, store, id, "land-1", ticket.ActionRequest{Transition: ticket.TransitionLand, Commit: sha})
		}},
		{"test row", func(t *testing.T, store ticket.Store, id, sha string) {
			if _, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
				t.Status = ticket.StatusTesting
				t.Outputs = map[string]string{ticket.OutputKeyCommit: sha}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			repoDir := initTestRepo(t)
			store := ticket.NewProjectFileStore(t.TempDir(), "replay-test")
			tk := testedTicket(t, store, "Tested leaf")

			commitFile(t, repoDir, "a.go", "package a\n", "Closes: ["+tk.ID+"] Land it")
			tc.land(t, store, tk.ID, headSHA(t, repoDir))
			applyTransition(t, store, tk.ID, "fail", ticket.ActionRequest{Transition: ticket.TransitionFail, Checks: []int{1}})
			commitFile(t, repoDir, "b.go", "package b\n", "Closes: ["+tk.ID+"] Land it again")
			second := headSHA(t, repoDir)
			applyTransition(t, store, tk.ID, "land-2", ticket.ActionRequest{Transition: ticket.TransitionLand, Commit: second})
			before := applyTransition(t, store, tk.ID, "pass", ticket.ActionRequest{Transition: ticket.TransitionPass})

			watchCycle(t, "replay-test", repoDir, store, 0)
			got, err := store.Get(tk.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Precondition() != before.Precondition() {
				t.Errorf("the replay wrote the ticket: %s at %v, notes %v", got.Status, got.Outputs, got.Notes)
			}
		})
	}
}
