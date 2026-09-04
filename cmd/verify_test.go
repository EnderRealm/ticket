package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
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

	mkVerifyTicket(t, store, id, body)
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

func TestVerifyWorkDirAcceptsConfiguredProjectName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	want := t.TempDir()
	if err := project.Save(project.Config{
		CentralRoot: t.TempDir(),
		Projects: map[string]project.ProjectConfig{
			"vf-name": {Path: want, Store: "central"},
		},
	}); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	repoFlag = "vf-name"
	defer func() { repoFlag = "" }()

	if got := verifyWorkDir(); got != want {
		t.Errorf("verifyWorkDir = %q, want configured repo path %q", got, want)
	}
}

// setVerifyFlag sets a verify flag through the flag set, so Changed reports
// true the way a parsed command line would. Set leaves Changed true, so the
// cleanup restores the default and clears it rather than only resetting the
// variable.
func setVerifyFlag(t *testing.T, name, value string) {
	t.Helper()
	f := verifyCmd.Flags().Lookup(name)
	if f == nil {
		t.Fatalf("no --%s flag", name)
	}
	if err := verifyCmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
	t.Cleanup(func() {
		f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

func setCriterion(t *testing.T, n int) {
	t.Helper()
	setVerifyFlag(t, "criterion", strconv.Itoa(n))
}

func TestVerifyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The marker only exists in --dir, so the check passes there and nowhere else.
	verifyStore(t, "vf-dir", "Description.\n\n## Acceptance Criteria\n\n- Runs where --dir says.\n  verify: /bin/sh -c 'test -f marker.txt'\n")

	setVerifyFlag(t, "dir", dir)
	jsonOutput = true
	defer func() { jsonOutput = false }()

	out, err := captureVerify(t, "vf-dir")
	if err != nil {
		t.Fatalf("verify in --dir should pass: %v\n%s", err, out)
	}

	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if report.Dir != dir {
		t.Errorf("report dir = %q, want the --dir value %q rather than the configured project path", report.Dir, dir)
	}
	if report.Summary.Pass != 1 {
		t.Errorf("summary = %+v, want the marker check to pass in --dir", report.Summary)
	}
}

func TestVerifyDirRejected(t *testing.T) {
	tmp := t.TempDir()
	sentinel := filepath.Join(tmp, "sentinel.txt")
	regular := filepath.Join(tmp, "regular.txt")
	if err := os.WriteFile(regular, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := verifyStore(t, "vf-baddir",
		"Description.\n\n## Acceptance Criteria\n\n- Touches a sentinel.\n  verify: /bin/sh -c 'touch "+sentinel+"'\n")

	// An explicitly empty --dir is refused too, rather than read as "not
	// passed" and quietly running in the configured tree.
	for _, dir := range []string{filepath.Join(tmp, "missing"), regular, ""} {
		setVerifyFlag(t, "dir", dir)
		if _, err := captureVerify(t, "vf-baddir"); err == nil {
			t.Errorf("--dir %q should be refused", dir)
		}
		if _, err := os.Stat(sentinel); err == nil {
			t.Fatalf("--dir %q ran the criterion's command", dir)
		}
	}

	tk, err := store.Get("vf-baddir")
	if err != nil {
		t.Fatal(err)
	}
	if contains(tk.Body, "## Test Results") {
		t.Errorf("a refused --dir must record nothing:\n%s", tk.Body)
	}
}

func TestVerifyCriterionSelect(t *testing.T) {
	verifyStore(t, "vf-one", mixedCriteriaBody)

	t.Run("failing criterion", func(t *testing.T) {
		setCriterion(t, 2)

		out, err := captureVerify(t, "vf-one")
		if err == nil {
			t.Error("selecting the failing criterion should return an error")
		}
		if !contains(out, "FAIL (exit 3) Failing check.") {
			t.Errorf("output missing the selected criterion:\n%s", out)
		}
		for _, unwanted := range []string{"Passing check.", "Docs updated."} {
			if contains(out, unwanted) {
				t.Errorf("output reports %q, which was not selected:\n%s", unwanted, out)
			}
		}
		if !contains(out, "0 pass, 1 fail, 0 refused, 0 unverified") {
			t.Errorf("summary should count the selected criterion alone:\n%s", out)
		}
	})

	t.Run("json report", func(t *testing.T) {
		setCriterion(t, 2)
		jsonOutput = true
		defer func() { jsonOutput = false }()

		out, _ := captureVerify(t, "vf-one")
		var report ticket.VerifyReport
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("json parse: %v\noutput: %s", err, out)
		}
		if len(report.Results) != 1 {
			t.Fatalf("got %d results, want only the selected criterion", len(report.Results))
		}
	})

	t.Run("recorded results", func(t *testing.T) {
		setCriterion(t, 2)

		store := TicketStore()
		captureVerify(t, "vf-one")
		tk, err := store.Get("vf-one")
		if err != nil {
			t.Fatal(err)
		}
		if !contains(tk.Body, "- FAIL (exit 3): Failing check.") {
			t.Errorf("recorded results missing the selected criterion:\n%s", tk.Body)
		}
		if contains(tk.Body, "- PASS (exit 0): Passing check.") {
			t.Errorf("recorded results carry a criterion that was not run:\n%s", tk.Body)
		}
	})

	t.Run("passing criterion", func(t *testing.T) {
		setCriterion(t, 1)

		out, err := captureVerify(t, "vf-one")
		if err != nil {
			t.Errorf("selecting the passing criterion should not error: %v", err)
		}
		if !contains(out, "PASS (exit 0) Passing check.") || contains(out, "Failing check.") {
			t.Errorf("output should report the passing criterion alone:\n%s", out)
		}
	})
}

func TestVerifyCriterionOutOfRange(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	store := verifyStore(t, "vf-range", "Description.\n\n## Acceptance Criteria\n\n"+
		"- One.\n  verify: /bin/sh -c 'touch "+sentinel+"'\n- Two.\n- Three.\n")

	for _, n := range []int{4, 0} {
		t.Run(fmt.Sprintf("criterion %d", n), func(t *testing.T) {
			setCriterion(t, n)

			_, err := captureVerify(t, "vf-range")
			if err == nil || !contains(err.Error(), "out of range") {
				t.Errorf("--criterion %d error = %v, want an out of range usage error", n, err)
			}
		})
	}

	if _, err := os.Stat(sentinel); err == nil {
		t.Error("an out of range --criterion ran a command")
	}
	tk, err := store.Get("vf-range")
	if err != nil {
		t.Fatal(err)
	}
	if contains(tk.Body, "## Test Results") {
		t.Errorf("an out of range --criterion must record nothing:\n%s", tk.Body)
	}
}

func TestVerifyNoRecord(t *testing.T) {
	store := verifyStore(t, "vf-norec", mixedCriteriaBody+"\n## Test Results\n\nprevious record\n")
	path := filepath.Join(store.Dir, "vf-norec.md")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	verifyNoRecord = true
	captureVerify(t, "vf-norec")
	verifyNoRecord = false

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("--no-record rewrote the ticket:\n%s", after)
	}

	// The same run without the flag must change the file, or the check above
	// proves nothing about the flag.
	captureVerify(t, "vf-norec")
	recorded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(recorded) == string(before) {
		t.Error("a recording run left the ticket unchanged")
	}
}

func TestVerifyExitCodes(t *testing.T) {
	// /usr/bin/true is a real program outside the fixture's allow-list, so it is
	// refused rather than failing.
	verifyStore(t, "vf-exit", "Description.\n\n## Acceptance Criteria\n\n"+
		"- Passing check.\n  verify: /bin/sh -c 'exit 0'\n"+
		"- Failing check.\n  verify: /bin/sh -c 'exit 3'\n"+
		"- Refused check.\n  verify: /usr/bin/true\n"+
		"- Docs updated.\n")

	if got := exitCode(nil); got != 0 {
		t.Errorf("exitCode(nil) = %d, want 0", got)
	}

	for _, tc := range []struct {
		name      string
		criterion int
		want      int
	}{
		{"pass", 1, 0},
		{"fail", 2, 1},
		{"refused", 3, verifyRefusedExit},
		{"unverified", 4, verifyRefusedExit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setCriterion(t, tc.criterion)

			_, err := captureVerify(t, "vf-exit")
			if got := exitCode(err); got != tc.want {
				t.Errorf("--criterion %d exits %d (err %v), want %d", tc.criterion, got, err, tc.want)
			}
		})
	}

	// A full run keeps today's contract: any failure or refusal exits 1.
	_, err := captureVerify(t, "vf-exit")
	if got := exitCode(err); got != 1 {
		t.Errorf("full run exits %d (err %v), want 1", got, err)
	}
}

