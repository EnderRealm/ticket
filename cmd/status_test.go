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
	t.Setenv("TICKETS_DIR", "")

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
	t.Setenv("TICKETS_DIR", "")

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
	t.Setenv("TICKETS_DIR", "")

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

	var result statusInfo
	if err := json.Unmarshal(buf[:n], &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, string(buf[:n]))
	}

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
