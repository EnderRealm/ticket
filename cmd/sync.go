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
// cannot commit or unstage anything outside the store. The remote half cannot
// be scoped — `git push` and `git pull --rebase --autostash` act on the
// repository's current branch and whole worktree, and `-C` only sets the
// working directory. Push is kept for a nested store regardless: it publishes
// commits already on the owner's branch to the owner's own upstream and
// rewrites neither. The rebase pull is refused there instead, at the single
// choke point both callers go through — see pullRebaseAutostash.
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
			msg := fmt.Sprintf("sync blocked: git add %s failed: %v (%s)", path, err, ticket.SanitizeControl(strings.TrimSpace(string(out))))
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
		return fmt.Sprintf("git commit failed: %v (%s)", err, ticket.SanitizeControl(strings.TrimSpace(string(out))))
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
	pushOut, err := exec.Command("git", "-C", storeRoot, "push").CombinedOutput()
	if err == nil {
		clearSyncBlocked(storeRoot)
		return ""
	}

	// Push failed — pull --rebase --autostash and retry, handing down the push's
	// own output. The retry runs on any push failure, not only a rejection, and
	// the pull fixes none of the others — a branch with no upstream configured
	// lands here too — so that output is the only account of why the push
	// failed, and it is composed into the message and the marker there.
	if msg := pullRebaseAutostash(storeRoot, string(pushOut)); msg != "" {
		return msg
	}

	if out, err := exec.Command("git", "-C", storeRoot, "push").CombinedOutput(); err != nil {
		return fmt.Sprintf("git push failed after rebase (%s)", ticket.SanitizeControl(strings.TrimSpace(string(out))))
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
// behind. Returns a warning (and writes the sync-blocked marker) on failure or
// when the rebase pull is gated for a nested store — see pullRebaseAutostash —
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
		msg := fmt.Sprintf("sync blocked: git fetch failed (%s)", ticket.SanitizeControl(strings.TrimSpace(string(out))))
		writeSyncBlocked(storeRoot, msg)
		return msg
	}

	if !behindUpstream(storeRoot) {
		return ""
	}

	return pullRebaseAutostash(storeRoot, "")
}

// pullRebaseAutostash runs the rebase pull both the pre-flight pull and the
// post-rejection retry need, writing the blocked marker and returning a warning
// on failure. It aborts only a rebase it started itself: `git rebase --abort` on
// one sync did not start discards the repo owner's in-progress rebase and every
// commit they made during it, so an unresolvable state counts as pre-existing.
//
// The pull is refused outright when the store root is not the repository's own
// toplevel. `pull --rebase --autostash` acts on the repository's current branch
// and whole worktree, and `-C` only sets the working directory, so for a store
// nested inside a repo tk does not own it stashes that owner's entire
// uncommitted worktree, rebases their current branch onto its upstream and pops
// the stash — on `tk serve`'s unattended five-second loop. That is the only
// genuinely destructive remote operation sync has, and refusing it costs only
// the diverged case: push keeps publishing the store's commits whenever the
// branch is not behind. Continuing with a warning was rejected, since it still
// rewrites someone else's branch unattended; so was refusing the whole remote
// half, which leaves the nested topology init's bootstrapCentralStoreGit
// deliberately supports half-functional.
//
// pushOut is the failed push's raw output when the retry path calls this, and
// empty for the pre-flight pull. It is appended to whatever refusal or failure
// comes out, so one blocked cycle has one message and one marker write.
//
// The pull's own output and the failed push's are both filtered before they
// reach the message, per writeSyncBlocked. Filtered here rather than at the
// call sites, which cannot then forget.
func pullRebaseAutostash(storeRoot, pushOut string) string {
	msg := nestedRebaseRefusal(storeRoot)
	if msg == "" {
		rebasing := rebaseInProgress(storeRoot)
		out, err := exec.Command("git", "-C", storeRoot, "pull", "--rebase", "--autostash").CombinedOutput()
		if err == nil {
			logForeignUnmergedPaths(storeRoot)
			return ""
		}
		aborted := ""
		if !rebasing {
			exec.Command("git", "-C", storeRoot, "rebase", "--abort").Run()
			aborted = "; aborted rebase"
		}
		msg = fmt.Sprintf("sync blocked: git pull --rebase --autostash failed%s (%s)", aborted, ticket.SanitizeControl(strings.TrimSpace(string(out))))
	}
	if cause := strings.TrimSpace(pushOut); cause != "" {
		msg = fmt.Sprintf("%s (git push failed: %s)", msg, ticket.SanitizeControl(cause))
	}
	writeSyncBlocked(storeRoot, msg)
	return msg
}

