package ticket

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func readTicketFile(t *testing.T, store *FileStore, id string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.Dir, id+".md"))
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return data
}

func mustApply(t *testing.T, store Store, req ActionRequest) *ActionResult {
	t.Helper()
	res, err := ApplyAction(store, req)
	if err != nil {
		t.Fatalf("ApplyAction %q: %v", req.ActionID, err)
	}
	return res
}

// ─── AC1: the precondition ─────────────────────────────────────────────────

func TestPreconditionRefusesAnInterveningEdit(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("pre-0001")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if (&Ticket{}).Precondition() != "" {
		t.Error("a ticket the caller built carries a precondition")
	}

	read := mustGet(t, store, tk.ID)
	pre := read.Precondition()
	if pre == "" {
		t.Fatal("Get handed back no precondition")
	}
	if again := mustGet(t, store, tk.ID).Precondition(); again != pre {
		t.Errorf("two reads of an unchanged ticket disagree: %s vs %s", pre, again)
	}

	// Another writer lands in between.
	read.Priority = 0
	if err := store.Update(read); err != nil {
		t.Fatalf("intervening Update: %v", err)
	}
	before := readTicketFile(t, store, tk.ID)

	res, err := ApplyAction(store, ActionRequest{ID: tk.ID, ActionID: "obs-1", Precondition: pre, Note: "regressed"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ApplyAction on a stale precondition = %v, %v; want ErrConflict", res, err)
	}
	if !strings.Contains(err.Error(), tk.ID) || !strings.Contains(err.Error(), "Re-read") {
		t.Errorf("conflict %q does not name the ticket and the remedy", err)
	}
	if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
		t.Errorf("a refused action changed the file:\n%s", after)
	}
	got := mustGet(t, store, tk.ID)
	if len(got.Notes) != 0 || len(got.Actions) != 0 {
		t.Errorf("notes = %d, actions = %d after a refused action; want none", len(got.Notes), len(got.Actions))
	}

	// The current precondition lands, and an empty one makes no claim.
	res = mustApply(t, store, ActionRequest{ID: tk.ID, ActionID: "obs-1", Precondition: got.Precondition(), Note: "regressed"})
	if res.Replayed || len(res.Ticket.Notes) != 1 {
		t.Errorf("result = replayed %v, %d notes; want a fresh application with one note", res.Replayed, len(res.Ticket.Notes))
	}
	res = mustApply(t, store, ActionRequest{ID: tk.ID, ActionID: "obs-2", Note: "no claim"})
	if res.Replayed || len(res.Ticket.Notes) != 2 {
		t.Errorf("result = replayed %v, %d notes; want a second application", res.Replayed, len(res.Ticket.Notes))
	}
}

// ─── AC2 + AC3: one write, replayed by id and digest ───────────────────────

func TestApplyActionLandsNoteTransitionAndReceiptTogether(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("act-0001")
	tk.Status = StatusDone
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if mustGet(t, store, tk.ID).Completed.IsZero() {
		t.Fatal("a done ticket carries no completed date")
	}

	req := ActionRequest{ID: tk.ID, ActionID: "obs-2026-09-18-1", Note: "check failed: regression", Transition: TransitionReopen}
	res := mustApply(t, store, req)
	if res.Replayed {
		t.Error("first application reported as replayed")
	}
	got := mustGet(t, store, tk.ID)
	if got.Status != StatusOpen || !got.Completed.IsZero() {
		t.Errorf("status = %s, completed = %v; want open with completed cleared", got.Status, got.Completed)
	}
	if len(got.Notes) != 1 || got.Notes[0].Text != req.Note {
		t.Errorf("notes = %+v, want the evidence note", got.Notes)
	}
	if len(got.Actions) != 1 {
		t.Fatalf("actions = %+v, want one receipt", got.Actions)
	}
	r := got.Actions[0]
	if r != res.Receipt {
		t.Errorf("stored receipt %+v differs from the returned one %+v", r, res.Receipt)
	}
	if r.ID != req.ActionID || r.Kind != ReceiptApply || r.Outcome != "open" || r.Source != SourceHuman {
		t.Errorf("receipt = %+v", r)
	}
	if err := ValidateActionReceipt(r); err != nil {
		t.Errorf("stored receipt does not validate: %v", err)
	}

	// The replay: same id and content, after the ticket moved on and under a
	// stale precondition — the recorded outcome is the answer.
	stale := got.Precondition()
	if _, err := Mutate(store, tk.ID, func(t *Ticket) error { t.Priority = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	before := readTicketFile(t, store, tk.ID)
	req.Precondition = stale
	replay := mustApply(t, store, req)
	if !replay.Replayed || replay.Receipt != r {
		t.Errorf("replay = replayed %v, receipt %+v; want the recorded receipt", replay.Replayed, replay.Receipt)
	}
	if replay.Ticket.ID != tk.ID || replay.Ticket.Status != StatusOpen {
		t.Errorf("replay ticket = %s %s", replay.Ticket.ID, replay.Ticket.Status)
	}
	if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
		t.Errorf("a replay changed the file:\n%s", after)
	}

	// Same id, different content: refused and nothing written.
	changed := req
	changed.Precondition = ""
	changed.Note = "check failed: something else"
	_, err := ApplyAction(store, changed)
	if !errors.Is(err, ErrActionConflict) || !strings.Contains(err.Error(), req.ActionID) {
		t.Errorf("ApplyAction with other content under the same id = %v; want ErrActionConflict naming it", err)
	}
	changed.Note = req.Note
	changed.Transition = TransitionNone
	if _, err := ApplyAction(store, changed); !errors.Is(err, ErrActionConflict) {
		t.Errorf("ApplyAction with another transition under the same id = %v; want ErrActionConflict", err)
	}
	if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
		t.Errorf("a refused conflict changed the file:\n%s", after)
	}
	got = mustGet(t, store, tk.ID)
	if len(got.Notes) != 1 || len(got.Actions) != 1 {
		t.Errorf("notes = %d, actions = %d; want one of each", len(got.Notes), len(got.Actions))
	}
}

