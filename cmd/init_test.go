package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

// TestMain points the user cache directory at a temp tree, so the per-ticket
// lock files a write takes (see ticket.FileStore.lockFile) land there and go
// away with it rather than accumulating in the developer's real cache
// directory. It covers the tests that do not call setupTestHome as well. Both
// variables: os.UserCacheDir reads HOME on darwin and prefers XDG_CACHE_HOME
// elsewhere.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tk-test-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func setupTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
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
	centralDir := filepath.Join(centralRoot, "tickets", "myproject")
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

	centralRoot := filepath.Join(home, "central")

	// Create a project directory
	projDir := filepath.Join(home, "myproject")
	os.MkdirAll(projDir, 0o755)

	// Create config pointing to central store
	cfg := project.Config{CentralRoot: centralRoot, Projects: map[string]project.ProjectConfig{
		"myproject": {
			Path:  projDir,
			Store: "central",
		},
	}}
	project.Save(cfg)

	// Create central store directory
	centralDir := filepath.Join(centralRoot, "tickets", "myproject")
	os.MkdirAll(centralDir, 0o755)

	// Resolution should find the central store
	// We need to be in the project directory for this to work
	oldDir, _ := os.Getwd()
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	dir, _, _, err := resolveTicketsDir()
	if err != nil {
		t.Fatalf("resolveTicketsDir should resolve a central store project: %v", err)
	}
	if dir != centralDir {
		t.Errorf("resolveTicketsDir = %q, want %q", dir, centralDir)
	}
}

