package mcp_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// wantClaim checks the claim a ticket object reports: absent when owner is "",
// else that owner's, live or not.
func wantClaim(t *testing.T, what string, got map[string]any, owner string, live bool) {
	t.Helper()
	c, ok := got["claim"].(map[string]any)
	if owner == "" {
		if got["claim"] != nil {
			t.Errorf("%s: claim = %v, want none", what, got["claim"])
		}
		return
	}
	if !ok || c["owner"] != owner || c["live"] != live || c["machine"] == "" || c["expires"] == "" || c["claimed_at"] == "" {
		t.Errorf("%s: claim = %v, want %s's with live %v", what, got["claim"], owner, live)
	}
}

// listRow returns the row ticket_list reports for id.
func listRow(t *testing.T, session *mcp.ClientSession, id string) map[string]any {
	t.Helper()
	for _, row := range callObject(t, session, "ticket_list", map[string]any{})["tickets"].([]any) {
		if row := row.(map[string]any); row["id"] == id {
			return row
		}
	}
	t.Fatalf("ticket_list does not list %s", id)
	return nil
}

func wantSchedulable(t *testing.T, session *mcp.ClientSession, what, id string, want bool) {
	t.Helper()
	if got := slices.Contains(readyIDs(t, session, map[string]any{}), id); got != want {
		t.Errorf("%s: in ready = %v, want %v", what, got, want)
	}
	if got := slices.Contains(frontierIDs(t, session, map[string]any{}), id); got != want {
		t.Errorf("%s: in frontier = %v, want %v", what, got, want)
	}
}

func TestClaimLifecycle(t *testing.T) {
	session, dir := testServerDir(t)
	id := createTicketID(t, session, map[string]any{"title": "Worked", "type": "feature"})
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "status": "ready"})
	wantSchedulable(t, session, "unclaimed", id, true)

	res := callObject(t, session, "ticket_claim", map[string]any{"id": id, "owner": "alice", "ttl": "90m"})
	wantClaim(t, "ticket_claim claim", map[string]any{"claim": res["claim"]}, "alice", true)
	wantClaim(t, "ticket_claim ticket", res["ticket"].(map[string]any), "alice", true)
	if res["replaced"] != nil {
		t.Errorf("replaced = %v, want none", res["replaced"])
	}
	claim := res["claim"].(map[string]any)
	claimedAt, _ := time.Parse(time.RFC3339, claim["claimed_at"].(string))
	expires, _ := time.Parse(time.RFC3339, claim["expires"].(string))
	if expires.Sub(claimedAt) != 90*time.Minute {
		t.Errorf("lease %s to %s, want 90m", claimedAt, expires)
	}
	wantClaim(t, "ticket_show", callObject(t, session, "ticket_show", map[string]any{"id": id}), "alice", true)
	wantClaim(t, "ticket_list", listRow(t, session, id), "alice", true)
	wantSchedulable(t, session, "claimed", id, false)

	text := errorText(t, callTool(t, session, "ticket_claim", map[string]any{"id": id, "owner": "bob"}), "bob's claim over alice's")
	if !strings.HasPrefix(text, "claim conflict:") || !strings.Contains(text, "alice") || !strings.Contains(text, claim["expires"].(string)) {
		t.Errorf("refusal = %q", text)
	}

	res = callObject(t, session, "ticket_claim", map[string]any{"id": id, "owner": "bob", "takeover": true, "source": "runner"})
	if r, _ := res["replaced"].(map[string]any); r["owner"] != "alice" {
		t.Errorf("takeover replaced = %v, want alice's claim", res["replaced"])
	}
	wantClaim(t, "taken over", callObject(t, session, "ticket_show", map[string]any{"id": id}), "bob", true)

	text = errorText(t, callTool(t, session, "ticket_release", map[string]any{"id": id, "owner": "alice"}), "alice's release of bob's claim")
	if !strings.HasPrefix(text, "claim conflict:") {
		t.Errorf("refusal = %q", text)
	}
	wantClaim(t, "after a refused release", callObject(t, session, "ticket_show", map[string]any{"id": id}), "bob", true)

	res = callObject(t, session, "ticket_release", map[string]any{"id": id, "owner": "bob"})
	if r, _ := res["released"].(map[string]any); r["owner"] != "bob" {
		t.Errorf("released = %v, want bob's claim", res["released"])
	}
	wantClaim(t, "released", res["ticket"].(map[string]any), "", false)
	wantSchedulable(t, session, "released", id, true)
	if res = callObject(t, session, "ticket_release", map[string]any{"id": id, "owner": "bob"}); res["released"] != nil {
		t.Errorf("a second release released %v", res["released"])
	}

	// An expired claim is reported as not live and no longer hides the ticket.
	store := ticket.NewFileStore(dir)
	if _, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
		t.Extra[ticket.ClaimField] = "carol gone.local 2026-01-01T08:00:00Z 2026-01-01T10:00:00Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantClaim(t, "expired ticket_show", callObject(t, session, "ticket_show", map[string]any{"id": id}), "carol", false)
	wantClaim(t, "expired ticket_list", listRow(t, session, id), "carol", false)
	wantSchedulable(t, session, "expired", id, true)
	res = callObject(t, session, "ticket_claim", map[string]any{"id": id, "owner": "bob"})
	if r, _ := res["replaced"].(map[string]any); r["owner"] != "carol" || r["expires"] != "2026-01-01T10:00:00Z" {
		t.Errorf("replaced = %v, want carol's stale claim", res["replaced"])
	}

	// Advisory: an edit by anyone lands on a claimed ticket, and done clears it.
	tk := callObject(t, session, "ticket_edit", map[string]any{"id": id, "status": "done"})
	wantClaim(t, "done", tk, "", false)
	wantClaim(t, "done ticket_show", callObject(t, session, "ticket_show", map[string]any{"id": id}), "", false)
}

