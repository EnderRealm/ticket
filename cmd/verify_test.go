package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// verifyStore creates a temp store, unconfigured HOME included, holding one
// ticket with the given body.
func verifyStore(t *testing.T, id, body string) *ticket.FileStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TICKETS_DIR", dir)

	store := ticket.NewFileStore(dir)
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
	"- Passing check.\n  verify: true\n" +
	"- Failing check.\n  verify: echo boom; exit 3\n" +
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
		"1 pass, 1 fail, 1 unverified",
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
		"1 pass, 1 fail, 1 unverified",
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
	if got := strings.Count(tk.Body, "1 pass, 1 fail, 1 unverified"); got != 1 {
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
	verifyStore(t, "vf-ok", "Description.\n\n## Acceptance Criteria\n\n- Passing check.\n  verify: true\n- Docs updated.\n")

	out, err := captureVerify(t, "vf-ok")
	if err != nil {
		t.Errorf("verify with no failures should not error: %v", err)
	}
	if !contains(out, "1 pass, 0 fail, 1 unverified") {
		t.Errorf("output missing summary:\n%s", out)
	}
}

func TestVerifyNoCriteria(t *testing.T) {
	verifyStore(t, "vf-none", "Description only.\n")

	if _, err := captureVerify(t, "vf-none"); err == nil {
		t.Error("verify on a ticket with no acceptance criteria should error")
	}
}