func TestApplyActionReplaysAfterRestart(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms, mk("warp/rs-0001", StatusReady))
	req := ActionRequest{ID: "warp/rs-0001", ActionID: "obs-restart", Note: "seen", Transition: TransitionReopen}
	first := mustApply(t, ms, req)
	if first.Ticket.ID != "warp/rs-0001" || first.Ticket.Status != StatusOpen {
		t.Errorf("result ticket = %s %s", first.Ticket.ID, first.Ticket.Status)
	}
	if first.Receipt.Source != SourceHuman {
		t.Errorf("receipt source = %q, want the store's attribution", first.Receipt.Source)
	}

	// A new process over the same store: a fresh MultiStore, and the project
	// store the CLI would build.
	for name, store := range map[string]Store{
		"multistore":    NewMultiStore(filepath.Join(root, "tickets")),
		"project store": nsStore(root, "warp"),
	} {
		id := req.ID
		if name == "project store" {
			id = "rs-0001"
		}
		res := mustApply(t, store, ActionRequest{ID: id, ActionID: req.ActionID, Note: req.Note, Transition: req.Transition})
		if !res.Replayed || res.Receipt != first.Receipt {
			t.Errorf("%s: replay = replayed %v, receipt %+v; want %+v", name, res.Replayed, res.Receipt, first.Receipt)
		}
		if res.Ticket.ID != id {
			t.Errorf("%s: replay ticket id = %s, want %s", name, res.Ticket.ID, id)
		}
	}
	got := mustGet(t, ms, "warp/rs-0001")
	if len(got.Notes) != 1 || len(got.Actions) != 1 {
		t.Errorf("notes = %d, actions = %d after replays; want one of each", len(got.Notes), len(got.Actions))
	}

	// A different writer replaying the same action gets the same receipt: the
	// recorded source is the first writer's.
	res := mustApply(t, WithSource(ms, "shuttle"), req)
	if !res.Replayed || res.Receipt.Source != SourceHuman {
		t.Errorf("replay by another source = replayed %v, source %q", res.Replayed, res.Receipt.Source)
	}
	res = mustApply(t, WithSource(ms, "shuttle"), ActionRequest{ID: req.ID, ActionID: "obs-2", Note: "again"})
	if res.Receipt.Source != "shuttle" {
		t.Errorf("new receipt source = %q, want shuttle", res.Receipt.Source)
	}
}

