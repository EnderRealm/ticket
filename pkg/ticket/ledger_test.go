package ticket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	shaA = "0123456789abcdef0123456789abcdef01234567"
	shaB = "fedcba9876543210fedcba9876543210fedcba98"
)

func TestValidateVerdictClass(t *testing.T) {
	for _, c := range []VerdictClass{VerdictLiveVerified, VerdictTestVerified, VerdictTypeCheckOnly, VerdictVerifierBlocked, VerdictVerifierFailed} {
		if err := ValidateVerdictClass(c); err != nil {
			t.Errorf("ValidateVerdictClass(%q) = %v, want nil", c, err)
		}
	}
	for _, c := range []VerdictClass{"passed", "", "verified", "Live-Verified"} {
		if err := ValidateVerdictClass(c); err == nil {
			t.Errorf("ValidateVerdictClass(%q) = nil, want error", c)
		}
	}
}

func TestValidateVerdictRole(t *testing.T) {
	for _, r := range []VerdictRole{VerdictRoleWorker, VerdictRoleVerifier} {
		if err := ValidateVerdictRole(r); err != nil {
			t.Errorf("ValidateVerdictRole(%q) = %v, want nil", r, err)
		}
	}
	for _, r := range []VerdictRole{"", "reviewer", "Worker"} {
		if err := ValidateVerdictRole(r); err == nil {
			t.Errorf("ValidateVerdictRole(%q) = nil, want error", r)
		}
	}
}

func TestVerdictClassPasses(t *testing.T) {
	if !VerdictLiveVerified.Passes() || !VerdictTestVerified.Passes() {
		t.Error("live-verified and test-verified must pass")
	}
	// The failure the ledger exists to prevent: a verifier that could not run
	// read as a verifier that agreed.
	if VerdictVerifierBlocked.Passes() {
		t.Error("verifier-blocked must not pass")
	}
	if VerdictTypeCheckOnly.Passes() {
		t.Error("type-check-only must not pass")
	}
	if VerdictVerifierFailed.Passes() {
		t.Error("verifier-failed must not pass")
	}
}

