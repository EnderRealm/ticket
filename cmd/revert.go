package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var revertCmd = &cobra.Command{
	Use:   "revert <id> --to <stage> --reason '...'",
	Short: "Revert to an earlier pipeline stage with audit trail",
	Args:  cobra.ExactArgs(1),
	RunE:  runRevert,
}

func init() {
	revertCmd.Flags().String("to", "", "target stage (required)")
	revertCmd.Flags().String("reason", "", "reason for reverting (required)")
	revertCmd.MarkFlagRequired("to")
	revertCmd.MarkFlagRequired("reason")

	rootCmd.AddCommand(revertCmd)
}

func runRevert(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	id := args[0]

	to, _ := cmd.Flags().GetString("to")
	reason, _ := cmd.Flags().GetString("reason")

	result, err := ticket.Revert(store, id, ticket.Stage(to), reason)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s → %s (reverted)\n", id, result.From, result.To)
	fmt.Printf("  reason: %s\n", reason)
	return nil
}