func TestApplyActionTransitions(t *testing.T) {
	root, ms := centralFixture(t, true)
	mustCreate(t, ms,
		mkEpic("warp/epic-0001", StatusBacklog, ""),
		mkWithParent("warp/open-0002", StatusOpen, "warp/epic-0001"),
		mk("warp/closed-0003", StatusClosed),
	)
	before := snapshotTree(t, filepath.Join(root, "tickets"))

	_, err := ApplyAction(ms, ActionRequest{ID: "warp/open-0002", ActionID: "a", Note: "n", Transition: "close"})
	if !errors.Is(err, ErrTransitionNotPermitted) || !strings.Contains(err.Error(), "close") {
		t.Errorf("unknown transition = %v; want ErrTransitionNotPermitted naming it", err)
	}
	_, err = ApplyAction(ms, ActionRequest{ID: "warp/epic-0001", ActionID: "a", Note: "n", Transition: TransitionReopen})
	if !errors.Is(err, ErrTransitionNotPermitted) || !strings.Contains(err.Error(), "epic") {
		t.Errorf("reopen on an epic = %v; want ErrTransitionNotPermitted", err)
	}
	for _, bad := range []ActionRequest{
		{ID: "warp/open-0002", ActionID: "a", Note: "  "},
		{ID: "warp/open-0002", ActionID: " padded ", Note: "n"},
		{ID: "warp/open-0002", ActionID: "", Note: "n"},
	} {
		if _, err := ApplyAction(ms, bad); err == nil {
			t.Errorf("ApplyAction(%+v) = nil, want a refusal", bad)
		}
	}
	assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
	if got := mustGet(t, ms, "warp/epic-0001"); len(got.Actions) != 0 || len(got.Notes) != 0 {
		t.Errorf("a refused action on the epic wrote %+v %+v", got.Actions, got.Notes)
	}

	// Reopen on an already-open ticket is a no-op transition with a receipt.
	res := mustApply(t, ms, ActionRequest{ID: "warp/open-0002", ActionID: "a", Note: "still open", Transition: TransitionReopen})
	if res.Ticket.Status != StatusOpen || res.Receipt.Outcome != "open" {
		t.Errorf("reopen on open = %s / %s", res.Ticket.Status, res.Receipt.Outcome)
	}
	// And on a closed one it is the transition the protocol is for.
	res = mustApply(t, ms, ActionRequest{ID: "warp/closed-0003", ActionID: "a", Note: "regressed", Transition: TransitionReopen})
	if res.Ticket.Status != StatusOpen || !res.Ticket.Completed.IsZero() {
		t.Errorf("reopen on closed = %s, completed %v", res.Ticket.Status, res.Ticket.Completed)
	}
	// An action with no transition leaves the status alone and records it.
	res = mustApply(t, ms, ActionRequest{ID: "warp/epic-0001", ActionID: "evidence", Note: "epic-level evidence"})
	if res.Receipt.Outcome != string(res.Ticket.Status) || len(res.Ticket.Notes) != 1 {
		t.Errorf("evidence-only action on the epic = %+v", res)
	}
}

// The write is one temp file plus one rename; a failure at that point leaves
// the file as it was, with none of the note, the transition or the receipt.
func TestApplyActionFaultLeavesTheTicketUntouched(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	store, _ := testStore(t)
	tk := sampleTicket("fault-0001")
	tk.Status = StatusDone
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before := readTicketFile(t, store, tk.ID)

	if err := os.Chmod(store.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(store.Dir, 0o755) })

	_, err := ApplyAction(store, ActionRequest{ID: tk.ID, ActionID: "obs", Note: "regressed", Transition: TransitionReopen})
	if err == nil {
		t.Fatal("ApplyAction succeeded with an unwritable store directory")
	}
	if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
		t.Errorf("a failed write changed the file:\n%s", after)
	}
	os.Chmod(store.Dir, 0o755)
	got := mustGet(t, store, tk.ID)
	if got.Status != StatusDone || len(got.Notes) != 0 || len(got.Actions) != 0 {
		t.Errorf("after the failed write: status %s, %d notes, %d actions", got.Status, len(got.Notes), len(got.Actions))
	}
	// The action then lands on retry, as a first application.
	res := mustApply(t, store, ActionRequest{ID: tk.ID, ActionID: "obs", Note: "regressed", Transition: TransitionReopen})
	if res.Replayed || res.Ticket.Status != StatusOpen {
		t.Errorf("retry = replayed %v, status %s", res.Replayed, res.Ticket.Status)
	}
}

func TestApplyActionConcurrentWriters(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("conc-0001")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	const n = 12

	// Distinct action ids: every one lands.
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := ApplyAction(store, ActionRequest{ID: tk.ID, ActionID: "obs-" + string(rune('a'+i)), Note: "evidence " + string(rune('a'+i))})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("ApplyAction: %v", err)
		}
	}
	got := mustGet(t, store, tk.ID)
	if len(got.Notes) != n || len(got.Actions) != n {
		t.Errorf("notes = %d, actions = %d; want %d of each", len(got.Notes), len(got.Actions), n)
	}
	seen := map[string]bool{}
	for _, r := range got.Actions {
		if seen[r.ID] {
			t.Errorf("receipt %q recorded twice", r.ID)
		}
		seen[r.ID] = true
	}

	// One action id from every writer: exactly one lands and every caller
	// holds the same receipt.
	results := make(chan *ActionResult, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := ApplyAction(store, ActionRequest{ID: tk.ID, ActionID: "shared", Note: "same evidence", Transition: TransitionReopen})
			if err != nil {
				t.Errorf("ApplyAction shared: %v", err)
				return
			}
			results <- res
		}()
	}
	wg.Wait()
	close(results)
	var receipts []ActionReceipt
	fresh := 0
	for res := range results {
		receipts = append(receipts, res.Receipt)
		if !res.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Errorf("%d callers applied the shared action, want exactly 1", fresh)
	}
	for _, r := range receipts[1:] {
		if r != receipts[0] {
			t.Errorf("receipts differ: %+v vs %+v", receipts[0], r)
		}
	}
	got = mustGet(t, store, tk.ID)
	if len(got.Notes) != n+1 || len(got.Actions) != n+1 || got.Status != StatusOpen {
		t.Errorf("notes = %d, actions = %d, status = %s; want %d, %d, open", len(got.Notes), len(got.Actions), got.Status, n+1, n+1)
	}
}

