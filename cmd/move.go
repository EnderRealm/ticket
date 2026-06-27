package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <id> <repo-path>",
	Short: "Move a ticket to another repo",
	Long:  "Move a ticket to another project's .tickets/ directory. Closes the original with a note.",
	Args:  cobra.ExactArgs(2),
	RunE:  runMove,
}

func init() {
	moveCmd.Flags().BoolP("recursive", "r", false, "move parent and all descendant tickets")
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	id := args[0]
	targetRepo := args[1]
	recursive, _ := cmd.Flags().GetBool("recursive")

	// Resolve target tickets directory.
	targetDir := filepath.Join(targetRepo, ".tickets")

	src := ticket.NewFileStore(TicketsDir())
	dst := ticket.NewFileStore(targetDir)

	results, err := ticket.MoveTicket(src, dst, id, recursive)
	if err != nil {
		return err
	}

	for _, r := range results {
		fmt.Printf("Moved %s -> %s\n", r.OldID, r.NewID)
	}
	return nil
}
