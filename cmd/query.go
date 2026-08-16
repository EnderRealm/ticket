package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Abandoned   bool              `json:"abandoned,omitempty"`
	Deps        []string          `json:"deps"`
	Links       []string          `json:"links"`
	Created     string            `json:"created"`
	Type        string            `json:"type"`
	Priority    int               `json:"priority"`
	ExternalRef string            `json:"external-ref,omitempty"`
	Parent      string            `json:"parent,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Title       string            `json:"title"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	DepCargo    map[string]string `json:"dep_cargo,omitempty"`
	Extra       map[string]string `json:"-"`
}

func (j ticketJSON) MarshalJSON() ([]byte, error) {
	type alias ticketJSON
	data, err := json.Marshal(alias(j))
	if err != nil {
		return nil, err
	}
	if len(j.Extra) == 0 {
		return data, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	for k, v := range j.Extra {
		b, _ := json.Marshal(v)
		m[k] = b
	}
	return json.Marshal(m)
}

func toTicketJSON(t *ticket.Ticket) ticketJSON {
	return ticketJSON{
		ID:          t.ID,
		Status:      string(t.Status),
		Abandoned:   t.Abandoned,
		Deps:        t.Deps,
		Links:       t.Links,
		Created:     t.Created.UTC().Format("2006-01-02T15:04:05Z"),
		Type:        string(t.Type),
		Priority:    t.Priority,
		ExternalRef: t.ExternalRef,
		Parent:      t.Parent,
		Tags:        t.Tags,
		Title:       t.Title,
		Outputs:     t.Outputs,
		DepCargo:    t.DepCargo,
		Extra:       t.Extra,
	}
}

func runQuery(cmd *cobra.Command, args []string) error {
	store := TicketStore()
	tickets, err := store.List()
	if err != nil {
		return err
	}

	var lines []string
	for _, t := range tickets {
		data, err := json.Marshal(toTicketJSON(t))
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
