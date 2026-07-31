package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var frontierCmd = &cobra.Command{
	Use:   "frontier",
	Short: "List ready tickets whose dependencies are all done or closed",
	Long:  "List ready tickets whose dependencies are all done or closed — the parallel-safe set to start next.",
	Args:  cobra.NoArgs,
	RunE:  runFrontier,
}

func init() {
	frontierCmd.Flags().String("project", "", "limit to a single project")
	rootCmd.AddCommand(frontierCmd)
}

func runFrontier(cmd *cobra.Command, args []string) error {
	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("frontier requires a configured central store: %w", err)
	}
	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))

	tickets, err := ticket.FrontierTickets(store)
	if err != nil {
		return err
	}

	if proj, _ := cmd.Flags().GetString("project"); proj != "" {
		cfg, err := project.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if _, ok := cfg.Projects[proj]; !ok {
			return fmt.Errorf("project %q not found in config", proj)
		}
		var filtered []*ticket.Ticket
		for _, t := range tickets {
			if p, _ := ticket.ParseNamespacedID(t.ID); p == proj {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	if len(tickets) == 0 {
		printEmptyMessage()
		return nil
	}

	ticket.SortByPriorityID(tickets)

	if jsonOutput {
		items := make([]ticketJSON, 0, len(tickets))
		for _, t := range tickets {
			items = append(items, toTicketJSON(t))
		}
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// STATUS is ready for every row by construction — drop the column.
	newTableWriter("STATUS").Print(tickets)
	return nil
}