func TestValidateVerdictSHA(t *testing.T) {
	sha256Hex := strings.Repeat("ab", 32)
	tests := []struct {
		in   string
		want string
	}{
		{shaA, shaA},
		{sha256Hex, sha256Hex},
		{strings.ToUpper(shaA), shaA},
		{" " + shaA + " ", shaA},
	}
	for _, tc := range tests {
		got, err := ValidateVerdictSHA(tc.in)
		if err != nil {
			t.Errorf("ValidateVerdictSHA(%q) = %v, want nil", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ValidateVerdictSHA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, in := range []string{"", "0123456", shaA[:39], shaA + "0", strings.Repeat("z", 40)} {
		if _, err := ValidateVerdictSHA(in); err == nil {
			t.Errorf("ValidateVerdictSHA(%q) = nil, want error", in)
		}
	}
}

func TestRecordVerdictAppends(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-verd")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Evidence and identity are trimmed rather than refused: a trailing newline
	// off a tool call would otherwise cost the recorder a round trip.
	_, row, err := RecordVerdict(store, tk.ID, strings.ToUpper(shaA), VerdictTestVerified, VerdictRoleWorker, "go test ./...\n", " worker-1")
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if row.Ticket != tk.ID || row.SHA != shaA || row.Class != VerdictTestVerified || row.Role != VerdictRoleWorker {
		t.Errorf("row = %+v, want ticket %s at %s", row, tk.ID, shaA)
	}
	if row.Evidence != "go test ./..." || row.By != "worker-1" {
		t.Errorf("row evidence/by = %q/%q, want them trimmed", row.Evidence, row.By)
	}
	if _, err := time.Parse(time.RFC3339, row.At); err != nil {
		t.Errorf("row.At = %q, want RFC3339: %v", row.At, err)
	}

	// Re-read from disk: the row has to survive the serialize/parse round trip.
	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Verdicts) != 1 {
		t.Fatalf("verdicts after one record = %d, want 1", len(got.Verdicts))
	}
	if got.Verdicts[0] != row {
		t.Errorf("stored row = %+v, want %+v", got.Verdicts[0], row)
	}

	if _, _, err := RecordVerdict(store, tk.ID, shaB, VerdictVerifierFailed, VerdictRoleVerifier, "review session 2", "verifier-1"); err != nil {
		t.Fatalf("second RecordVerdict: %v", err)
	}
	got, err = store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Verdicts) != 2 {
		t.Fatalf("verdicts after two records = %d, want 2", len(got.Verdicts))
	}
	if got.Verdicts[0] != row {
		t.Errorf("first row changed: %+v, want %+v", got.Verdicts[0], row)
	}
}

func TestRecordVerdictRefusesBeforeWriting(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-refu")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := filepath.Join(store.Dir, tk.ID+".md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ticket: %v", err)
	}

	for _, tc := range []struct {
		name     string
		class    VerdictClass
		role     VerdictRole
		sha      string
		evidence string
		by       string
	}{
		{"unknown class", "passed", VerdictRoleWorker, shaA, "evidence", "worker-1"},
		{"unknown role", VerdictTestVerified, "reviewer", shaA, "evidence", "worker-1"},
		{"abbreviated sha", VerdictTestVerified, VerdictRoleWorker, shaA[:7], "evidence", "worker-1"},
		{"empty evidence", VerdictTestVerified, VerdictRoleWorker, shaA, "  ", "worker-1"},
		{"empty identity", VerdictTestVerified, VerdictRoleWorker, shaA, "evidence", ""},
	} {
		if _, _, err := RecordVerdict(store, tk.ID, tc.sha, tc.class, tc.role, tc.evidence, tc.by); err == nil {
			t.Errorf("%s: RecordVerdict = nil, want error", tc.name)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read ticket: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a refused verdict changed the ticket file")
	}
}

func verdictRow(sha string, class VerdictClass, role VerdictRole, by string) VerdictRow {
	return VerdictRow{
		Ticket:   "t-cur",
		SHA:      sha,
		Class:    class,
		Role:     role,
		Evidence: "evidence for " + by,
		By:       by,
		At:       "2026-08-31T00:00:00Z",
	}
}

func TestCurrentVerdict(t *testing.T) {
	old := verdictRow(shaB, VerdictTestVerified, VerdictRoleVerifier, "verifier-old")
	worker := verdictRow(shaA, VerdictTestVerified, VerdictRoleWorker, "worker-1")
	verifier := verdictRow(shaA, VerdictLiveVerified, VerdictRoleVerifier, "verifier-1")
	later := verdictRow(shaA, VerdictVerifierFailed, VerdictRoleVerifier, "verifier-2")

	// A row at another SHA is stale, however recent: it judged other code.
	current, stale, err := CurrentVerdict([]VerdictRow{old}, shaA)
	if err != nil {
		t.Fatalf("CurrentVerdict: %v", err)
	}
	if current != nil {
		t.Errorf("current = %+v, want nil", *current)
	}
	if len(stale) != 1 || stale[0] != old {
		t.Errorf("stale = %+v, want the row at the other sha", stale)
	}

	// A verifier row supersedes a worker row on the same key, whichever order
	// the two were recorded in.
	for _, rows := range [][]VerdictRow{{worker, verifier}, {verifier, worker}} {
		current, _, err = CurrentVerdict(rows, shaA)
		if err != nil {
			t.Fatalf("CurrentVerdict: %v", err)
		}
		if current == nil || *current != verifier {
			t.Errorf("current = %+v, want the verifier row", current)
		}
	}

	// A correction is a new row, so the later verifier row is the operative one.
	current, stale, err = CurrentVerdict([]VerdictRow{old, verifier, later}, shaA)
	if err != nil {
		t.Fatalf("CurrentVerdict: %v", err)
	}
	if current == nil || *current != later {
		t.Errorf("current = %+v, want the later verifier row", current)
	}
	if current != nil && current.Class.Passes() {
		t.Error("verifier-failed must not read as a pass")
	}
	if len(stale) != 1 || stale[0] != old {
		t.Errorf("stale = %+v, want only the row at the other sha", stale)
	}

	if _, _, err := CurrentVerdict([]VerdictRow{worker}, shaA[:7]); err == nil {
		t.Error("CurrentVerdict with an abbreviated head = nil, want error")
	}
}

func TestUpdateRefusesDroppedOrRewrittenVerdicts(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-appe")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := RecordVerdict(store, tk.ID, shaA, VerdictTestVerified, VerdictRoleWorker, "go test ./...", "worker-1"); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}

	dropped, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	dropped.Verdicts = nil
	err = store.Update(dropped)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("Update dropping a row = %v, want an append-only error", err)
	}

	rewritten, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rewritten.Verdicts[0].Class = VerdictLiveVerified
	err = store.Update(rewritten)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("Update rewriting a row = %v, want an append-only error", err)
	}

	// An ordinary edit that leaves the rows alone still writes.
	edited, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	edited.Title = "Retitled"
	if err := store.Update(edited); err != nil {
		t.Fatalf("Update with rows untouched: %v", err)
	}

	// So does an append.
	appended, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	appended.Verdicts = append(appended.Verdicts, verdictRow(shaB, VerdictLiveVerified, VerdictRoleVerifier, "verifier-1"))
	if err := store.Update(appended); err != nil {
		t.Fatalf("Update appending a row: %v", err)
	}

	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Verdicts) != 2 || got.Verdicts[0].Class != VerdictTestVerified {
		t.Errorf("verdicts = %+v, want the original row plus the appended one", got.Verdicts)
	}
	if got.Title != "Retitled" {
		t.Errorf("title = %q, want %q", got.Title, "Retitled")
	}
}