// TestVerifyAllowUnmovedByFlags pins the allow-list to the machine owner's own
// config while the flags move the run: --dir does not make the run directory a
// source of permission, and TK_STORE_ROOT does not relocate the file the list
// is read from.
func TestVerifyAllowUnmovedByFlags(t *testing.T) {
	allowBody := func(sentinel string) string {
		return "Description.\n\n## Acceptance Criteria\n\n- Touches a sentinel.\n  verify: /bin/sh -c 'touch " + sentinel + "'\n"
	}
	// The machine-local list refuses everything; each branch below plants
	// /bin/sh somewhere a flag reached.
	narrowHomeAllow := func(t *testing.T) {
		t.Helper()
		cfg, err := project.Load()
		if err != nil {
			t.Fatalf("Load config: %v", err)
		}
		cfg.VerifyAllow = project.VerifyAllowList{}
		if err := project.Save(cfg); err != nil {
			t.Fatalf("Save config: %v", err)
		}
	}
	assertRefused := func(t *testing.T, out, sentinel string) {
		t.Helper()
		if !contains(out, "0 pass, 0 fail, 1 refused, 0 unverified") {
			t.Errorf("run was not refused:\n%s", out)
		}
		if _, err := os.Stat(sentinel); err == nil {
			t.Error("the criterion's command ran")
		}
	}

	t.Run("config in --dir", func(t *testing.T) {
		sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
		store := centralStore(t, "vf-allow")
		narrowHomeAllow(t)
		mkVerifyTicket(t, store, "vf-ad", allowBody(sentinel))

		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".ticket"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".ticket", "config.yaml"),
			[]byte("verify_allow:\n  - /bin/sh\nprojects: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		setVerifyFlag(t, "dir", dir)

		out, err := captureVerify(t, "vf-ad")
		if err == nil {
			t.Error("a refused criterion should return an error")
		}
		assertRefused(t, out, sentinel)
	})

	t.Run("config in the store root override", func(t *testing.T) {
		sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
		centralStore(t, "vf-allow")
		// Written before the override is set, so it lands in HOME.
		narrowHomeAllow(t)

		override := t.TempDir()
		t.Setenv(project.StoreRootEnv, override)
		if err := project.Save(project.Config{
			VerifyAllow: project.VerifyAllowList{"/bin/sh"},
			Projects: map[string]project.ProjectConfig{
				"vf-allow": {Path: mustGetwd(), Store: "central"},
			},
		}); err != nil {
			t.Fatalf("Save override config: %v", err)
		}

		dir := filepath.Join(override, "tickets", "vf-allow")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mkVerifyTicket(t, ticket.NewProjectFileStore(dir, "vf-allow"), "vf-ao", allowBody(sentinel))

		out, err := captureVerify(t, "vf-ao")
		if err == nil {
			t.Error("a refused criterion should return an error")
		}
		assertRefused(t, out, sentinel)
	})
}

// mkVerifyTicket seeds a ticket in an already-configured store: verifyStore's
// own construction, and the fixtures its allow-list would defeat.
func mkVerifyTicket(t *testing.T, store *ticket.FileStore, id, body string) {
	t.Helper()
	if err := store.Create(&ticket.Ticket{
		ID:      id,
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Verifiable ticket",
		Body:    body,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}
