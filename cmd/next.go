package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show per-project next actions",
	RunE:  runNext,
}

func init() {
	rootCmd.AddCommand(nextCmd)
}

func runNext(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	projects, err := ticket.Projects(store)
	if err != nil {
		return err
	}

	if len(projects) == 0 {
		printEmptyMessage()
		return nil
	}

	for i, p := range projects {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("%-8s P%d  %s  [%.0f%% done, %d tasks]\n",
			p.Epic.ID, p.Epic.Priority, p.Epic.Title, p.CompletionPct, p.Total)

		if len(p.StageBreakdown) > 0 {
			fmt.Print("  stages:")
			for _, stage := range []ticket.Stage{
				ticket.StageBacklog, ticket.StageTriage, ticket.StageSpec, ticket.StageDesign,
				ticket.StageImplement, ticket.StageTest, ticket.StageVerify, ticket.StageDone,
			} {
				if count, ok := p.StageBreakdown[stage]; ok && count > 0 {
					fmt.Printf(" %s:%d", stage, count)
				}
			}
			fmt.Println()
		}

		if len(p.NextActions) > 0 {
			fmt.Println("  needs attention:")
			for _, a := range p.NextActions {
				fmt.Printf("    %-8s %s — %s\n", a.Ticket.ID, a.Action, a.Detail)
			}
		}
	}
	return nil
}
