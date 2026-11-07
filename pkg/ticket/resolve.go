package ticket

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
)

// CentralStoreForRepo resolves a repo directory to the project store it owns in
// the central store: the store, whether that project is unregistered, whether
// anything resolved at all, and the error a config that cannot be read or a
// project directory that is not one is. That refusal reaches reads as well as
// writes, since this is the only resolution: a `tk ls` in a repo whose project
// directory is a symlink fails rather than listing through it, which is the
// answer every other surface already gives — MultiStore.projects() skips such
// an entry, so resolving it would hand back a store nothing else agrees exists.
// Reads ~/.ticket/config.yaml plus the shared <central_root>/config.yaml,
// resolves the project name via explicit path mapping, git remote, or directory
// basename, and returns a FileStore rooted at <central_root>/tickets/<name>.
//
// This answers "where do this repo's tickets live" for the working directory,
// --repo and MCP's repo argument, all of which read through ResolveStoreForRepo.
// `tk move`'s destination picks its project by a stricter rule,
// ResolveMoveDestination, but opens it through the same centralProjectStore, so
// a move cannot land tickets somewhere the repo's own `tk ls` will never look.
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
		return nil, false, false, fmt.Errorf("load ticket config: %w", err)
	}
	name, _ := project.ResolveName(cfg, repoDir, "")
	return centralProjectStore(cfg, name)
}

// centralProjectStore opens a resolved project name's store in the central store,
// under the same return contract as CentralStoreForRepo: every resolution that
// ends in a name ends here, so all of them refuse the same project directories.
func centralProjectStore(cfg project.Config, name string) (store *FileStore, unregistered, ok bool, err error) {
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
	// The root the refusal names, and the directory it stats, is the tickets
	// root the project directory hangs off — dir is <root>/<name>.
	root := filepath.Dir(dir)
	if project.CentralRegistered(cfg, name) {
		// Refused here because every caller of this resolution writes straight
		// to the returned FileStore rather than through MultiStore, so without
		// the check a repo argument writes through a project directory that the
		// identical write by project name is refused. A missing directory still
		// resolves.
		if _, err := lstatProjectDir(root, name); err != nil {
			return nil, false, false, err
		}
		return NewProjectFileStore(dir, name), false, true, nil
	}
	// An unregistered project can still hold tickets centrally — written before
	// MultiStore.Create started refusing them, or replicated from another
	// machine. Reporting the repo as having no store would leave those tickets
	// unlisted on disk. Surfacing them is read-only: registering the project is
	// `tk init`'s job, so the caller warns rather than writing config.
	//
	// A directory that exists and is not one refuses here as it does above,
	// rather than reading as "no store": a write through it is the same
	// out-of-store hazard whether or not the project is registered, and `tk
	// init` fixes neither that nor a stat failure. Only a missing directory is
	// no store.
	missing, err := lstatProjectDir(root, name)
	if err != nil {
		return nil, false, false, err
	}
	if missing {
		return nil, false, false, nil
	}
	return NewProjectFileStore(dir, name), true, true, nil
}

// ResolveStoreForNamespace opens the store a namespace selected by name holds
// in the central store, for a caller that named the destination rather than a
// repository: `tk --project <ns>`. No repository is resolved and none has to
// exist. The name has to be one the store knows: Root is always selectable —
// whether a write to it is allowed is the catalog guard's call, which every
// write makes — and any other name has to be registered in config, hold a
// directory under <root>/tickets, or be catalogued; anything else is a name
// that would otherwise conjure a namespace out of a typo. A directory that is
// not one refuses here as CentralStoreForRepo refuses it, since the store this
// returns is written through.
func ResolveStoreForNamespace(ns string) (*FileStore, error) {
	cfg, err := project.Load()
	if err != nil {
		return nil, fmt.Errorf("load ticket config: %w", err)
	}
	dir, err := project.CentralProjectDir(ns)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(dir)
	missing, err := lstatProjectDir(root, ns)
	if err != nil {
		return nil, err
	}
	known := project.IsRoot(ns) || project.CentralRegistered(cfg, ns) || !missing
	if !known {
		cat, err := LoadCatalog(filepath.Dir(root))
		if err != nil {
			return nil, err
		}
		_, known = cat.entry(ns)
	}
	if !known {
		return nil, fmt.Errorf("namespace %q is not in the central store (registered projects, catalogued namespaces and %s are selectable)", ns, project.RootNamespace)
	}
	return NewProjectFileStore(dir, ns), nil
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
// own resolution and `tk move` — says the same sentence, so the condition reads
// as a single state rather than as several.
func UnregisteredWarning(store *FileStore) string {
	return fmt.Sprintf("warning: project %q is not registered but has a ticket directory at %s — run `tk init` to register it", store.Project, store.Dir)
}

// ResolveStoreForRepo opens the store a repo's tickets live in: its project
// directory in the central store, which is the only store tk resolves. It
// reports whether that project is unregistered, because writing into one puts a
// ticket where MultiStore.Create refuses to write and the caller has to say so.
// A repo that resolves to no project is noStoreError; a config that cannot be
// read, or a project directory that is not one, comes back named by
// CentralStoreForRepo instead.
//
// Every CLI lookup of a repo's store and MCP's repo argument resolve here, so a
// directory reaches the same store whichever of them names it. A caller that
// needs only a project name, as `tk serve`'s default project and `tk recompute`
// do, calls project.ResolveName itself, and runCreate's guard sits above this
// function: it refuses a bare `tk create` before any store is resolved.
//
// A .tickets/ in the repo has no say in the answer. Here, only a repo that
// resolves to no project hears about one, through noStoreError. A repo that
// resolves gets no error or warning about it: the directory goes unread and stays
// in place, and `tk init` there imports what the central store lacks. A file whose
// ID the central store already holds is skipped rather than merged, see
// FileStore.ImportAll.
//
// The project name keeps ResolveName's git-remote and basename fallbacks on
// purpose. A second clone or linked worktree outside the checkout the config
// records has only those rules to find its project by, and --repo and MCP's repo
// have to reach the store that a `tk ls` run in that directory reads.
//
// The rules match a name rather than a repository, and that is the accepted cost.
// A path that is no registered repo still resolves to a real project's store if
// its remote or basename names one, and this feeds writes as well as reads: `tk
// --repo <path> create` and ticket_create's repo land there, where a bare `tk
// create` refuses any directory the config does not register. The other way
// round, a linked worktree with no origin remote, in a directory not named for
// its project, resolves to nothing.
//
// `tk move`'s destination differs on purpose and does not read through here: a
// move closes its source, so it takes no guess. See ResolveMoveDestination.
func ResolveStoreForRepo(repoDir string) (*FileStore, bool, error) {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, false, fmt.Errorf("invalid repo path %s: %w", repoDir, err)
	}
	store, unregistered, ok, err := CentralStoreForRepo(abs)
	if err != nil {
		// Returned as it came back: the resolution has already named the
		// condition — an unreadable config, or a project directory that is not
		// one — and either reads wrong as the repo having no store, which sends
		// the user to `tk init` and fixes neither.
		return nil, false, err
	}
	if ok {
		return store, unregistered, nil
	}
	return nil, false, noStoreError(abs)
}

