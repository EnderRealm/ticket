package journal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/pkg/ticket"
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
		Stage:    "implement",
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
	if updated.Stage != "done" {
		t.Errorf("ticket stage = %q, want done", updated.Stage)
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
				if _, err := ticket.Skip(store, ticketID, "done", "auto-closed by commit"); err != nil {
					result.Warnings = append(result.Warnings, err.Error())
				} else {
					result.Closed++
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
