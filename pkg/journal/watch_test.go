package journal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
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
						Text:      "auto-closed by commit",
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
