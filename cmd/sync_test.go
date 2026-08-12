package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "test")
	// Initial commit so HEAD exists
	os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func TestSyncCommit(t *testing.T) {
	dir := setupGitRepo(t)

	// Create a ticket file under tickets/
	os.MkdirAll(filepath.Join(dir, "tickets", "proj"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "proj", "test-ticket.md"), []byte("---\ntitle: Test\n---\n"), 0o644)

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify commit was created
	out, _ := execCommand("git", "-C", dir, "log", "--oneline", "-1")
	if out == "" {
		t.Fatal("expected a commit")
	}
	if !contains(out, "tk: sync tickets") {
		t.Errorf("commit message = %q, want 'tk: sync tickets'", out)
	}
}

func TestSyncNoOp(t *testing.T) {
	dir := setupGitRepo(t)

	// Count commits before
	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Count commits after — should be the same
	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit count changed from %s to %s on no-op sync", before, after)
	}
}

func TestSyncPush(t *testing.T) {
	// Set up a bare remote and a working repo
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	dir := setupGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-u", "origin", "HEAD")

	// Create a file and sync
	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "pushed-ticket.md"), []byte("---\ntitle: Pushed\n---\n"), 0o644)

	warning := syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify the commit was pushed by checking the bare repo
	out, _ := execCommand("git", "-C", bare, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("bare repo HEAD = %q, expected sync commit", out)
	}
}

func TestSyncPushConflict(t *testing.T) {
	// Set up bare remote
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	// Clone A (our sync repo)
	repoA := setupGitRepo(t)
	runGit(t, repoA, "remote", "add", "origin", bare)
	runGit(t, repoA, "push", "-u", "origin", "HEAD")

	// Clone B (simulates another writer)
	repoB := t.TempDir()
	exec.Command("git", "clone", bare, repoB).Run()
	exec.Command("git", "-C", repoB, "config", "user.email", "other@test.com").Run()
	exec.Command("git", "-C", repoB, "config", "user.name", "other").Run()
	os.WriteFile(filepath.Join(repoB, "conflict.md"), []byte("from B"), 0o644)
	exec.Command("git", "-C", repoB, "add", "-A").Run()
	exec.Command("git", "-C", repoB, "commit", "-m", "from B").Run()
	exec.Command("git", "-C", repoB, "push").Run()

	// Now sync from A — push should fail, pull --rebase should succeed
	os.MkdirAll(filepath.Join(repoA, "tickets"), 0o755)
	os.WriteFile(filepath.Join(repoA, "tickets", "local-ticket.md"), []byte("from A"), 0o644)

	warning := syncCentralStore(repoA)
	// Should succeed after rebase (no actual conflict in file content)
	if warning != "" {
		t.Logf("warning (may be expected): %s", warning)
	}

	// Verify both commits are in the bare repo
	out, _ := execCommand("git", "-C", bare, "log", "--oneline")
	if !contains(out, "from B") {
		t.Error("bare repo missing commit from B")
	}
	if !contains(out, "tk: sync tickets") {
		t.Error("bare repo missing sync commit")
	}
}

func TestSyncParentRepo(t *testing.T) {
	// Create a parent repo with a tickets subdirectory
	parent := setupGitRepo(t)
	ticketsDir := filepath.Join(parent, "tickets", "myproject")
	os.MkdirAll(ticketsDir, 0o755)

	// Write a ticket in the subdirectory
	os.WriteFile(filepath.Join(ticketsDir, "sub-ticket.md"), []byte("---\ntitle: Sub\n---\n"), 0o644)

	// findGitRoot from the tickets subdir should return the parent
	gitRoot, err := findGitRoot(ticketsDir)
	if err != nil {
		t.Fatalf("findGitRoot: %v", err)
	}
	if gitRoot != parent {
		// On macOS, paths may differ due to /private prefix
		evalParent, _ := filepath.EvalSymlinks(parent)
		if gitRoot != evalParent {
			t.Errorf("findGitRoot = %q, want %q", gitRoot, parent)
		}
	}

	// The store root here is the repo root, so syncing it is the same directory
	// findGitRoot resolved above; syncCentralStore now takes the store root, not
	// that resolved toplevel.
	warning := syncCentralStore(parent)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	// Verify the ticket file was committed
	out, _ := execCommand("git", "-C", gitRoot, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("commit message = %q, want sync commit", out)
	}
}