// ResolveMoveDestination is the one resolver of a `tk move` destination. It
// returns the store and whether its project is unregistered, as
// ResolveStoreForRepo does.
//
// The argument's shape decides what it is; no rule falls back to another. With
// no path separator, and not "." or "..", it is a project name and has to be a
// registered one. Anything else is a path: it has to exist, be a directory, and
// be the exact directory a config entry names, symlinks resolved. So a
// subdirectory or linked worktree of a checkout owns nothing, and is refused.
// That refusal names the project rather than pointing at `tk init`: run in a
// linked worktree, init repoints the project's registered path at the worktree,
// and in a subdirectory it either does the same or changes nothing.
//
// ResolveName's remote, basename and prefix rules stay out of this because they
// answer "which project am I probably standing in", a guess ResolveStoreForRepo
// accepts so that a second clone or linked worktree finds its project. A move
// writes to two stores and closes the source: a guess there lands a ticket in a
// project the user did not name, or back in its own, from a path nothing ever
// checked. Which of those rules fired also depended on the working directory,
// so the same argument meant different things in different places.
//
// The cost, taken deliberately: `tk move <id> loom` and a directory ./loom are
// different targets, and the bare form never means the directory. Git draws the
// same line between a branch and a path and errors rather than guessing, so the
// refusal names the ./loom spelling instead of trying it.
//
// The CLI is the only surface that moves: the picker that used to feed this
// paths went with `tk ui`, and MCP has no move tool, so nothing else has to agree.
func ResolveMoveDestination(target string) (*FileStore, bool, error) {
	// Neither shape: as a path "" would mean the working directory, which nobody
	// typed.
	if target == "" {
		return nil, false, fmt.Errorf("move target is empty")
	}
	cfg, err := project.Load()
	if err != nil {
		return nil, false, fmt.Errorf("load ticket config: %w", err)
	}
	// ValidName is exactly the shape test once "" is out: no separator, not "."
	// or "..".
	if project.ValidName(target) {
		// CentralRegistered rather than a config path: a registered project needs
		// no checkout on this machine to receive a ticket.
		if !project.CentralRegistered(cfg, target) {
			return nil, false, fmt.Errorf("%q is not a registered project — a directory of that name is written with a path separator, e.g. ./%s", target, target)
		}
		store, unregistered, ok, err := centralProjectStore(cfg, target)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, fmt.Errorf("no ticket store found for project %q", target)
		}
		return store, unregistered, nil
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, false, fmt.Errorf("invalid repo path %s: %w", target, err)
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return nil, false, fmt.Errorf("no such directory: %s", abs)
	}
	if err != nil {
		return nil, false, fmt.Errorf("destination: %w", err)
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("destination %s is not a directory", abs)
	}
	owners := project.ProjectsAtPath(cfg, abs)
	if len(owners) == 0 {
		if name, ok := project.EnclosingProject(cfg, abs); ok {
			return nil, false, fmt.Errorf("no ticket store found for %s: it is part of the checkout of project %q — name the project instead", abs, name)
		}
		return nil, false, noStoreError(abs)
	}
	if len(owners) > 1 {
		return nil, false, fmt.Errorf("%s is the configured path of more than one project (%s) — name the project instead", abs, strings.Join(owners, ", "))
	}
	store, unregistered, ok, err := centralProjectStore(cfg, owners[0])
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, noStoreError(abs)
	}
	return store, unregistered, nil
}