// syncBlockedRefusePrefix leads every marker the nested-store rebase gate
// writes, and is what syncBlockResolved identifies its own marker by. Not
// full-text equality: the retry path appends the failed push's output to the
// same message, and either half's wording can be edited, both of which would
// switch the hold off with nothing at the call site to show it.
const syncBlockedRefusePrefix = "sync blocked: refusing to rebase and autostash "

// nestedRebaseRefusal returns the blocked message when the store root is not
// the toplevel of the repository it sits in, or "" when the rebase pull may
// run. `git rev-parse --show-toplevel` resolves symlinks and the store root as
// tk computes it may not be, so both sides are canonicalized before comparing —
// otherwise every store under a symlinked home or temp dir reads as nested.
//
// An unanswerable comparison counts as nested: refusing to rebase a repository
// we cannot identify is the safe side, the same unknown-is-not-none split
// repoStateInProgress makes.
func nestedRebaseRefusal(storeRoot string) string {
	unresolved := syncBlockedRefusePrefix + "a repository tk may not own: cannot resolve the repository containing the store"
	top, err := findGitRoot(storeRoot)
	if err != nil {
		return unresolved
	}
	resolvedTop, topErr := filepath.EvalSymlinks(top)
	resolvedStore, storeErr := filepath.EvalSymlinks(storeRoot)
	if topErr != nil || storeErr != nil {
		return unresolved
	}
	if resolvedTop == resolvedStore {
		return ""
	}
	msg := fmt.Sprintf("%s%s, the repository the store is nested inside and tk does not own", syncBlockedRefusePrefix, ticket.SanitizeControl(top))
	// The instruction to reconcile is only true when there is something to
	// reconcile. The post-push-failure retry reaches this for any failed push,
	// a branch with no upstream among them, and a marker sending someone to
	// resolve a divergence that does not exist is worse than one that states
	// the refusal alone — the caller carries the push's own error alongside it.
	if behindUpstream(storeRoot) {
		msg += "; reconcile the divergence there by hand (git pull --rebase) and sync resumes"
	}
	return msg
}

// logForeignUnmergedPaths reports unmerged paths anywhere in the repository
// that fall outside centralStorePaths. `pull --rebase --autostash` exits 0 even
// when the autostash pop conflicts, and the blocking guard is scoped to the
// store's own paths on purpose, so a pop this sync just caused in the repo
// owner's files would otherwise leave no trace anywhere. A log line — it reaches
// the serve log and the CLI's stderr — is the whole signal: the store's own
// syncing carries on, and paths inside centralStorePaths stay
// checkUnmergedPaths' job rather than being reported twice.
//
// The line states what is observed and not what caused it: an unmerged path the
// owner left behind — a stash pop of their own, which leaves no MERGE_HEAD and
// so passes the pre-flight state guard — is indistinguishable here from one
// this pull just made, and telling them apart would need a snapshot taken
// before the pull for a signal that only reports.
func logForeignUnmergedPaths(storeRoot string) {
	// `ls-files -u` with no pathspec lists only what is under the working
	// directory, so `:/` re-roots the pathspec at the repo toplevel. Defensive
	// rather than load-bearing today: this runs only after nestedRebaseRefusal
	// returned "", which means the store root is the toplevel and the two
	// spellings list the same thing. It costs nothing and keeps the scan honest
	// if the gate above it is ever narrowed.
	//
	// The two encoding flags travel together the way checkStagedConflictMarkers
	// pins them, and here the path feeds a comparison rather than only a message:
	// a name that arrives C-quoted fails the `tickets/` prefix test, so a conflict
	// inside the store gets reported as being outside it. `core.quotePath`
	// defaults to true, which quotes and octal-escapes any non-ASCII name — and
	// slugifyTitle keeps Unicode letters, so tk writes such names itself; turning
	// it off still leaves a name holding a control character quoted, which git
	// permits and this store sees, since ticket files arrive over git from other
	// machines. `-z` is what removes the quoting entirely, delimiting with NUL
	// instead, and hands the name over raw for both the comparison and
	// SanitizeControl below.
	out, err := exec.Command("git", "-C", storeRoot, "-c", "core.quotePath=false", "ls-files", "-u", "-z", "--", ":/").Output()
	if err != nil {
		// Fail open: this scan only reports, and the store's own paths are
		// blocked on by checkUnmergedPaths whether or not it runs. Blocking a
		// sync on an advisory scan would trade a missing log line for a store
		// that stops syncing.
		return
	}
	seen := map[string]struct{}{}
	var names []string
	for _, rec := range strings.Split(string(out), "\x00") {
		// Format: <mode> <sha> <stage>\t<path>, one record per stage.
		i := strings.IndexByte(rec, '\t')
		if i < 0 {
			continue
		}
		path := rec[i+1:]
		if _, dup := seen[path]; dup || insideCentralStorePaths(path) {
			continue
		}
		seen[path] = struct{}{}
		// Same forging risk the other guards sanitize against: a path arrives
		// over git from another machine and can otherwise inject whole lines
		// into the serve log.
		names = append(names, ticket.SanitizeControl(path))
	}
	if len(names) == 0 {
		return
	}
	if len(names) > maxForeignUnmergedPaths {
		names = append(names[:maxForeignUnmergedPaths], fmt.Sprintf("+%d more", len(names)-maxForeignUnmergedPaths))
	}
	log.Printf("sync: unmerged paths outside the store after git pull --rebase --autostash (%s); resolve them in the enclosing repository — the store keeps syncing", strings.Join(names, ", "))
}