// nestedStore builds a store root inside an enclosing repo that has a bare
// remote, and returns the enclosing repo's root, the store root and the bare
// remote. The enclosing repo carries work in flight the sync must not touch: a
// staged file, a modified tracked file and an untracked file.
func nestedStore(t *testing.T) (parent, storeRoot, bare string) {
	t.Helper()
	bare = t.TempDir()
	runGit(t, bare, "init", "--bare")

	parent = setupGitRepo(t)
	runGit(t, parent, "remote", "add", "origin", bare)
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("v1\n"), 0o644)
	runGit(t, parent, "add", "-A")
	runGit(t, parent, "commit", "-m", "add tracked")
	runGit(t, parent, "push", "-u", "origin", "HEAD")

	storeRoot = filepath.Join(parent, "store")
	os.MkdirAll(filepath.Join(storeRoot, "tickets", "proj"), 0o755)

	os.WriteFile(filepath.Join(parent, "staged.txt"), []byte("staged\n"), 0o644)
	runGit(t, parent, "add", "--", "staged.txt")
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("v2\n"), 0o644)
	os.WriteFile(filepath.Join(parent, "untracked.txt"), []byte("untracked\n"), 0o644)
	return parent, storeRoot, bare
}

func TestSyncNestedStoreCommitsAndPushesOnlyStorePaths(t *testing.T) {
	parent, storeRoot, bare := nestedStore(t)

	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)
	os.WriteFile(filepath.Join(storeRoot, "config.yaml"), []byte("projects: {}\n"), 0o644)

	warning := syncCentralStore(storeRoot)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	log, _ := execCommand("git", "-C", parent, "log", "--oneline", "-1")
	if !contains(log, "tk: sync tickets") {
		t.Fatalf("HEAD = %q, want sync commit", log)
	}

	// The commit carries the store's paths and nothing else.
	committed, _ := execCommand("git", "-C", parent, "show", "--pretty=format:", "--name-only", "HEAD")
	for _, want := range []string{"store/config.yaml", "store/tickets/proj/nested.md"} {
		if !contains(committed, want) {
			t.Errorf("sync commit missing %s, got %q", want, committed)
		}
	}
	for _, unwanted := range []string{"staged.txt", "tracked.txt", "untracked.txt"} {
		if contains(committed, unwanted) {
			t.Errorf("sync commit swept up %s: %q", unwanted, committed)
		}
	}

	// A pathspec'd commit leaves the rest of the index alone.
	staged, _ := execCommand("git", "-C", parent, "diff", "--cached", "--name-only")
	if !contains(staged, "staged.txt") {
		t.Errorf("staged.txt no longer staged after sync, index = %q", staged)
	}
	modified, _ := execCommand("git", "-C", parent, "diff", "--name-only")
	if !contains(modified, "tracked.txt") {
		t.Errorf("tracked.txt no longer modified after sync, worktree = %q", modified)
	}
	untracked, _ := execCommand("git", "-C", parent, "ls-files", "--others", "--exclude-standard")
	if !contains(untracked, "untracked.txt") {
		t.Errorf("untracked.txt no longer untracked after sync, got %q", untracked)
	}

	// The push path is exercised: assert what actually landed in the remote.
	pushed, _ := execCommand("git", "-C", bare, "show", "--pretty=format:", "--name-only", "HEAD")
	if !contains(pushed, "store/tickets/proj/nested.md") {
		t.Errorf("bare repo HEAD missing the store's ticket, got %q", pushed)
	}
	tree, _ := execCommand("git", "-C", bare, "ls-tree", "-r", "--name-only", "HEAD")
	for _, unwanted := range []string{"staged.txt", "untracked.txt"} {
		if contains(tree, unwanted) {
			t.Errorf("bare repo contains the enclosing repo's %s: %q", unwanted, tree)
		}
	}
	blob, _ := execCommand("git", "-C", bare, "show", "HEAD:tracked.txt")
	if trimNL(blob) != "v1" {
		t.Errorf("bare repo tracked.txt = %q, want the unmodified v1", blob)
	}
}