func TestLandingClearsClaim(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Landed", "type": "feature"})
	callObject(t, session, "ticket_claim", map[string]any{"id": id, "owner": "alice"})
	res := callObject(t, session, "ticket_apply_action", map[string]any{"id": id, "action_id": "land-1", "note": "Landed.", "transition": "land", "commit": strings.Repeat("b", 40)})
	tk := res["ticket"].(map[string]any)
	if tk["status"] != "done" {
		t.Fatalf("status = %v, want done", tk["status"])
	}
	wantClaim(t, "landed", tk, "", false)
}

// Through the central store the refusal still opens with its prefix, and the
// stored key cannot be written as an extra field.
func TestClaimConflictPrefixOnCentralStore(t *testing.T) {
	session, _ := testCentralServer(t, "warp")
	id := createTicketID(t, session, map[string]any{"title": "Worked", "type": "feature", "project": "warp"})
	callObject(t, session, "ticket_claim", map[string]any{"id": id, "owner": "alice"})

	text := errorText(t, callTool(t, session, "ticket_claim", map[string]any{"id": id, "owner": "bob"}), "bob's claim")
	if !strings.HasPrefix(text, "claim conflict: "+id+" ") {
		t.Errorf("refusal = %q", text)
	}
	text = errorText(t, callTool(t, session, "ticket_edit", map[string]any{"id": id, "set": map[string]any{ticket.ClaimField: "x"}}), "setting claim as an extra")
	if !strings.Contains(text, "is reserved") {
		t.Errorf("refusal = %q", text)
	}
	for _, c := range []map[string]any{
		{"id": id, "owner": "bob", "ttl": "soon"},
		{"id": id, "owner": "bob", "ttl": "0s"},
		{"id": id, "owner": "b o b"},
	} {
		errorText(t, callTool(t, session, "ticket_claim", c), "bad claim arguments")
	}
	wantClaim(t, "after the refusals", callObject(t, session, "ticket_show", map[string]any{"id": id}), "alice", true)
}
