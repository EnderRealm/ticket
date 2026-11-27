package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func claimStore(t *testing.T, ids ...string) *FileStore {
	t.Helper()
	store := NewFileStore(t.TempDir())
	for _, id := range ids {
		if err := store.Create(sampleTicket(id)); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func mustClaim(t *testing.T, store Store, id, owner string, ttl time.Duration, takeover bool) *ClaimResult {
	t.Helper()
	res, err := ClaimTicket(store, id, owner, ttl, takeover)
	if err != nil {
		t.Fatalf("claim %s by %s: %v", id, owner, err)
	}
	return res
}

func readFile(t *testing.T, store *FileStore, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.Dir, id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// plantClaim stores c on id as it is, so a test can hold a claim that has
// already expired.
func plantClaim(t *testing.T, store Store, id string, c Claim) {
	t.Helper()
	if _, err := Mutate(store, id, func(t *Ticket) error {
		t.Extra[ClaimField] = c.storedValue()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func wantConflict(t *testing.T, err error, held Claim) {
	t.Helper()
	var conflict *ClaimConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a claim conflict", err)
	}
	msg := conflict.Error()
	if !strings.HasPrefix(msg, ClaimConflictPrefix) {
		t.Errorf("message %q does not open with %q", msg, ClaimConflictPrefix)
	}
	for _, want := range []string{held.Owner, held.Machine, held.Expires.Format(time.RFC3339)} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not name %q", msg, want)
		}
	}
}

func TestClaimSetsRenewsAndRefusesAnotherOwner(t *testing.T) {
	store := claimStore(t, "cl-1")
	host, _ := os.Hostname()

	before := time.Now().UTC().Truncate(time.Second)
	first := mustClaim(t, store, "cl-1", "alice", DefaultClaimTTL, false)
	c := first.Claim
	if c.Owner != "alice" || c.Machine != host || first.Replaced != nil {
		t.Errorf("claim = %+v replaced %+v", c, first.Replaced)
	}
	if c.ClaimedAt.Before(before) || c.Expires.Sub(c.ClaimedAt) != DefaultClaimTTL {
		t.Errorf("claimed at %s expires %s, want a 2h lease from %s", c.ClaimedAt, c.Expires, before)
	}
	got, err := store.Get("cl-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored := ClaimOf(got); stored == nil || *stored != c {
		t.Errorf("stored claim = %+v, want %+v", stored, c)
	}

	file := readFile(t, store, "cl-1")
	_, err = ClaimTicket(store, "cl-1", "bob", time.Hour, false)
	wantConflict(t, err, c)
	if after := readFile(t, store, "cl-1"); after != file {
		t.Errorf("a refused claim rewrote the file:\n%s\nwas:\n%s", after, file)
	}

	time.Sleep(1100 * time.Millisecond)
	renewed := mustClaim(t, store, "cl-1", "alice", 3*time.Hour, false)
	if renewed.Replaced != nil || renewed.Claim.ClaimedAt != c.ClaimedAt || !renewed.Claim.Expires.After(c.Expires) {
		t.Errorf("renewal = %+v replaced %+v, want the lease extended from %s", renewed.Claim, renewed.Replaced, c.ClaimedAt)
	}
}

func TestClaimReplacesAnExpiredClaim(t *testing.T) {
	store := claimStore(t, "cl-1")
	stale := Claim{Owner: "alice", Machine: "gone.local", ClaimedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), Expires: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
	plantClaim(t, store, "cl-1", stale)

	res := mustClaim(t, store, "cl-1", "bob", time.Hour, false)
	if res.Replaced == nil || *res.Replaced != stale {
		t.Errorf("replaced = %+v, want %+v", res.Replaced, stale)
	}
	if res.Claim.Owner != "bob" || !res.Claim.LiveAt(time.Now()) {
		t.Errorf("claim = %+v, want bob's live claim", res.Claim)
	}
}

func TestClaimTakeoverAndRelease(t *testing.T) {
	store := claimStore(t, "cl-1")
	alice := mustClaim(t, store, "cl-1", "alice", time.Hour, false).Claim

	bob := mustClaim(t, store, "cl-1", "bob", time.Hour, true)
	if bob.Replaced == nil || *bob.Replaced != alice || bob.Claim.Owner != "bob" {
		t.Errorf("takeover = %+v replaced %+v, want alice's claim replaced", bob.Claim, bob.Replaced)
	}

	// alice's release does not touch bob's live claim.
	file := readFile(t, store, "cl-1")
	_, err := ReleaseTicket(store, "cl-1", "alice")
	wantConflict(t, err, bob.Claim)
	if after := readFile(t, store, "cl-1"); after != file {
		t.Errorf("a refused release rewrote the file")
	}

	res, err := ReleaseTicket(store, "cl-1", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if res.Released == nil || *res.Released != bob.Claim || ClaimOf(res.Ticket) != nil {
		t.Errorf("release = %+v, ticket claim %+v", res.Released, ClaimOf(res.Ticket))
	}

	// Nothing left to release: no error, and nothing written.
	file = readFile(t, store, "cl-1")
	res, err = ReleaseTicket(store, "cl-1", "bob")
	if err != nil || res.Released != nil || res.Ticket == nil {
		t.Errorf("second release = %+v, %v", res, err)
	}
	if after := readFile(t, store, "cl-1"); after != file {
		t.Errorf("a release with nothing to clear rewrote the file")
	}

	// Another owner's expired claim needs no release and is left alone.
	stale := Claim{Owner: "carol", Machine: "gone.local", ClaimedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC), Expires: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)}
	plantClaim(t, store, "cl-1", stale)
	if res, err = ReleaseTicket(store, "cl-1", "bob"); err != nil || res.Released != nil {
		t.Errorf("release over an expired claim = %+v, %v", res, err)
	}
	if got := ClaimOf(res.Ticket); got == nil || *got != stale {
		t.Errorf("claim after = %+v, want carol's expired one kept", got)
	}
}

func TestClaimRefusesBadArguments(t *testing.T) {
	store := claimStore(t, "cl-1")
	for _, c := range []struct {
		owner string
		ttl   time.Duration
	}{
		{"", time.Hour},
		{"a b", time.Hour},
		{"-a", time.Hour},
		{"a#b", time.Hour},
		{"a\nparent: x", time.Hour},
		{"alice", 0},
		{"alice", -time.Hour},
		{"alice", 500 * time.Millisecond},
	} {
		if _, err := ClaimTicket(store, "cl-1", c.owner, c.ttl, false); err == nil {
			t.Errorf("claim by %q for %s was accepted", c.owner, c.ttl)
		}
	}
	if _, err := ReleaseTicket(store, "cl-1", "a b"); err == nil {
		t.Error("release by an invalid owner was accepted")
	}
	got, err := store.Get("cl-1")
	if err != nil {
		t.Fatal(err)
	}
	if ClaimOf(got) != nil {
		t.Errorf("a refused claim was stored: %+v", ClaimOf(got))
	}
}

func TestConcurrentClaimsYieldOneHolder(t *testing.T) {
	store := claimStore(t, "cl-1")
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ClaimTicket(store, "cl-1", fmt.Sprintf("owner-%d", i), time.Hour, false)
		}()
	}
	wg.Wait()

	var winners []string
	for i, err := range errs {
		var conflict *ClaimConflictError
		switch {
		case err == nil:
			winners = append(winners, fmt.Sprintf("owner-%d", i))
		case !errors.As(err, &conflict):
			t.Errorf("owner-%d: %v, want success or a claim conflict", i, err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("winners = %v, want exactly one", winners)
	}
	got, err := store.Get("cl-1")
	if err != nil {
		t.Fatal(err)
	}
	if c := ClaimOf(got); c == nil || c.Owner != winners[0] {
		t.Errorf("stored claim = %+v, want %s's", c, winners[0])
	}
}

func TestLiveClaimLeavesReadyAndFrontier(t *testing.T) {
	store := claimStore(t, "cl-1")
	in := func(what string, want bool) {
		t.Helper()
		ready, err := ReadyTickets(store)
		if err != nil {
			t.Fatal(err)
		}
		open, err := ReadyTicketsOpen(store)
		if err != nil {
			t.Fatal(err)
		}
		frontier, err := FrontierTickets(store)
		if err != nil {
			t.Fatal(err)
		}
		for name, list := range map[string][]*Ticket{"ready": ready, "ready open": open, "frontier": frontier} {
			if got := slices.ContainsFunc(list, func(t *Ticket) bool { return t.ID == "cl-1" }); got != want {
				t.Errorf("%s: in %s = %v, want %v", what, name, got, want)
			}
		}
	}
	in("unclaimed", true)
	mustClaim(t, store, "cl-1", "alice", time.Hour, false)
	in("claimed", false)
	if _, err := ReleaseTicket(store, "cl-1", "alice"); err != nil {
		t.Fatal(err)
	}
	in("released", true)
	plantClaim(t, store, "cl-1", Claim{Owner: "alice", Machine: "m", ClaimedAt: time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second), Expires: time.Now().Add(-time.Hour).UTC().Truncate(time.Second)})
	in("expired", true)
}

// Every write that leaves a ticket done or closed drops its claim — an edit
// through Update, a land, a mark — and one that leaves it testing keeps it.
func TestFinishingATicketClearsItsClaim(t *testing.T) {
	store := claimStore(t, "cl-edit", "cl-closed", "cl-land", "cl-testing")
	generic := sampleTicket("cl-mark")
	generic.WorkflowKind = WorkflowGeneric
	if err := store.Create(generic); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cl-edit", "cl-closed", "cl-land", "cl-testing", "cl-mark"} {
		mustClaim(t, store, id, "alice", time.Hour, false)
	}
	claimed := func(id string) bool {
		t.Helper()
		got, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		return ClaimOf(got) != nil || strings.Contains(readFile(t, store, id), ClaimField+":")
	}

	for id, status := range map[string]Status{"cl-edit": StatusDone, "cl-closed": StatusClosed, "cl-testing": StatusTesting} {
		got, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		got.Status = status
		if err := store.Update(got); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ApplyAction(store, ActionRequest{ID: "cl-land", ActionID: "land-1", Note: "Landed.", Transition: TransitionLand, Commit: strings.Repeat("a", 40)}); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyAction(store, ActionRequest{ID: "cl-mark", ActionID: "mark-1", Note: "Done by hand.", Transition: TransitionMark, Mark: MarkNotNeeded}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"cl-edit": false, "cl-closed": false, "cl-land": false, "cl-mark": false, "cl-testing": true} {
		if got := claimed(id); got != want {
			t.Errorf("%s claimed = %v, want %v", id, got, want)
		}
	}
}

func TestClaimIsReservedAndNamedInMutationLog(t *testing.T) {
	if err := ValidateExtraKey(ClaimField); err == nil {
		t.Error("ValidateExtraKey accepted the claim key")
	}
	prior := &Ticket{Extra: map[string]string{"env": "prod"}}
	next := &Ticket{Extra: map[string]string{"env": "prod", ClaimField: "alice m 2026-01-01T08:00:00Z 2026-01-01T10:00:00Z"}}
	if got := changedFields(prior, next); !slices.Equal(got, []string{ClaimField}) {
		t.Errorf("changed fields = %v", got)
	}
	if got := ExtraFields(next); !reflect.DeepEqual(got, map[string]string{"env": "prod"}) {
		t.Errorf("extra fields = %v, want the claim left out", got)
	}
}

// A hand-edited value that is not a claim is none, and is not lost.
func TestMalformedClaimIsNoneAndKept(t *testing.T) {
	for _, v := range []string{
		"alice",
		"alice m 2026-01-01T08:00:00Z",
		"alice m yesterday 2026-01-01T10:00:00Z",
		"alice m 2026-01-01T10:00:00Z 2026-01-01T08:00:00Z",
		"alice  m 2026-01-01T08:00:00Z 2026-01-01T10:00:00Z",
	} {
		tk, err := Parse(strings.NewReader("---\nid: a-1\nstatus: ready\ntype: feature\npriority: 2\nclaim: " + yamlScalar(v) + "\n---\n# T\n"))
		if err != nil {
			t.Fatal(err)
		}
		if c := ClaimOf(tk); c != nil {
			t.Errorf("%q reads as claim %+v", v, c)
		}
		if tk.Extra[ClaimField] != v {
			t.Errorf("%q stored as %q", v, tk.Extra[ClaimField])
		}
	}
}

// A tk released before claims keeps the claim as an extra field, and writes
// it back as one: the ticket still holds the same claim afterwards.
func TestClaimSurvivesUnawareWrite(t *testing.T) {
	for name, scalar := range map[string]func(string) string{
		"unquoted writer, before v8.6.0": func(v string) string { return v },
		"quoting writer":                 yamlScalar,
	} {
		t.Run(name, func(t *testing.T) {
			store := NewFileStore(t.TempDir())
			tk := &Ticket{ID: "un-1", Status: StatusOpen, Type: TypeFeature, Created: time.Now(), Title: "Before", Body: "Why.\n"}
			if err := store.Create(tk); err != nil {
				t.Fatal(err)
			}
			res, err := ClaimTicket(store, "un-1", "work:session_01@host/a.b-c", time.Hour, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, title := range []string{"Once", "Twice"} {
				unawareWrite(t, filepath.Join(store.Dir, "un-1.md"), title, scalar)
				got, err := store.Get("un-1")
				if err != nil {
					t.Fatal(err)
				}
				if got.Title != title {
					t.Errorf("title %q, want the unaware write's", got.Title)
				}
				if c := ClaimOf(got); c == nil || *c != res.Claim {
					t.Errorf("claim after %q = %+v, want %+v", title, c, res.Claim)
				}
			}
		})
	}
}
