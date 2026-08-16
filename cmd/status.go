package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show tk system status",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type statusInfo struct {
	Version      string          `json:"version"`
	ConfigPath   string          `json:"config_path"`
	CentralRoot  string          `json:"central_root"`
	SharedConfig string          `json:"shared_config"`
	DataRemote   string          `json:"data_remote"`
	DataBranch   string          `json:"data_branch"`
	RepoStatus   string          `json:"repo_status"`
	LastSync     string          `json:"last_sync"`
	SyncStatus   string          `json:"sync_status"`
	Projects     []projectStatus `json:"projects"`
}

type projectStatus struct {
	Name    string `json:"name"`
	Store   string `json:"store"`
	Tickets int    `json:"tickets"`
	Path    string `json:"path"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := project.Load()
	if err != nil {
		return err
	}

	centralRoot, _ := project.CentralStoreRoot()
	cfgPath, _ := project.ConfigPath()
	sharedPath, _ := project.SharedConfigPath()

	info := statusInfo{
		Version:      version(),
		ConfigPath:   cfgPath,
		CentralRoot:  centralRoot,
		SharedConfig: sharedPath,
	}

	// Data repo info
	if centralRoot != "" {
		if gitRoot, err := findGitRoot(centralRoot); err == nil {
			info.DataRemote = gitRemoteURL(gitRoot)
			info.DataBranch = gitBranchName(gitRoot)
			info.RepoStatus = gitRepoStatus(gitRoot)
			info.LastSync = lastSyncTime(gitRoot)
			// The blocked marker is a machine-local tk artifact sync writes in
			// the store root, which for a nested store is not the toplevel the
			// other four read.
			info.SyncStatus = syncStatus(centralRoot)
		}
	}

	// Projects
	names := make([]string, 0, len(cfg.Projects))
	for name := range cfg.Projects {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		p := cfg.Projects[name]
		tickets := countTickets(centralRoot, name)
		info.Projects = append(info.Projects, projectStatus{
			Name:    name,
			Store:   p.Store,
			Tickets: tickets,
			Path:    p.Path,
		})
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Text output
	fmt.Printf("tk %s\n", info.Version)
	fmt.Printf("config:        %s\n", info.ConfigPath)
	fmt.Printf("central root:  %s\n", info.CentralRoot)
	fmt.Printf("shared config: %s\n", info.SharedConfig)
	if info.DataRemote != "" {
		fmt.Printf("data repo:     %s (%s)\n", info.DataRemote, info.DataBranch)
	} else {
		fmt.Printf("data repo:     no remote\n")
	}
	fmt.Printf("repo status:   %s\n", info.RepoStatus)
	fmt.Printf("last sync:     %s\n", info.LastSync)
	fmt.Printf("sync status:   %s\n", info.SyncStatus)

	if len(info.Projects) > 0 {
		fmt.Println()

		// Calculate column widths
		nameW, storeW, ticketW, pathW := 4, 5, 7, 4
		for _, p := range info.Projects {
			if len(p.Name) > nameW {
				nameW = len(p.Name)
			}
			if len(p.Store) > storeW {
				storeW = len(p.Store)
			}
			ts := fmt.Sprintf("%d", p.Tickets)
			if len(ts) > ticketW {
				ticketW = len(ts)
			}
			if len(p.Path) > pathW {
				pathW = len(p.Path)
			}
		}

		fmt.Printf("  %-*s  %-*s  %-*s  %s\n", nameW, "NAME", storeW, "STORE", ticketW, "TICKETS", "PATH")
		for _, p := range info.Projects {
			fmt.Printf("  %-*s  %-*s  %-*d  %s\n", nameW, p.Name, storeW, p.Store, ticketW, p.Tickets, p.Path)
		}
	}

	return nil
}

func gitRemoteURL(gitDir string) string {
	out, err := exec.Command("git", "-C", gitDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitBranchName(gitDir string) string {
	out, err := exec.Command("git", "-C", gitDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func gitRepoStatus(gitDir string) string {
	err := exec.Command("git", "-C", gitDir, "diff", "--quiet").Run()
	if err != nil {
		return "dirty"
	}
	err = exec.Command("git", "-C", gitDir, "diff", "--cached", "--quiet").Run()
	if err != nil {
		return "dirty"
	}
	return "clean"
}

func lastSyncTime(gitDir string) string {
	out, err := exec.Command("git", "-C", gitDir, "log", "--all", "--grep=tk: sync tickets", "--format=%ci", "-1").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "never"
	}
	ts := strings.TrimSpace(string(out))
	t, err := time.Parse("2006-01-02 15:04:05 -0700", ts)
	if err != nil {
		return ts
	}
	ago := time.Since(t).Truncate(time.Second)
	if ago < time.Minute {
		return fmt.Sprintf("%ds ago", int(ago.Seconds()))
	}
	if ago < time.Hour {
		return fmt.Sprintf("%dm ago", int(ago.Minutes()))
	}
	if ago < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(ago.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(ago.Hours()/24))
}

func syncStatus(storeRoot string) string {
	blocked := readSyncBlocked(storeRoot)
	if blocked != "" {
		return "blocked: " + blocked
	}
	return "ok"
}

func countTickets(centralRoot, projectName string) int {
	dir := filepath.Join(centralRoot, "tickets", projectName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			count++
		}
	}
	return count
}