// ─── AC4: discovery under a finding key ────────────────────────────────────

func discovery(id, title string) *Ticket {
	tk := mk(id, StatusBacklog)
	tk.Title = title
	tk.Body = "\nFound by the observer.\n\n## Acceptance Criteria\n\n- fixed\n  verify: go test ./...\n"
	tk.Tags = []string{"observer"}
	return tk
}

func TestCreateDiscoveryDeduplicatesByKey(t *testing.T) {
	root, ms := centralFixture(t, true)
	const key = "flaky:TestFoo"
	const n = 8

	var wg sync.WaitGroup
	results := make(chan *ActionResult, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := CreateDiscovery(WithSource(ms, "observer"), DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
			if err != nil {
				t.Errorf("CreateDiscovery: %v", err)
				return
			}
			results <- res
		}()
	}
	wg.Wait()
	close(results)
	var ids []string
	fresh := 0
	var receipt ActionReceipt
	for res := range results {
		ids = append(ids, res.Ticket.ID)
		if !res.Replayed {
			fresh++
			receipt = res.Receipt
		}
	}
	if fresh != 1 {
		t.Errorf("%d callers created a ticket, want exactly 1", fresh)
	}
	for _, id := range ids[1:] {
		if id != ids[0] {
			t.Errorf("callers got different tickets: %s vs %s", ids[0], id)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "tickets", "warp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("store holds %d files after %d concurrent discoveries, want 1", len(entries), n)
	}
	created := mustGet(t, ms, ids[0])
	if len(created.Actions) != 1 || created.Actions[0] != receipt {
		t.Errorf("stored receipt = %+v, want %+v", created.Actions, receipt)
	}
	_, bare := ParseNamespacedID(created.ID)
	if receipt.ID != key || receipt.Kind != ReceiptDiscover || receipt.Outcome != bare || receipt.Source != "observer" {
		t.Errorf("receipt = %+v", receipt)
	}
	if created.Title != "Flaky TestFoo" || created.Parent != "" {
		t.Errorf("created = %+v", created)
	}

	// The lost-response retry, from a fresh store: the same ticket, replayed.
	again := NewMultiStore(filepath.Join(root, "tickets"))
	res, err := CreateDiscovery(again, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !res.Replayed || res.Ticket.ID != created.ID || res.Receipt != receipt {
		t.Errorf("retry = replayed %v, %s, %+v", res.Replayed, res.Ticket.ID, res.Receipt)
	}
	if res.Ticket.Precondition() == "" {
		t.Error("the replayed ticket carries no precondition")
	}

	// Changed content under the key: refused, naming the existing ticket, and
	// still one ticket.
	changed := discovery("warp/flaky-test-foo", "Flaky TestFoo (worse now)")
	_, err = CreateDiscovery(ms, DiscoveryRequest{Ticket: changed, FindingKey: key})
	if !errors.Is(err, ErrActionConflict) || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), bare) {
		t.Errorf("changed content under the key = %v; want ErrActionConflict naming the key and the ticket", err)
	}
	if changed.ID != "warp/flaky-test-foo" {
		t.Errorf("a refused discovery left the caller's id as %q", changed.ID)
	}
	entries, _ = os.ReadDir(filepath.Join(root, "tickets", "warp"))
	if len(entries) != 1 {
		t.Errorf("store holds %d files after a refused discovery, want 1", len(entries))
	}

	// A key is refused empty, and a foreign-looking ID goes through Create's
	// own rules.
	if _, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/x-0001", "x")}); err == nil {
		t.Error("an empty finding key was accepted")
	}
	if _, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("bare-0001", "x"), FindingKey: "k"}); err == nil {
		t.Error("a bare ID on a MultiStore was accepted")
	}
}

func TestCreateDiscoveryKeyIsProjectScoped(t *testing.T) {
	_, ms := centralFixture(t, true)
	const key = "lint:unused-import"
	warp, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/unused-import", "Unused import"), FindingKey: key})
	if err != nil {
		t.Fatal(err)
	}
	loom, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("loom/unused-import", "Unused import"), FindingKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if loom.Replayed || loom.Ticket.ID == warp.Ticket.ID || !strings.HasPrefix(loom.Ticket.ID, "loom/") {
		t.Errorf("the same key in another project = replayed %v, %s (warp: %s)", loom.Replayed, loom.Ticket.ID, warp.Ticket.ID)
	}
	// Each project answers its own key on retry.
	res, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("loom/unused-import", "Unused import"), FindingKey: key})
	if err != nil || !res.Replayed || res.Ticket.ID != loom.Ticket.ID {
		t.Errorf("loom retry = %+v, %v", res, err)
	}
}

