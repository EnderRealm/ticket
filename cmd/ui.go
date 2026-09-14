package cmd

import (
	"fmt"
	"os"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Interactive ticket browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := project.Load()
		ticketsDir, projectName, unregistered := mustResolveTicketsDir()
		workDir := uiWorkDir(ticketsDir, projectName, cfg)
		// From machine-local config only — the template is handed to `sh -c`, so
		// neither the synced store nor a TK_STORE_ROOT root may supply it.
		//
		// An unreadable home config is not fatal the way it is for VerifyAllow:
		// the fallback on an empty template is tk's own constant in
		// buildSpawnCommand, not anything a caller supplies, so there is nothing
		// to widen — and refusing would cost the browser over a file the browser
		// does not otherwise need.
		spawnCommand, err := project.SpawnCommand()
		if err != nil {
			fmt.Fprintf(os.Stderr, "spawn_command unavailable, using the default: %v\n", err)
			spawnCommand = ""
		}
		// The checkout a spawn runs in is the selected ticket's own project's,
		// resolved per spawn: a foreign child reached through an epic's detail
		// is not this board's, and Root has none.
		execDir := func(ns string) (string, error) { return project.ExecutionDir(cfg, ns) }
		app := tui.New(ticketsDir, projectName, version(), spawnCommand, workDir, unregistered, execDir)
		p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
		restoreWarnings := tui.CaptureWarnings(p)
		defer restoreWarnings()
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(uiCmd)
}

// uiWorkDir is the repo directory the TUI lists move targets beside: the
// project's recorded path, or the repo the store was resolved from when the
// config records none — a project registered on another machine has no local
// path here. ResolveWorkDir has nothing of its own to fall back to, since a
// tickets directory sits under the central store rather than in a repo. The
// error is the one mustResolveTicketsDir has already exited on. Root has no
// repository and gets no fallback: the working directory is whatever the user
// happened to launch from, and a spawn never reads this value anyway.
func uiWorkDir(ticketsDir, projectName string, cfg project.Config) string {
	if project.IsRoot(projectName) {
		return ""
	}
	if dir := project.ResolveWorkDir(ticketsDir, cfg); dir != "" {
		return dir
	}
	dir, _ := repoDir()
	return dir
}
