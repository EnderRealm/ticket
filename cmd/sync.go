package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

const (
	defaultSyncInterval = 5 * time.Second
	syncBlockedFile     = ".tk-sync-blocked"
)

// In-progress git operations sync refuses to run a cycle inside. The values are
// interpolated into the blocked-marker message, so they read as noun phrases.
const (
	stateRebase     = "a rebase"
	stateMerge      = "a merge"
	stateCherryPick = "a cherry-pick"
	stateRevert     = "a revert"
	// The sequencer directory backs the multi-commit forms of both cherry-pick
	// and revert, and telling them apart means parsing sequencer/todo. The
	// per-operation entries are checked first, so this only names the state in
	// the window where the current commit's HEAD file is gone but the sequence
	// has not finished — where either is still true.
	stateSequence = "a cherry-pick or revert sequence"
)

// centralStorePaths are the tk-managed paths inside the central store root: the
// tickets directory and the shared config. They are the only paths init's
// bootstrap and every sync cycle stage. `git -C` sets the working directory but
// not the pathspec, so staging without one sweeps up the entire worktree of a
// repo the store root happens to be nested inside. Both consumers run git with
// `-C <storeRoot>` and scope staging, the staged-changes check and the commit to
// these same relative paths, so neither can commit anything outside the store
// root.
var centralStorePaths = []string{"tickets/", "config.yaml"}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync ticket changes to git (stage, commit, push)",
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("sync"); err != nil {
		return err
	}

	storeRoot, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("cannot resolve central store: %w", err)
	}

	// findGitRoot is only the "is the store inside a git repo at all" gate. Its
	// answer is the enclosing repo's toplevel for a nested store, which is not
	// the directory sync operates on — see syncCentralStore.
	if _, err := findGitRoot(storeRoot); err != nil {
		return fmt.Errorf("central store is not in a git repository: %w", err)
	}

	warning := syncCentralStore(storeRoot)

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]any{
			"synced":  warning == "",
			"warning": warning,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	} else {
		fmt.Println("sync complete")
	}
	return nil
}

// syncLoop runs syncCentralStore on a ticker until ctx is cancelled.
func syncLoop(ctx context.Context, storeRoot string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if warning := syncCentralStore(storeRoot); warning != "" {
				log.Printf("sync: %s", warning)
			}
		}
	}
}

