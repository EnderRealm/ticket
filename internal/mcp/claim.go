package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type claimArgs struct {
	ID       string    `json:"id" jsonschema:"ticket ID"`
	Owner    string    `json:"owner" jsonschema:"the label of whoever holds the claim, e.g. a session; letters, digits, '.', '_', '-', ':', '@' and '/', starting with a letter or digit"`
	TTL      string    `json:"ttl,omitempty" jsonschema:"how long the claim lasts from now, as a Go duration such as 90m or 2h (default 2h, at least 1s)"`
	Takeover *FlexBool `json:"takeover,omitempty" jsonschema:"replace a live claim another owner holds instead of being refused"`
	Source   string    `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

type releaseArgs struct {
	ID     string `json:"id" jsonschema:"ticket ID"`
	Owner  string `json:"owner" jsonschema:"the owner whose claim is released"`
	Source string `json:"source,omitempty" jsonschema:"who is making this change; defaults to the MCP client name"`
}

// claimResultJSON is ticket_claim's response: the ticket as written, the claim
// it holds, and the claim that one replaced.
type claimResultJSON struct {
	Ticket   ticketJSON         `json:"ticket"`
	Claim    ticket.ClaimReport `json:"claim"`
	Replaced *ticket.Claim      `json:"replaced,omitempty"`
}

// releaseResultJSON is ticket_release's response: the ticket as it stands and
// the claim released, absent when there was none of the owner's to release.
type releaseResultJSON struct {
	Ticket   ticketJSON    `json:"ticket"`
	Released *ticket.Claim `json:"released,omitempty"`
}

// claimError surfaces a claim refusal as its own message, which opens with the
// stable prefix whatever store wrapped it, and frames every other failure by
// what was attempted.
func claimError(what string, err error) (*mcp.CallToolResult, error) {
	var conflict *ticket.ClaimConflictError
	if errors.As(err, &conflict) {
		return errResult("%v", conflict)
	}
	return errResult("failed to %s: %v", what, err)
}

func registerClaim(server *mcp.Server, store ticket.Store) {
	addFlexTool(server, &mcp.Tool{
		Name: "ticket_claim",
		Description: "Claim a ticket: a lease saying `owner` is working it now, on this machine, until `ttl` from now (default 2h). A claim is live while now is before its `expires`; one whose owner died stops counting by itself. " +
			"The owner's own live claim is renewed. Another owner's live claim refuses the call with the stable prefix `claim conflict:` naming that owner, its machine and its expiry, and leaves the ticket unchanged; `takeover: true` replaces it. An expired claim is replaced, and `replaced` names it, as it names a live claim taken over. " +
			"Advisory: nothing else is refused on a claimed ticket. ticket_ready and ticket_frontier leave out a ticket under a live claim, every ticket row and ticket_show report `claim` with a `live` flag, and moving a ticket to done or closed clears it. Returns `ticket`, `claim` and `replaced`.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args claimArgs) (*mcp.CallToolResult, any, error) {
		ttl := ticket.DefaultClaimTTL
		if args.TTL != "" {
			var err error
			if ttl, err = time.ParseDuration(args.TTL); err != nil {
				r, _ := errResult("invalid ttl %q: %v", args.TTL, err)
				return r, nil, nil
			}
		}
		takeover := args.Takeover != nil && bool(*args.Takeover)
		res, err := ticket.ClaimTicket(ticket.WithSource(store, sourceFor(req, args.Source)), args.ID, args.Owner, ttl, takeover)
		if err != nil {
			r, _ := claimError("claim ticket", err)
			return r, nil, nil
		}
		r, err := jsonResult(claimResultJSON{
			Ticket:   toJSON(res.Ticket),
			Claim:    ticket.ClaimReport{Claim: res.Claim, Live: res.Claim.LiveAt(time.Now())},
			Replaced: res.Replaced,
		})
		return r, nil, err
	})

	addFlexTool(server, &mcp.Tool{
		Name:        "ticket_release",
		Description: "Release `owner`'s claim on a ticket (see ticket_claim), live or expired. Another owner's live claim refuses the call with the stable prefix `claim conflict:` and leaves it in place. No claim, or another owner's expired one, is nothing to release: the ticket is left unwritten and `released` is absent. Returns `ticket` and `released`.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args releaseArgs) (*mcp.CallToolResult, any, error) {
		res, err := ticket.ReleaseTicket(ticket.WithSource(store, sourceFor(req, args.Source)), args.ID, args.Owner)
		if err != nil {
			r, _ := claimError("release claim", err)
			return r, nil, nil
		}
		r, err := jsonResult(releaseResultJSON{Ticket: toJSON(res.Ticket), Released: res.Released})
		return r, nil, err
	})
}
