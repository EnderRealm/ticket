package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ticket storage for this project",
	RunE:  runInit,
}

func init() {
	f := initCmd.Flags()
	f.String("project", "", "project name (default: auto-detect)")
	f.Bool("yes", false, "non-interactive mode")
	f.String("central-root", "", "path to central ticket store root")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	projectFlag, _ := cmd.Flags().GetString("project")
	centralRootFlag, _ := cmd.Flags().GetString("central-root")
	yes, _ := cmd.Flags().GetBool("yes")

	cfg, err := project.Load()
	if err != nil {
		return err
	}

	// If central store isn't configured, set it up now.
	centralRoot, err := project.CentralStoreRoot()
	if err != nil {
		// Not configured yet — need a central root path.
		if centralRootFlag != "" {
			centralRoot = centralRootFlag
		} else if yes {
			return fmt.Errorf("central store not configured: use --central-root to specify path")
		} else {
			fmt.Print("Central ticket store path: ")
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				centralRoot = strings.TrimSpace(scanner.Text())
			}
			if centralRoot == "" {
				return fmt.Errorf("central store path is required")
			}
		}

		// Expand ~ if present.
		if strings.HasPrefix(centralRoot, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			centralRoot = filepath.Join(home, centralRoot[2:])
		}

		abs, err := filepath.Abs(centralRoot)
		if err != nil {
			return err
		}
		centralRoot = abs

		// Persist the central root into config.
		cfg.CentralRoot = centralRoot
		if err := project.Save(cfg); err != nil {
			return err
		}
		// Reload config after saving central root.
		cfg, err = project.Load()
		if err != nil {
			return err
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	repoPath := project.DetectProjectPath(cwd)
	localTicketsDir := filepath.Join(repoPath, ".tickets")
	hasLocalTickets := dirExists(localTicketsDir)

	copyLocalToCentral := hasLocalTickets

	projectName, _ := project.ResolveName(cfg, repoPath, projectFlag)
	if projectName == "" {
		return fmt.Errorf("failed to resolve project name")
	}

	registeredAt := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := cfg.Projects[projectName]; ok && existing.RegisteredAt != "" {
		registeredAt = existing.RegisteredAt
	}

	centralDir, err := project.CentralProjectDir(projectName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(centralDir, 0o755); err != nil {
		return err
	}
	if copyLocalToCentral {
		if err := copyTicketFiles(localTicketsDir, centralDir); err != nil {
			return err
		}
	}
	// A throwaway store keeps no history, and bootstrapping git in one is not
	// merely useless: an override root nested inside another repo would stage
	// and commit the sandbox's own files into that repo, which the override's
	// "nothing is committed or pushed" contract forbids.
	//
	// `init` is gate-exempt, so the pre-run check that refuses a set-but-unusable
	// override never ran for it — but project.Load above resolves through the
	// same override and has already failed on one, so the error return here is
	// defence in depth rather than where that refusal lives.
	_, isolated, err := project.StoreRootOverride()
	if err != nil {
		return err
	}
	if !isolated {
		if err := bootstrapCentralStoreGit(centralRoot); err != nil {
			return err
		}
	}

	// Journaling on by registration: the `[ticket-id]` commit discipline exists
	// for the watcher, and the commit journal is the only commit→ticket trail
	// there is. Registering with both flags off made every store's journal inert
	// — a default hardcoded here rather than one anybody chose. The flags stay
	// explicit in the shared config, so turning either back off is an edit.
	cfg.UpsertProject(projectName, project.ProjectConfig{
		Path:         repoPath,
		Store:        "central",
		AutoLink:     true,
		AutoClose:    true,
		RegisteredAt: registeredAt,
	})
	if err := project.Save(cfg); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("── Setup complete ──")
	fmt.Println()
	fmt.Printf("  project:  %s\n", projectName)
	fmt.Printf("  store:    central (%s)\n", centralDir)
	if copyLocalToCentral {
		fmt.Println("  migrated: tickets copied to central store")
		fmt.Println("            original .tickets/ kept as backup")
	}
	fmt.Println()

	return nil
}

func bootstrapCentralStoreGit(storeRoot string) error {
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		return err
	}

	// Check if storeRoot is already inside a git repo (e.g., a subdirectory
	// of another repo). If so, skip git init — the parent repo owns history.
	alreadyInRepo := false
	if _, err := exec.Command("git", "-C", storeRoot, "rev-parse", "--is-inside-work-tree").Output(); err == nil {
		alreadyInRepo = true
	}

	if !alreadyInRepo && !dirExists(filepath.Join(storeRoot, ".git")) {
		if out, err := exec.Command("git", "-C", storeRoot, "init").CombinedOutput(); err != nil {
			return fmt.Errorf("central store git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}

	// Set local identity if not already configured (only for standalone repos)
	if !alreadyInRepo {
		email, _ := execCommand("git", "-C", storeRoot, "config", "--get", "user.email")
		if email == "" {
			cfgEmail := "tk@local"
			cfgName := "tk"
			if cfg, err := project.Load(); err == nil {
				if cfg.GitEmail != "" {
					cfgEmail = cfg.GitEmail
				}
				if cfg.GitName != "" {
					cfgName = cfg.GitName
				}
			}
			exec.Command("git", "-C", storeRoot, "config", "user.email", cfgEmail).Run()
			exec.Command("git", "-C", storeRoot, "config", "user.name", cfgName).Run()
		}
	}

	// Stage the store's own contents, and scope the staged-changes check and the
	// commit to the same paths. `-C` sets the working directory but not the
	// pathspec, so a pathspec-less add, diff or commit reaches the whole index of
	// the enclosing repo when the store root is nested inside one: the store's
	// init commit would carry whatever that repo's owner had staged, and could
	// consist of nothing else.
	var changed []string
	for _, path := range centralStorePaths {
		// config.yaml is normally written before the bootstrap runs, but a
		// central root whose config.yaml was deleted reaches here without one,
		// and `git add` on a pathspec that matches nothing is a hard error.
		// Only absence is skippable — a stat that fails any other way would
		// otherwise drop the path from the commit silently.
		if _, err := os.Stat(filepath.Join(storeRoot, path)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("central store stat %s failed: %w", path, err)
		}
		if out, err := exec.Command("git", "-C", storeRoot, "add", "--", path).CombinedOutput(); err != nil {
			return fmt.Errorf("central store git add %s failed: %v (%s)", path, err, strings.TrimSpace(string(out)))
		}
		// Commit only the paths that actually have staged changes: `git commit`
		// errors on a pathspec it knows nothing about, and an empty tickets/
		// directory stages nothing. Unlike add, a diff pathspec matching nothing
		// is not an error — it simply reports no changes.
		diff := exec.Command("git", "-C", storeRoot, "diff", "--cached", "--quiet", "--", path)
		if err := diff.Run(); err != nil {
			changed = append(changed, path)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	commitArgs := append([]string{"-C", storeRoot, "commit", "-m", "tk: init central store", "--"}, changed...)
	if out, err := exec.Command("git", commitArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("central store git commit failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func copyTicketFiles(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
