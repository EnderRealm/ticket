package mcp

import (
	"context"
	"errors"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The stable prefixes of the three refusals a caller has to tell apart from a
// text result: a stale precondition is re-read and decided again, an action
// conflict is a bug in the caller's action identity, and a refused transition
// is not retried at all.
const (
	preconditionConflictPrefix = "precondition conflict: "
	actionConflictPrefix       = "action conflict: "
	transitionRefusedPrefix    = "transition not permitted: "
)

// actionResultJSON is the response of both action tools: the ticket as the
// store holds it after the action, the receipt recorded for it, and whether
// the call replayed an action already recorded rather than applying one.
type actionResultJSON struct {
	Ticket   ticketJSON           `json:"ticket"`
	Receipt  ticket.ActionReceipt `json:"receipt"`
	Replayed bool                 `json:"replayed"`
}

// actionError maps the protocol's sentinels onto the stable prefixes and
// leaves every other failure framed by what was attempted.
func actionError(what string, err error) (*mcp.CallToolResult, error) {
	switch {
	case errors.Is(err, ticket.ErrConflict):
		return errResult("%s%v", preconditionConflictPrefix, err)
	case errors.Is(err, ticket.ErrActionConflict):
		return errResult("%s%v", actionConflictPrefix, err)
	case errors.Is(err, ticket.ErrTransitionNotPermitted):
		return errResult("%s%v", transitionRefusedPrefix, err)
	}
	return errResult("failed to %s: %v", what, err)
}

type applyActionArgs struct {
	ID           string `json:"id" jsonschema:"ticket ID"`
	ActionID     string `json:"action_id" jsonschema:"the caller's stable identity for this action; the same id sent again with the same note and transition returns the recorded outcome instead of applying the action twice"`
	Precondition string `json:"precondition,omitempty" jsonschema:"the opaque precondition ticket_show reported for the state this action was decided on; omit to make no claim about the state"`
	Note         string `json:"note" jsonschema:"the evidence note to append"`
	Transition   string `json:"transition,omitempty" jsonschema:"optional status transition: reopen (sets status to open; refused on an epic)"`
	Source       string `json:"source,omitempty" jsonschema:"who is applying this; defaults to the MCP client name"`
}

func registerApplyAction(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name: "ticket_apply_action",
		Description: "Apply one observer action to an existing ticket: append an evidence note, optionally reopen it, and record a receipt — all in one write, so no partial action can land. " +
			"`action_id` is the caller's stable identity for the action: repeating the same id with the same note and transition (after a lost response, a retry or a restart) returns the recorded receipt with `replayed: true` and writes nothing, whatever the ticket's state is by then; the same id with different content is refused. " +
			"`precondition` is the opaque token ticket_show reported: when given, the action lands only if the ticket is still in that state, otherwise it is refused and the ticket is left unchanged. The permitted transitions are `reopen` only. " +
			"The three refusals carry stable prefixes so they can be told apart from the text: `precondition conflict:` (re-read the ticket and decide again), `action conflict:` (the action id was already used for other content — use a new id), `transition not permitted:`. " +
			"`source` is a declared label attributing the write, recorded on the receipt and in the mutation log; it does not authenticate anyone. " +
			"Receipts live in the ticket's append-only `actions` block and replicate with it. Retries are deduplicated on one machine; two machines recording under one action id surface as a merge conflict in that block.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args applyActionArgs) (*mcp.CallToolResult, any, error) {
		res, err := ticket.ApplyAction(ticket.WithSource(store, sourceFor(req, args.Source)), ticket.ActionRequest{
			ID:           args.ID,
			ActionID:     args.ActionID,
			Precondition: args.Precondition,
			Note:         args.Note,
			Transition:   ticket.Transition(args.Transition),
		})
		if err != nil {
			r, _ := actionError("apply action", err)
			return r, nil, nil
		}
		r, err := jsonResult(actionResultJSON{Ticket: toJSON(res.Ticket), Receipt: res.Receipt, Replayed: res.Replayed})
		return r, nil, err
	})
}

