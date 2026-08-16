// Package state locates and appends to the JSONL records tk keeps beside the
// ticket store — the commit journal, the mutation log and the retrospect
// markers the journal watcher fires from. It is a leaf package
// so that both pkg/journal and pkg/ticket can reach it: pkg/journal already
// imports pkg/ticket for the watch cycle, so the primitives the store layer
// needs cannot live there.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// Dir returns ~/.ticket/state/<project>/.
func Dir(proj string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return projDir(filepath.Join(home, ".ticket", "state"), proj)
}

// projDir joins proj under a state root after checking the name, the way
// project.CentralProjectDir bounds the store-path join: filepath.Join cleans
// traversal segments rather than failing, so a name carrying "/" or ".."
// resolves state files outside the root — and names reach here from config map
// keys, which the shared config replicates from other machines. Every state-path
// helper resolves through this, so the bound is not copied at call sites.
func projDir(stateRoot, proj string) (string, error) {
	if !project.ValidName(proj) {
		return "", fmt.Errorf("invalid project name %q", proj)
	}
	return filepath.Join(stateRoot, proj), nil
}

// MutationLogPath returns the project's mutations.jsonl — a sibling of the
// commit journal under ~/.ticket/state/<project>/, or the override root's own
// state tree when TK_STORE_ROOT is set.
//
// The one state path that follows the override. The commit journal stays
// HOME-bound because the commands that write it are refused outright while the
// override is set (refuseIsolatedStore in cmd/root.go); a mutation is appended
// by every write and cannot be refused, so a sandbox left writing into HOME
// would file its throwaway tickets in the machine's real audit trail.
func MutationLogPath(proj string) (string, error) {
	return overridablePath(proj, "mutations.jsonl")
}

// RetrospectLogPath returns the project's retrospects.jsonl — the markers that
// keep the watch cycle from firing `loom retrospect` twice for one close, beside
// the commit journal under ~/.ticket/state/<project>/.
//
// It follows TK_STORE_ROOT on the same terms as MutationLogPath. The watcher
// never runs under the override — refuseIsolatedStore refuses it, and `tk serve`
// starts no journal loop there — but a path helper that resolved HOME regardless
// would be the thing that breaks that seam if one ever did.
func RetrospectLogPath(proj string) (string, error) {
	return overridablePath(proj, "retrospects.jsonl")
}

// overridablePath resolves one of the project's state files under the override
// root when TK_STORE_ROOT is set and under ~/.ticket/state otherwise.
func overridablePath(proj, file string) (string, error) {
	root, ok, err := project.StoreRootOverride()
	if err != nil {
		return "", err
	}
	if ok {
		dir, err := projDir(filepath.Join(root, "state"), proj)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, file), nil
	}
	dir, err := Dir(proj)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, file), nil
}

// AppendJSONL appends each value to path as one JSON line, creating the parent
// directory and the file if they do not exist. Each line is written by a single
// Write to a descriptor opened O_APPEND, which is what keeps two processes
// appending to one log from interleaving halves of a record.
func AppendJSONL(path string, values ...any) error {
	if len(values) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, v := range values {
		line, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}
