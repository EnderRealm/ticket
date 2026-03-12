package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query [jq-filter]",
	Short: "Output tickets as JSONL with optional jq filtering",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runQuery,
}

func init() {
	rootCmd.AddCommand(queryCmd)
}

// ticketJSON mirrors the bash query output format.
type ticketJSON struct {
	ID          string   `json:"id"`
	Stage       string   `json:"stage"`
	Deps        []string `json:"deps"`
	Links       []string `json:"links"`
	Created     string   `json:"created"`
	Type        string   `json:"type"`
	Priority    int      `json:"priority"`
	Assignee    string   `json:"assignee,omitempty"`
	ExternalRef string   `json:"external-ref,omitempty"`
	Parent      string   `json:"parent,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Title       string   `json:"title"`
}

func runQuery(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	tickets, err := store.List()
	if err != nil {
		return err
	}

	var lines []string
	for _, t := range tickets {
		j := ticketJSON{
			ID:          t.ID,
			Stage:       string(t.Stage),
			Deps:        t.Deps,
			Links:       t.Links,
			Created:     t.Created.UTC().Format("2006-01-02T15:04:05Z"),
			Type:        string(t.Type),
			Priority:    t.Priority,
			Assignee:    t.Assignee,
			ExternalRef: t.ExternalRef,
			Parent:      t.Parent,
			Tags:        t.Tags,
			Title:       t.Title,
		}
		data, err := json.Marshal(j)
		if err != nil {
			continue
		}
		lines = append(lines, string(data))
	}

	jsonl := strings.Join(lines, "\n")

	if len(args) > 0 && args[0] != "" {
		// Pipe through jq.
		filter := fmt.Sprintf("select(%s)", args[0])
		jq := exec.Command("jq", "-c", filter)
		jq.Stdin = strings.NewReader(jsonl)
		jq.Stdout = cmd.OutOrStdout()
		jq.Stderr = cmd.ErrOrStderr()
		return jq.Run()
	}

	fmt.Println(jsonl)
	return nil
}