// syncCentralStore performs a single sync cycle: pull, stage, commit, push.
// Returns a warning message on problems, or empty string on success/no-op.
//
// Every git invocation runs with `-C storeRoot`, not with the repo toplevel
// findGitRoot resolves. For a store root nested inside another repo — the
// topology init's bootstrapCentralStoreGit deliberately supports, skipping
// `git init` because the enclosing repo owns history — the toplevel is that
// enclosing repo, whose worktree is not the store's. Refusing to sync there
// would silently stop syncing for stores initialized that way on purpose, so
// sync operates on the store root instead.
//
// Only the index-touching commands are scoped: staging, the staged-changes
// checks, the commit and the reset that follows a refusal all carry
// centralStorePaths, making sync and init identical consumers of it, so sync
// cannot commit or unstage anything outside the store. The remote half is not
// scoped and cannot be — `git push` and `git pull --rebase --autostash` act on
// the repository's current branch and whole worktree, and `-C` only sets the
// working directory. So for a nested store whose push is rejected, sync
// stashes the enclosing repo owner's entire uncommitted worktree, rebases
// their branch onto its upstream and pops the stash. A pop conflict outside
// centralStorePaths is deliberately invisible to sync's guards, which are
// scoped to the store's paths so that a conflict sync neither created nor can
// resolve does not stop the store's own syncing. That unscoped remote half is
// left as it stands here and tracked as ticket/tk-sync-runs-51d4.
func syncCentralStore(storeRoot string) string {
	// Check sync-blocked marker
	if blocked := readSyncBlocked(storeRoot); blocked != "" {
		if syncBlockResolved(storeRoot) {
			clearSyncBlocked(storeRoot)
		} else {
			return blocked
		}
	}

	// Refuse the whole cycle while the repository owning the store is mid
	// operation, before anything touches the index. A pathspec'd commit is a
	// partial commit, which git refuses outright during a merge; and for a
	// nested store the pause is usually outside centralStorePaths, so the
	// scoped guards below see a clean store and sync would commit onto the
	// owner's detached rebase HEAD, then fail to push from it. Git's own
	// refusal covers only some of these states — a conflicted revert is not one
	// of them — so this guard is what catches the rest.
	state, known := repoStateInProgress(storeRoot)
	if !known {
		msg := "sync blocked: cannot resolve the store's git directory; refusing to commit into an unknown repository state"
		writeSyncBlocked(storeRoot, msg)
		return msg
	}
	if state != "" {
		msg := fmt.Sprintf("sync blocked: %s is in progress in the repository containing the store; finish or abort it", state)
		writeSyncBlocked(storeRoot, msg)
		return msg
	}

	// Pull remote changes first so the push branch never starts from a stale
	// base. Runs every cycle regardless of local changes — without this, a
	// machine with no outgoing commits never picks up incoming ones.
	if msg := pullIfBehind(storeRoot); msg != "" {
		return msg
	}

	// Refuse to proceed if the working tree has unmerged paths. `pull --rebase
	// --autostash` exits 0 even when the autostash pop conflicts, so a stash
	// pop conflict on config.yaml would otherwise be staged and committed
	// verbatim with the conflict markers intact.
	if msg := checkUnmergedPaths(storeRoot); msg != "" {
		writeSyncBlocked(storeRoot, msg)
		return msg
	}

	// Stage only tk-managed paths (tickets directory and shared config), and
	// collect the ones that actually have staged changes. `git commit` errors on
	// a pathspec it knows nothing about, while a diff pathspec matching nothing
	// simply reports no changes — the same asymmetry init's bootstrap handles.
	var changed []string
	for _, path := range centralStorePaths {
		// `git add` on a pathspec that matches nothing is a hard error, and a
		// store with no tickets yet — or whose shared config was deleted — has
		// nothing at that path. Only absence is skippable: a swallowed add
		// failure, an enclosing `.gitignore` covering the store root among
		// them, leaves sync finding nothing staged and returning the no-op
		// forever with nothing in the log. Both failures write the blocked
		// marker like every other terminal refusal here — the gitignored case is
		// permanent rather than transient, and without a marker `tk status`
		// reports `ok` forever while every cycle fails.
		if _, err := os.Stat(filepath.Join(storeRoot, path)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			msg := fmt.Sprintf("sync blocked: central store stat %s failed: %v", path, err)
			writeSyncBlocked(storeRoot, msg)
			return msg
		}
		if out, err := exec.Command("git", "-C", storeRoot, "add", "--", path).CombinedOutput(); err != nil {
			msg := fmt.Sprintf("sync blocked: git add %s failed: %v (%s)", path, err, strings.TrimSpace(string(out)))
			writeSyncBlocked(storeRoot, msg)
			return msg
		}
		diff := exec.Command("git", "-C", storeRoot, "diff", "--cached", "--quiet", "--", path)
		if err := diff.Run(); err != nil {
			changed = append(changed, path)
		}
	}
	if len(changed) == 0 {
		return "" // nothing to commit
	}

	// Belt-and-braces: scan staged blobs for unresolved conflict markers
	// before committing. Catches any path we might add that slipped through
	// the unmerged-paths check.
	//
	// The scan reads the index (`git show :<name>`) while the commit below is a
	// partial commit, which builds its tree from HEAD plus the *working tree*
	// contents of the named paths, not those index entries. The add immediately
	// above makes the two agree in practice, but a ticket rewritten in between —
	// plausible, since `tk serve` writes tickets in the same process as this
	// loop — is committed without having been scanned. The guard is a net, not a
	// proof.
	if msg := checkStagedConflictMarkers(storeRoot); msg != "" {
		// Scoped like everything else here: a bare `git reset` would unstage the
		// enclosing repo owner's work along with ours.
		resetArgs := append([]string{"-C", storeRoot, "reset", "--"}, centralStorePaths...)
		exec.Command("git", resetArgs...).Run()
		writeSyncBlocked(storeRoot, msg)
		return msg
	}

	// Commit
	msg := "tk: sync tickets"
	commitArgs := append([]string{"-C", storeRoot, "commit", "-m", msg, "--"}, changed...)
	if out, err := exec.Command("git", commitArgs...).CombinedOutput(); err != nil {
		return fmt.Sprintf("git commit failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Check for remote
	remotes, err := gitRemoteNames(storeRoot)
	if err != nil || len(remotes) == 0 {
		return "" // no remote, commit-only mode
	}

	// Check for unpushed commits
	if !hasUnpushedCommits(storeRoot) {
		return ""
	}

	// Push
	if out, err := exec.Command("git", "-C", storeRoot, "push").CombinedOutput(); err == nil {
		clearSyncBlocked(storeRoot)
		return ""
	} else {
		_ = out
	}

	// Push failed — pull --rebase --autostash and retry.
	if msg := pullRebaseAutostash(storeRoot); msg != "" {
		return msg
	}

	if out, err := exec.Command("git", "-C", storeRoot, "push").CombinedOutput(); err != nil {
		return fmt.Sprintf("git push failed after rebase (%s)", strings.TrimSpace(string(out)))
	}

	clearSyncBlocked(storeRoot)
	return ""
}

// checkUnmergedPaths returns a non-empty warning when the store's paths have
// unmerged entries. Used as a guard before staging — a non-empty result means
// a previous merge or stash pop left conflicts in the index. Scoped to
// centralStorePaths so a conflict elsewhere in an enclosing repo, which sync
// neither created nor can resolve, does not block the store's own syncing.
// `ls-files` reports paths relative to its working directory.
func checkUnmergedPaths(storeRoot string) string {
	args := append([]string{"-C", storeRoot, "ls-files", "-u", "--"}, centralStorePaths...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return ""
	}
	paths := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// Format: <mode> <sha> <stage>\t<path>
		if i := strings.IndexByte(line, '\t'); i >= 0 {
			paths[line[i+1:]] = struct{}{}
		}
	}
	names := make([]string, 0, len(paths))
	for p := range paths {
		// Belt-and-braces against the same forging the conflict-marker scan
		// guards: `ls-files` C-quotes a path holding control characters, so
		// nothing raw reaches here today, but that is git's choice of output
		// format rather than a guarantee this message can rely on.
		names = append(names, ticket.SanitizeControl(p))
	}
	return fmt.Sprintf("sync blocked: unmerged paths in working tree (%s); resolve manually", strings.Join(names, ", "))
}

// checkStagedConflictMarkers scans the store's staged blobs for git merge
// conflict marker lines. Returns a non-empty warning when any are found.
//
// The names must feed straight into `git show :<path>`, which resolves index
// paths from the repo toplevel and rejects a working-directory-relative one, so
// the scan pins the two settings that decide how `diff --cached --name-only`
// prints them rather than inheriting the user's config. `core.quotePath`
// defaults to true, which octal-escapes and quotes any non-ASCII name — and
// slugifyTitle keeps Unicode letters, so tk itself writes such names; a
// user-set `diff.relative` would report store-relative names instead of
// toplevel-relative ones. Either way every `git show` would miss, and a staged
// ticket carrying conflict markers would be committed and pushed. `-z` removes
// the last encoding variable by delimiting with NUL. Pinned that way the names
// are toplevel-relative and verbatim whatever the user's config says, so they
// always feed straight through.
//
// A failing invocation blocks rather than reporting no markers. `--no-relative`
// needs git 2.28+, so on an older git the guard would otherwise vanish silently
// every cycle and let conflict-markered content that arrived over git from
// another machine be pushed on to every other machine. Same unknown-is-not-none
// split repoStateInProgress makes.
func checkStagedConflictMarkers(storeRoot string) string {
	args := append([]string{"-C", storeRoot, "-c", "core.quotePath=false", "diff", "--cached", "--name-only", "--no-relative", "-z", "--"}, centralStorePaths...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return fmt.Sprintf("sync blocked: cannot list the store's staged files to scan for merge conflict markers: %v", err)
	}
	for _, name := range strings.Split(string(out), "\x00") {
		if name == "" {
			continue
		}
		blob, err := exec.Command("git", "-C", storeRoot, "show", ":"+name).Output()
		if err != nil {
			continue
		}
		if hasConflictMarker(blob) {
			// `-z` hands the name over raw, and git permits newlines and other
			// control characters in a path. A ticket filename arrives over git
			// from other machines, so without this it can forge lines in a
			// message `tk status` prints and `tk serve` writes to its log.
			return fmt.Sprintf("sync blocked: staged %s contains merge conflict markers; resolve manually", ticket.SanitizeControl(name))
		}
	}
	return ""
}

func hasConflictMarker(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") ||
			strings.HasPrefix(line, ">>>>>>> ") ||
			line == "=======" {
			return true
		}
	}
	return false
}