func TestSyncNestedStoreDetectsConflictMarkers(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	// `diff.relative` is the user's to set, and under it the staged names come
	// back store-relative rather than toplevel-relative, which is not what
	// `git show :<path>` resolves — the scan must pin it off.
	runGit(t, parent, "config", "diff.relative", "true")

	corrupt := []byte(`projects:
    loom:
        store: central
<<<<<<< Updated upstream
        registered_at: "2026-04-25T20:38:12Z"
=======
        registered_at: "2026-04-25T20:38:35Z"
>>>>>>> Stashed changes
`)
	os.WriteFile(filepath.Join(storeRoot, "config.yaml"), corrupt, 0o644)

	before, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(storeRoot)
	if !contains(warning, "sync blocked") || !contains(warning, "conflict marker") {
		t.Fatalf("expected conflict-marker block, got %q", warning)
	}

	after, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created with conflict markers (before=%s after=%s)", before, after)
	}

	// The marker is a machine-local tk artifact and belongs in the store root,
	// not in the enclosing repo's worktree root.
	if blocked := readSyncBlocked(storeRoot); blocked == "" {
		t.Error("expected .tk-sync-blocked marker in the store root")
	}
	if blocked := readSyncBlocked(parent); blocked != "" {
		t.Errorf("marker written to the enclosing repo root: %q", blocked)
	}

	staged, _ := execCommand("git", "-C", parent, "diff", "--cached", "--name-only")
	if contains(staged, "store/config.yaml") {
		t.Errorf("store config.yaml left staged after refusal: %q", staged)
	}
	if !contains(staged, "staged.txt") {
		t.Errorf("refusal unstaged the enclosing repo's work, index = %q", staged)
	}
}

func TestSyncNestedStoreRespectsRebaseInProgress(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)
	writeSyncBlocked(storeRoot, "sync blocked: test conflict")
	// The rebase state lives in the enclosing repo's git dir — the store root
	// has no .git of its own.
	os.MkdirAll(filepath.Join(parent, ".git", "rebase-merge"), 0o755)

	before, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(storeRoot)
	if !contains(warning, "sync blocked") {
		t.Fatalf("expected sync to stay blocked mid-rebase, got %q", warning)
	}

	after, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created mid-rebase (before=%s after=%s)", before, after)
	}
}

// pauseRebase leaves repo mid-rebase the way `edit` or `break` does: detached
// HEAD, a rebase-merge directory, a clean working tree and no unmerged paths.
// `--exec false` gets there without an interactive editor.
func pauseRebase(t *testing.T, repo string) {
	t.Helper()
	if err := exec.Command("git", "-C", repo, "rebase", "--exec", "false", "HEAD~1").Run(); err == nil {
		t.Fatal("git rebase --exec false: expected the exec to fail and stop the rebase")
	}
	gitDir, _ := execCommand("git", "-C", repo, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(trimNL(gitDir), "rebase-merge")); err != nil {
		t.Fatalf("expected a paused rebase: %v", err)
	}
}

