package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/journal"
)

// A rebuild has to key entries exactly as the watch cycle does. Both write the
// same file, so a recompute that kept the namespaced form would split one
// ticket's history across two keys and hide the recomputed half from every
// reader that queries by bare ID.
func TestRecomputeKeysNamespacedRefToBareID(t *testing.T) {
	home := setupTestHome(t)

	repo := filepath.Join(home, "rc-proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@test.com")
	runGit(t, repo, "config", "user.name", "test")

	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"rc-proj": {Path: repo, Store: "central", AutoLink: true, AutoClose: true},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "[rc-proj/rc-1234] Namespaced ref")

	recomputeProject = "rc-proj"
	t.Cleanup(func() { recomputeProject = "" })

	if err := runRecompute(recomputeCmd, nil); err != nil {
		t.Fatalf("runRecompute: %v", err)
	}

	entries, err := journal.ReadEntries("rc-proj")
	if err != nil {
		t.Fatalf("ReadEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1: %+v", len(entries), entries)
	}
	if entries[0].Ticket != "rc-1234" {
		t.Errorf("entry ticket = %q, want the bare %q", entries[0].Ticket, "rc-1234")
	}
}
