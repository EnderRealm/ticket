package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

func TestStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := setupGitRepo(t)

	// Create config
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"myproj": {Path: "/tmp/myproj", Store: "central"},
		},
	}
	project.Save(cfg)

	// Create ticket dir with some tickets
	ticketDir := filepath.Join(centralRoot, "tickets", "myproj")
	os.MkdirAll(ticketDir, 0o755)
	os.WriteFile(filepath.Join(ticketDir, "t-1234.md"), []byte("test"), 0o644)
	os.WriteFile(filepath.Join(ticketDir, "t-5678.md"), []byte("test"), 0o644)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runStatus(statusCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Verify key fields are present
	checks := []string{"tk ", "config:", "central root:", "shared config:", "repo status:", "sync status:"}
	for _, check := range checks {
		if !contains(output, check) {
			t.Errorf("output missing %q", check)
		}
	}
}

func TestStatusProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := setupGitRepo(t)

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"alpha": {Path: "/tmp/alpha", Store: "central"},
			"beta":  {Path: "/tmp/beta", Store: "local"},
		},
	}
	project.Save(cfg)

	// Create tickets for alpha
	alphaDir := filepath.Join(centralRoot, "tickets", "alpha")
	os.MkdirAll(alphaDir, 0o755)
	os.WriteFile(filepath.Join(alphaDir, "t-1.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(alphaDir, "t-2.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(alphaDir, "t-3.md"), []byte("x"), 0o644)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runStatus(statusCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !contains(output, "alpha") {
		t.Error("output missing project alpha")
	}
	if !contains(output, "beta") {
		t.Error("output missing project beta")
	}
	if !contains(output, "3") {
		t.Error("output missing ticket count 3 for alpha")
	}
}

func TestStatusJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := setupGitRepo(t)

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"proj": {Path: "/tmp/proj", Store: "central"},
		},
	}
	project.Save(cfg)

	ticketDir := filepath.Join(centralRoot, "tickets", "proj")
	os.MkdirAll(ticketDir, 0o755)
	os.WriteFile(filepath.Join(ticketDir, "t-1.md"), []byte("x"), 0o644)

	result := statusJSON(t)

	if result.CentralRoot != centralRoot {
		t.Errorf("central_root = %q, want %q", result.CentralRoot, centralRoot)
	}
	if len(result.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result.Projects))
	}
	if result.Projects[0].Name != "proj" {
		t.Errorf("project name = %q, want proj", result.Projects[0].Name)
	}
	if result.Projects[0].Tickets != 1 {
		t.Errorf("ticket count = %d, want 1", result.Projects[0].Tickets)
	}
	if result.SyncStatus != "ok" {
		t.Errorf("sync_status = %q, want ok", result.SyncStatus)
	}
}

func TestStatusSyncBlockedNestedStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	parent, storeRoot, _ := nestedStore(t)
	cfg := project.Config{CentralRoot: storeRoot, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	// The marker sync writes lives in the store root, which for a nested store
	// is not the toplevel findGitRoot resolves.
	writeSyncBlocked(storeRoot, "sync blocked: test conflict")
	if got := statusJSON(t).SyncStatus; got != "blocked: sync blocked: test conflict" {
		t.Errorf("sync_status = %q, want the store root's marker", got)
	}

	// A marker at the enclosing repo's root is either another repo's business
	// or one a pre-upgrade tk left behind; neither blocks this store.
	clearSyncBlocked(storeRoot)
	writeSyncBlocked(parent, "sync blocked: stale marker")
	if got := statusJSON(t).SyncStatus; got != "ok" {
		t.Errorf("sync_status = %q, want ok", got)
	}
}

func statusJSON(t *testing.T) statusInfo {
	t.Helper()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	jsonOutput = true
	defer func() { jsonOutput = false }()

	err := runStatus(statusCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runStatus --json: %v", err)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)

	var info statusInfo
	if err := json.Unmarshal(buf[:n], &info); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, string(buf[:n]))
	}
	return info
}
