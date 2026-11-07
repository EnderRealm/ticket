package cmd

import (
	"fmt"
	"os"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <id> <project|repo-path>",
	Short: "Move a ticket to another project's ticket store",
	Long: "Move a ticket to another project's store in the central store. " +
		"The target's shape says which form it is. With no path separator it is a project name, " +
		"which has to be registered: a directory ./loom is written with the separator, never as loom. " +
		"With a separator, or as . or .., it is a path, which has to be a directory and the exact " +
		"checkout the config records for a project, symlinks resolved; a subdirectory or linked " +
		"worktree of a checkout owns nothing, so name the project instead. " +
		"A path that does not exist or owns no store is refused, as is a target " +
		"that resolves to the store the ticket already lives in — that would rename it rather " +
		"than move it; re-parenting within a project is 'tk edit --parent'. " +
		"Only an isolated leaf moves: a ticket with a parent, deps or links, one other tickets reference, and any epic are refused, " +
		"because the move copies the ticket under a new ID and closes the original, which would leave those references pointing at a closed copy. " +
		"--recursive is refused for the same reason. " +
		"Closes the original with a note. " +
		"A closed ticket is hidden from a default 'tk ls', so list moved tickets with 'tk ls --status=closed'.",
	Args: cobra.ExactArgs(2),
	RunE: runMove,
}

func init() {
	moveCmd.Flags().BoolP("recursive", "r", false, "refused: a move copies under a new ID and closes the original, so a tree cannot move as one")
	rootCmd.AddCommand(moveCmd)
}

func runMove(cmd *cobra.Command, args []string) error {
	id := args[0]
	target := args[1]
	recursive, _ := cmd.Flags().GetBool("recursive")
	// Everything below fails on the state of the store, not on how the command
	// was typed — the usage dump would bury the error naming the repo, and a
	// partial move's diagnostic with it. Arg count is still cobra's, before this.
	cmd.SilenceUsage = true

	// Resolve the target to a project in the central store, so the moved ticket
	// lands where `tk ls` and the MCP server will find it.
	src := TicketStore()
	dst, unregistered, err := ticket.ResolveMoveDestination(target)
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
		// any target copy whose source ticket is unchanged. The moved IDs are
		// the result and stay on stdout; this banner is a diagnostic.
		if len(results) > 0 {
			landed := fmt.Sprintf("the %d tickets above are", len(results))
			if len(results) == 1 {
				landed = "the ticket above is"
			}
			fmt.Fprintf(os.Stderr, "Move failed partway: %s in %s and recorded as moved here.\n",
				landed, target)
		}
		return err
	}
	return nil
}