type discoverArgs struct {
	FindingKey  string   `json:"finding_key" jsonschema:"the stable, project-scoped key of the finding; a retry or a concurrent call under the same key with the same content returns the ticket it already created"`
	Title       string   `json:"title" jsonschema:"ticket title"`
	Description string   `json:"description,omitempty" jsonschema:"description text"`
	Design      string   `json:"design,omitempty" jsonschema:"design notes"`
	Acceptance  string   `json:"acceptance,omitempty" jsonschema:"acceptance criteria"`
	Type        string   `json:"type,omitempty" jsonschema:"ticket type: bug, feature, epic (default: feature)"`
	Priority    *FlexInt `json:"priority,omitempty" jsonschema:"priority 0-4, 0=highest (default: 2)"`
	Parent      string   `json:"parent,omitempty" jsonschema:"parent epic ID: an epic in the same project, or a qualified project/id naming an epic in another namespace once the catalog requires cross-project-parents"`
	Tags        string   `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	Project     string   `json:"project,omitempty" jsonschema:"destination namespace in multi-project mode (namespaces the ticket ID): a registered project, or _root for an idea with no repository yet"`
	Repo        string   `json:"repo,omitempty" jsonschema:"registered project name or path to repo root"`
	Source      string   `json:"source,omitempty" jsonschema:"who is filing this; defaults to the MCP client name"`
}

func registerDiscover(server *mcp.Server, store ticket.Store, defaultProject string) {
	addFlexTool(server, &mcp.Tool{
		Name: "ticket_discover",
		Description: "File a discovery: create a ticket under a stable `finding_key`, or return the ticket that key already created. The destination (`project`, `repo` or the server's default project) and the content arguments are ticket_create's; the key is scoped to the destination project, so the same key in another project is a different finding. " +
			"Under the key, a retry or a concurrent call with the same title, description, design, acceptance, type, priority, parent and tags returns the existing ticket with `replayed: true` and creates nothing; different content under the same key is refused with the `action conflict:` prefix, naming the existing ticket, and creates nothing. " +
			"Deduplication is exact-content by key — finding that a new discovery describes a problem an existing ticket already tracks is the caller's job (ticket_search). " +
			"`source` is a declared label attributing the write, recorded on the receipt and in the mutation log; it does not authenticate anyone. " +
			"The receipt lives in the created ticket's append-only `actions` block. Retries are deduplicated on one machine; two machines filing under one key each create their own file, and those merge cleanly when the store syncs, so once more than one ticket in the project carries the key every call under it is refused with the `action conflict:` prefix naming the claimants until an operator keeps one. A description, design or acceptance value that ends in a tool-call envelope fragment is refused rather than stored.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args discoverArgs) (*mcp.CallToolResult, any, error) {
		content := ticketContent{
			Title:       args.Title,
			Description: args.Description,
			Design:      args.Design,
			Acceptance:  args.Acceptance,
			Type:        args.Type,
			Priority:    args.Priority,
			Parent:      args.Parent,
			Tags:        args.Tags,
		}
		if r := content.refusal(); r != nil {
			return r, nil, nil
		}
		dst, r := resolveDestination(store, sourceFor(req, args.Source), args.Repo, args.Project)
		if r != nil {
			return r, nil, nil
		}
		t := content.ticket()
		if r := dst.qualify(t, store, args.Project, defaultProject); r != nil {
			return r, nil, nil
		}

		res, err := ticket.CreateDiscovery(dst.store, ticket.DiscoveryRequest{Ticket: t, FindingKey: args.FindingKey})
		if err != nil {
			r, _ := actionError("create discovery", dst.frame(err))
			return r, nil, nil
		}
		dst.settle(res.Ticket)

		j := toJSON(res.Ticket)
		j.UnregisteredWarning = dst.unregisteredWarning
		if !res.Replayed {
			content.warn(&j, res.Ticket)
		}
		r, err = jsonResult(actionResultJSON{Ticket: j, Receipt: res.Receipt, Replayed: res.Replayed})
		return r, nil, err
	})
}
