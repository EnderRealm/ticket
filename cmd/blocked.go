package cmd

import (
	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var blockedCmd = &cobra.Command{
	Use:   "blocked",
	Short: "Show tickets with unresolved dependencies",
	RunE:  runBlocked,
}

func init() {
	addFilterFlags(blockedCmd)
	rootCmd.AddCommand(blockedCmd)
}

func runBlocked(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := ticket.BlockedTickets(store)
	if err != nil {
		return err
	}

	opts := parseFilterFlags(cmd)
	tickets = ticket.Filter(tickets, opts)

	if len(tickets) == 0 {
		printEmptyMessage()
		return nil
	}

	ticket.SortByPriorityID(tickets)
	newTableWriter().Print(tickets)
	return nil
}
