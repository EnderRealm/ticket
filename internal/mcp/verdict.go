package mcp

import (
	"context"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type verdictRecordArgs struct {
	ID       string `json:"id" jsonschema:"ticket ID"`
	SHA      string `json:"sha" jsonschema:"the full 40- or 64-character commit hash the verdict was rendered against; an abbreviation is refused"`
	Class    string `json:"class" jsonschema:"verdict class: live-verified, test-verified, type-check-only, verifier-blocked, verifier-failed"`
	Role     string `json:"role" jsonschema:"worker for a self-report, verifier for an independent check; a verifier row supersedes a worker row on the same sha"`
	Evidence string `json:"evidence" jsonschema:"pointer to what backs the verdict: a test run, a session, a URL"`
	Source   string `json:"source,omitempty" jsonschema:"who is recording this; defaults to the MCP client name"`
}

func registerVerdictRecord(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verdict_record",
		Description: "Record one review verdict on a ticket, keyed by the commit SHA it was rendered against. `sha` must be the full commit hash — staleness is decided by exact comparison against the head a reader supplies, so an abbreviation would read as a verdict about other code. `class` is one of live-verified, test-verified, type-check-only, verifier-blocked, verifier-failed; anything else is refused. Rows are append-only: a correction is a new row, and nothing edits or deletes one.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args verdictRecordArgs) (*mcp.CallToolResult, any, error) {
		// The identity recorded on the row is the attribution every other write
		// tool uses, so who wrote the ticket and who rendered the verdict are the
		// same declared name.
		source := sourceFor(req, args.Source)
		_, row, err := ticket.RecordVerdict(
			ticket.WithSource(store, source),
			args.ID,
			args.SHA,
			ticket.VerdictClass(args.Class),
			ticket.VerdictRole(args.Role),
			args.Evidence,
			source,
		)
		if err != nil {
			r, _ := errResult("failed to record verdict: %v", err)
			return r, nil, nil
		}

		r, err := jsonResult(row)
		return r, nil, err
	})
}

type verdictCurrentArgs struct {
	ID   string `json:"id" jsonschema:"ticket ID"`
	Head string `json:"head" jsonschema:"the full commit hash to judge the ticket's verdict rows against"`
}

// verdictCurrentJSON is the read side of the ledger: the operative verdict at
// the given head, whether it passes, and the rows recorded against some other
// SHA.
type verdictCurrentJSON struct {
	ID      string              `json:"id"`
	Head    string              `json:"head"`
	Current *ticket.VerdictRow  `json:"current"`
	Passes  bool                `json:"passes"`
	Stale   []ticket.VerdictRow `json:"stale"`
}

func registerVerdictCurrent(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_verdict_current",
		Description: "Read a ticket's current review verdict as of a given head commit. A row whose SHA is not that head is a verdict about other code: it is listed in `stale` and never returned as `current`. Among the rows at head, a verifier row supersedes a worker row. `passes` is true only for live-verified and test-verified — verifier-blocked is a verifier that could not run, not a pass, and type-check-only is not proof either. Nothing in tk gates on this; it is a read for orchestrators.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args verdictCurrentArgs) (*mcp.CallToolResult, any, error) {
		t, err := store.Get(args.ID)
		if err != nil {
			r, _ := errResult("ticket not found: %v", err)
			return r, nil, nil
		}
		// Normalized here as well as inside CurrentVerdict, so the response can
		// echo the value the rows were compared against rather than the one the
		// caller spelled — including when no row matches it.
		head, err := ticket.ValidateVerdictSHA(args.Head)
		if err != nil {
			r, _ := errResult("invalid head: %v", err)
			return r, nil, nil
		}
		current, stale, err := ticket.CurrentVerdict(t.Verdicts, head)
		if err != nil {
			r, _ := errResult("failed to read verdicts: %v", err)
			return r, nil, nil
		}

		resp := verdictCurrentJSON{
			ID:      t.ID,
			Head:    head,
			Current: current,
			Passes:  current != nil && current.Class.Passes(),
			Stale:   stale,
		}
		if resp.Stale == nil {
			resp.Stale = []ticket.VerdictRow{}
		}
		r, err := jsonResult(resp)
		return r, nil, err
	})
}
