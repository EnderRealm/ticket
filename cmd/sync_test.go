package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/internal/project"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "test")
	// Initial commit so HEAD exists
	os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func TestSyncCommit(t *testing.T) {
	dir := setupGitRepo(t)

	// Create a ticket file under tickets/
	os.MkdirAll(filepath.Join(dir, "tickets", "proj"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "proj", "test-ticket.md"), []byte("---\ntitle: Test\n---\n"), 0o644)

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify commit was created
	out, _ := execCommand("git", "-C", dir, "log", "--oneline", "-1")
	if out == "" {
		t.Fatal("expected a commit")
	}
	if !contains(out, "tk: sync tickets") {
		t.Errorf("commit message = %q, want 'tk: sync tickets'", out)
	}
}

func TestSyncNoOp(t *testing.T) {
	dir := setupGitRepo(t)

	// Count commits before
	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Count commits after — should be the same
	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit count changed from %s to %s on no-op sync", before, after)
	}
}

func TestSyncPush(t *testing.T) {
	// Set up a bare remote and a working repo
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	dir := setupGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-u", "origin", "HEAD")

	// Create a file and sync
	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "pushed-ticket.md"), []byte("---\ntitle: Pushed\n---\n"), 0o644)

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify the commit was pushed by checking the bare repo
	out, _ := execCommand("git", "-C", bare, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("bare repo HEAD = %q, expected sync commit", out)
	}
}

func TestSyncPushConflict(t *testing.T) {
	// Set up bare remote
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	// Clone A (our sync repo)
	repoA := setupGitRepo(t)
	runGit(t, repoA, "remote", "add", "origin", bare)
	runGit(t, repoA, "push", "-u", "origin", "HEAD")

	// Clone B (simulates another writer)
	repoB := t.TempDir()
	exec.Command("git", "clone", bare, repoB).Run()
	exec.Command("git", "-C", repoB, "config", "user.email", "other@test.com").Run()
	exec.Command("git", "-C", repoB, "config", "user.name", "other").Run()
	os.WriteFile(filepath.Join(repoB, "conflict.md"), []byte("from B"), 0o644)
	exec.Command("git", "-C", repoB, "add", "-A").Run()
	exec.Command("git", "-C", repoB, "commit", "-m", "from B").Run()
	exec.Command("git", "-C", repoB, "push").Run()

	// Now sync from A — push should fail, pull --rebase should succeed
	os.MkdirAll(filepath.Join(repoA, "tickets"), 0o755)
	os.WriteFile(filepath.Join(repoA, "tickets", "local-ticket.md"), []byte("from A"), 0o644)

	warning := syncCentralStore(repoA)
	// Should succeed after rebase (no actual conflict in file content)
	if warning != "" {
		t.Logf("warning (may be expected): %s", warning)
	}

	// Verify both commits are in the bare repo
	out, _ := execCommand("git", "-C", bare, "log", "--oneline")
	if !contains(out, "from B") {
		t.Error("bare repo missing commit from B")
	}
	if !contains(out, "tk: sync tickets") {
		t.Error("bare repo missing sync commit")
	}
}

func TestSyncParentRepo(t *testing.T) {
	// Create a parent repo with a tickets subdirectory
	parent := setupGitRepo(t)
	ticketsDir := filepath.Join(parent, "tickets", "myproject")
	os.MkdirAll(ticketsDir, 0o755)

	// Write a ticket in the subdirectory
	os.WriteFile(filepath.Join(ticketsDir, "sub-ticket.md"), []byte("---\ntitle: Sub\n---\n"), 0o644)

	// findGitRoot from the tickets subdir should return the parent
	gitRoot, err := findGitRoot(ticketsDir)
	if err != nil {
		t.Fatalf("findGitRoot: %v", err)
	}
	if gitRoot != parent {
		// On macOS, paths may differ due to /private prefix
		evalParent, _ := filepath.EvalSymlinks(parent)
		if gitRoot != evalParent {
			t.Errorf("findGitRoot = %q, want %q", gitRoot, parent)
		}
	}

	// Sync using the parent git root
	warning := syncCentralStore(gitRoot)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify the ticket file was committed
	out, _ := execCommand("git", "-C", gitRoot, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("commit message = %q, want sync commit", out)
	}
}

