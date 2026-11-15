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
	Long: `Output every ticket as JSONL, one JSON object per line.

The optional filter is passed to jq's select() automatically. Do NOT wrap
your filter in select() -- just provide the expression. Always single-quote
the filter so the shell leaves ! and " alone.

JSON fields: id, status, abandoned, type, priority, title, deps[], links[],
  tags[], created, updated, closed, parent, external-ref, outputs, dep_cargo,
  description, design, acceptance_criteria, test_results,
  notes[] (timestamp, text; every note, oldest first),
  plus any custom extra fields (flattened to top level)
An empty body section's key is omitted.

The body fields are the ones ticket_show carries, split the same way, and no
other ## heading becomes one. Headings match on prefix: ## Design Notes opens
design. Which headings open and close each field, and which text reaches no
field, is in the README's Body Fields section.`,
	Example: `  tk query                                       # all tickets as JSONL
  tk query '.status == "open"'                   # filter by field
  tk query '.type == "bug" and .priority <= 1'   # compound filter
  tk query '.title | test("deploy"; "i")'        # regex search
  tk query --all-projects                        # every namespace, IDs qualified`,
	Args: cobra.MaximumNArgs(1),
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().Bool("all-projects", false, "output every namespace in the central store, IDs qualified")
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
	Updated     string            `json:"updated,omitempty"`
	Closed      string            `json:"closed,omitempty"`
	Type        string            `json:"type"`
	Priority    int               `json:"priority"`
	ExternalRef string            `json:"external-ref,omitempty"`
	Parent      string            `json:"parent,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Design      string            `json:"design,omitempty"`
	Acceptance  string            `json:"acceptance_criteria,omitempty"`
	TestResults string            `json:"test_results,omitempty"`
	Notes       []noteJSON        `json:"notes,omitempty"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	DepCargo    map[string]string `json:"dep_cargo,omitempty"`
	Extra       map[string]string `json:"-"`
}

type noteJSON struct {
	Timestamp string `json:"timestamp"`
	Text      string `json:"text"`
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
	j := ticketJSON{
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
	// A zero time is a date the ticket's file does not record: the key is
	// omitted rather than rendered as year 1.
	if !t.Updated.IsZero() {
		j.Updated = t.Updated.UTC().Format("2006-01-02T15:04:05Z")
	}
	if closed := t.ClosedAt(); !closed.IsZero() {
		j.Closed = closed.UTC().Format("2006-01-02T15:04:05Z")
	}
	return j
}

// toQueryJSON adds the body fields, under the keys and in the shape ticket_show
// carries them. `tk frontier --json` stays on toTicketJSON: its rows are
// metadata, like ticket_list's.
func toQueryJSON(t *ticket.Ticket) ticketJSON {
	j := toTicketJSON(t)
	j.Description, j.Design, j.Acceptance, j.TestResults = ticket.BodySections(t.Body)
	for _, n := range t.Notes {
		j.Notes = append(j.Notes, noteJSON{
			Timestamp: n.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			Text:      n.Text,
		})
	}
	return j
}

func runQuery(cmd *cobra.Command, args []string) error {
	tickets, err := listingFor(cmd)
	if err != nil {
		return err
	}

	var lines []string
	for _, t := range tickets {
		data, err := json.Marshal(toQueryJSON(t))
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
