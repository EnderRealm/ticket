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
		// central_root, which is the local-only setup the caller falls through
		// to a .tickets/ for rather than an error to report.
		return nil, false, false, nil
	}
	if project.CentralRegistered(cfg, name) {
		return NewProjectFileStore(dir, name), false, true, nil
	}
	// An unregistered project can still hold tickets centrally — written before
	// MultiStore.Create started refusing them, or replicated from another
	// machine. Falling through would resolve to a local store and report an
	// empty list while the tickets sit on disk. Surfacing them is read-only:
	// registering the project is `tk init`'s job, so the caller warns rather
	// than writing config.
	//
	// Lstat, not Stat: this resolution feeds writes as well as reads, so
	// following a symlink here would land a `tk create` outside the store —
	// exactly what MultiStore.Create refuses — and the listing walk does not
	// follow it either.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, false, false, nil
	}
	// The name that got here is a guess — a git remote or a directory basename —
	// so it can collide with an unrelated project's central dir. A repo holding
	// its own .tickets/ keeps it, which also leaves a project deliberately
	// registered with a non-central store on the store it actually has.
	if _, found := FindTicketsDir(repoDir); found {
		return nil, false, false, nil
	}
	return NewProjectFileStore(dir, name), true, true, nil
}

// UnregisteredWarning is the one phrasing for a store that resolved to an
// unregistered central project. Every surface that can land on one — the CLI's
// own resolution, `tk move` and the TUI's move — says the same sentence, so the
// condition reads as a single state rather than as several.
func UnregisteredWarning(store *FileStore) string {
	return fmt.Sprintf("warning: project %q is not registered but has a ticket directory at %s — run `tk init` to register it", store.Project, store.Dir)
}

// ResolveStoreForRepo opens the store a repo's tickets live in: its project
// directory in the central store, then a .tickets/ the repo owns. It reports
// whether that project is unregistered, because writing into one puts a ticket
// where MultiStore.Create refuses to write and the caller has to say so. A repo
// that resolves to neither is an error naming it — the alternative, joining
// ".tickets" onto the path and letting the write create it, mints a store
// nothing else reads and orphans whatever lands there.
//
// Two known divergences, both filed rather than resolved here: the CLI's
// --repo resolution searches the repo's .tickets/ before the central project,
// the reverse of this order, so a centrally registered repo that still holds a
// stale .tickets/ is read locally and moved into centrally
// (ticket/repo-resolves-local-5111); and the project name falls back to a git
// remote or the directory basename, so a path that is no registered repo can
// still resolve to a real project's store (ticket/tk-move-resolves-25f7).
func ResolveStoreForRepo(repoDir string) (*FileStore, bool, error) {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, false, fmt.Errorf("invalid repo path %s: %w", repoDir, err)
	}
	store, unregistered, ok, err := CentralStoreForRepo(abs)
	if err != nil {
		// Not a fallthrough to the local search: the config that failed to load
		// is what decides whether this repo's tickets are central, so a
		// .tickets/ found without it may be the stale one the move must not use.
		return nil, false, fmt.Errorf("load ticket config: %w", err)
	}
	if ok {
		return store, unregistered, nil
	}
	if dir, ok := FindTicketsDir(abs); ok {
		return NewFileStore(dir), false, nil
	}
	return nil, false, fmt.Errorf("no ticket store found for %s — run `tk init` there to register the project", abs)
}