func TestTicketsDirNoLocalPath(t *testing.T) {
	home := setupTestHome(t)

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(filepath.Join(centralRoot, "tickets", "ticket"), 0o755)

	// Write local config with central_root but no project path
	localCfg := project.Config{CentralRoot: centralRoot, Projects: map[string]project.ProjectConfig{}}
	project.Save(localCfg)

	// Write shared config with project
	sharedData := []byte("projects:\n    ticket:\n        store: central\n")
	os.WriteFile(filepath.Join(centralRoot, "config.yaml"), sharedData, 0o644)

	// Resolution should go via git remote
	// Since we're in the ticket repo, git remote gives us "ticket"
	oldDir, _ := os.Getwd()
	projDir := filepath.Join(home, "fakerepo")
	os.MkdirAll(projDir, 0o755)
	runGit(t, projDir, "init")
	runGit(t, projDir, "config", "user.email", "t@t.com")
	runGit(t, projDir, "config", "user.name", "t")
	os.Chdir(projDir)
	defer os.Chdir(oldDir)

	// Resolution goes via ResolveName (dirname fallback)
	// which matches the shared config's "ticket" project... but our dir
	// is "fakerepo" not "ticket". Let's use a dir named "ticket".
	projDir2 := filepath.Join(home, "ticket")
	os.MkdirAll(projDir2, 0o755)
	runGit(t, projDir2, "init")
	runGit(t, projDir2, "config", "user.email", "t@t.com")
	runGit(t, projDir2, "config", "user.name", "t")
	os.Chdir(projDir2)

	dir, _, _, err := resolveTicketsDir()
	if err != nil {
		t.Fatalf("resolveTicketsDir should resolve via shared config + dirname: %v", err)
	}
	expected := filepath.Join(centralRoot, "tickets", "ticket")
	if dir != expected {
		t.Errorf("resolveTicketsDir = %q, want %q", dir, expected)
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

	centralRoot := filepath.Join(home, "central")
	os.MkdirAll(centralRoot, 0o755)
	cfg := project.Config{CentralRoot: centralRoot, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

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
	initCmd.Flags().Set("project", "niproject")
	defer func() {
		initCmd.Flags().Set("yes", "false")
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
		t.Errorf("store = %q, want central", p.Store)
	}
}

// TestInitUnderStoreRootOverrideLeavesEnclosingRepoAlone holds `tk init` to the
// documented "nothing is committed or pushed" under TK_STORE_ROOT. Running the
// bootstrap against an override root nested inside another repo would stage and
// commit the sandbox's own store files into that repo, so the bootstrap is
// skipped entirely. The enclosing repo here is built under t.TempDir: no test
// may reach the machine's own store.
func TestInitUnderStoreRootOverrideLeavesEnclosingRepoAlone(t *testing.T) {
	setupTestHome(t)

	enclosing := t.TempDir()
	runGit(t, enclosing, "init")
	runGit(t, enclosing, "config", "user.email", "test@test.com")
	runGit(t, enclosing, "config", "user.name", "test")
	tracked := filepath.Join(enclosing, "tracked.txt")
	os.WriteFile(tracked, []byte("committed\n"), 0o644)
	runGit(t, enclosing, "add", "-A")
	runGit(t, enclosing, "commit", "-m", "initial")

	// Uncommitted work of the kind the bootstrap would sweep up.
	os.WriteFile(tracked, []byte("uncommitted edit\n"), 0o644)
	os.WriteFile(filepath.Join(enclosing, ".env"), []byte("SECRET\n"), 0o644)

	override := filepath.Join(enclosing, "sandbox")
	os.MkdirAll(override, 0o755)
	t.Setenv(project.StoreRootEnv, override)

	oldDir, _ := os.Getwd()
	os.Chdir(enclosing)
	defer os.Chdir(oldDir)

	initCmd.Flags().Set("yes", "true")
	initCmd.Flags().Set("project", "sandbox")
	defer func() {
		initCmd.Flags().Set("yes", "false")
		initCmd.Flags().Set("project", "")
	}()

	if err := runInit(initCmd, nil); err != nil {
		t.Fatalf("runInit under the override: %v", err)
	}

	// The project must still register — a harness needs `tk init` to work
	// under the override, because central writes to unregistered projects are
	// refused.
	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p, ok := cfg.Projects["sandbox"]; !ok || p.Store != "central" {
		t.Errorf("sandbox project = %+v (present=%v), want store: central", p, ok)
	}

	count, _ := execCommand("git", "-C", enclosing, "rev-list", "--count", "HEAD")
	if count != "1" {
		t.Errorf("enclosing repo has %s commits, want 1: init committed under the override", count)
	}
	staged, _ := execCommand("git", "-C", enclosing, "diff", "--cached", "--name-only")
	if staged != "" {
		t.Errorf("enclosing repo has staged paths %q, want none", staged)
	}
}

// TestInitBootstrapGitNestedStoreStagesOnlyStorePaths pins the bootstrap's
// pathspec on every git operation it runs. `-C` sets the working directory but
// not the pathspec, so a pathspec-less `add -A` swept the enclosing repo's
// entire worktree — unrelated in-progress edits and untracked files such as
// .env — into the store's init commit, and a pathspec-less commit did the same
// with anything the repo's owner had already staged. The enclosing repo here is
// built under t.TempDir: no test may reach the machine's own store.
func TestInitBootstrapGitNestedStoreStagesOnlyStorePaths(t *testing.T) {
	setupTestHome(t)

	enclosing := t.TempDir()
	runGit(t, enclosing, "init")
	runGit(t, enclosing, "config", "user.email", "test@test.com")
	runGit(t, enclosing, "config", "user.name", "test")
	tracked := filepath.Join(enclosing, "tracked.txt")
	os.WriteFile(tracked, []byte("committed\n"), 0o644)
	runGit(t, enclosing, "add", "-A")
	runGit(t, enclosing, "commit", "-m", "initial")

	// Unrelated in-progress work the bootstrap must not touch: an edit left in
	// the worktree, a file the repo's owner had already staged, and a file that
	// is never staged at all — `add -A` swept up all three.
	os.WriteFile(tracked, []byte("uncommitted edit\n"), 0o644)
	os.WriteFile(filepath.Join(enclosing, ".env"), []byte("SECRET\n"), 0o644)
	runGit(t, enclosing, "add", ".env")
	os.WriteFile(filepath.Join(enclosing, "scratch.txt"), []byte("untracked\n"), 0o644)

	storeRoot := filepath.Join(enclosing, "store")
	os.MkdirAll(filepath.Join(storeRoot, "tickets", "myproject"), 0o755)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "myproject", "ticket-1234.md"),
		[]byte("---\ntitle: Test\n---\n"), 0o644)
	os.WriteFile(filepath.Join(storeRoot, "config.yaml"), []byte("projects: {}\n"), 0o644)

	if err := bootstrapCentralStoreGit(storeRoot); err != nil {
		t.Fatalf("bootstrapCentralStoreGit: %v", err)
	}

	committed, _ := execCommand("git", "-C", enclosing, "show", "--name-only", "--pretty=format:", "HEAD")
	for _, name := range []string{"tracked.txt", ".env", "scratch.txt"} {
		if contains(committed, name) {
			t.Errorf("commit contains enclosing repo path %q; committed files:\n%s", name, committed)
		}
	}
	for _, name := range []string{"store/tickets/myproject/ticket-1234.md", "store/config.yaml"} {
		if !contains(committed, name) {
			t.Errorf("commit missing store path %q; committed files:\n%s", name, committed)
		}
	}

	// The enclosing repo's own work stays exactly as its owner left it: .env
	// still staged and uncommitted, tracked.txt's edit still only in the
	// worktree, scratch.txt still untracked.
	staged, _ := execCommand("git", "-C", enclosing, "diff", "--cached", "--name-only")
	if staged != ".env" {
		t.Errorf("enclosing repo staged paths = %q, want %q", staged, ".env")
	}
	if head, _ := execCommand("git", "-C", enclosing, "show", "HEAD:tracked.txt"); head != "committed" {
		t.Errorf("tracked.txt at HEAD = %q, want the original content", head)
	}
	untracked, _ := execCommand("git", "-C", enclosing, "ls-files", "--others", "--exclude-standard")
	if !contains(untracked, "scratch.txt") {
		t.Errorf("scratch.txt is no longer untracked; untracked files:\n%s", untracked)
	}
}