func TestSyncRefusesWhileEnclosingRepoMidRebase(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	// Start from a clean enclosing worktree — rebase refuses otherwise.
	runGit(t, parent, "reset", "--hard", "HEAD")
	os.Remove(filepath.Join(parent, "untracked.txt"))
	pauseRebase(t, parent)

	// Work the owner does during the pause, which `git rebase --abort` throws
	// away along with the rebase itself.
	os.WriteFile(filepath.Join(parent, "owner-work.txt"), []byte("in flight\n"), 0o644)
	runGit(t, parent, "add", "--", "owner-work.txt")
	runGit(t, parent, "commit", "-m", "owner work during rebase")
	ownerHead, _ := execCommand("git", "-C", parent, "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)

	warning := syncCentralStore(storeRoot)
	if !contains(warning, "sync blocked") || !contains(warning, "a rebase is in progress") {
		t.Fatalf("expected a rebase block, got %q", warning)
	}

	gitDir, _ := execCommand("git", "-C", storeRoot, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(trimNL(gitDir), "rebase-merge")); err != nil {
		t.Errorf("sync destroyed the enclosing repo's in-progress rebase: %v", err)
	}
	if head, _ := execCommand("git", "-C", parent, "rev-parse", "HEAD"); trimNL(head) != trimNL(ownerHead) {
		t.Errorf("HEAD = %q, want the owner's in-rebase commit %q", trimNL(head), trimNL(ownerHead))
	}
	log, _ := execCommand("git", "-C", parent, "log", "--oneline", "-1")
	if contains(log, "tk: sync tickets") {
		t.Errorf("sync committed onto the rebase's detached HEAD: %q", log)
	}
	if blocked := readSyncBlocked(storeRoot); blocked == "" {
		t.Error("expected .tk-sync-blocked marker in the store root")
	}
}

func TestSyncRefusesWhileEnclosingRepoMidMerge(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	runGit(t, parent, "reset", "--hard", "HEAD")
	os.Remove(filepath.Join(parent, "untracked.txt"))
	runGit(t, parent, "checkout", "-b", "side")
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("side\n"), 0o644)
	runGit(t, parent, "commit", "-am", "side change")
	runGit(t, parent, "checkout", "-")
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("main\n"), 0o644)
	runGit(t, parent, "commit", "-am", "main change")
	if err := exec.Command("git", "-C", parent, "merge", "side").Run(); err == nil {
		t.Fatal("expected the merge to conflict")
	}

	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)

	// The conflict is outside centralStorePaths, so the scoped unmerged-paths
	// check does not see it; without the pre-flight guard sync reached the
	// commit and got git's raw "cannot do a partial commit during a merge"
	// every cycle, with no marker.
	warning := syncCentralStore(storeRoot)
	if !contains(warning, "sync blocked") || !contains(warning, "a merge is in progress") {
		t.Fatalf("expected a merge block, got %q", warning)
	}
	if blocked := readSyncBlocked(storeRoot); blocked == "" {
		t.Error("expected .tk-sync-blocked marker in the store root")
	}
}

func TestSyncRefusesWhileEnclosingRepoMidRevert(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	runGit(t, parent, "reset", "--hard", "HEAD")
	os.Remove(filepath.Join(parent, "untracked.txt"))
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("v2\n"), 0o644)
	runGit(t, parent, "commit", "-am", "v2")
	os.WriteFile(filepath.Join(parent, "tracked.txt"), []byte("v3\n"), 0o644)
	runGit(t, parent, "commit", "-am", "v3")
	if err := exec.Command("git", "-C", parent, "revert", "--no-edit", "HEAD~1").Run(); err == nil {
		t.Fatal("expected the revert to conflict")
	}

	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)

	before, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")

	// Unlike a merge, git does not refuse a partial commit during a revert —
	// determine_whence keys only on MERGE_HEAD, CHERRY_PICK_HEAD and the rebase
	// directories — and the conflict is outside centralStorePaths, so the scoped
	// unmerged-paths check cannot see it either. Only the pre-flight guard stops
	// sync committing onto the mid-revert index.
	warning := syncCentralStore(storeRoot)
	if !contains(warning, "sync blocked") || !contains(warning, "a revert is in progress") {
		t.Fatalf("expected a revert block, got %q", warning)
	}
	after, _ := execCommand("git", "-C", parent, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created mid-revert (before=%s after=%s)", before, after)
	}
	if blocked := readSyncBlocked(storeRoot); blocked == "" {
		t.Error("expected .tk-sync-blocked marker in the store root")
	}
}

