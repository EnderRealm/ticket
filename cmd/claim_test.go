package cmd

import (
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

// runClaiming runs tk claim or tk release against a fresh flag set parsed
// from args.
func runClaiming(t *testing.T, name string, args ...string) error {
	t.Helper()
	cmd := &cobra.Command{}
	run := runClaim
	if name == "claim" {
		registerClaimFlags(cmd)
	} else {
		registerReleaseFlags(cmd)
		run = runRelease
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return run(cmd, cmd.Flags().Args())
}

func TestClaimCommands(t *testing.T) {
	store := centralStore(t, "cl")
	mkCentral(t, store, "cl-1", ticket.TypeFeature, ticket.StatusReady, "")
	holder := func() string {
		t.Helper()
		if c := ticket.ClaimOf(getTicket(t, store, "cl-1")); c != nil {
			return c.Owner
		}
		return ""
	}

	if err := runClaiming(t, "claim", "cl-1", "--owner", "alice", "--ttl", "30m"); err != nil {
		t.Fatal(err)
	}
	c := ticket.ClaimOf(getTicket(t, store, "cl-1"))
	if c == nil || c.Owner != "alice" || c.Expires.Sub(c.ClaimedAt).Minutes() != 30 {
		t.Fatalf("claim = %+v, want alice's for 30m", c)
	}
	if out := captureShow(t, store, "cl-1", false); !strings.Contains(out, "claim: "+getTicket(t, store, "cl-1").Extra[ticket.ClaimField]+"  # live") {
		t.Errorf("tk show does not mark the claim live:\n%s", out)
	}
	if row := captureQuery(t)["cl-1"]; row["claim"].(map[string]any)["live"] != true {
		t.Errorf("tk query claim = %v", row["claim"])
	}

	err := runClaiming(t, "claim", "cl-1", "--owner", "bob")
	if err == nil || !strings.HasPrefix(err.Error(), "claim conflict: ") || !strings.Contains(err.Error(), "alice") {
		t.Errorf("bob's claim: err = %v, want a claim conflict naming alice", err)
	}
	if err := runClaiming(t, "claim", "cl-1", "--owner", "bob", "--takeover"); err != nil {
		t.Fatal(err)
	}
	if got := holder(); got != "bob" {
		t.Errorf("after takeover held by %q", got)
	}

	if err := runClaiming(t, "release", "cl-1", "--owner", "alice"); err == nil || !strings.HasPrefix(err.Error(), "claim conflict: ") {
		t.Errorf("alice's release: err = %v, want a claim conflict", err)
	}
	if err := runClaiming(t, "release", "cl-1", "--owner", "bob"); err != nil {
		t.Fatal(err)
	}
	if got := holder(); got != "" {
		t.Errorf("after release held by %q", got)
	}
	if err := runClaiming(t, "release", "cl-1", "--owner", "bob"); err != nil {
		t.Errorf("release with nothing held: %v", err)
	}

	for _, set := range []string{"claim=x", "claim="} {
		if err := runEditWith(t, "cl-1", "set", set); err == nil || !strings.Contains(err.Error(), "is reserved") {
			t.Errorf("tk edit --set %s: err = %v, want the key refused as reserved", set, err)
		}
	}

	// Done through tk edit clears it.
	if err := runClaiming(t, "claim", "cl-1", "--owner", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := runEditWith(t, "cl-1", "status", "done"); err != nil {
		t.Fatal(err)
	}
	if got := holder(); got != "" {
		t.Errorf("done ticket held by %q", got)
	}
}
