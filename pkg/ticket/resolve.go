package ticket

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

// CentralStoreForRepo resolves a repo directory to the project store it owns in
// the central store: the store, whether that project is unregistered, whether
// anything resolved at all, and the error a config that cannot be read is.
// Reads ~/.ticket/config.yaml plus the shared <central_root>/config.yaml,
// resolves the project name via explicit path mapping, git remote, or directory
// basename, and returns a FileStore rooted at <central_root>/tickets/<name>.
//
// This is the choke point for "where do this repo's tickets live" — the CLI's
// own resolution and `tk move`'s destination both read through it, so a move
// cannot land tickets somewhere the repo's own `tk ls` will never look.
//
// The returned FileStore may wrap a directory that does not yet exist: a
// registered project that has never held a ticket arrives without one from a
// fresh clone of the central store, and List() on a missing directory returns
// (nil, nil) so callers can treat that as an empty project.
func CentralStoreForRepo(repoDir string) (store *FileStore, unregistered, ok bool, err error) {
	cfg, err := project.Load()
	if err != nil {
		// A config that fails to load is not the same answer as "this repo has
		// no central project": nothing resolves either way, but a caller that
		// writes has to name the config rather than send the user to `tk init`,
		// which will not fix a half-written or unreadable one.
		return nil, false, false, err
	}
	name, _ := project.ResolveName(cfg, repoDir, "")
	// ValidName, not merely non-empty: the config-path source of ResolveName
	// returns the config map key verbatim, and filepath.Join cleans traversal
	// segments rather than failing, so a crafted key would resolve a store
	// outside the central root — and this resolution feeds writes, not just
	// reads. MultiStore.storeFor guards the identical name-into-path join.
	if !project.ValidName(name) {
		return nil, false, false, nil
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		// Not carried out as a reason: the failure here is an unconfigured
		// central_root, which the caller reports as the repo having no store at
		// all rather than as a distinct error.
		return nil, false, false, nil
	}
	if project.CentralRegistered(cfg, name) {
		return NewProjectFileStore(dir, name), false, true, nil
	}
	// An unregistered project can still hold tickets centrally — written before
	// MultiStore.Create started refusing them, or replicated from another
	// machine. Reporting the repo as having no store would leave those tickets
	// unlisted on disk. Surfacing them is read-only: registering the project is
	// `tk init`'s job, so the caller warns rather than writing config.
	//
	// Lstat, not Stat: this resolution feeds writes as well as reads, so
	// following a symlink here would land a `tk create` outside the store —
	// exactly what MultiStore.Create refuses — and the listing walk does not
	// follow it either.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, false, false, nil
	}
	return NewProjectFileStore(dir, name), true, true, nil
}

// noStoreError is the one error a repo resolving to no central project is. Every
// resolution reports it — the CLI's own, `tk move`'s destination, and
// ticket_create's repo argument — so the state reads the same way wherever it is
// hit, and it never mints a store instead: joining ".tickets" onto the path and
// letting a write create it produces a directory nothing else reads and orphans
// whatever lands there.
//
// A repo still holding a local .tickets/ is named it. tk reads only the central
// store now, so those tickets have gone quiet, and the user has to be told where
// they are rather than left thinking they were lost. Naming it is all that
// happens to it: nothing in tk deletes or rewrites a .tickets/, and the `tk init`
// this points at copies the files into the central store and leaves the original
// in place as a backup.
//
// The message names the directory to run `tk init` in rather than saying
// "there", because the .tickets/ found is not always in the directory the
// resolution failed for, and the two can be spelled differently — see
// legacyStoreDir.
func noStoreError(repoDir string) error {
	if legacy := legacyStoreDir(repoDir); legacy != "" {
		return fmt.Errorf("no ticket store found for %s: %s is a local ticket store, which tk no longer reads — run `tk init` in %s to copy those tickets into the central store, which leaves the directory in place", repoDir, legacy, filepath.Dir(legacy))
	}
	return fmt.Errorf("no ticket store found for %s — run `tk init` there to register the project", repoDir)
}

// legacyStoreDir returns the .tickets/ a repo still holds, or "" for none. Two
// stats, not the unbounded walk-up the old resolution did: the directory itself,
// then the git top level, which is the same root `tk init` would migrate — so
// standing in a subdirectory of the repo gets the same answer as standing in its
// root, where the plain error would have read as the tickets having been lost. A
// walk-up instead of the git root would, from anywhere under $HOME, reach a
// central root named ~/.tickets and report the central store itself as a stale
// local one.
//
// The repo directory is probed first so the common case names the path in the
// caller's own spelling: the git top level comes back canonicalized, which on
// macOS prints /private/var beside a caller's /var.
func legacyStoreDir(repoDir string) string {
	dirs := []string{repoDir}
	if top := project.DetectProjectPath(repoDir); top != "" && top != repoDir {
		dirs = append(dirs, top)
	}
	for _, dir := range dirs {
		legacy := filepath.Join(dir, ".tickets")
		if info, err := os.Stat(legacy); err == nil && info.IsDir() {
			return legacy
		}
	}
	return ""
}

// UnregisteredWarning is the one phrasing for a store that resolved to an
// unregistered central project. Every surface that can land on one — the CLI's
// own resolution, `tk move` and the TUI's move — says the same sentence, so the
// condition reads as a single state rather than as several.
func UnregisteredWarning(store *FileStore) string {
	return fmt.Sprintf("warning: project %q is not registered but has a ticket directory at %s — run `tk init` to register it", store.Project, store.Dir)
}

// ResolveStoreForRepo opens the store a repo's tickets live in: its project
// directory in the central store, which is the only store tk resolves. It
// reports whether that project is unregistered, because writing into one puts a
// ticket where MultiStore.Create refuses to write and the caller has to say so.
// A repo that resolves to no project is noStoreError.
//
// One known divergence, filed rather than resolved here: the project name falls
// back to a git remote or the directory basename, so a path that is no
// registered repo can still resolve to a real project's store
// (ticket/tk-move-resolves-25f7).
func ResolveStoreForRepo(repoDir string) (*FileStore, bool, error) {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, false, fmt.Errorf("invalid repo path %s: %w", repoDir, err)
	}
	store, unregistered, ok, err := CentralStoreForRepo(abs)
	if err != nil {
		// The config that failed to load is what decides where this repo's
		// tickets are, so it is named rather than reported as the repo having
		// none: `tk init` does not fix a malformed config.
		return nil, false, fmt.Errorf("load ticket config: %w", err)
	}
	if ok {
		return store, unregistered, nil
	}
	return nil, false, noStoreError(abs)
}
