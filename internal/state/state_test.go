package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

// traversingNames are the names project.ValidName rejects: they either escape
// the state root when joined into it or collapse onto the root itself.
var traversingNames = []string{"../../elsewhere", "a/b", "", ".", ".."}

// unsetStoreRoot clears the override for the tests that assert the HOME-bound
// paths. Absent rather than empty: an empty TK_STORE_ROOT is a set-but-unusable
// value, which is an error of its own and not the one under test.
func unsetStoreRoot(t *testing.T) {
	t.Helper()
	t.Setenv(project.StoreRootEnv, "")
	os.Unsetenv(project.StoreRootEnv)
}

func TestPathHelpersRefuseTraversingName(t *testing.T) {
	helpers := map[string]func(string) (string, error){
		"Dir":               Dir,
		"MutationLogPath":   MutationLogPath,
		"RetrospectLogPath": RetrospectLogPath,
	}

	t.Run("home root", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		unsetStoreRoot(t)
		for name, fn := range helpers {
			for _, proj := range traversingNames {
				if got, err := fn(proj); err == nil {
					t.Errorf("%s(%q) = %q, want an error", name, proj, got)
				}
			}
		}
	})

	t.Run("override root", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv(project.StoreRootEnv, t.TempDir())
		for name, fn := range helpers {
			for _, proj := range traversingNames {
				if got, err := fn(proj); err == nil {
					t.Errorf("%s(%q) = %q, want an error", name, proj, got)
				}
			}
		}
	})
}

func TestPathHelpersValidName(t *testing.T) {
	t.Run("home root", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		unsetStoreRoot(t)

		dir, err := Dir("myproject")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, ".ticket", "state", "myproject")
		if dir != want {
			t.Errorf("Dir = %q, want %q", dir, want)
		}

		path, err := MutationLogPath("myproject")
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(want, "mutations.jsonl") {
			t.Errorf("MutationLogPath = %q, want %q", path, filepath.Join(want, "mutations.jsonl"))
		}

		path, err = RetrospectLogPath("myproject")
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(want, "retrospects.jsonl") {
			t.Errorf("RetrospectLogPath = %q, want %q", path, filepath.Join(want, "retrospects.jsonl"))
		}
	})

	t.Run("override root", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HOME", t.TempDir())
		t.Setenv(project.StoreRootEnv, root)

		want := filepath.Join(root, "state", "myproject")

		path, err := MutationLogPath("myproject")
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(want, "mutations.jsonl") {
			t.Errorf("MutationLogPath = %q, want %q", path, filepath.Join(want, "mutations.jsonl"))
		}

		path, err = RetrospectLogPath("myproject")
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(want, "retrospects.jsonl") {
			t.Errorf("RetrospectLogPath = %q, want %q", path, filepath.Join(want, "retrospects.jsonl"))
		}
	})
}
