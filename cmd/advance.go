package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var advanceCmd = &cobra.Command{
	Use:   "advance <id> [--to <stage>]",
	Short: "Move ticket to next pipeline stage",
	Long:  "Advance a ticket through its type-dependent pipeline. Enforces gate checks unless --force is set.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAdvance,
}

func init() {
	advanceCmd.Flags().String("to", "", "target stage (default: next in pipeline)")
	advanceCmd.Flags().String("reason", "", "reason for skip (required when skipping stages)")
	advanceCmd.Flags().Bool("force", false, "bypass gate checks")

	rootCmd.AddCommand(advanceCmd)
}

func runAdvance(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	id := args[0]

	to, _ := cmd.Flags().GetString("to")
	reason, _ := cmd.Flags().GetString("reason")
	force, _ := cmd.Flags().GetBool("force")

	opts := ticket.AdvanceOptions{
		Force: force,
	}
	if to != "" {
		opts.SkipTo = ticket.Stage(to)
		opts.Reason = reason
	}

	result, err := ticket.Advance(store, id, opts)
	if err != nil {
		if result != nil && len(result.GateErrors) > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Gate failures for %s → %s:\n", result.From, result.To)
			for _, e := range result.GateErrors {
				fmt.Fprintf(cmd.ErrOrStderr(), "  ✗ %s\n", e)
			}
		}
		return err
	}

	fmt.Printf("%s: %s → %s\n", id, result.From, result.To)
	if len(result.Skipped) > 0 {
		names := make([]string, len(result.Skipped))
		for i, s := range result.Skipped {
			names[i] = string(s)
		}
		fmt.Printf("  skipped: %s\n", strings.Join(names, ", "))
	}

	for _, c := range result.Propagated {
		fmt.Printf("  -> %s: %s → %s\n", c.ID, c.OldStage, c.NewStage)
	}

	return nil
}
