package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// verifyStore creates a central-store project in a sandboxed HOME, holding one
// ticket with the given body. That HOME gets a machine-local verify_allow
// permitting the stock binaries the fixtures below run.
func verifyStore(t *testing.T, id, body string) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "vf-verify")

	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	cfg.VerifyAllow = []string{"/bin/echo", "/bin/sh"}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	tk := &ticket.Ticket{
		ID:      id,
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Verifiable ticket",
		Body:    body,
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store
}

// mixedCriteriaBody has passing, failing, and command-less criteria.
const mixedCriteriaBody = "Description.\n\n## Acceptance Criteria\n\n" +
	"- Passing check.\n  verify: /bin/sh -c 'exit 0'\n" +
	"- Failing check.\n  verify: /bin/sh -c 'echo boom; exit 3'\n" +
	"- Docs updated.\n"

func captureVerify(t *testing.T, id string) (string, error) {
	t.Helper()
	// Execute() sets the command context; running RunE directly doesn't.
	verifyCmd.SetContext(context.Background())

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runVerify(verifyCmd, []string{id})

	w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestVerifyReportsPerCriterion(t *testing.T) {
	store := verifyStore(t, "vf-1234", mixedCriteriaBody)

	out, err := captureVerify(t, "vf-1234")
	if err == nil {
		t.Error("verify with a failing criterion should return an error")
	}

	for _, want := range []string{
		"verifying vf-1234 in ",
		"PASS (exit 0) Passing check.",
		"FAIL (exit 3) Failing check.",
		"UNVERIFIED Docs updated.",
		"1 pass, 1 fail, 0 refused, 1 unverified",
		"boom",
	} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	tk, err := store.Get("vf-1234")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Test Results",
		"1 pass, 1 fail, 0 refused, 1 unverified",
		"- PASS (exit 0): Passing check.",
		"- FAIL (exit 3): Failing check.",
		"- UNVERIFIED: Docs updated.",
	} {
		if !contains(tk.Body, want) {
			t.Errorf("recorded test results missing %q:\n%s", want, tk.Body)
		}
	}
}

func TestVerifyReplacesPreviousResults(t *testing.T) {
	store := verifyStore(t, "vf-1234", mixedCriteriaBody)

	captureVerify(t, "vf-1234")
	captureVerify(t, "vf-1234")

	tk, err := store.Get("vf-1234")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(tk.Body, "## Test Results"); got != 1 {
		t.Errorf("got %d Test Results sections after two runs, want 1:\n%s", got, tk.Body)
	}
	if got := strings.Count(tk.Body, "1 pass, 1 fail, 0 refused, 1 unverified"); got != 1 {
		t.Errorf("got %d verify records after two runs, want 1:\n%s", got, tk.Body)
	}
}

func TestVerifyJSON(t *testing.T) {
	verifyStore(t, "vf-1234", mixedCriteriaBody)

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out, err := captureVerify(t, "vf-1234")
	if err == nil {
		t.Error("verify with a failing criterion should return an error")
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if report.OK {
		t.Error("ok = true, want false with a failing criterion")
	}
	if report.Summary.Pass != 1 || report.Summary.Fail != 1 || report.Summary.Unverified != 1 {
		t.Errorf("summary = %+v, want 1 pass, 1 fail, 1 unverified", report.Summary)
	}
	if len(report.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(report.Results))
	}
	if report.Results[1].Status != string(ticket.VerifyFail) || report.Results[1].ExitCode != 3 {
		t.Errorf("second result = %+v, want fail with exit 3", report.Results[1])
	}
}

func TestVerifyAllPassingExitsZero(t *testing.T) {
	verifyStore(t, "vf-ok", "Description.\n\n## Acceptance Criteria\n\n- Passing check.\n  verify: /bin/echo ok\n- Docs updated.\n")

	out, err := captureVerify(t, "vf-ok")
	if err != nil {
		t.Errorf("verify with no failures should not error: %v", err)
	}
	if !contains(out, "1 pass, 0 fail, 0 refused, 1 unverified") {
		t.Errorf("output missing summary:\n%s", out)
	}
}

func TestVerifyRefusesCommandOutsideAllowList(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := verifyStore(t, "vf-evil",
		"Description.\n\n## Acceptance Criteria\n\n- Hostile check.\n  verify: rm -rf "+sentinel+"\n")

	out, err := captureVerify(t, "vf-evil")
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Fatalf("ticket content deleted the sentinel: %v", statErr)
	}
	if err == nil {
		t.Error("verify with a refused criterion should return an error")
	}
	for _, want := range []string{"REFUSED Hostile check.", "verify_allow", "0 pass, 0 fail, 1 refused, 0 unverified"} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	tk, err := store.Get("vf-evil")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(tk.Body, "- REFUSED (rm -rf "+sentinel+"): Hostile check.") {
		t.Errorf("recorded test results should name the refused command:\n%s", tk.Body)
	}
	if contains(tk.Body, "FAIL") {
		t.Errorf("a refusal must not be recorded as a failure:\n%s", tk.Body)
	}
}

func TestVerifyStripsControlCharactersFromCriterionText(t *testing.T) {
	// The bullet text is ticket content too: an escape planted there prints on
	// the same line as the verdict, and the recorded record replays it on every
	// later `tk show`.
	store := verifyStore(t, "vf-spoof",
		"Description.\n\n## Acceptance Criteria\n\n- \x1b[1A\x1b[2KPASS (exit 0) all good.\n  verify: /bin/echo ok\n")

	out, err := captureVerify(t, "vf-spoof")
	if err != nil {
		t.Fatalf("verify with a passing criterion should not error: %v", err)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("CLI output printed a raw escape from the criterion text:\n%q", out)
	}

	tk, err := store.Get("vf-spoof")
	if err != nil {
		t.Fatal(err)
	}
	// The criteria section still holds what the author wrote; the record verify
	// wrote must not.
	_, record, _ := strings.Cut(tk.Body, "## Test Results")
	if strings.ContainsRune(record, 0x1b) {
		t.Errorf("recorded test results replay a raw escape from the criterion text:\n%q", record)
	}
}

func TestVerifyNoCriteria(t *testing.T) {
	verifyStore(t, "vf-none", "Description only.\n")

	if _, err := captureVerify(t, "vf-none"); err == nil {
		t.Error("verify on a ticket with no acceptance criteria should error")
	}
}
