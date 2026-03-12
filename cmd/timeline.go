package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var timelineCmd = &cobra.Command{
	Use:   "timeline",
	Short: "Tickets closed by week",
	RunE:  runTimeline,
}

func init() {
	timelineCmd.Flags().Int("weeks", 4, "number of weeks to show")
	rootCmd.AddCommand(timelineCmd)
}

func runTimeline(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	weeks, _ := cmd.Flags().GetInt("weeks")

	// Collect closed tickets by ISO week of their created date
	// (matches bash behavior).
	weeklyCounts := map[string]int{}
	for _, t := range tickets {
		if t.Stage != ticket.StageDone || t.Created.IsZero() {
			continue
		}
		yr, wk := t.Created.ISOWeek()
		label := fmt.Sprintf("%d-W%02d", yr, wk)
		weeklyCounts[label]++
	}

	// Sort week labels and take the last N.
	var labels []string
	for l := range weeklyCounts {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	if len(labels) > weeks {
		labels = labels[len(labels)-weeks:]
	}

	// Find max for bar scaling.
	maxCount := 0
	for _, l := range labels {
		if weeklyCounts[l] > maxCount {
			maxCount = weeklyCounts[l]
		}
	}
	if maxCount == 0 {
		maxCount = 1
	}

	// Color support: green bars unless NO_COLOR or non-TTY.
	isTTY := false
	if fi, err := os.Stdout.Stat(); err == nil {
		isTTY = fi.Mode()&os.ModeCharDevice != 0
	}
	useColor := os.Getenv("NO_COLOR") == "" && isTTY
	green, reset := "", ""
	if useColor {
		green = "\033[32m"
		reset = "\033[0m"
	}

	const barWidth = 30

	fmt.Printf("\n  TICKETS CLOSED BY WEEK\n\n")
	if len(labels) == 0 {
		fmt.Printf("  No closed tickets found.\n")
	}
	for _, l := range labels {
		c := weeklyCounts[l]
		filled := c * barWidth / maxCount
		if filled == 0 && c > 0 {
			filled = 1
		}
		bar := ""
		for i := 0; i < filled; i++ {
			bar += "█"
		}
		fmt.Printf("  %s  %s%s%s %d\n", l, green, bar, reset, c)
	}
	fmt.Println()

	return nil
}
