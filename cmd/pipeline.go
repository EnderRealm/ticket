package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:   "pipeline [--stage <stage>]",
	Short: "Show tickets grouped by pipeline stage",
	RunE:  runPipeline,
}

func init() {
	pipelineCmd.Flags().String("stage", "", "filter to a single stage")
	addFilterFlags(pipelineCmd)

	rootCmd.AddCommand(pipelineCmd)
}

func runPipeline(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	opts := parseFilterFlags(cmd)
	tickets = ticket.Filter(tickets, opts)

	stageFilter, _ := cmd.Flags().GetString("stage")

	// Group by stage (from config).
	stages := ticket.AllStages()

	grouped := map[ticket.Stage][]*ticket.Ticket{}
	var allFiltered []*ticket.Ticket
	for _, t := range tickets {
		stage := t.Stage
		if stage == "" || stage == ticket.StageDone {
			continue
		}
		if stageFilter != "" && string(stage) != stageFilter {
			continue
		}
		grouped[stage] = append(grouped[stage], t)
		allFiltered = append(allFiltered, t)
	}

	if len(grouped) == 0 {
		printEmptyMessage()
		return nil
	}

	// Compute global column widths across all groups.
	tw := newTableWriter("STAGE")
	tw.computeWidths(allFiltered)

	first := true
	for _, stage := range stages {
		group := grouped[stage]
		if len(group) == 0 {
			continue
		}

		ticket.SortByPriorityID(group)

		if !first {
			fmt.Println()
		}
		first = false

		fmt.Println(colorGroupHeader(fmt.Sprintf("=== %s (%d) ===", stage, len(group))))
		tw.PrintGroup(group)
	}

	return nil
}
