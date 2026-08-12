package cmd

import (
	"fmt"
	"os"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <id> <repo-path>",
	Short: "Move a ticket to another repo's ticket store",
	Long: "Move a ticket to the target repo's store: the project it owns in the central store. " +
		"A repo that owns none is refused, as is a target " +
		"that resolves to the store the ticket already lives in — that would rename it rather " +
		"than move it; re-parenting within a project is 'tk edit --parent'. " +
		"Closes the original with a note. " +
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
	// Everything below fails on the state of the store, not on how the command
	// was typed — the usage dump would bury the error naming the repo, and a
	// partial move's diagnostic with it. Arg count is still cobra's, before this.
	cmd.SilenceUsage = true

	// Resolve the target to the project it owns in the central store, so the
	// moved ticket lands where `tk ls`, `tk ui` and the MCP server will find it.
	src := TicketStore()
	dst, unregistered, err := ticket.ResolveStoreForRepo(targetRepo)
	if err != nil {
		return err
	}
	// The same warning the CLI's own resolution prints: an unregistered project
	// is one MultiStore.Create refuses to write to, so a move into it is the
	// quietest way to put a ticket where nothing else will.
	if unregistered {
		fmt.Fprintln(os.Stderr, ticket.UnregisteredWarning(dst))
	}

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
