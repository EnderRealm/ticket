package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ticket storage for this project",
	RunE:  runInit,
}

func init() {
	f := initCmd.Flags()
	f.String("store", "", "storage type: central or local")
	f.String("project", "", "project name (default: auto-detect)")
	f.Bool("yes", false, "non-interactive mode (defaults to central)")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	storeFlag, _ := cmd.Flags().GetString("store")
	projectFlag, _ := cmd.Flags().GetString("project")
	yes, _ := cmd.Flags().GetBool("yes")

	if storeFlag != "" && storeFlag != "central" && storeFlag != "local" {
		return fmt.Errorf("store must be central or local")
	}

	cfg, err := project.Load()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	repoPath := project.DetectProjectPath(cwd)
	localTicketsDir := filepath.Join(repoPath, ".tickets")
	hasLocalTickets := dirExists(localTicketsDir)
	hasGit := dirExists(filepath.Join(repoPath, ".git"))

	store := storeFlag
	copyLocalToCentral := false

	if store == "" {
		if yes {
			store = "central"
			if cfg.DefaultStore == "local" || cfg.DefaultStore == "central" {
				store = cfg.DefaultStore
			}
			if hasLocalTickets {
				copyLocalToCentral = true
			}
		} else {
			store, err = promptStoreChoice(hasLocalTickets)
			if err != nil {
				return err
			}
			if store == "central" && hasLocalTickets {
				copyLocalToCentral = true
			}
		}
	} else if store == "central" && hasLocalTickets {
		copyLocalToCentral = true
	}

	projectName, _ := project.ResolveName(cfg, repoPath, projectFlag)
	if projectName == "" {
		return fmt.Errorf("failed to resolve project name")
	}

	registeredAt := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := cfg.Projects[projectName]; ok && existing.RegisteredAt != "" {
		registeredAt = existing.RegisteredAt
	}

	var centralDir string
	if store == "central" {
		centralRoot, err := project.CentralStoreRoot()
		if err != nil {
			return err
		}
		centralDir, err = project.CentralProjectDir(projectName)
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
		if err := bootstrapCentralStoreGit(centralRoot); err != nil {
			return err
		}
	}
	if store == "local" && !dirExists(localTicketsDir) {
		if err := os.MkdirAll(localTicketsDir, 0o755); err != nil {
			return err
		}
	}

	cfg.UpsertProject(projectName, project.ProjectConfig{
		Path:         repoPath,
		Store:        store,
		AutoLink:     false,
		AutoClose:    false,
		RegisteredAt: registeredAt,
	})
	if err := project.Save(cfg); err != nil {
		return err
	}

	if jsonOutput {
		return emitInitJSON(projectName, repoPath, store, cfg.Projects[projectName], hasGit, copyLocalToCentral)
	}

	fmt.Println()
	fmt.Println("── Setup complete ──")
	fmt.Println()
	if store == "central" {
		fmt.Printf("  project:  %s\n", projectName)
		fmt.Printf("  store:    central (%s)\n", centralDir)
	} else {
		fmt.Printf("  project:  %s\n", projectName)
		fmt.Printf("  store:    local (%s)\n", localTicketsDir)
	}
	if copyLocalToCentral {
		fmt.Println("  migrated: tickets copied to central store")
		fmt.Println("            original .tickets/ kept as backup")
	}
	fmt.Println()

	return nil
}

func promptStoreChoice(hasLocalTickets bool) (string, error) {
	reader := bufio.NewReader(os.Stdin)

	if hasLocalTickets {
		fmt.Println("Existing tickets found in .tickets/")
		fmt.Println()
		fmt.Println("  [1] Copy to central store (recommended)")
		fmt.Println("  [2] Keep local in .tickets/")
	} else {
		fmt.Println("Where should tickets be stored?")
		fmt.Println()
		fmt.Println("  [1] Central store (recommended)")
		fmt.Println("  [2] Local .tickets/ inside this repo")
	}

	fmt.Println()
	fmt.Print("Choose [1/2]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return "central", nil
	}
	choice := strings.TrimSpace(line)
	if choice == "2" {
		return "local", nil
	}
	return "central", nil
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

	if out, err := exec.Command("git", "-C", storeRoot, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("central store git add failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	// Only commit if there are staged changes
	diff := exec.Command("git", "-C", storeRoot, "diff", "--cached", "--quiet")
	if err := diff.Run(); err == nil {
		return nil
	}

	if out, err := exec.Command("git", "-C", storeRoot, "commit", "-m", "tk: init central store").CombinedOutput(); err != nil {
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

func emitInitJSON(projectName, repoPath, store string, config project.ProjectConfig, hasGit, copiedLocal bool) error {
	data, err := json.MarshalIndent(map[string]any{
		"project":                 projectName,
		"path":                    repoPath,
		"store":                   store,
		"config":                  config,
		"has_git":                 hasGit,
		"copied_local_to_central": copiedLocal,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
