package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var addNoteCmd = &cobra.Command{
	Use:   "add-note <id> [text]",
	Short: "Append timestamped note to a ticket",
	Long:  "Append a timestamped note. If no text is given, reads from stdin.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAddNote,
}

func init() {
	rootCmd.AddCommand(addNoteCmd)
}

func runAddNote(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	id := args[0]

	t, err := store.Get(id)
	if err != nil {
		return err
	}

	var noteText string
	if len(args) > 1 {
		noteText = strings.Join(args[1:], " ")
	} else {
		// Read from stdin if not a terminal.
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			var lines []string
			for scanner.Scan() {
				lines = append(lines, scanner.Text())
			}
			noteText = strings.Join(lines, "\n")
		} else {
			return fmt.Errorf("no note provided")
		}
	}

	if noteText == "" {
		return fmt.Errorf("no note provided")
	}

	t.Notes = append(t.Notes, ticket.Note{
		Timestamp: time.Now().UTC(),
		Text:      noteText,
	})

	// Rebuild body with notes section to ensure it's written.
	// The serializer handles notes from the Notes field.
	// Strip existing notes section from body to avoid duplication.
	if idx := strings.Index(t.Body, "\n## Notes\n"); idx >= 0 {
		t.Body = t.Body[:idx+1]
	} else if strings.HasPrefix(t.Body, "## Notes\n") {
		t.Body = "\n"
	}

	if err := store.Update(t); err != nil {
		return err
	}

	fmt.Printf("Note added to %s\n", t.ID)
	return nil
}
