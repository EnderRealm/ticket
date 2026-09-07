package ticket

import (
	"fmt"
	"os"
	"path/filepath"
)

// lstatProjectDir reports whether the project directory name under root may be
// written to, and whether it is missing.
//
// Lstat, not Stat: the central store is a git repo and git tracks symlinks, so a
// project directory can arrive as one from another committer. A write through it
// lands at its target, outside the store, and MultiStore.projects() does not
// follow it either, so the tickets would be unlistable.
//
// A missing directory is not an error but a state the caller decides on: git
// tracks no empty directories, so a registered project that has never held a
// ticket arrives from a fresh clone of the central store without one.
//
// This is the one definition MultiStore.Create and CentralStoreForRepo both
// call, and the two refusal messages live only here. Both write paths carried
// their own copy of the check, and a branch of one of them was missed — see
// ticket d845 — so a write by repo was refused where the identical write by
// project name was not.
func lstatProjectDir(root, name string) (missing bool, err error) {
	info, err := os.Lstat(filepath.Join(root, name))
	switch {
	case err == nil && !info.IsDir():
		return false, fmt.Errorf("project %q in %s is not a directory — refusing to write outside the store", name, root)
	case err != nil && !os.IsNotExist(err):
		return false, fmt.Errorf("project %q in %s: %w", name, root, err)
	case err != nil:
		return true, nil
	}
	return false, nil
}
