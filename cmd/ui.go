package cmd

import (
	"fmt"
	"os"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Interactive ticket browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := project.Load()
		ticketsDir, projectName, unregistered := resolveTicketsDir()
		workDir := project.ResolveWorkDir(ticketsDir, cfg)
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
		app := tui.New(ticketsDir, projectName, version(), spawnCommand, workDir, unregistered)
		p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
