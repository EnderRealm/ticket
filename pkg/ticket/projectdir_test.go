package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// sharedProjectFixture registers the project "shared" centrally against a repo
// directory and returns that repo, the central tickets root and a MultiStore
// over it. The tickets root is left uncreated: each caller decides what stands
// at <tickets root>/shared, which is the whole subject here.
func sharedProjectFixture(t *testing.T) (repo, ticketsRoot string, ms *MultiStore) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	repo = filepath.Join(home, "shared")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := project.Save(project.Config{
		CentralRoot: centralRoot,
		Projects:    map[string]project.ProjectConfig{"shared": {Path: repo, Store: "central"}},
	}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	ticketsRoot = filepath.Join(centralRoot, "tickets")
	return repo, ticketsRoot, NewMultiStore(ticketsRoot)
}

// TestProjectDirRefusalIsOneDefinition holds the two write paths — a write by
// project name through MultiStore.Create, and a write through the store
// CentralStoreForRepo resolves for a repo — against each other for the same
// root and name. Each carried its own copy of this check and one branch of one
// of them was missed (ticket d845), so the interesting assertion is that the two
// answers are the same string, not that each matches a substring.
func TestProjectDirRefusalIsOneDefinition(t *testing.T) {
	t.Run("a project dir that is not a directory refuses on both paths", func(t *testing.T) {
		repo, ticketsRoot, ms := sharedProjectFixture(t)
		if err := os.MkdirAll(ticketsRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(ticketsRoot, "shared")); err != nil {
			t.Fatalf("Symlink: %v", err)
		}

		errMulti := ms.Create(sampleTicket("shared/link-1234"))
		store, _, errResolve := ResolveStoreForRepo(repo)
		if errMulti == nil || errResolve == nil {
			t.Fatalf("MultiStore.Create = %v, ResolveStoreForRepo = %v, want both refused", errMulti, errResolve)
		}
		if store != nil {
			t.Errorf("returned a store (%q) alongside the refusal", store.Dir)
		}
		if errMulti.Error() != errResolve.Error() {
			t.Errorf("the two write paths refuse differently:\n  MultiStore.Create:    %q\n  ResolveStoreForRepo:  %q", errMulti, errResolve)
		}
		// Pins the root the message names to <central_root>/tickets, and makes a
		// reword of the one definition break here on purpose.
		want := fmt.Sprintf("project %q in %s is not a directory — refusing to write outside the store", "shared", ticketsRoot)
		if errMulti.Error() != want {
			t.Errorf("refusal = %q, want %q", errMulti, want)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("a write landed outside the store: %v", entries)
		}
	})

	t.Run("a missing project dir resolves on both paths", func(t *testing.T) {
		// Git tracks no empty directories, so a registered project that has
		// never held a ticket arrives from a fresh clone without one.
		repo, ticketsRoot, ms := sharedProjectFixture(t)
		if err := os.MkdirAll(ticketsRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		store, unregistered, err := ResolveStoreForRepo(repo)
		if err != nil {
			t.Fatalf("ResolveStoreForRepo: %v", err)
		}
		if want := filepath.Join(ticketsRoot, "shared"); store.Dir != want {
			t.Errorf("Dir = %q, want %q", store.Dir, want)
		}
		if unregistered {
			t.Error("resolution reported a registered project as unregistered")
		}
		if err := ms.Create(sampleTicket("shared/fresh-1234")); err != nil {
			t.Fatalf("MultiStore.Create into a registered project with no dir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(ticketsRoot, "shared", "fresh-1234.md")); err != nil {
			t.Errorf("the ticket did not land in the project dir: %v", err)
		}
	})

	t.Run("a stat error that is not IsNotExist is carried out on both paths", func(t *testing.T) {
		// A regular file at the tickets root makes the Lstat below it fail with
		// ENOTDIR rather than ENOENT, without permission tricks.
		repo, ticketsRoot, ms := sharedProjectFixture(t)
		if err := os.WriteFile(ticketsRoot, []byte("not a directory\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		errMulti := ms.Create(sampleTicket("shared/stat-1234"))
		store, _, errResolve := ResolveStoreForRepo(repo)
		if errMulti == nil || errResolve == nil {
			t.Fatalf("MultiStore.Create = %v, ResolveStoreForRepo = %v, want both to fail", errMulti, errResolve)
		}
		if store != nil {
			t.Errorf("returned a store (%q) alongside the error", store.Dir)
		}
		if errMulti.Error() != errResolve.Error() {
			t.Errorf("the two write paths report the stat failure differently:\n  MultiStore.Create:    %q\n  ResolveStoreForRepo:  %q", errMulti, errResolve)
		}
		if strings.Contains(errMulti.Error(), "refusing to write outside the store") {
			t.Errorf("a stat failure reported as the not-a-directory refusal: %q", errMulti)
		}
		if errors.Is(errMulti, os.ErrNotExist) {
			t.Errorf("a stat failure reported as a missing directory: %q", errMulti)
		}
		for _, want := range []string{ticketsRoot, "shared"} {
			if !strings.Contains(errMulti.Error(), want) {
				t.Errorf("error %q does not name %q", errMulti, want)
			}
		}
	})
}

func TestResolveStoreForRepoRefusesAnUnregisteredProjectDirThatIsNotOne(t *testing.T) {
	// The unregistered branch reported any stat failure or non-directory as "no
	// ticket store found", which sends the user to `tk init`. A write through a
	// symlinked project directory lands outside the store whether or not the
	// project is registered, and `tk init` fixes that no more than it fixes an
	// unreadable config — so it refuses with the same one definition.
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	repo := filepath.Join(home, "loose")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// No entry for "loose": the name comes back from the directory basename.
	if err := project.Save(project.Config{CentralRoot: centralRoot}); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	ticketsRoot := filepath.Join(centralRoot, "tickets")
	if err := os.MkdirAll(ticketsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ticketsRoot, "loose")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	store, _, err := ResolveStoreForRepo(repo)
	if err == nil {
		t.Fatalf("resolved %q through a symlinked project dir, want an error", store.Dir)
	}
	if store != nil {
		t.Errorf("returned a store (%q) alongside the refusal", store.Dir)
	}
	want := fmt.Sprintf("project %q in %s is not a directory — refusing to write outside the store", "loose", ticketsRoot)
	if err.Error() != want {
		t.Errorf("refusal = %q, want %q", err, want)
	}
	if !strings.Contains(err.Error(), "refusing to write outside the store") {
		t.Errorf("error %q does not name the condition the registered branch names", err)
	}
	if strings.Contains(err.Error(), "tk init") {
		t.Errorf("error %q sends the user to `tk init`, which does not fix a symlinked project dir", err)
	}
}
