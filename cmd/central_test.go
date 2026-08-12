package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// centralStore registers the test process's working directory as a
// central-store project and returns a project-scoped store over its central
// ticket dir. Commands resolve the same dir through TicketsDir().
func centralStore(t *testing.T, name string) *ticket.FileStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	centralRoot := t.TempDir()
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			name: {Path: mustGetwd(), Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	dir := filepath.Join(centralRoot, "tickets", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return ticket.NewProjectFileStore(dir, name)
}

// registerCentralProject adds a project to the config a central-store fixture
// has already written, and returns a store over its central ticket dir.
func registerCentralProject(t *testing.T, name, repoPath string) *ticket.FileStore {
	t.Helper()
	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	cfg.UpsertProject(name, project.ProjectConfig{Path: repoPath, Store: "central"})
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	dir, err := project.CentralProjectDir(name)
	if err != nil {
		t.Fatalf("CentralProjectDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return ticket.NewProjectFileStore(dir, name)
}

// mkCentral creates a ticket in the given store.
func mkCentral(t *testing.T, store *ticket.FileStore, id string, typ ticket.TicketType, status ticket.Status, parent string) {
	t.Helper()
	tk := &ticket.Ticket{
		ID:      id,
		Status:  status,
		Type:    typ,
		Parent:  parent,
		Created: time.Now(),
		Title:   "Ticket " + id,
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func TestEditDerivesNamespacedParentStatus(t *testing.T) {
	store := centralStore(t, "cs-prop")
	mkCentral(t, store, "epic-1111", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, store, "child-2222", ticket.TypeFeature, ticket.StatusOpen, "cs-prop/epic-1111")

	if err := runEditWith(t, "child-2222", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	epic, err := store.Get("epic-1111")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != ticket.StatusDone {
		t.Errorf("epic status = %q, want %q (a namespaced child has to count towards its epic)", epic.Status, ticket.StatusDone)
	}
}

func TestEditRejectsManualEpicStatus(t *testing.T) {
	store := centralStore(t, "cs-guard")
	mkCentral(t, store, "epic-3333", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkCentral(t, store, "child-4444", ticket.TypeFeature, ticket.StatusOpen, "cs-guard/epic-3333")

	err := runEditWith(t, "epic-3333", "status", "done")
	if err == nil {
		t.Fatal("expected error marking an epic done by hand, got nil")
	}
	if !contains(err.Error(), "epic-3333") || !contains(err.Error(), "derived from its children") {
		t.Errorf("error should name the epic and say the status is derived, got: %v", err)
	}
	epic, err := store.Get("epic-3333")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != ticket.StatusOpen {
		t.Errorf("epic status = %q, want the derived %q", epic.Status, ticket.StatusOpen)
	}
}

func TestTicketStoreCentralProject(t *testing.T) {
	store := centralStore(t, "cs-scope")

	got := TicketStore()
	if got.Project != "cs-scope" {
		t.Errorf("Project = %q, want %q", got.Project, "cs-scope")
	}
	if got.Dir != store.Dir {
		t.Errorf("Dir = %q, want %q", got.Dir, store.Dir)
	}
}

// resolveFor runs the CLI's own store resolution against a repo path, the way
// `tk --repo <path>` does — the resolution every command below it gets its store
// from.
func resolveFor(t *testing.T, repo string) (string, string, bool, error) {
	t.Helper()
	repoFlag = repo
	defer func() { repoFlag = "" }()
	return resolveTicketsDir()
}

// unregisteredCentral builds a central store that registers project "cs-known"
// and leaves a directory for "cs-stray" on disk. strayEntry, when non-nil, adds
// a config entry for cs-stray with its Path filled in — a project can be present
// in config and still not registered with the central store, since `store:
// central` lives in the shared config alone. Returns the stray project's central
// ticket dir and the repo path that resolves to it.
func unregisteredCentral(t *testing.T, strayDirExists bool, strayEntry *project.ProjectConfig) (string, string) {
	t.Helper()
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")

	known := filepath.Join(home, "cs-known")
	if err := os.MkdirAll(known, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	strayRepo := filepath.Join(home, "cs-stray")
	if err := os.MkdirAll(strayRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"cs-known": {Path: known, Store: "central"},
		},
	}
	if strayEntry != nil {
		entry := *strayEntry
		entry.Path = strayRepo
		cfg.Projects["cs-stray"] = entry
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	strayDir := filepath.Join(centralRoot, "tickets", "cs-stray")
	if strayDirExists {
		if err := os.MkdirAll(strayDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	return strayDir, strayRepo
}

func TestTicketsDirUnregisteredCentralDir(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, nil)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	dir, name, unregistered, err := resolveFor(t, strayRepo)

	w.Close()
	os.Stderr = oldStderr
	warning, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("resolution should find the central dir of an unregistered project: %v", err)
	}
	if dir != strayDir {
		t.Errorf("dir = %q, want %q", dir, strayDir)
	}
	if name != "cs-stray" {
		t.Errorf("name = %q, want %q", name, "cs-stray")
	}
	if !unregistered {
		t.Error("resolution should report the project as unregistered")
	}
	for _, want := range []string{"cs-stray", "tk init"} {
		if !contains(string(warning), want) {
			t.Errorf("warning = %q, want to contain %q", string(warning), want)
		}
	}
}

// Presence in config is not registration. `store: central` lives in the shared
// config alone, so a project whose shared config is missing — never cloned, or
// lost in a sync — merges to an entry carrying only a path. Keying on presence
// sent that project back down the local .tickets/ search with its central
// tickets sitting unlisted on disk, which is the bug this fixes.
func TestTicketsDirCentralDirForEntryWithoutStore(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, &project.ProjectConfig{})

	dir, name, unregistered, err := resolveFor(t, strayRepo)
	if err != nil {
		t.Fatalf("resolution should find the central dir of a project whose config entry has no store: %v", err)
	}
	if dir != strayDir {
		t.Errorf("dir = %q, want %q", dir, strayDir)
	}
	if name != "cs-stray" {
		t.Errorf("name = %q, want %q", name, "cs-stray")
	}
	if !unregistered {
		t.Error("a config entry without `store: central` is not a registration")
	}
}

// The central store is a git repo and git tracks symlinks, so a project
// directory can arrive as one from another committer. This resolution feeds
// writes as well as reads, so following it would land a `tk create` outside the
// store — exactly what MultiStore.Create refuses — and the listing walk does not
// follow it either.
func TestTicketsDirRefusesSymlinkedCentralDir(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, false, nil)
	if err := os.MkdirAll(filepath.Dir(strayDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(t.TempDir(), strayDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if dir, name, _, err := resolveFor(t, strayRepo); err == nil {
		t.Fatalf("a symlinked project directory must not resolve, got dir=%q name=%q", dir, name)
	}
}

func TestLsListsUnregisteredCentralProject(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, nil)
	mkCentral(t, ticket.NewProjectFileStore(strayDir, "cs-stray"), "stray-9999", ticket.TypeFeature, ticket.StatusOpen, "")

	oldDir, _ := os.Getwd()
	if err := os.Chdir(strayRepo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(oldDir)

	out := captureLs(t)
	if !contains(out, "stray-9999") {
		t.Errorf("ls output missing the unregistered project's ticket:\n%s", out)
	}
}

func TestLsReadsCentralDirBesideALegacyLocalStore(t *testing.T) {
	// A repo that still holds a .tickets/ reads its central project all the
	// same — the local directory is no longer a store tk resolves — and the
	// files in it are left exactly as they were.
	strayDir, strayRepo := unregisteredCentral(t, true, nil)
	mkCentral(t, ticket.NewProjectFileStore(strayDir, "cs-stray"), "central-9999", ticket.TypeFeature, ticket.StatusOpen, "")

	local := filepath.Join(strayRepo, ".tickets")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	legacy := filepath.Join(local, "local-1111.md")
	if err := os.WriteFile(legacy, []byte("---\nid: local-1111\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if dir, _, _, err := resolveFor(t, strayRepo); err != nil || dir != strayDir {
		t.Fatalf("resolution = (%q, %v), want the central dir %q", dir, err, strayDir)
	}

	oldDir, _ := os.Getwd()
	if err := os.Chdir(strayRepo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(oldDir)

	out := captureLs(t)
	if !contains(out, "central-9999") {
		t.Errorf("ls should list the central project:\n%s", out)
	}
	if contains(out, "local-1111") {
		t.Errorf("a .tickets/ is not a store tk reads:\n%s", out)
	}
	raw, err := os.ReadFile(legacy)
	if err != nil || string(raw) != "---\nid: local-1111\n---\n" {
		t.Errorf("the legacy ticket file was modified: %q, %v", string(raw), err)
	}
}

func TestTicketsDirUnregisteredWithoutCentralDir(t *testing.T) {
	// No directory on disk means no store to surface — resolution must report
	// nothing rather than name a path that nothing created.
	_, strayRepo := unregisteredCentral(t, false, nil)

	dir, name, _, err := resolveFor(t, strayRepo)
	if err == nil {
		t.Fatalf("resolution should fail, got dir=%q name=%q", dir, name)
	}
}

func TestResolveTicketsDirRefusesARepoWithNoProject(t *testing.T) {
	// Nothing resolves and nothing is minted: the error has to name the repo
	// and the command that would register it.
	home := setupTestHome(t)
	repo := filepath.Join(home, "unregistered-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.Save(project.Config{CentralRoot: filepath.Join(home, "central")}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	_, _, _, err := resolveFor(t, repo)
	if err == nil {
		t.Fatal("resolution succeeded for a repo owning no project, want an error")
	}
	for _, want := range []string{repo, "tk init"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, ".tickets")); !os.IsNotExist(err) {
		t.Errorf("a .tickets/ was created in %s: %v", repo, err)
	}
}

func TestResolveTicketsDirTreatsALegacyStoreValueAsUnregistered(t *testing.T) {
	// `store: local` is what an old shared config can still carry. It is not a
	// registration and it names nothing tk resolves, so the repo reads as
	// owning no store — not as a directory tk would silently create or read.
	home := setupTestHome(t)
	repo := filepath.Join(home, "legacy-store-value")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"legacy-store-value": {Path: repo, Store: "local"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	if _, _, _, err := resolveFor(t, repo); err == nil {
		t.Fatal("`store: local` resolved a store, want the repo reported as owning none")
	}

	// The value is left alone rather than migrated: a save round-trips it.
	reloaded, err := project.Load()
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if got := reloaded.Projects["legacy-store-value"].Store; got != "local" {
		t.Errorf("stored store value = %q, want it left as %q", got, "local")
	}
	if project.CentralRegistered(reloaded, "legacy-store-value") {
		t.Error("`store: local` must not read as a central registration")
	}
}

func TestResolveTicketsDirNamesASharedConfigItCannotLoad(t *testing.T) {
	// The shared config arrives over git from other machines, so a half-written
	// or conflicted one is the ordinary way this fails. It decides where the
	// repo's tickets are, so it is named as itself: reporting the repo as
	// unregistered sends the user to `tk init`, which does not fix a config.
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")
	repo := filepath.Join(home, "bad-config-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.Save(project.Config{CentralRoot: centralRoot}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(centralRoot, "config.yaml"), []byte("projects:\n  <<<<<<< HEAD\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _, _, err := resolveFor(t, repo)
	if err == nil {
		t.Fatal("resolution succeeded with an unreadable shared config, want an error")
	}
	if !contains(err.Error(), "config") {
		t.Errorf("error %q does not name the config that failed to load", err)
	}
	if contains(err.Error(), "tk init") {
		t.Errorf("error %q sends the user to `tk init`, which does not fix a malformed config", err)
	}
}

func TestUIWorkDirFallsBackToTheResolvedRepo(t *testing.T) {
	// A project registered on another machine has no local `path`, and a tickets
	// dir's parent is the central tickets dir rather than a repo — so the
	// directory `tk ui` spawns a work session in is the repo it resolved from,
	// never one inside the ticket store.
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")
	repo := filepath.Join(home, "pathless-repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects:    map[string]project.ProjectConfig{"pathless": {Store: "central"}},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	repoFlag = repo
	defer func() { repoFlag = "" }()

	ticketsDir := filepath.Join(centralRoot, "tickets", "pathless")
	if got := uiWorkDir(ticketsDir, cfg); got != repo {
		t.Errorf("uiWorkDir = %q, want the resolved repo %q", got, repo)
	}
}

func TestResolveTicketsDirNamesALegacyLocalStore(t *testing.T) {
	// The one case a user can mistake for lost tickets: the repo has a
	// .tickets/ and no central project. The error names that directory and the
	// command that migrates it, and the directory is left byte for byte alone.
	home := setupTestHome(t)
	repo := filepath.Join(home, "legacy-repo")
	local := filepath.Join(repo, ".tickets")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "---\nid: old-1111\nstatus: open\n---\n\nStill here.\n"
	if err := os.WriteFile(filepath.Join(local, "old-1111.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := project.Save(project.Config{CentralRoot: filepath.Join(home, "central")}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	_, _, _, err := resolveFor(t, repo)
	if err == nil {
		t.Fatal("resolution succeeded for a repo holding only a .tickets/, want an error")
	}
	for _, want := range []string{local, "tk init"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	entries, readErr := os.ReadDir(local)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "old-1111.md" {
		t.Fatalf(".tickets/ holds %v, want only the original ticket", entries)
	}
	raw, readErr := os.ReadFile(filepath.Join(local, "old-1111.md"))
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(raw) != body {
		t.Errorf("the legacy ticket file was rewritten:\n%s", string(raw))
	}
}
