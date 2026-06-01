package cmd

import (
	"fmt"
	"os"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Interactive ticket browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := project.Load()
		app := tui.New(TicketsDir(), version(), cfg.SpawnCommand)
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