// Two machines can each create a discovery under one key: the files are
// separate, so git merges them cleanly and the store ends up with two
// claimants. Replay refuses the ambiguity rather than picking one.
func TestCreateDiscoveryRefusesAKeyTwoTicketsClaim(t *testing.T) {
	root, ms := centralFixture(t, true)
	const key = "flaky:TestFoo"
	first, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
	if err != nil {
		t.Fatal(err)
	}
	// The other machine's file, as a sync would land it: a different ID
	// carrying its own receipt under the same key.
	other := discovery("warp/flaky-test-foo-2", "Flaky TestFoo")
	other.Actions = []ActionReceipt{{ID: key, Kind: ReceiptDiscover, Digest: first.Receipt.Digest, Source: "observer", At: first.Receipt.At, Outcome: "flaky-test-foo-2"}}
	mustCreate(t, ms, other)
	warp := filepath.Join(root, "tickets", "warp")
	before := snapshotTree(t, warp)

	res, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
	if !errors.Is(err, ErrActionConflict) {
		t.Fatalf("CreateDiscovery over two claimants = %v, %v; want ErrActionConflict", res, err)
	}
	_, firstBare := ParseNamespacedID(first.Ticket.ID)
	if !strings.Contains(err.Error(), firstBare) || !strings.Contains(err.Error(), "flaky-test-foo-2") {
		t.Errorf("refusal %q does not name both claimants", err)
	}
	assertTreeUnchanged(t, before, snapshotTree(t, warp))
}

// A discovery lands where a create does: the ID collision retry is settled
// before the receipt is stamped, so the receipt names the file it is in.
func TestCreateDiscoveryReceiptNamesTheSettledID(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("taken-0001")); err != nil {
		t.Fatal(err)
	}
	res, err := CreateDiscovery(store, DiscoveryRequest{Ticket: discovery("taken-0001", "Taken id"), FindingKey: "k"})
	if err != nil {
		t.Fatalf("CreateDiscovery: %v", err)
	}
	if res.Ticket.ID == "taken-0001" {
		t.Fatal("the discovery kept an ID the store already held")
	}
	got := mustGet(t, store, res.Ticket.ID)
	if len(got.Actions) != 1 || got.Actions[0].Outcome != got.ID {
		t.Errorf("receipt outcome = %+v, want the id the file was created under (%s)", got.Actions, got.ID)
	}
}

// A namespace with a file that could not be read may hold the ticket a key
// created; absence cannot be proved, so the discovery is refused.
func TestCreateDiscoveryRefusesAnIncompleteNamespace(t *testing.T) {
	root, ms := centralFixture(t, true)
	if err := os.WriteFile(filepath.Join(root, "tickets", "warp", "bad.md"), []byte("---\nid: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	captureWarnings(t)
	_, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/d-0001", "d"), FindingKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "complete snapshot") {
		t.Errorf("CreateDiscovery into a namespace with an unreadable file = %v; want the incompleteness refusal", err)
	}
	// Another namespace is unaffected.
	if _, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("loom/d-0001", "d"), FindingKey: "k"}); err != nil {
		t.Errorf("CreateDiscovery into a complete namespace: %v", err)
	}
}

// ─── Fail closed: a store this package does not own ─────────────────────────

