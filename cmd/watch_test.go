package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// TestWatchCycleWritesOnlyToCentralProjects holds the watch cycle to the one
// store tk resolves. An old `store: local` entry is not a registration, so it
// gets no store at all: the commit that would auto-close a ticket in a
// .tickets/ beside that repo leaves the file exactly as it was, while the
// registered project's ticket is closed in the same cycle — without which this
// test would pass on a watcher that ran nothing.
//
// `store: local` rather than an absent store because only a non-empty value
// survives into the shared config, which is where the auto flags live.
func TestWatchCycleWritesOnlyToCentralProjects(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")

	centralRepo := filepath.Join(home, "wt-central")
	legacyRepo := filepath.Join(home, "wt-legacy")
	for _, dir := range []string{centralRepo, legacyRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		runGit(t, dir, "init")
		runGit(t, dir, "config", "user.email", "test@test.com")
		runGit(t, dir, "config", "user.name", "test")
	}

	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"wt-central": {Path: centralRepo, Store: "central", AutoLink: true, AutoClose: true},
			"wt-legacy":  {Path: legacyRepo, Store: "local", AutoLink: true, AutoClose: true},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	central := ticket.NewProjectFileStore(filepath.Join(centralRoot, "tickets", "wt-central"), "wt-central")
	mkCentral(t, central, "wt-central-0001", ticket.TypeFeature, ticket.StatusOpen, "")

	legacyDir := filepath.Join(legacyRepo, ".tickets")
	legacyStore := ticket.NewFileStore(legacyDir)
	mkCentral(t, legacyStore, "wt-legacy-0002", ticket.TypeFeature, ticket.StatusOpen, "")
	legacyFile := filepath.Join(legacyDir, "wt-legacy-0002.md")
	before, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	commitClosing(t, centralRepo, "wt-central-0001")
	commitClosing(t, legacyRepo, "wt-legacy-0002")

	watchOnce = true
	watchInterval = time.Second
	t.Cleanup(func() { watchOnce = false })

	if err := runWatchRun(watchRunCmd, nil); err != nil {
		t.Fatalf("runWatchRun: %v", err)
	}

	closed, err := central.Get("wt-central-0001")
	if err != nil {
		t.Fatalf("Get central ticket: %v", err)
	}
	if closed.Status != ticket.StatusDone {
		t.Errorf("central ticket status = %q, want %q — the cycle wrote nothing at all", closed.Status, ticket.StatusDone)
	}

	after, err := os.ReadFile(legacyFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("the watcher wrote into a .tickets/:\n%s", string(after))
	}
}

// The "no store" watchStoreFor returns for an unregistered project has to be a
// nil interface rather than a nil *FileStore carried in one: RunWatchCycle
// decides whether to auto-close on `store != nil`, which a typed nil passes, so
// a typed one would send every unregistered project's auto-close into a store
// with no directory.
func TestWatchStoreForUnregisteredProjectIsANilInterface(t *testing.T) {
	home := setupTestHome(t)
	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"ws-central": {Store: "central"},
			"ws-legacy":  {Store: "local"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	store, err := watchStoreFor(cfg, "ws-legacy")
	if err != nil {
		t.Fatalf("watchStoreFor: %v", err)
	}
	if store != nil {
		t.Errorf("store = %#v, want a nil interface", store)
	}

	store, err = watchStoreFor(cfg, "ws-central")
	if err != nil {
		t.Fatalf("watchStoreFor: %v", err)
	}
	want := filepath.Join(home, "central", "tickets", "ws-central")
	if fs, ok := store.(*ticket.FileStore); !ok || fs.Dir != want || fs.Project != "ws-central" {
		t.Errorf("store = %#v, want a project-scoped store over %q", store, want)
	}
}

func commitClosing(t *testing.T, repo, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, id+".txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "Closes: ["+id+"] Done")
}