// pullIfBehind fetches origin and rebases local commits onto upstream when
// behind. Returns a warning (and writes the sync-blocked marker) on failure,
// or empty string on success / no remote / no upstream / already up to date.
func pullIfBehind(storeRoot string) string {
	remotes, err := gitRemoteNames(storeRoot)
	if err != nil || len(remotes) == 0 {
		return ""
	}

	// Skip when no upstream is configured for the current branch.
	if err := exec.Command("git", "-C", storeRoot, "rev-parse", "--abbrev-ref", "@{u}").Run(); err != nil {
		return ""
	}

	if out, err := exec.Command("git", "-C", storeRoot, "fetch").CombinedOutput(); err != nil {
		msg := fmt.Sprintf("sync blocked: git fetch failed (%s)", strings.TrimSpace(string(out)))
		writeSyncBlocked(storeRoot, msg)
		return msg
	}

	behindOut, err := exec.Command("git", "-C", storeRoot, "rev-list", "--count", "HEAD..@{u}").Output()
	if err != nil || strings.TrimSpace(string(behindOut)) == "0" {
		return ""
	}

	return pullRebaseAutostash(storeRoot)
}

// pullRebaseAutostash runs the rebase pull both the pre-flight pull and the
// post-rejection retry need, writing the blocked marker and returning a warning
// on failure. It aborts only a rebase it started itself: `git rebase --abort` on
// one sync did not start discards the repo owner's in-progress rebase and every
// commit they made during it, so an unresolvable state counts as pre-existing.
//
// This is the unscoped half of sync — `pull --rebase --autostash` acts on the
// repository's current branch and whole worktree, and `-C` only sets the working
// directory, so for a nested store it stashes, rebases and pops the enclosing
// repo owner's work. Deliberately out of scope here; tracked as
// ticket/tk-sync-runs-51d4.
func pullRebaseAutostash(storeRoot string) string {
	rebasing := rebaseInProgress(storeRoot)
	out, err := exec.Command("git", "-C", storeRoot, "pull", "--rebase", "--autostash").CombinedOutput()
	if err == nil {
		return ""
	}
	aborted := ""
	if !rebasing {
		exec.Command("git", "-C", storeRoot, "rebase", "--abort").Run()
		aborted = "; aborted rebase"
	}
	msg := fmt.Sprintf("sync blocked: git pull --rebase --autostash failed%s (%s)", aborted, strings.TrimSpace(string(out)))
	writeSyncBlocked(storeRoot, msg)
	return msg
}