// TestInitBootstrapGitMissingSharedConfig covers a central root whose
// config.yaml is absent — `git add` on a pathspec that matches nothing is a
// hard error, so the bootstrap must skip the missing path rather than fail.
func TestInitBootstrapGitMissingSharedConfig(t *testing.T) {
	home := setupTestHome(t)

	storeRoot := filepath.Join(home, "central")
	os.MkdirAll(filepath.Join(storeRoot, "tickets", "myproject"), 0o755)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "myproject", "ticket-1234.md"),
		[]byte("---\ntitle: Test\n---\n"), 0o644)

	if err := bootstrapCentralStoreGit(storeRoot); err != nil {
		t.Fatalf("bootstrapCentralStoreGit without config.yaml: %v", err)
	}

	committed, _ := execCommand("git", "-C", storeRoot, "show", "--name-only", "--pretty=format:", "HEAD")
	if !contains(committed, "tickets/myproject/ticket-1234.md") {
		t.Errorf("commit missing the ticket file; committed files:\n%s", committed)
	}
	if contains(committed, "config.yaml") {
		t.Errorf("commit contains config.yaml, which never existed; committed files:\n%s", committed)
	}
}

// TestInitBootstrapGitIgnoredStoreRoot pins the one case the pathspec'd add
// fails loudly where `add -A` used to succeed badly: an enclosing repo whose
// .gitignore covers the store root. `git add -- tickets/` exits non-zero there,
// so the bootstrap must return the error — and init abort before registering —
// rather than report success on a store the enclosing repo would never keep.
// The enclosing repo here is built under t.TempDir: no test may reach the
// machine's own store.
func TestInitBootstrapGitIgnoredStoreRoot(t *testing.T) {
	setupTestHome(t)

	enclosing := t.TempDir()
	runGit(t, enclosing, "init")
	runGit(t, enclosing, "config", "user.email", "test@test.com")
	runGit(t, enclosing, "config", "user.name", "test")
	os.WriteFile(filepath.Join(enclosing, ".gitignore"), []byte("store/\n"), 0o644)
	runGit(t, enclosing, "add", ".gitignore")
	runGit(t, enclosing, "commit", "-m", "initial")

	storeRoot := filepath.Join(enclosing, "store")
	os.MkdirAll(filepath.Join(storeRoot, "tickets", "myproject"), 0o755)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "myproject", "ticket-1234.md"),
		[]byte("---\ntitle: Test\n---\n"), 0o644)
	os.WriteFile(filepath.Join(storeRoot, "config.yaml"), []byte("projects: {}\n"), 0o644)

	err := bootstrapCentralStoreGit(storeRoot)
	if err == nil {
		t.Fatal("bootstrapCentralStoreGit on an ignored store root returned nil, want an error")
	}
	if !contains(err.Error(), "tickets/") {
		t.Errorf("error does not name the path it failed on: %v", err)
	}
	if !contains(err.Error(), "ignored") {
		t.Errorf("error does not report the .gitignore as the cause: %v", err)
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	out, err := execCommand("git", allArgs...)
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}