func TestSyncBlocked(t *testing.T) {
	dir := setupGitRepo(t)

	// Write a sync-blocked marker and simulate active rebase
	writeSyncBlocked(dir, "sync blocked: test conflict")
	rebaseDir := filepath.Join(dir, ".git", "rebase-merge")
	os.MkdirAll(rebaseDir, 0o755)

	// Sync should skip and return the blocked message
	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") {
		t.Errorf("expected blocked warning, got %q", warning)
	}

	// Remove rebase dir (simulate conflict resolution) and the marker
	os.RemoveAll(rebaseDir)
	// syncCentralStore should now clear the block and proceed

	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "unblocked.md"), []byte("---\ntitle: Unblocked\n---\n"), 0o644)
	warning = syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("sync after resolution returned warning: %s", warning)
	}

	// Verify the blocked marker was cleared
	if blocked := readSyncBlocked(dir); blocked != "" {
		t.Errorf("sync-blocked marker should be cleared, got %q", blocked)
	}
}

func TestServeSyncStarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

	// Create a central store with git
	storeRoot := filepath.Join(home, ".tickets")
	os.MkdirAll(storeRoot, 0o755)
	runGit(t, storeRoot, "init")
	runGit(t, storeRoot, "config", "user.email", "test@test.com")
	runGit(t, storeRoot, "config", "user.name", "test")
	os.WriteFile(filepath.Join(storeRoot, ".gitkeep"), []byte(""), 0o644)
	runGit(t, storeRoot, "add", "-A")
	runGit(t, storeRoot, "commit", "-m", "init")

	// Save config with central_root
	cfg := project.Config{CentralRoot: storeRoot, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	// Verify findGitRoot works
	gitRoot, err := findGitRoot(storeRoot)
	if err != nil {
		t.Fatalf("findGitRoot: %v", err)
	}

	// Launch sync loop with short interval, cancel quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Write a file to sync under tickets/
	os.MkdirAll(filepath.Join(storeRoot, "tickets"), 0o755)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "sync-test.md"), []byte("test"), 0o644)

	started := make(chan struct{})
	go func() {
		close(started)
		syncLoop(ctx, gitRoot, 100*time.Millisecond)
	}()
	<-started

	// Wait for at least one cycle
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Verify the file was committed
	out, _ := execCommand("git", "-C", gitRoot, "log", "--oneline")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("sync loop did not commit, log: %s", out)
	}
}

func TestServeSyncShutdown(t *testing.T) {
	dir := setupGitRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		syncLoop(ctx, dir, 50*time.Millisecond)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Should stop promptly
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("syncLoop did not stop after context cancel")
	}
}

func TestSyncIntervalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No config — should return default
	d := syncInterval()
	if d != defaultSyncInterval {
		t.Errorf("default interval = %v, want %v", d, defaultSyncInterval)
	}

	// Set custom interval
	cfg := project.Config{SyncInterval: "10s", Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	d = syncInterval()
	if d != 10*time.Second {
		t.Errorf("custom interval = %v, want 10s", d)
	}
}

func TestSyncCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

	centralRoot := filepath.Join(home, ".tickets")
	dir := setupGitRepo(t)

	// Set up config pointing to our test repo as central root
	cfg := project.Config{CentralRoot: dir, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)
	_ = centralRoot

	// Create a file to sync under tickets/
	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "sync-cmd-test.md"), []byte("test"), 0o644)

	err := runSync(syncCmd, nil)
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}

	// Verify committed
	out, _ := execCommand("git", "-C", dir, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("expected sync commit, got %q", out)
	}
}

func TestSyncCommandJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")

	dir := setupGitRepo(t)

	cfg := project.Config{CentralRoot: dir, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	jsonOutput = true
	defer func() { jsonOutput = false }()

	err := runSync(syncCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runSync --json: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, output)
	}

	if _, ok := result["synced"]; !ok {
		t.Error("missing 'synced' field")
	}
	if _, ok := result["warning"]; !ok {
		t.Error("missing 'warning' field")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
