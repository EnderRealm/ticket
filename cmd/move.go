package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <id> <repo-path>",
	Short: "Move a ticket to another repo",
	Long: "Move a ticket to another project's .tickets/ directory. Closes the original with a note. " +
		"A closed ticket is hidden from a default 'tk ls', so list moved tickets with 'tk ls --status=closed'.",
	Args: cobra.ExactArgs(2),
	RunE: runMove,
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

	src := TicketStore()
	dst := ticket.NewFileStore(targetDir)

	results, err := ticket.MoveTicket(src, dst, id, recursive)
	for _, r := range results {
		fmt.Printf("Moved %s -> %s\n", r.OldID, r.NewID)
	}
	if err != nil {
		// The move is not rolled back, so name what completed; the error names
		// any target copy whose source ticket is still open. The moved IDs are
		// the result and stay on stdout; this banner is a diagnostic.
		if len(results) > 0 {
			landed := fmt.Sprintf("the %d tickets above are", len(results))
			if len(results) == 1 {
				landed = "the ticket above is"
			}
			fmt.Fprintf(os.Stderr, "Move failed partway: %s in %s and closed here.\n",
				landed, targetRepo)
		}
		return err
	}
	return nil
}
