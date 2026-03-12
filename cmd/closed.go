package cmd

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var closedCmd = &cobra.Command{
	Use:   "closed",
	Short: "Show recently closed tickets",
	RunE:  runClosed,
}

func init() {
	addFilterFlags(closedCmd)
	closedCmd.Flags().Int("limit", 20, "maximum number of tickets to show")

	rootCmd.AddCommand(closedCmd)
}

func runClosed(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	// Filter to done only.
	var closed []*ticket.Ticket
	for _, t := range tickets {
		if t.Stage == ticket.StageDone {
			closed = append(closed, t)
		}
	}

	opts := parseFilterFlags(cmd)
	closed = ticket.Filter(closed, opts)

	// Sort by file mtime (most recent first).
	dir := TicketsDir()
	type mtimeTicket struct {
		ticket *ticket.Ticket
		mtime  int64
	}
	var mt []mtimeTicket
	for _, t := range closed {
		info, err := os.Stat(filepath.Join(dir, t.ID+".md"))
		mtime := int64(0)
		if err == nil {
			mtime = info.ModTime().UnixNano()
		}
		mt = append(mt, mtimeTicket{ticket: t, mtime: mtime})
	}
	sort.Slice(mt, func(i, j int) bool {
		return mt[i].mtime > mt[j].mtime
	})

	limit, _ := cmd.Flags().GetInt("limit")
	if limit > 0 && len(mt) > limit {
		mt = mt[:limit]
	}

	if len(mt) == 0 {
		printEmptyMessage()
		return nil
	}

	var result []*ticket.Ticket
	for _, m := range mt {
		result = append(result, m.ticket)
	}
	newTableWriter().Print(result)
	return nil
}