func TestRepoStateInProgressSequencer(t *testing.T) {
	dir := setupGitRepo(t)

	// The sequencer directory is what a multi-commit `git cherry-pick <range>`
	// or `git revert <range>` leaves behind, and it outlives the per-commit
	// CHERRY_PICK_HEAD / REVERT_HEAD within a sequence.
	os.MkdirAll(filepath.Join(dir, ".git", "sequencer"), 0o755)

	state, known := repoStateInProgress(dir)
	if !known || state != stateSequence {
		t.Errorf("repoStateInProgress = (%q, %v), want (%q, true)", state, known, stateSequence)
	}
}

func TestSyncBlocksWhenConflictMarkerScanCannotRun(t *testing.T) {
	dir := setupGitRepo(t)

	os.MkdirAll(filepath.Join(dir, "tickets", "proj"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "proj", "clean.md"), []byte("---\ntitle: Clean\n---\n"), 0o644)

	// Break the scan's own `git diff` while everything around it still works —
	// the shape of running against a git older than the 2.28 that introduced
	// `--no-relative`. Failing open would drop the guard silently every cycle
	// and push conflict-markered content on to every other machine.
	runGit(t, dir, "config", "diff.orderfile", filepath.Join(dir, "no-such-orderfile"))

	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") || !contains(warning, "cannot list") {
		t.Fatalf("expected the unrunnable scan to block, got %q", warning)
	}

	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created with the marker scan unrunnable (before=%s after=%s)", before, after)
	}
	if blocked := readSyncBlocked(dir); blocked == "" {
		t.Error("expected .tk-sync-blocked marker to be written")
	}
}

func TestSyncSanitizesTicketFilenameInWarning(t *testing.T) {
	dir := setupGitRepo(t)

	// git permits newlines in a path and `-z` hands the name over raw, so a
	// ticket file pushed from another machine can forge lines in the message
	// `tk status` prints and `tk serve` writes to its log.
	name := "ticket-\nsync status:   ok\n-7cd2.md"
	os.MkdirAll(filepath.Join(dir, "tickets", "proj"), 0o755)
	corrupt := []byte("---\ntitle: Forged\n---\n<<<<<<< Updated upstream\nours\n=======\ntheirs\n>>>>>>> Stashed changes\n")
	os.WriteFile(filepath.Join(dir, "tickets", "proj", name), corrupt, 0o644)

	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") || !contains(warning, "conflict marker") {
		t.Fatalf("expected conflict-marker block, got %q", warning)
	}
	if strings.ContainsAny(warning, "\n\r") {
		t.Errorf("warning carries the filename's line breaks verbatim: %q", warning)
	}
	if !contains(warning, "�") {
		t.Errorf("warning does not replace the filename's control characters: %q", warning)
	}
	// The marker is what `tk status` reads back, so it must be sanitized too.
	if blocked := readSyncBlocked(dir); strings.ContainsAny(blocked, "\n\r") {
		t.Errorf("blocked marker carries the filename's line breaks: %q", blocked)
	}
}

func TestRebaseInProgress(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "second.txt"), []byte("second\n"), 0o644)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "second")

	if rebaseInProgress(dir) {
		t.Error("clean repo reported as mid-rebase")
	}

	pauseRebase(t, dir)
	if !rebaseInProgress(dir) {
		t.Error("paused rebase not reported")
	}

	// Unknown state counts as in progress: an abort that should not have run
	// destroys work, one that is skipped leaves a rebase to finish.
	if !rebaseInProgress(t.TempDir()) {
		t.Error("a directory git cannot answer for should count as mid-rebase")
	}
}

