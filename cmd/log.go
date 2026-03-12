package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log <id>",
	Short: "Show stage transition and review history",
	Args:  cobra.ExactArgs(1),
	RunE:  runLog,
}

func init() {
	rootCmd.AddCommand(logCmd)
}

func runLog(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s\n\n", t.ID, t.Title)

	fmt.Printf("Current stage: %s\n", t.Stage)
	if t.Review != "" {
		fmt.Printf("Review: %s\n", t.Review)
	}
	if len(t.Skipped) > 0 {
		fmt.Print("Skipped stages:")
		for _, s := range t.Skipped {
			fmt.Printf(" %s", s)
		}
		fmt.Println()
	}

	if len(t.Reviews) > 0 {
		fmt.Printf("\nReview Log:\n")
		for _, r := range t.Reviews {
			ts := r.Timestamp.Format("2006-01-02 15:04")
			if r.Comment != "" {
				fmt.Printf("  %s [%s] %s — %s\n", ts, r.Reviewer, r.Verdict, r.Comment)
			} else {
				fmt.Printf("  %s [%s] %s\n", ts, r.Reviewer, r.Verdict)
			}
		}
	}

	if len(t.Conversations) > 0 {
		fmt.Printf("\nConversations:\n")
		for _, c := range t.Conversations {
			fmt.Printf("  %s\n", c)
		}
	}

	return nil
}
