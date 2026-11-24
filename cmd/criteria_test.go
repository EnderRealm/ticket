package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func captureCriteria(t *testing.T, id string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		r.Close()
	})

	err := runCriteria(criteriaCmd, []string{id})

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runCriteria: %v", err)
	}
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestCriteriaParsesTestMarker(t *testing.T) {
	verifyStore(t, "cr-kinds", "Description.\n\n## Acceptance Criteria\n\n"+
		"- Runs.\n  verify: test -f x\n"+
		"- Rows \x1b[2Jfit.\n  test: run tk ls \x1b[2Jat 40 columns\n"+
		"- Reads plainly.\n")

	out := captureCriteria(t, "cr-kinds")
	for _, want := range []string{
		"1. [verify] Runs.\n   verify: test -f x\n",
		"2. [test] Rows \ufffd[2Jfit.\n   test: run tk ls \ufffd[2Jat 40 columns\n",
		"3. [judged] Reads plainly.\n",
	} {
		if !contains(out, want) {
			t.Errorf("criteria output missing %q:\n%s", want, out)
		}
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var report struct {
		ID           string           `json:"id"`
		AcceptanceID string           `json:"acceptance_id"`
		Criteria     []map[string]any `json:"criteria"`
	}
	out = captureCriteria(t, "cr-kinds")
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if report.ID != "cr-kinds" || !strings.HasPrefix(report.AcceptanceID, "sha256:") || len(report.Criteria) != 3 {
		t.Fatalf("report = %+v, want the ticket's three criteria under its identity", report)
	}
	for i, want := range []struct {
		kind ticket.CriterionKind
		test any
	}{
		{ticket.CriterionVerify, nil},
		{ticket.CriterionTest, "run tk ls \ufffd[2Jat 40 columns"},
		{ticket.CriterionJudged, nil},
	} {
		got := report.Criteria[i]
		if got["index"] != float64(i+1) || got["kind"] != string(want.kind) || got["test"] != want.test {
			t.Errorf("criteria[%d] = %v, want kind %s and test %v", i, got, want.kind, want.test)
		}
	}
}

func TestCriteriaListsNoneForATicketWithoutThem(t *testing.T) {
	verifyStore(t, "cr-none", "Description.\n")

	if out := captureCriteria(t, "cr-none"); out != "cr-none has no acceptance criteria\n" {
		t.Errorf("criteria output = %q", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	if out := captureCriteria(t, "cr-none"); !contains(out, `"criteria": []`) {
		t.Errorf("criteria --json should carry an empty list:\n%s", out)
	}
}

// A ticket file arrives over git with whatever id its frontmatter holds, and
// resolving an ordinary filename returns that id to be printed.
func TestCriteriaSanitizesTheStoredID(t *testing.T) {
	store := centralStore(t, "cr-ids")
	for file, acceptance := range map[string]string{
		"cr-empty-0001": "",
		"cr-full-0002":  "\n## Acceptance Criteria\n\n- Reads plainly.\n",
	} {
		content := "---\nid: \"" + file + "\\u001b[2J\\u001b[H\"\nstatus: open\ntype: feature\npriority: 2\ncreated: 2026-01-02T03:04:05Z\n---\n# Planted\n\nDescription.\n" + acceptance
		if err := os.WriteFile(filepath.Join(store.Dir, file+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for file, want := range map[string]string{
		"cr-empty-0001": "cr-empty-0001\ufffd[2J\ufffd[H has no acceptance criteria\n",
		"cr-full-0002":  "cr-full-0002\ufffd[2J\ufffd[H acceptance sha256:",
	} {
		out := captureCriteria(t, file)
		if !strings.HasPrefix(out, want) || strings.ContainsRune(out, 0x1b) {
			t.Errorf("criteria %s = %q, want it to open %q with no raw escape", file, out, want)
		}
	}
}

func TestCriteriaShowsAnUnverifiableLineWithNoReason(t *testing.T) {
	verifyStore(t, "cr-noreason", "Description.\n\n## Acceptance Criteria\n\n- Recipe recorded.\n  unverifiable:\n")

	if out := captureCriteria(t, "cr-noreason"); !strings.HasSuffix(out, "1. [judged] Recipe recorded.\n   unverifiable:\n") {
		t.Errorf("criteria output = %q, want the line shown with no trailing space", out)
	}
}

func TestBareCriterionIsNotFlagged(t *testing.T) {
	store := centralStore(t, "cr-bare")
	const acceptance = "- Reads plainly.\n- Checked.\n  verify: go test ./..."

	id, stderr := captureCreateStderr(t, store, "Judged by the run", "Why it matters.\n\n## Acceptance Criteria\n\n"+acceptance+"\n")
	if stderr != "" {
		t.Errorf("create warned about a criterion with no line:\n%s", stderr)
	}
	if stderr := captureEditStderr(t, id, "acceptance", acceptance); stderr != "" {
		t.Errorf("edit warned about a criterion with no line:\n%s", stderr)
	}
	tk, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got := ticket.AcceptanceCriteria(tk.Body); got != acceptance {
		t.Errorf("stored acceptance = %q, want %q", got, acceptance)
	}

	if out := captureAudit(t, ""); !contains(out, "No findings") || contains(out, "bare-acceptance") {
		t.Errorf("audit should report nothing and name no bare-acceptance cause:\n%s", out)
	}
	if out, err := captureAuditErr(t, "bare-acceptance"); err == nil || !contains(err.Error(), "unknown audit cause") {
		t.Errorf("audit bare-acceptance = %v, want an unknown cause:\n%s", err, out)
	}
	if out := captureShow(t, store, id, false); contains(out, "## Findings") {
		t.Errorf("show reports findings on a criterion with no line:\n%s", out)
	}
}

func TestUnverifiableReadsAsJudgedByRun(t *testing.T) {
	store := verifyStore(t, "cr-unver", "Description.\n\n## Acceptance Criteria\n\n- Recipe recorded.\n  unverifiable: a recorded artifact\n")

	if out := captureCriteria(t, "cr-unver"); !contains(out, "1. [judged] Recipe recorded.\n   unverifiable: a recorded artifact\n") {
		t.Errorf("criteria output does not read the line as judged with its reason:\n%s", out)
	}
	if out := captureShow(t, store, "cr-unver", false); contains(out, "## Findings") {
		t.Errorf("show reports findings on an unverifiable criterion:\n%s", out)
	}

	out, err := captureVerify(t, "cr-unver")
	if err != nil || !contains(out, "UNVERIFIED Recipe recorded.") || !contains(out, "1 unverified\n") {
		t.Errorf("verify = %v, want the criterion unverified:\n%s", err, out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var report ticket.CriteriaReport
	out = captureCriteria(t, "cr-unver")
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if got := report.Criteria[0]; got.Kind != ticket.CriterionJudged || !got.Unverifiable || got.UnverifiableReason != "a recorded artifact" {
		t.Errorf("criterion = %+v, want judged keeping its reason", got)
	}
}

func TestCriterionWithTwoMarkersIsReported(t *testing.T) {
	store := centralStore(t, "cr-both")
	const both = "- Rows fit.\n  verify: go test ./cmd/\n  test: run tk ls at 40 columns"

	id, stderr := captureCreateStderr(t, store, "Two markers", "Why it matters.\n\n## Acceptance Criteria\n\n"+both+"\n- Checked.\n  verify: go test ./...\n")
	if want := ticket.TwoMarkersWarning(id, []string{"Rows fit."}); !contains(stderr, want) {
		t.Errorf("create stderr = %q, want to contain %q", stderr, want)
	}

	if stderr := captureEditStderr(t, id, "acceptance", "- Rows fit.\n  test: run tk ls at 40 columns"); stderr != "" {
		t.Errorf("an edit leaving one marker warned:\n%s", stderr)
	}
	if out := captureAudit(t, string(ticket.ContentTwoMarkers)); out != "No two-markers findings.\n" {
		t.Errorf("audit reports a criterion with one marker:\n%s", out)
	}

	stderr = captureEditStderr(t, id, "acceptance", both)
	if want := ticket.TwoMarkersWarning(id, []string{"Rows fit."}); !contains(stderr, want) {
		t.Errorf("edit stderr = %q, want to contain %q", stderr, want)
	}
	if stderr := captureEditStderr(t, id, "design", "unrelated"); stderr != "" {
		t.Errorf("an edit writing neither section warned:\n%s", stderr)
	}

	if count, command := summaryRow(t, captureAudit(t, ""), string(ticket.ContentTwoMarkers)); count != 1 || command != "tk audit two-markers" {
		t.Errorf("summary row = %d %q, want 1 and its drill-in", count, command)
	}
	drill := captureAudit(t, string(ticket.ContentTwoMarkers))
	for _, want := range []string{id + "  two-markers  1 criterion(s)", "1 ticket(s) carry 1 acceptance criterion(s) with both"} {
		if !contains(drill, want) {
			t.Errorf("audit drill-in missing %q:\n%s", want, drill)
		}
	}
	if out := captureShow(t, store, id, false); !contains(out, "- two-markers  1 criterion(s)") {
		t.Errorf("show missing the two-markers finding:\n%s", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var report ticket.AuditReport
	out := captureAudit(t, "")
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	want := ticket.ContentIssue{ID: "cr-both/" + id, Kind: ticket.ContentTwoMarkers, Criteria: 1}
	if len(report.Content) != 1 || report.Content[0] != want {
		t.Errorf("json content = %+v, want %+v alone", report.Content, want)
	}
}

func TestVerifyReportsTestCriterionAsWaiting(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	store := verifyStore(t, "cr-wait", "Description.\n\n## Acceptance Criteria\n\n"+
		"- Passing check.\n  verify: /bin/sh -c 'exit 0'\n"+
		"- Rows fit.\n  test: run tk ls at 40 columns\n"+
		"- Both.\n  verify: /bin/sh -c 'touch "+sentinel+"'\n  test: look\n"+
		"- Reads plainly.\n")

	out, err := captureVerify(t, "cr-wait")
	if err != nil {
		t.Errorf("a waiting criterion failed the run: %v", err)
	}
	for _, want := range []string{
		"PASS (exit 0) Passing check.",
		"WAITING Rows fit.",
		"WAITING Both.",
		"UNVERIFIED Reads plainly.",
		"1 pass, 0 fail, 0 refused, 1 unverified, 2 waiting",
	} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	tk, err := store.Get("cr-wait")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"1 pass, 0 fail, 0 refused, 1 unverified, 2 waiting\n",
		"- WAITING: Rows fit.",
		"- WAITING: Both.",
		"- UNVERIFIED: Reads plainly.",
	} {
		if !contains(tk.Body, want) {
			t.Errorf("recorded test results missing %q:\n%s", want, tk.Body)
		}
	}

	for _, check := range []bool{true, false} {
		for n, text := range map[int]string{2: "run tk ls at 40 columns", 3: "look"} {
			got, code := verifyCriterionJSON(t, "cr-wait", n, check)
			if got.Status != string(ticket.VerifyWaiting) || got.Test != text || code != verifyRefusedExit {
				t.Errorf("criterion %d check=%v = %+v exit %d, want waiting exit %d", n, check, got, code, verifyRefusedExit)
			}
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("a test criterion ran its command: sentinel stat err = %v", err)
	}
}
