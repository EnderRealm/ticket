package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/internal/project"
)

func setupTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TICKETS_DIR", "")
	return home
}

func TestInitBootstrapGit(t *testing.T) {
	home := setupTestHome(t)

	// Create a fake project directory with git
	projDir := filepath.Join(home, "myproject")
	os.MkdirAll(projDir, 0o755)
	runGit(t, projDir, "init")
	runGit(t, projDir, "config", "user.email", "test@test.com")
	runGit(t, projDir, "config", "user.name", "test")

	centralRoot := filepath.Join(home, ".tickets")

	// Simulate what init does for central store
	centralDir := filepath.Join(centralRoot, "myproject")
	os.MkdirAll(centralDir, 0o755)

	err := bootstrapCentralStoreGit(centralRoot)
	if err != nil {
		t.Fatalf("bootstrapCentralStoreGit: %v", err)
	}

	// Verify .git exists
	if !dirExists(filepath.Join(centralRoot, ".git")) {
		t.Error("expected .git directory in central store")
	}

	// Verify git identity was set
	email, _ := execCommand("git", "-C", centralRoot, "config", "--get", "user.email")
	if email != "tk@local" {
		t.Errorf("git email = %q, want tk@local", email)
	}
}

func TestInitCopyLocal(t *testing.T) {
	home := setupTestHome(t)

	src := filepath.Join(home, "project", ".tickets")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "ticket-1234.md"), []byte("---\ntitle: Test\n---\n"), 0o644)
	os.WriteFile(filepath.Join(src, "other-5678.md"), []byte("---\ntitle: Other\n---\n"), 0o644)
	os.WriteFile(filepath.Join(src, "notamd.txt"), []byte("ignored"), 0o644)

	dst := filepath.Join(home, ".tickets", "project")
	err := copyTicketFiles(src, dst)
	if err != nil {
		t.Fatalf("copyTicketFiles: %v", err)
	}

	// Verify .md files were copied
	entries, _ := os.ReadDir(dst)
	mdCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdCount++
		}
	}
	if mdCount != 2 {
		t.Errorf("copied %d .md files, want 2", mdCount)
	}

	// Verify content is identical
	original, _ := os.ReadFile(filepath.Join(src, "ticket-1234.md"))
	copied, _ := os.ReadFile(filepath.Join(dst, "ticket-1234.md"))
	if string(original) != string(copied) {
		t.Error("copied file content differs from original")
	}

	// Verify non-.md files were not copied
	if _, err := os.Stat(filepath.Join(dst, "notamd.txt")); !os.IsNotExist(err) {
		t.Error("non-.md file should not be copied")
	}

	// Verify originals are preserved
	if _, err := os.Stat(filepath.Join(src, "ticket-1234.md")); err != nil {
		t.Error("original file should be preserved")
	}
}

func TestTicketsDirCentral(t *testing.T) {
	home := setupTestHome(t)

	// Create a project directory
	projDir := filepath.Join(home, "myproject")
	os.MkdirAll(projDir, 0o755)

	// Create config pointing to central store
	cfg := project.Config{Projects: map[string]project.ProjectConfig{
		"myproject": {
			Path:  projDir,
			Store: "central",
		},
	}}
	project.Save(cfg)

	// Create central store directory
	centralDir := filepath.Join(home, ".tickets", "myproject")
	os.MkdirAll(centralDir, 0o755)

	// ticketsDirFromConfig should resolve to central
	// We need to be in the project directory for this to work
	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	dir, ok := ticketsDirFromConfig()
	if !ok {
		t.Fatal("ticketsDirFromConfig should return true for central store project")
	}
	if dir != centralDir {
		t.Errorf("ticketsDirFromConfig = %q, want %q", dir, centralDir)
	}
}

func TestTicketsDirPrecedence(t *testing.T) {
	home := setupTestHome(t)

	// Set up config with central store
	projDir := filepath.Join(home, "myproject")
	os.MkdirAll(projDir, 0o755)
	cfg := project.Config{Projects: map[string]project.ProjectConfig{
		"myproject": {Path: projDir, Store: "central"},
	}}
	project.Save(cfg)

	// TICKETS_DIR env should take precedence over config
	envDir := filepath.Join(home, "envtickets")
	t.Setenv("TICKETS_DIR", envDir)

	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	result := TicketsDir()
	if result != envDir {
		t.Errorf("TicketsDir with TICKETS_DIR env = %q, want %q", result, envDir)
	}

	// Relative path should be used as-is
	t.Setenv("TICKETS_DIR", "relative/path")
	result = TicketsDir()
	if result != "relative/path" {
		t.Errorf("TicketsDir with relative TICKETS_DIR = %q, want relative/path", result)
	}
}