func TestSyncDetectsConflictMarkersInNonASCIIFilename(t *testing.T) {
	dir := setupGitRepo(t)

	// slugifyTitle keeps Unicode letters, so tk writes such names itself. Under
	// git's default core.quotePath the staged name comes back octal-escaped and
	// quoted, which no `git show` resolves.
	name := "ticket-über-größe-7cd2.md"
	os.MkdirAll(filepath.Join(dir, "tickets", "proj"), 0o755)
	corrupt := []byte("---\ntitle: Über\n---\n<<<<<<< Updated upstream\nours\n=======\ntheirs\n>>>>>>> Stashed changes\n")
	os.WriteFile(filepath.Join(dir, "tickets", "proj", name), corrupt, 0o644)

	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") || !contains(warning, "conflict marker") {
		t.Fatalf("expected conflict-marker block, got %q", warning)
	}
	if !contains(warning, name) {
		t.Errorf("warning does not name the ticket: %q", warning)
	}

	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created with conflict markers (before=%s after=%s)", before, after)
	}
}

func TestSyncWarnsWhenStoreIsGitignored(t *testing.T) {
	parent, storeRoot, _ := nestedStore(t)

	// The store root ignored by the enclosing repo: `git add -- tickets/` exits
	// non-zero, and a swallowed failure leaves sync returning the no-op forever.
	os.WriteFile(filepath.Join(parent, ".gitignore"), []byte("store/\n"), 0o644)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "proj", "nested.md"), []byte("---\ntitle: Nested\n---\n"), 0o644)

	warning := syncCentralStore(storeRoot)
	if !contains(warning, "git add tickets/ failed") {
		t.Fatalf("expected the add failure to surface, got %q", warning)
	}
	// Match init's test and assert on the word rather than git's full English
	// sentence, which wording changes and a translated locale both break.
	if !contains(warning, "ignored") {
		t.Errorf("warning does not carry git's message: %q", warning)
	}
	// A gitignored store root is permanent, not transient: without the marker
	// `tk status` reports ok forever while every cycle fails.
	if blocked := readSyncBlocked(storeRoot); blocked == "" {
		t.Error("expected .tk-sync-blocked marker after the add failure")
	}
}

func TestSyncAutoPull(t *testing.T) {
	// Bare remote
	bare := t.TempDir()
	runGit(t, bare, "init", "--bare")

	// Repo A — the syncing repo, no local changes
	repoA := setupGitRepo(t)
	runGit(t, repoA, "remote", "add", "origin", bare)
	runGit(t, repoA, "push", "-u", "origin", "HEAD")

	// Repo B — another writer pushes a commit
	repoB := t.TempDir()
	exec.Command("git", "clone", bare, repoB).Run()
	exec.Command("git", "-C", repoB, "config", "user.email", "other@test.com").Run()
	exec.Command("git", "-C", repoB, "config", "user.name", "other").Run()
	os.MkdirAll(filepath.Join(repoB, "tickets"), 0o755)
	os.WriteFile(filepath.Join(repoB, "tickets", "from-b.md"), []byte("---\ntitle: From B\n---\n"), 0o644)
	exec.Command("git", "-C", repoB, "add", "-A").Run()
	exec.Command("git", "-C", repoB, "commit", "-m", "from B").Run()
	exec.Command("git", "-C", repoB, "push").Run()

	// Sync from A with no local changes — should pull B's commit
	warning := syncCentralStore(repoA)
	if warning != "" {
		t.Fatalf("syncCentralStore returned warning: %s", warning)
	}

	if _, err := os.Stat(filepath.Join(repoA, "tickets", "from-b.md")); err != nil {
		t.Errorf("expected from-b.md to be pulled into repoA, got %v", err)
	}
}

