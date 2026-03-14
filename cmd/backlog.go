package cmd

import (
	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var backlogCmd = &cobra.Command{
	Use:   "backlog",
	Short: "List tickets in the backlog",
	RunE:  runBacklog,
}

func init() {
	addFilterFlags(backlogCmd)
	rootCmd.AddCommand(backlogCmd)
}

func runBacklog(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	var backlog []*ticket.Ticket
	for _, t := range tickets {
		if t.Stage == ticket.StageBacklog {
			backlog = append(backlog, t)
		}
	}

	opts := parseFilterFlags(cmd)
	backlog = ticket.Filter(backlog, opts)

	if len(backlog) == 0 {
		printEmptyMessage()
		return nil
	}

	ticket.SortByPriorityID(backlog)
	newTableWriter().Print(backlog)
	return nil
}
