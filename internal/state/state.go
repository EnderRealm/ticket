// Package state locates and appends to the JSONL records tk keeps beside the
// ticket store — the commit journal and the mutation log. It is a leaf package
// so that both pkg/journal and pkg/ticket can reach it: pkg/journal already
// imports pkg/ticket for the watch cycle, so the primitives the store layer
// needs cannot live there.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

// Dir returns ~/.ticket/state/<project>/.
func Dir(proj string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ticket", "state", proj), nil
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
	root, ok, err := project.StoreRootOverride()
	if err != nil {
		return "", err
	}
	if ok {
		return filepath.Join(root, "state", proj, "mutations.jsonl"), nil
	}
	dir, err := Dir(proj)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mutations.jsonl"), nil
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