func TestSyncRefusesConflictMarkers(t *testing.T) {
	dir := setupGitRepo(t)

	// Drop a config.yaml containing unresolved git conflict markers, the
	// exact failure we shipped in 7.2 when autostash pop collided on
	// registered_at.
	corrupt := []byte(`projects:
    loom:
        store: central
<<<<<<< Updated upstream
        registered_at: "2026-04-25T20:38:12Z"
=======
        registered_at: "2026-04-25T20:38:35Z"
>>>>>>> Stashed changes
`)
	os.WriteFile(filepath.Join(dir, "config.yaml"), corrupt, 0o644)

	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") || !contains(warning, "conflict marker") {
		t.Fatalf("expected conflict-marker block, got %q", warning)
	}

	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created with conflict markers (before=%s after=%s)", before, after)
	}

	if blocked := readSyncBlocked(dir); blocked == "" {
		t.Error("expected .tk-sync-blocked marker to be written")
	}

	// Index must not retain the bad blob.
	staged, _ := execCommand("git", "-C", dir, "diff", "--cached", "--name-only")
	if contains(staged, "config.yaml") {
		t.Errorf("config.yaml left staged after refusal: %q", staged)
	}
}

func TestSyncRefusesUnmergedPaths(t *testing.T) {
	dir := setupGitRepo(t)

	// Synthesize an unmerged index entry by writing the same path at
	// stages 2 and 3 with different blobs. This is what `git ls-files -u`
	// reports after an autostash pop conflict, even though no rebase
	// directory exists.
	os.WriteFile(filepath.Join(dir, "ours.txt"), []byte("ours\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "theirs.txt"), []byte("theirs\n"), 0o644)
	oursSHA, _ := execCommand("git", "-C", dir, "hash-object", "-w", "ours.txt")
	theirsSHA, _ := execCommand("git", "-C", dir, "hash-object", "-w", "theirs.txt")
	oursSHA = trimNL(oursSHA)
	theirsSHA = trimNL(theirsSHA)

	exec.Command("git", "-C", dir, "rm", "--cached", "-f", "ours.txt").Run()
	exec.Command("git", "-C", dir, "rm", "--cached", "-f", "theirs.txt").Run()

	idx := "100644 " + oursSHA + " 2\tconfig.yaml\n100644 " + theirsSHA + " 3\tconfig.yaml\n"
	cmd := exec.Command("git", "-C", dir, "update-index", "--index-info")
	cmd.Stdin = strings.NewReader(idx)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update-index: %v (%s)", err, string(out))
	}

	before, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")

	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") || !contains(warning, "unmerged") {
		t.Fatalf("expected unmerged-paths block, got %q", warning)
	}

	after, _ := execCommand("git", "-C", dir, "rev-list", "--count", "HEAD")
	if before != after {
		t.Errorf("commit was created with unmerged paths (before=%s after=%s)", before, after)
	}

	if blocked := readSyncBlocked(dir); blocked == "" {
		t.Error("expected .tk-sync-blocked marker to be written")
	}
}

func trimNL(s string) string { return strings.TrimSpace(s) }

func TestSyncBlocked(t *testing.T) {
	dir := setupGitRepo(t)

	// Write a sync-blocked marker and simulate active rebase
	writeSyncBlocked(dir, "sync blocked: test conflict")
	rebaseDir := filepath.Join(dir, ".git", "rebase-merge")
	os.MkdirAll(rebaseDir, 0o755)

	// Sync should skip and return the blocked message
	warning := syncCentralStore(dir)
	if !contains(warning, "sync blocked") {
		t.Errorf("expected blocked warning, got %q", warning)
	}

	// Remove rebase dir (simulate conflict resolution) and the marker
	os.RemoveAll(rebaseDir)
	// syncCentralStore should now clear the block and proceed

	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "unblocked.md"), []byte("---\ntitle: Unblocked\n---\n"), 0o644)
	warning = syncCentralStore(dir)
	if warning != "" {
		t.Fatalf("sync after resolution returned warning: %s", warning)
	}

	// Verify the blocked marker was cleared
	if blocked := readSyncBlocked(dir); blocked != "" {
		t.Errorf("sync-blocked marker should be cleared, got %q", blocked)
	}
}

func TestServeSyncStarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a central store with git
	storeRoot := filepath.Join(home, ".tickets")
	os.MkdirAll(storeRoot, 0o755)
	runGit(t, storeRoot, "init")
	runGit(t, storeRoot, "config", "user.email", "test@test.com")
	runGit(t, storeRoot, "config", "user.name", "test")
	os.WriteFile(filepath.Join(storeRoot, ".gitkeep"), []byte(""), 0o644)
	runGit(t, storeRoot, "add", "-A")
	runGit(t, storeRoot, "commit", "-m", "init")

	// Save config with central_root
	cfg := project.Config{CentralRoot: storeRoot, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	// Verify findGitRoot works — it is the gate serve applies before starting
	// the loop, which then runs against the store root.
	gitRoot, err := findGitRoot(storeRoot)
	if err != nil {
		t.Fatalf("findGitRoot: %v", err)
	}

	// Launch sync loop with short interval, cancel quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Write a file to sync under tickets/
	os.MkdirAll(filepath.Join(storeRoot, "tickets"), 0o755)
	os.WriteFile(filepath.Join(storeRoot, "tickets", "sync-test.md"), []byte("test"), 0o644)

	started := make(chan struct{})
	go func() {
		close(started)
		syncLoop(ctx, storeRoot, 100*time.Millisecond)
	}()
	<-started

	// Wait for at least one cycle
	time.Sleep(300 * time.Millisecond)
	cancel()

	// Verify the file was committed
	out, _ := execCommand("git", "-C", gitRoot, "log", "--oneline")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("sync loop did not commit, log: %s", out)
	}
}

func TestServeSyncShutdown(t *testing.T) {
	dir := setupGitRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		syncLoop(ctx, dir, 50*time.Millisecond)
		close(done)
	}()

	// Let it run briefly
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Should stop promptly
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("syncLoop did not stop after context cancel")
	}
}

func TestSyncIntervalConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No config — should return default
	d := syncInterval()
	if d != defaultSyncInterval {
		t.Errorf("default interval = %v, want %v", d, defaultSyncInterval)
	}

	// Set custom interval
	cfg := project.Config{SyncInterval: "10s", Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	d = syncInterval()
	if d != 10*time.Second {
		t.Errorf("custom interval = %v, want 10s", d)
	}
}

func TestSyncCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	centralRoot := filepath.Join(home, ".tickets")
	dir := setupGitRepo(t)

	// Set up config pointing to our test repo as central root
	cfg := project.Config{CentralRoot: dir, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)
	_ = centralRoot

	// Create a file to sync under tickets/
	os.MkdirAll(filepath.Join(dir, "tickets"), 0o755)
	os.WriteFile(filepath.Join(dir, "tickets", "sync-cmd-test.md"), []byte("test"), 0o644)

	err := runSync(syncCmd, nil)
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}

	// Verify committed
	out, _ := execCommand("git", "-C", dir, "log", "--oneline", "-1")
	if !contains(out, "tk: sync tickets") {
		t.Errorf("expected sync commit, got %q", out)
	}
}

func TestSyncCommandJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := setupGitRepo(t)

	cfg := project.Config{CentralRoot: dir, Projects: map[string]project.ProjectConfig{}}
	project.Save(cfg)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	jsonOutput = true
	defer func() { jsonOutput = false }()

	err := runSync(syncCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runSync --json: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, output)
	}

	if _, ok := result["synced"]; !ok {
		t.Error("missing 'synced' field")
	}
	if _, ok := result["warning"]; !ok {
		t.Error("missing 'warning' field")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