func TestActionProtocolRefusesAStoreThatCannotLock(t *testing.T) {
	inner, _ := testStore(t)
	if err := inner.Create(sampleTicket("fb-0001")); err != nil {
		t.Fatal(err)
	}
	store := &fallbackStore{inner: inner}
	_, err := ApplyAction(store, ActionRequest{ID: "fb-0001", ActionID: "a", Note: "n"})
	if err == nil || !strings.Contains(err.Error(), "atomicity") {
		t.Errorf("ApplyAction on a foreign store = %v; want the fail-closed refusal", err)
	}
	_, err = CreateDiscovery(store, DiscoveryRequest{Ticket: discovery("d-0001", "d"), FindingKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "atomicity") {
		t.Errorf("CreateDiscovery on a foreign store = %v; want the fail-closed refusal", err)
	}
	got := mustGet(t, inner, "fb-0001")
	if len(got.Notes) != 0 || len(got.Actions) != 0 {
		t.Errorf("the refusal wrote: %+v %+v", got.Notes, got.Actions)
	}
}

// ─── The block: append-only, round-tripped, refused when damaged ───────────

func TestUpdateRefusesDroppedOrRewrittenReceipts(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("ao-0001")
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, ActionRequest{ID: tk.ID, ActionID: "a", Note: "n"})

	dropped := mustGet(t, store, tk.ID)
	dropped.Actions = nil
	if err := store.Update(dropped); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("Update dropping a receipt = %v, want an append-only error", err)
	}
	rewritten := mustGet(t, store, tk.ID)
	rewritten.Actions[0].Outcome = "done"
	if err := store.Update(rewritten); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("Update rewriting a receipt = %v, want an append-only error", err)
	}
	invalid := mustGet(t, store, tk.ID)
	invalid.Actions = append(invalid.Actions, ActionReceipt{ID: "b", Kind: "bogus"})
	if err := store.Update(invalid); err == nil {
		t.Error("Update appending an invalid receipt = nil, want error")
	}

	// Ordinary writes that leave the receipts alone still land — the
	// compatibility every existing tool relies on.
	edited := mustGet(t, store, tk.ID)
	edited.Title = "Retitled"
	if err := store.Update(edited); err != nil {
		t.Fatalf("Update with receipts untouched: %v", err)
	}
	if _, err := Mutate(store, tk.ID, func(t *Ticket) error {
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "plain note"})
		return nil
	}); err != nil {
		t.Fatalf("Mutate with receipts untouched: %v", err)
	}
	got := mustGet(t, store, tk.ID)
	if got.Title != "Retitled" || len(got.Notes) != 2 || len(got.Actions) != 1 || got.Actions[0].ID != "a" {
		t.Errorf("after ordinary writes: %+v", got)
	}
}

// A version-less Update writes unconditionally, and a damaged block is the
// one prior it cannot compare against; the write is refused rather than
// allowed to replace the receipts, or a retry would apply the action again.
func TestUpdateRefusesToReplaceDamagedReceipts(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("dmg-0002")
	if err := store.Create(tk); err != nil {
		t.Fatal(err)
	}
	mustApply(t, store, ActionRequest{ID: tk.ID, ActionID: "obs", Note: "regressed"})
	path := filepath.Join(store.Dir, tk.ID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("    kind: apply\n")) {
		t.Fatalf("no receipt on disk:\n%s", data)
	}
	before := bytes.Replace(data, []byte("    kind: apply\n"), nil, 1)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}

	rebuilt := sampleTicket(tk.ID)
	rebuilt.Title = "Rebuilt by hand"
	err = store.Update(rebuilt)
	if err == nil || !strings.Contains(err.Error(), tk.ID) || !strings.Contains(err.Error(), "actions") {
		t.Fatalf("Update over a damaged receipt = %v; want a refusal naming the ticket and the actions block", err)
	}
	if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
		t.Errorf("the refused update changed the file:\n%s", after)
	}
}

