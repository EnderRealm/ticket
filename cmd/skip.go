package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var skipCmd = &cobra.Command{
	Use:   "skip <id> --to <stage> --reason '...'",
	Short: "Skip to a named stage with audit trail",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkip,
}

func init() {
	skipCmd.Flags().String("to", "", "target stage (required)")
	skipCmd.Flags().String("reason", "", "reason for skipping (required)")
	skipCmd.MarkFlagRequired("to")
	skipCmd.MarkFlagRequired("reason")

	rootCmd.AddCommand(skipCmd)
}

func runSkip(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	id := args[0]

	to, _ := cmd.Flags().GetString("to")
	reason, _ := cmd.Flags().GetString("reason")

	result, err := ticket.Skip(store, id, ticket.Stage(to), reason)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s → %s\n", id, result.From, result.To)
	if len(result.Skipped) > 0 {
		names := make([]string, len(result.Skipped))
		for i, s := range result.Skipped {
			names[i] = string(s)
		}
		fmt.Printf("  skipped: %s\n", strings.Join(names, ", "))
		fmt.Printf("  reason: %s\n", reason)
	}
	for _, c := range result.Propagated {
		fmt.Printf("  -> %s: %s → %s\n", c.ID, c.OldStage, c.NewStage)
	}
	return nil
}
