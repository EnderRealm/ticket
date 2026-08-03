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
	t.Setenv("TICKETS_DIR", "")

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

func TestEditPropagatesToNamespacedParent(t *testing.T) {
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
		t.Errorf("epic status = %q, want %q (CLI write must propagate to a namespaced parent)", epic.Status, ticket.StatusDone)
	}
}

func TestEditEpicDoneGuardWithNamespacedChild(t *testing.T) {
	store := centralStore(t, "cs-guard")
	mkCentral(t, store, "epic-3333", ticket.TypeEpic, ticket.StatusOpen, "")
	mkCentral(t, store, "child-4444", ticket.TypeFeature, ticket.StatusOpen, "cs-guard/epic-3333")

	err := runEditWith(t, "epic-3333", "status", "done")
	if err == nil {
		t.Fatal("expected error marking epic done with an open namespaced child, got nil")
	}
	if !contains(err.Error(), "child-4444") {
		t.Errorf("error should name child-4444, got: %v", err)
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

	dir, name, unregistered, ok := ticketsDirFromConfigFor(strayRepo)

	w.Close()
	os.Stderr = oldStderr
	warning, _ := io.ReadAll(r)

	if !ok {
		t.Fatal("resolution should find the central dir of an unregistered project")
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

	dir, name, unregistered, ok := ticketsDirFromConfigFor(strayRepo)
	if !ok {
		t.Fatal("resolution should find the central dir of a project whose config entry has no store")
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

	if dir, name, _, ok := ticketsDirFromConfigFor(strayRepo); ok {
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

func TestLsPrefersLocalStoreOverUnregisteredCentralDir(t *testing.T) {
	// The name behind an unregistered fallback is a guess — a git remote or a
	// directory basename — so it can collide with an unrelated project's central
	// dir. A repo holding its own .tickets/ keeps it: TicketStore() feeds writes
	// too, so shadowing would land a `tk create` in the other project's store.
	strayDir, strayRepo := unregisteredCentral(t, true, nil)
	mkCentral(t, ticket.NewProjectFileStore(strayDir, "cs-stray"), "central-9999", ticket.TypeFeature, ticket.StatusOpen, "")

	local := filepath.Join(strayRepo, ".tickets")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mkCentral(t, ticket.NewProjectFileStore(local, ""), "local-1111", ticket.TypeFeature, ticket.StatusOpen, "")

	if dir, name, _, ok := ticketsDirFromConfigFor(strayRepo); ok {
		t.Fatalf("config resolution should defer to the repo's own .tickets/, got dir=%q name=%q", dir, name)
	}

	oldDir, _ := os.Getwd()
	if err := os.Chdir(strayRepo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(oldDir)

	out := captureLs(t)
	if !contains(out, "local-1111") {
		t.Errorf("ls should list the repo's own .tickets/:\n%s", out)
	}
	if contains(out, "central-9999") {
		t.Errorf("unregistered central dir shadowed the repo's own .tickets/:\n%s", out)
	}
}

func TestTicketsDirUnregisteredWithoutCentralDir(t *testing.T) {
	// No directory on disk means no store to surface — resolution must fall
	// through to the local .tickets/ search rather than name a path that
	// nothing created.
	_, strayRepo := unregisteredCentral(t, false, nil)

	dir, name, _, ok := ticketsDirFromConfigFor(strayRepo)
	if ok {
		t.Fatalf("resolution should fail, got dir=%q name=%q", dir, name)
	}
}

func TestTicketStoreEnvDirHasNoProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TICKETS_DIR", dir)

	got := TicketStore()
	if got.Project != "" {
		t.Errorf("Project = %q, want empty (TICKETS_DIR is a single-project store)", got.Project)
	}
	if got.Dir != dir {
		t.Errorf("Dir = %q, want %q", got.Dir, dir)
	}
}

func TestTicketStoreLocalRepoHasNoProject(t *testing.T) {
	// A repo with its own .tickets/ never sees namespaced IDs, so accepting
	// them would let a "someproject/foo-abcd" reference resolve locally.
	repo := t.TempDir()
	ticketsDir := filepath.Join(repo, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	repoFlag = repo
	defer func() { repoFlag = "" }()

	got := TicketStore()
	if got.Project != "" {
		t.Errorf("Project = %q, want empty (local .tickets is a single-project store)", got.Project)
	}
	if got.Dir != ticketsDir {
		t.Errorf("Dir = %q, want %q", got.Dir, ticketsDir)
	}
}
