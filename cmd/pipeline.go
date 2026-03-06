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
	for _, t := range tickets {
		stage := t.Stage
		if stage == "" {
			// Map legacy status for display.
			if s, ok := ticket.StatusToStage[t.Status]; ok {
				stage = s
			} else {
				continue
			}
		}
		if stageFilter != "" && string(stage) != stageFilter {
			continue
		}
		grouped[stage] = append(grouped[stage], t)
	}

	if len(grouped) == 0 {
		printEmptyMessage()
		return nil
	}

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

		fmt.Printf("=== %s (%d) ===\n", stage, len(group))
		printHeader()
		for _, t := range group {
			printRow(t)
		}
	}

	return nil
}