func TestUpdateRefusesInvalidAppendedVerdict(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("t-inva")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	badClass := verdictRow(shaA, "bogus", VerdictRoleWorker, "worker-1")
	noSHA := verdictRow(shaA, VerdictTestVerified, VerdictRoleWorker, "worker-1")
	noSHA.SHA = ""
	for _, row := range []VerdictRow{badClass, noSHA} {
		appended, err := store.Get(tk.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		appended.Verdicts = append(appended.Verdicts, row)
		if err := store.Update(appended); err == nil {
			t.Errorf("Update appending %+v = nil, want error", row)
		}
	}

	got, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Verdicts) != 0 {
		t.Errorf("verdicts = %+v, want none written", got.Verdicts)
	}
}

func TestParseRefusesUnreadableVerdicts(t *testing.T) {
	// The ledger is append-only, so a tolerated decode loss would be replayed by
	// the next write-back as the deletion or the rewriting of the rows the file
	// held — past the append-only check, which compares the damaged prior against
	// the damaged next and finds them equal. The file is refused instead.
	//
	// yaml.v3 records a type error per element and decodes the rest, so the loss
	// is partial as often as it is total: a malformed element drops out of the
	// slice entirely, and an element with one unreadable field yields a row
	// missing that field.
	const head = `---
id: t-bad1
status: ready
deps: []
links: []
created: 2026-01-01T00:00:00Z
type: feature
priority: 2
`
	const tail = `---
# Bad verdicts
`
	const validRow = `  - ticket: t-bad1
    sha: ` + shaA + `
    class: test-verified
    role: worker
    evidence: go test ./...
    by: worker-1
    at: "2026-01-01T00:00:00Z"
`
	const rowWithSeqSHA = `  - ticket: t-bad1
    sha: [` + shaA + `]
    class: test-verified
    role: worker
    evidence: go test ./...
    by: worker-1
    at: "2026-01-01T00:00:00Z"
`
	const rowWithSeqEvidence = `  - ticket: t-bad1
    sha: ` + shaA + `
    class: test-verified
    role: worker
    evidence: [x]
    by: worker-1
    at: "2026-01-01T00:00:00Z"
`
	for _, tc := range []struct{ name, verdicts string }{
		{"whole block unreadable", "verdicts: notalist\n"},
		{"one element unreadable", "verdicts:\n  - notalist\n" + validRow},
		{"one field of a row unreadable", "verdicts:\n" + rowWithSeqSHA},
		{"evidence of a row unreadable", "verdicts:\n" + rowWithSeqEvidence},
	} {
		if _, err := Parse(strings.NewReader(head + tc.verdicts + tail)); err == nil {
			t.Errorf("%s: Parse = nil, want error", tc.name)
		}
	}

	// A ledger that decoded whole is untouched by the check, type error elsewhere
	// in the frontmatter or not: `abandoned: maybe` costs the abandon flag and
	// nothing else.
	parsed, err := Parse(strings.NewReader(head + "abandoned: maybe\nverdicts:\n" + validRow + tail))
	if err != nil {
		t.Fatalf("Parse with intact verdicts beside a mistyped field: %v", err)
	}
	if len(parsed.Verdicts) != 1 || parsed.Verdicts[0].SHA != shaA {
		t.Errorf("verdicts = %+v, want the one intact row", parsed.Verdicts)
	}

	// A hand-written row missing a field decodes cleanly, so nothing puts the
	// file on the tolerated-type-error path and it parses as written.
	sparse := head + "verdicts:\n  - ticket: t-bad1\n    sha: " + shaA + "\n" + tail
	if _, err := Parse(strings.NewReader(sparse)); err != nil {
		t.Errorf("Parse with a sparse hand-written row = %v, want nil", err)
	}
}