// repoStateInProgress names the git operation underway in the repository that
// owns storeRoot, or "" when there is none. The second return is false when git
// cannot say where its git dir is: unknown is not the same as none, and callers
// treat it as blocking.
//
// Ask git rather than assuming <storeRoot>/.git — a nested store has none of
// its own, and joining a path that cannot exist reports every rebase as
// finished.
//
// The list must cover every pause, not just the ones git itself refuses a
// partial commit during: determine_whence keys only on MERGE_HEAD,
// CHERRY_PICK_HEAD and the rebase directories, so a conflicted `git revert`
// leaves unmerged paths that sync's scoped guards cannot see and a
// `git commit -- <paths>` that succeeds anyway, committing onto a mid-revert
// index.
func repoStateInProgress(storeRoot string) (string, bool) {
	out, err := exec.Command("git", "-C", storeRoot, "rev-parse", "--absolute-git-dir").Output()
	if err != nil {
		return "", false
	}
	gitDir := strings.TrimSpace(string(out))

	for _, s := range []struct{ entry, state string }{
		{"rebase-merge", stateRebase},
		{"rebase-apply", stateRebase},
		{"MERGE_HEAD", stateMerge},
		{"CHERRY_PICK_HEAD", stateCherryPick},
		{"REVERT_HEAD", stateRevert},
		{"sequencer", stateSequence},
	} {
		if _, err := os.Stat(filepath.Join(gitDir, s.entry)); err == nil {
			return s.state, true
		}
	}
	return "", true
}

