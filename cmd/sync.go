package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/spf13/cobra"
)

const (
	defaultSyncInterval = 5 * time.Second
	syncBlockedFile     = ".tk-sync-blocked"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync ticket changes to git (stage, commit, push)",
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	storeRoot, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("cannot resolve central store: %w", err)
	}

	gitRoot, err := findGitRoot(storeRoot)
	if err != nil {
		return fmt.Errorf("central store is not in a git repository: %w", err)
	}

	warning := syncCentralStore(gitRoot)

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
func syncLoop(ctx context.Context, gitDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if warning := syncCentralStore(gitDir); warning != "" {
				log.Printf("sync: %s", warning)
			}
		}
	}
}

// syncCentralStore performs a single sync cycle: pull, stage, commit, push.
// Returns a warning message on problems, or empty string on success/no-op.
func syncCentralStore(gitDir string) string {
	// Check sync-blocked marker
	if blocked := readSyncBlocked(gitDir); blocked != "" {
		if syncBlockResolved(gitDir) {
			clearSyncBlocked(gitDir)
		} else {
			return blocked
		}
	}

	// Pull remote changes first so the push branch never starts from a stale
	// base. Runs every cycle regardless of local changes — without this, a
	// machine with no outgoing commits never picks up incoming ones.
	if msg := pullIfBehind(gitDir); msg != "" {
		return msg
	}

	// Stage only tk-managed paths (tickets directory and shared config)
	exec.Command("git", "-C", gitDir, "add", "tickets/").Run()
	exec.Command("git", "-C", gitDir, "add", "config.yaml").Run()

	// Check for staged changes
	diff := exec.Command("git", "-C", gitDir, "diff", "--cached", "--quiet")
	if err := diff.Run(); err == nil {
		return "" // nothing to commit
	}

	// Commit
	msg := "tk: sync tickets"
	if out, err := exec.Command("git", "-C", gitDir, "commit", "-m", msg).CombinedOutput(); err != nil {
		return fmt.Sprintf("git commit failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Check for remote
	remotes, err := gitRemoteNames(gitDir)
	if err != nil || len(remotes) == 0 {
		return "" // no remote, commit-only mode
	}

	// Check for unpushed commits
	if !hasUnpushedCommits(gitDir) {
		return ""
	}

	// Push
	if out, err := exec.Command("git", "-C", gitDir, "push").CombinedOutput(); err == nil {
		clearSyncBlocked(gitDir)
		return ""
	} else {
		_ = out
	}

	// Push failed — pull --rebase --autostash and retry. autostash is safe
	// because the central store contains only tk-managed files.
	if out, err := exec.Command("git", "-C", gitDir, "pull", "--rebase", "--autostash").CombinedOutput(); err != nil {
		exec.Command("git", "-C", gitDir, "rebase", "--abort").Run()
		msg := fmt.Sprintf("sync blocked: git pull --rebase failed; aborted rebase (%s)", strings.TrimSpace(string(out)))
		writeSyncBlocked(gitDir, msg)
		return msg
	}

	if out, err := exec.Command("git", "-C", gitDir, "push").CombinedOutput(); err != nil {
		return fmt.Sprintf("git push failed after rebase (%s)", strings.TrimSpace(string(out)))
	}

	clearSyncBlocked(gitDir)
	return ""
}

// pullIfBehind fetches origin and rebases local commits onto upstream when
// behind. Returns a warning (and writes the sync-blocked marker) on failure,
// or empty string on success / no remote / no upstream / already up to date.
func pullIfBehind(gitDir string) string {
	remotes, err := gitRemoteNames(gitDir)
	if err != nil || len(remotes) == 0 {
		return ""
	}

	// Skip when no upstream is configured for the current branch.
	if err := exec.Command("git", "-C", gitDir, "rev-parse", "--abbrev-ref", "@{u}").Run(); err != nil {
		return ""
	}

	if out, err := exec.Command("git", "-C", gitDir, "fetch").CombinedOutput(); err != nil {
		msg := fmt.Sprintf("sync blocked: git fetch failed (%s)", strings.TrimSpace(string(out)))
		writeSyncBlocked(gitDir, msg)
		return msg
	}

	behindOut, err := exec.Command("git", "-C", gitDir, "rev-list", "--count", "HEAD..@{u}").Output()
	if err != nil || strings.TrimSpace(string(behindOut)) == "0" {
		return ""
	}

	if out, err := exec.Command("git", "-C", gitDir, "pull", "--rebase", "--autostash").CombinedOutput(); err != nil {
		exec.Command("git", "-C", gitDir, "rebase", "--abort").Run()
		msg := fmt.Sprintf("sync blocked: git pull --rebase --autostash failed; aborted rebase (%s)", strings.TrimSpace(string(out)))
		writeSyncBlocked(gitDir, msg)
		return msg
	}
	return ""
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

func gitRemoteNames(gitDir string) ([]string, error) {
	out, err := exec.Command("git", "-C", gitDir, "remote").Output()
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

func hasUnpushedCommits(gitDir string) bool {
	out, err := exec.Command("git", "-C", gitDir, "rev-list", "--count", "@{u}..HEAD").CombinedOutput()
	if err != nil {
		// No upstream — treat as unpushed
		return true
	}
	return strings.TrimSpace(string(out)) != "0"
}

func readSyncBlocked(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, syncBlockedFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeSyncBlocked(gitDir, msg string) {
	os.WriteFile(filepath.Join(gitDir, syncBlockedFile), []byte(msg+"\n"), 0o644)
}

func clearSyncBlocked(gitDir string) {
	os.Remove(filepath.Join(gitDir, syncBlockedFile))
}

// syncBlockResolved checks if the conflict that caused the block is gone.
func syncBlockResolved(gitDir string) bool {
	// If there's no rebase in progress, the block is resolved
	rebaseDir := filepath.Join(gitDir, ".git", "rebase-merge")
	if _, err := os.Stat(rebaseDir); err == nil {
		return false
	}
	rebaseApplyDir := filepath.Join(gitDir, ".git", "rebase-apply")
	if _, err := os.Stat(rebaseApplyDir); err == nil {
		return false
	}
	return true
}