func TestInitProjectFlag(t *testing.T) {
	home := setupTestHome(t)

	cfg := project.Config{Projects: map[string]project.ProjectConfig{}}
	dir := filepath.Join(home, "somedir")
	os.MkdirAll(dir, 0o755)

	name, source := project.ResolveName(cfg, dir, "custom-name")
	if name != "custom-name" {
		t.Errorf("name = %q, want custom-name", name)
	}
	if source != "flag" {
		t.Errorf("source = %q, want flag", source)
	}
}

func TestInitNonInteractive(t *testing.T) {
	home := setupTestHome(t)

	projDir := filepath.Join(home, "niproject")
	os.MkdirAll(projDir, 0o755)
	runGit(t, projDir, "init")
	runGit(t, projDir, "config", "user.email", "test@test.com")
	runGit(t, projDir, "config", "user.name", "test")

	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	// Call runInit directly via the cobra command's RunE
	initCmd.Flags().Set("yes", "true")
	initCmd.Flags().Set("store", "")
	initCmd.Flags().Set("project", "niproject")
	defer func() {
		initCmd.Flags().Set("yes", "false")
		initCmd.Flags().Set("store", "")
		initCmd.Flags().Set("project", "")
	}()

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit --yes: %v", err)
	}

	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.Projects["niproject"]
	if !ok {
		t.Fatal("project not found in config after --yes init")
	}
	if p.Store != "central" {
		t.Errorf("store = %q, want central (--yes should default to central)", p.Store)
	}
}

func TestInitNonInteractiveDefaultStoreLocal(t *testing.T) {
	home := setupTestHome(t)

	// Set default_store to local in config
	cfg := project.Config{DefaultStore: "local", Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	projDir := filepath.Join(home, "localproj")
	os.MkdirAll(projDir, 0o755)
	runGit(t, projDir, "init")
	runGit(t, projDir, "config", "user.email", "test@test.com")
	runGit(t, projDir, "config", "user.name", "test")

	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	initCmd.Flags().Set("yes", "true")
	initCmd.Flags().Set("store", "")
	initCmd.Flags().Set("project", "localproj")
	defer func() {
		initCmd.Flags().Set("yes", "false")
		initCmd.Flags().Set("store", "")
		initCmd.Flags().Set("project", "")
	}()

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit --yes with default_store local: %v", err)
	}

	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.Projects["localproj"]
	if !ok {
		t.Fatal("project not found")
	}
	if p.Store != "local" {
		t.Errorf("store = %q, want local (default_store: local should override)", p.Store)
	}
}

func TestInitBootstrapGitConfigIdentity(t *testing.T) {
	home := setupTestHome(t)

	// Set custom git identity in config
	cfg := project.Config{
		GitEmail: "custom@example.com",
		GitName:  "Custom User",
		Projects: map[string]project.ProjectConfig{},
	}
	project.Save(cfg)

	centralRoot := filepath.Join(home, ".tickets")
	os.MkdirAll(centralRoot, 0o755)

	err := bootstrapCentralStoreGit(centralRoot)
	if err != nil {
		t.Fatalf("bootstrapCentralStoreGit: %v", err)
	}

	email, _ := execCommand("git", "-C", centralRoot, "config", "--get", "user.email")
	if email != "custom@example.com" {
		t.Errorf("git email = %q, want custom@example.com", email)
	}

	name, _ := execCommand("git", "-C", centralRoot, "config", "--get", "user.name")
	if name != "Custom User" {
		t.Errorf("git name = %q, want Custom User", name)
	}
}

func TestInitJSON(t *testing.T) {
	home := setupTestHome(t)

	projDir := filepath.Join(home, "jsonproject")
	os.MkdirAll(projDir, 0o755)
	runGit(t, projDir, "init")
	runGit(t, projDir, "config", "user.email", "test@test.com")
	runGit(t, projDir, "config", "user.name", "test")

	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	jsonOutput = true
	initCmd.Flags().Set("yes", "true")
	initCmd.Flags().Set("project", "jsonproject")
	defer func() {
		jsonOutput = false
		initCmd.Flags().Set("yes", "false")
		initCmd.Flags().Set("project", "")
	}()

	err := runInit(initCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runInit --json: %v", err)
	}

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json parse output: %v\noutput: %s", err, output)
	}

	required := []string{"project", "path", "store", "config", "has_git", "copied_local_to_central"}
	for _, key := range required {
		if _, ok := result[key]; !ok {
			t.Errorf("missing JSON field: %s", key)
		}
	}
}

func TestInitInvalidStore(t *testing.T) {
	setupTestHome(t)

	initCmd.Flags().Set("store", "invalid")
	initCmd.Flags().Set("project", "")
	defer initCmd.Flags().Set("store", "")

	err := runInit(initCmd, nil)
	if err == nil {
		t.Fatal("expected error for invalid store value")
	}
	if !strings.Contains(err.Error(), "store must be central or local") {
		t.Errorf("error = %q, want 'store must be central or local'", err.Error())
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	out, err := execCommand("git", allArgs...)
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}