// maxForeignUnmergedPaths bounds the names one log line carries. The scan is
// repo-wide, unlike the store-scoped checkUnmergedPaths, so a conflict spanning
// a large tree would otherwise put every name in a single record; the tail
// keeps the count honest.
const maxForeignUnmergedPaths = 10

// insideCentralStorePaths reports whether a store-root-relative path is one of
// the tk-managed paths. The directory entry in centralStorePaths carries a
// trailing slash and matches by prefix; a file entry matches exactly.
func insideCentralStorePaths(path string) bool {
	for _, p := range centralStorePaths {
		if strings.HasSuffix(p, "/") {
			if strings.HasPrefix(path, p) {
				return true
			}
		} else if path == p {
			return true
		}
	}
	return false
}

// behindUpstream reports whether the current branch has commits to pull. An
// unanswerable count reads as not behind: the pull it gates is the operation
// that would find out anyway, and no rebase runs on a guess.
func behindUpstream(storeRoot string) bool {
	out, err := exec.Command("git", "-C", storeRoot, "rev-list", "--count", "HEAD..@{u}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0"
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

// writeSyncBlocked records why a cycle stopped, in a file `tk status` prints
// back on every later run and `tk serve` copies into its log.
//
// Every git output a caller interpolates into msg — fetch, add, pull, push —
// has been through ticket.SanitizeControl first, as have the paths the guards
// name. That output carries the remote's own transport and hook text and the
// names of files that arrived over git from other machines, so raw control
// characters would forge status and serve-log lines and drive the terminal long
// after the cycle that wrote them. Each is filtered where the message is built,
// which is also where the same text is returned as the cycle's warning, so the
// two cannot disagree.
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
	if checkUnmergedPaths(storeRoot) != "" {
		return false
	}
	// The marker the nested-store gate wrote stands while the divergence it
	// refuses to rebase away is still there. Without this it is cleared at the
	// top of every cycle and rewritten by the gate moments later, leaving
	// `tk status` reporting ok in the window between. It cannot wedge: the pair
	// tested here is what the gate itself keys on, and once the owner reconciles
	// their branch contains its upstream, so the remote-tracking ref the last
	// fetch left behind already reports not behind.
	//
	// Scoped to that marker by its fixed lead, and only while the gate would
	// still refuse: a marker written for any other cause falls through and is
	// replaced next cycle with whatever reason still applies, so fixing the
	// cause a marker names always changes what the user is told.
	if strings.HasPrefix(readSyncBlocked(storeRoot), syncBlockedRefusePrefix) && nestedRebaseRefusal(storeRoot) != "" {
		return !behindUpstream(storeRoot)
	}
	return true
}