// rebaseInProgress reports whether a rebase is already underway before sync
// runs a `pull --rebase` of its own. `git rebase --abort` on a rebase sync did
// not start discards the repo owner's in-progress rebase and every commit they
// made during it, so an unresolvable state counts as one: a skipped abort
// leaves a rebase for someone to finish, an unwarranted one destroys work.
func rebaseInProgress(storeRoot string) bool {
	state, known := repoStateInProgress(storeRoot)
	return !known || state == stateRebase
}

// findGitRoot walks up from path to find the directory containing .git.
func findGitRoot(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}

// syncInterval returns the configured interval or the default.
func syncInterval() time.Duration {
	cfg, err := project.Load()
	if err != nil {
		return defaultSyncInterval
	}
	if cfg.SyncInterval == "" {
		return defaultSyncInterval
	}
	d, err := time.ParseDuration(cfg.SyncInterval)
	if err != nil || d <= 0 {
		return defaultSyncInterval
	}
	return d
}

func gitRemoteNames(storeRoot string) ([]string, error) {
	out, err := exec.Command("git", "-C", storeRoot, "remote").Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func hasUnpushedCommits(storeRoot string) bool {
	out, err := exec.Command("git", "-C", storeRoot, "rev-list", "--count", "@{u}..HEAD").CombinedOutput()
	if err != nil {
		// No upstream — treat as unpushed
		return true
	}
	return strings.TrimSpace(string(out)) != "0"
}

func readSyncBlocked(storeRoot string) string {
	data, err := os.ReadFile(filepath.Join(storeRoot, syncBlockedFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeSyncBlocked(storeRoot, msg string) {
	os.WriteFile(filepath.Join(storeRoot, syncBlockedFile), []byte(msg+"\n"), 0o644)
}

func clearSyncBlocked(storeRoot string) {
	os.Remove(filepath.Join(storeRoot, syncBlockedFile))
}

// syncBlockResolved checks if the conflict that caused the block is gone.
func syncBlockResolved(storeRoot string) bool {
	// Unresolved while the repository is still mid rebase, merge, cherry-pick or
	// revert, and while git cannot say which — same detection the pre-flight
	// guard runs, so the two cannot disagree about the state.
	if state, known := repoStateInProgress(storeRoot); !known || state != "" {
		return false
	}
	// Also unresolved when the working tree still has unmerged paths (e.g.
	// after an autostash pop conflict, where no rebase dir exists).
	return checkUnmergedPaths(storeRoot) == ""
}
