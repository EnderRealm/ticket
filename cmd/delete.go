package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <id> [id...]",
	Short: "Delete tickets",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDelete,
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	store := TicketStore()
	for _, id := range args {
		// Resolve to get the actual ID for display.
		t, err := store.Get(id)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
			continue
		}
		actualID := t.ID
		if err := store.Delete(actualID); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error deleting %s: %v\n", actualID, err)
			continue
		}
		fmt.Printf("Deleted %s\n", actualID)
	}
	return nil
}
