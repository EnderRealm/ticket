package cmd

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var doneCmd = &cobra.Command{
	Use:   "done",
	Short: "Show recently done tickets",
	RunE:  runDone,
}

func init() {
	addFilterFlags(doneCmd)
	doneCmd.Flags().Int("limit", 20, "maximum number of tickets to show")

	rootCmd.AddCommand(doneCmd)
}

func runDone(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	// Filter to done only.
	var done []*ticket.Ticket
	for _, t := range tickets {
		if t.Stage == ticket.StageDone {
			done = append(done, t)
		}
	}

	opts := parseFilterFlags(cmd)
	done = ticket.Filter(done, opts)

	// Sort by file mtime (most recent first).
	dir := TicketsDir()
	type mtimeTicket struct {
		ticket *ticket.Ticket
		mtime  int64
	}
	var mt []mtimeTicket
	for _, t := range done {
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
