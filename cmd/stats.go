package cmd

import (
	"fmt"
	"math"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Project health at a glance",
	RunE:  runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	now := time.Now()

	// Counters.
	stages := map[ticket.Stage]int{}
	types := map[ticket.TicketType]int{}
	priorities := map[int]int{}

	var openCount int
	var totalAgeDays float64
	var ageCount int
	var oldestDays int
	var oldestID string

	for _, t := range tickets {
		stages[t.Stage]++
		types[t.Type]++
		priorities[t.Priority]++

		if t.Stage != ticket.StageDone {
			openCount++
			if !t.Created.IsZero() {
				days := int(math.Floor(now.Sub(t.Created).Hours() / 24))
				if days < 0 {
					days = 0
				}
				totalAgeDays += float64(days)
				ageCount++
				if days > oldestDays {
					oldestDays = days
					oldestID = t.ID
				}
			}
		}
	}

	// Header.
	fmt.Printf("\n  PROJECT HEALTH\n\n")

	// Stage breakdown.
	fmt.Printf("  Stage:\n")
	allStages := ticket.AllStages()
	for _, s := range allStages {
		if stages[s] > 0 {
			fmt.Printf("    %-15s %d\n", s, stages[s])
		}
	}
	fmt.Printf("    %-15s %d\n", "TOTAL", len(tickets))

	// Type breakdown.
	fmt.Printf("\n  Types:\n")
	typeOrder := []ticket.TicketType{
		ticket.TypeEpic,
		ticket.TypeFeature,
		ticket.TypeTask,
		ticket.TypeBug,
		ticket.TypeChore,
	}
	for _, t := range typeOrder {
		if types[t] > 0 {
			fmt.Printf("    %-15s %d\n", t, types[t])
		}
	}

	// Priority breakdown.
	fmt.Printf("\n  Priority:\n")
	for p := 0; p <= 4; p++ {
		if priorities[p] > 0 {
			fmt.Printf("    P%-14d %d\n", p, priorities[p])
		}
	}

	// Age stats.
	if ageCount > 0 {
		avgAge := int(totalAgeDays / float64(ageCount))
		fmt.Printf("\n  Open Tickets:\n")
		fmt.Printf("    %-15s %d\n", "Count", openCount)
		fmt.Printf("    %-15s %d days\n", "Average age", avgAge)
		fmt.Printf("    %-15s %d days (%s)\n", "Oldest", oldestDays, oldestID)
	}

	fmt.Println()

	return nil
}