// The receipts must be preserved whatever made the prior unreadable. A file
// refused for a fault in another field — `deps: notalist` — carries receipts
// Parse never reached, and a version-less Update of a rebuilt ticket without
// them would erase the record, so the retry applies the action again or
// creates a second ticket. The write is refused until the file is repaired;
// once it is, the original action replays.
func TestUpdateRefusesToReplaceReceiptsBehindAnUnrelatedFault(t *testing.T) {
	corrupt := func(t *testing.T, path string) (before []byte) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("deps: []\n")) {
			t.Fatalf("%s does not hold an empty deps line:\n%s", path, data)
		}
		before = bytes.Replace(data, []byte("deps: []\n"), []byte("deps: notalist\n"), 1)
		if err := os.WriteFile(path, before, 0o644); err != nil {
			t.Fatal(err)
		}
		return before
	}
	repair := func(t *testing.T, path string, before []byte) {
		t.Helper()
		if err := os.WriteFile(path, bytes.Replace(before, []byte("deps: notalist\n"), []byte("deps: []\n"), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("apply", func(t *testing.T) {
		store, _ := testStore(t)
		tk := sampleTicket("dmg-0003")
		tk.Status = StatusDone
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
		req := ActionRequest{ID: tk.ID, ActionID: "obs", Note: "regressed", Transition: TransitionReopen}
		mustApply(t, store, req)
		path := filepath.Join(store.Dir, tk.ID+".md")
		before := corrupt(t, path)

		rebuilt := sampleTicket(tk.ID)
		rebuilt.Title = "Rebuilt by hand"
		err := store.Update(rebuilt)
		if err == nil || !strings.Contains(err.Error(), tk.ID) {
			t.Fatalf("Update over an unreadable prior carrying receipts = %v; want a refusal naming the ticket", err)
		}
		if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
			t.Errorf("the refused update changed the file:\n%s", after)
		}

		repair(t, path, before)
		res := mustApply(t, store, req)
		if !res.Replayed {
			t.Errorf("retry after repair was applied again rather than replayed: %+v", res.Receipt)
		}
		got := mustGet(t, store, tk.ID)
		if len(got.Notes) != 1 || len(got.Actions) != 1 || got.Actions[0] != res.Receipt {
			t.Errorf("after the replay: notes=%d actions=%+v", len(got.Notes), got.Actions)
		}
	})

	t.Run("discover", func(t *testing.T) {
		root, ms := centralFixture(t, true)
		captureWarnings(t)
		const key = "flaky:TestFoo"
		res, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
		if err != nil {
			t.Fatal(err)
		}
		_, bare := ParseNamespacedID(res.Ticket.ID)
		path := filepath.Join(root, "tickets", "warp", bare+".md")
		corrupt(t, path)
		before := snapshotTree(t, filepath.Join(root, "tickets"))

		rebuilt := discovery(res.Ticket.ID, "Flaky TestFoo")
		if err := ms.Update(rebuilt); err == nil || !strings.Contains(err.Error(), bare) {
			t.Fatalf("Update over an unreadable discovery = %v; want a refusal naming the ticket", err)
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))

		res, err = CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
		if err == nil || !strings.Contains(err.Error(), "complete snapshot") {
			t.Fatalf("CreateDiscovery retry over an unreadable namespace = %v, %v; want the incompleteness refusal", res, err)
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
		entries, err := os.ReadDir(filepath.Join(root, "tickets", "warp"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("warp holds %d files after the refused retry, want 1", len(entries))
		}
	})
}

func TestActionsBlockRoundTripsAndRefusesDamage(t *testing.T) {
	tk := sampleTicket("rt-0001")
	tk.Actions = []ActionReceipt{
		{ID: "obs: with punctuation #1", Kind: ReceiptApply, Digest: strings.Repeat("ab", 32), Source: "observer", At: "2026-09-18T00:00:00Z", Outcome: "open"},
		{ID: "flaky:TestFoo", Kind: ReceiptDiscover, Digest: strings.Repeat("cd", 32), Source: "observer", At: "2026-09-18T00:00:01Z", Outcome: "rt-0001"},
	}
	data, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	if len(parsed.Actions) != 2 || parsed.Actions[0] != tk.Actions[0] || parsed.Actions[1] != tk.Actions[1] {
		t.Errorf("round trip = %+v, want %+v", parsed.Actions, tk.Actions)
	}
	if _, extra := parsed.Extra["actions"]; extra {
		t.Error("the actions block was read as an extra field")
	}

	const head = `---
id: rt-0002
status: ready
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: feature
priority: 2
`
	const tail = "---\n# Damaged\n"
	const validRow = `  - id: a
    kind: apply
    digest: ` + "abababababababababababababababababababababababababababababababab" + `
    source: observer
    at: "2026-09-18T00:00:00Z"
    outcome: open
`
	const rowWithSeqDigest = `  - id: a
    kind: apply
    digest: [x]
    source: observer
    at: "2026-09-18T00:00:00Z"
    outcome: open
`
	// Damage that decodes without a type error: a field a merge dropped, or
	// left empty. findReceipt would never match such a row, so the file is
	// refused rather than read as a ticket the replay check cannot see.
	const rowWithoutKind = `  - id: a
    digest: ` + "abababababababababababababababababababababababababababababababab" + `
    source: observer
    at: "2026-09-18T00:00:00Z"
    outcome: open
`
	const rowWithoutID = `  - kind: apply
    digest: ` + "abababababababababababababababababababababababababababababababab" + `
    source: observer
    at: "2026-09-18T00:00:00Z"
    outcome: open
`
	for _, tc := range []struct{ name, actions string }{
		{"whole block unreadable", "actions: notalist\n"},
		{"one element unreadable", "actions:\n  - notalist\n" + validRow},
		{"one field unreadable", "actions:\n" + rowWithSeqDigest},
		{"kind omitted", "actions:\n" + rowWithoutKind},
		{"kind empty", "actions:\n" + strings.Replace(validRow, "kind: apply", "kind:", 1)},
		{"kind null", "actions:\n" + strings.Replace(validRow, "kind: apply", "kind: null", 1)},
		{"kind unknown", "actions:\n" + strings.Replace(validRow, "kind: apply", "kind: note", 1)},
		{"id omitted", "actions:\n" + rowWithoutID},
		{"digest not a sha256", "actions:\n" + strings.Replace(validRow, "abab", "zzzz", 1)},
		{"kind omitted beside a mistyped field", "abandoned: maybe\nactions:\n" + rowWithoutKind},
	} {
		if _, err := Parse(strings.NewReader(head + tc.actions + tail)); err == nil {
			t.Errorf("%s: Parse = nil, want error", tc.name)
		}
	}
	parsed, err = Parse(strings.NewReader(head + "abandoned: maybe\nactions:\n" + validRow + tail))
	if err != nil {
		t.Fatalf("Parse with an intact block beside a mistyped field: %v", err)
	}
	if len(parsed.Actions) != 1 || parsed.Actions[0].ID != "a" {
		t.Errorf("actions = %+v, want the one intact receipt", parsed.Actions)
	}
}

// A receipt damaged in a way that decodes cleanly must still refuse the file,
// or a retry lands twice: ApplyAction would append a second note and receipt,
// and CreateDiscovery a second ticket, because neither finds the record.
func TestRetriesRefuseAStoreWithADamagedReceipt(t *testing.T) {
	damage := func(t *testing.T, path, line string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(line)) {
			t.Fatalf("%s does not hold %q:\n%s", path, line, data)
		}
		if err := os.WriteFile(path, bytes.Replace(data, []byte(line), nil, 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("apply", func(t *testing.T) {
		store, _ := testStore(t)
		tk := sampleTicket("dmg-0001")
		tk.Status = StatusDone
		if err := store.Create(tk); err != nil {
			t.Fatal(err)
		}
		req := ActionRequest{ID: tk.ID, ActionID: "obs", Note: "regressed", Transition: TransitionReopen}
		mustApply(t, store, req)
		path := filepath.Join(store.Dir, tk.ID+".md")
		damage(t, path, "    kind: apply\n")
		before := readTicketFile(t, store, tk.ID)

		res, err := ApplyAction(store, req)
		var unreadable *UnreadableTicketError
		if !errors.As(err, &unreadable) {
			t.Fatalf("ApplyAction retry over a damaged receipt = %v, %v; want an UnreadableTicketError", res, err)
		}
		if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
			t.Errorf("the refused retry changed the file:\n%s", after)
		}
		// Nor does a fresh action, or an ordinary write, land over it.
		if _, err := ApplyAction(store, ActionRequest{ID: tk.ID, ActionID: "obs-2", Note: "more"}); err == nil {
			t.Error("a new action landed on a ticket whose receipts cannot be read")
		}
		if _, err := store.Get(tk.ID); err == nil {
			t.Error("Get read a ticket whose receipts cannot be read")
		}
		if after := readTicketFile(t, store, tk.ID); !bytes.Equal(before, after) {
			t.Errorf("a refused write changed the file:\n%s", after)
		}
	})

	t.Run("discover", func(t *testing.T) {
		root, ms := centralFixture(t, true)
		captureWarnings(t)
		const key = "flaky:TestFoo"
		res, err := CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
		if err != nil {
			t.Fatal(err)
		}
		_, bare := ParseNamespacedID(res.Ticket.ID)
		damage(t, filepath.Join(root, "tickets", "warp", bare+".md"), "    kind: discover\n")
		before := snapshotTree(t, filepath.Join(root, "tickets"))

		res, err = CreateDiscovery(ms, DiscoveryRequest{Ticket: discovery("warp/flaky-test-foo", "Flaky TestFoo"), FindingKey: key})
		if err == nil || !strings.Contains(err.Error(), "complete snapshot") {
			t.Fatalf("CreateDiscovery retry over a damaged receipt = %v, %v; want the incompleteness refusal", res, err)
		}
		assertTreeUnchanged(t, before, snapshotTree(t, filepath.Join(root, "tickets")))
		entries, err := os.ReadDir(filepath.Join(root, "tickets", "warp"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("warp holds %d files after the refused retry, want 1", len(entries))
		}
	})
}

func TestValidateActionReceipt(t *testing.T) {
	good := ActionReceipt{ID: "a", Kind: ReceiptApply, Digest: strings.Repeat("0", 64), Source: "s", At: "2026-09-18T00:00:00Z", Outcome: "open"}
	if err := ValidateActionReceipt(good); err != nil {
		t.Fatalf("valid receipt refused: %v", err)
	}
	for name, mutate := range map[string]func(*ActionReceipt){
		"empty id":         func(r *ActionReceipt) { r.ID = "" },
		"padded id":        func(r *ActionReceipt) { r.ID = " a" },
		"control in id":    func(r *ActionReceipt) { r.ID = "a\nb" },
		"unknown kind":     func(r *ActionReceipt) { r.Kind = "note" },
		"short digest":     func(r *ActionReceipt) { r.Digest = "abc" },
		"non-hex digest":   func(r *ActionReceipt) { r.Digest = strings.Repeat("z", 64) },
		"empty source":     func(r *ActionReceipt) { r.Source = "" },
		"bad timestamp":    func(r *ActionReceipt) { r.At = "yesterday" },
		"empty outcome":    func(r *ActionReceipt) { r.Outcome = "" },
		"multiline digest": func(r *ActionReceipt) { r.Digest = strings.Repeat("0", 63) + "\n" },
	} {
		r := good
		mutate(&r)
		if err := ValidateActionReceipt(r); err == nil {
			t.Errorf("%s: ValidateActionReceipt = nil, want error", name)
		}
	}
}
