package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// verifyWorktree turns verifyServer's repo directory into a git repository
// whose committed marker reads "main", and adds a linked worktree beside it
// whose marker reads "worktree". A criterion reading the marker then reports
// which of the two it ran in. Returns the worktree path as git created it.
func verifyWorktree(t *testing.T, repoDir string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, "marker"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repoDir), filepath.Base(repoDir)+"-wt")
	for _, argv := range [][]string{
		{"init", "-q"},
		{"add", "marker"},
		{"-c", "user.email=tk@test", "-c", "user.name=tk", "commit", "-q", "-m", "marker"},
		{"worktree", "add", "-q", worktree},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repoDir}, argv...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", argv, err, out)
		}
	}
	t.Cleanup(func() { os.RemoveAll(worktree) })
	if err := os.WriteFile(filepath.Join(worktree, "marker"), []byte("worktree"), 0o644); err != nil {
		t.Fatal(err)
	}
	return worktree
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	eval, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return eval
}

func TestCriteriaReturnsCanonicalCriteriaAndIdentity(t *testing.T) {
	session, _ := verifyServer(t)
	id := createTicketID(t, session, map[string]any{
		"title": "Contract",
		"type":  "feature",
		"acceptance": "- Runs.\n  verify: /bin/echo ok\n" +
			"- Hand-checked \x1b[2Jspoof.\n  unverifiable: needs a \x1b[1mhuman\n" +
			"- Bare.\n",
	})

	got := callObject(t, session, "ticket_criteria", map[string]any{"id": id})
	if got["id"] != id {
		t.Errorf("id = %v, want %s", got["id"], id)
	}
	acceptanceID, _ := got["acceptance_id"].(string)
	if !strings.HasPrefix(acceptanceID, "sha256:") {
		t.Errorf("acceptance_id = %q, want a sha256: identity", acceptanceID)
	}
	raw, _ := json.Marshal(got["criteria"])
	var criteria []map[string]any
	if err := json.Unmarshal(raw, &criteria); err != nil || len(criteria) != 3 {
		t.Fatalf("criteria = %s, want 3 entries", raw)
	}
	if criteria[0]["index"] != float64(1) || criteria[0]["text"] != "Runs." || criteria[0]["command"] != "/bin/echo ok" || criteria[0]["unverifiable"] != false {
		t.Errorf("criteria[0] = %v", criteria[0])
	}
	if criteria[1]["index"] != float64(2) || criteria[1]["unverifiable"] != true || criteria[1]["command"] != nil {
		t.Errorf("criteria[1] = %v", criteria[1])
	}
	for _, field := range []string{"text", "unverifiable_reason"} {
		if s, _ := criteria[1][field].(string); strings.ContainsRune(s, 0x1b) {
			t.Errorf("criteria[1].%s carries a raw escape: %q", field, s)
		}
	}
	if criteria[2]["index"] != float64(3) || criteria[2]["command"] != nil || criteria[2]["unverifiable"] != false || criteria[2]["unverifiable_reason"] != nil {
		t.Errorf("criteria[2] = %v", criteria[2])
	}

	// The identity ticket_verify reports is the one ticket_criteria handed out,
	// so a caller can hold the run to it.
	report := verifyReportArgs(t, session, map[string]any{"id": id, "acceptance_id": acceptanceID})
	if report.AcceptanceID != acceptanceID {
		t.Errorf("ticket_verify acceptance_id = %q, want %q", report.AcceptanceID, acceptanceID)
	}

	// No criteria is an empty contract, not an error.
	bare := createTicketID(t, session, map[string]any{"title": "No contract", "type": "feature"})
	empty := callObject(t, session, "ticket_criteria", map[string]any{"id": bare})
	if list, ok := empty["criteria"].([]any); !ok || len(list) != 0 {
		t.Errorf("criteria = %v, want an empty list", empty["criteria"])
	}
	if empty["acceptance_id"] != ticket.CriteriaIdentity(nil) {
		t.Errorf("acceptance_id = %v, want the identity of the empty contract", empty["acceptance_id"])
	}
}

// verifyReportArgs runs ticket_verify with the given arguments and returns
// the quick-path report.
func verifyReportArgs(t *testing.T, session *mcp.ClientSession, args map[string]any) ticket.VerifyReport {
	t.Helper()
	result := callTool(t, session, "ticket_verify", args)
	if result.IsError {
		t.Fatalf("ticket_verify error: %v", result.Content)
	}
	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &report); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	return report
}

func TestVerifyDirRunsInTheWorktree(t *testing.T) {
	session, repoDir := verifyServer(t)
	worktree := verifyWorktree(t, repoDir)
	id := createTicketID(t, session, map[string]any{
		"title":      "Where it ran",
		"type":       "feature",
		"acceptance": "- Reads the marker.\n  verify: /bin/cat marker\n",
	})
	acceptanceID := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)

	// Without dir: the configured checkout, as before.
	report := verifyReportArgs(t, session, map[string]any{"id": id})
	if report.Dir != repoDir || report.Results[0].Output != "main" {
		t.Fatalf("ID-only run = %q in %q, want the checkout's marker in %q", report.Results[0].Output, report.Dir, repoDir)
	}
	if report.AcceptanceID != acceptanceID || report.Candidate != "" {
		t.Errorf("ID-only provenance = %q %q, want the identity and no candidate", report.AcceptanceID, report.Candidate)
	}

	// With dir: the worktree, and the report names it canonical.
	report = verifyReportArgs(t, session, map[string]any{
		"id": id, "dir": worktree, "acceptance_id": acceptanceID, "candidate": "run-42",
	})
	if report.Dir != canonical(t, worktree) {
		t.Errorf("dir = %q, want the canonical worktree %q", report.Dir, canonical(t, worktree))
	}
	if !report.OK || len(report.Results) != 1 || report.Results[0].Output != "worktree" {
		t.Errorf("report = %+v, want a pass reading the worktree's marker", report)
	}
	if report.AcceptanceID != acceptanceID || report.Candidate != "run-42" {
		t.Errorf("provenance = %q %q, want %q and run-42", report.AcceptanceID, report.Candidate, acceptanceID)
	}

	// The durable record carries the same provenance.
	shown := showTicket(t, session, id)
	record, _ := shown["test_results"].(string)
	for _, want := range []string{
		"1 pass, 0 fail, 0 refused, 0 unverified",
		"checkout " + canonical(t, worktree) + "; acceptance " + acceptanceID + "; candidate run-42",
		"- PASS (exit 0): Reads the marker.",
	} {
		if !strings.Contains(record, want) {
			t.Errorf("test_results lacks %q:\n%s", want, record)
		}
	}
}

func TestVerifyDirEqualToTheCheckoutIsAccepted(t *testing.T) {
	session, repoDir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{
		"title":      "Same place",
		"type":       "feature",
		"acceptance": "- Echoes.\n  verify: /bin/echo here\n",
	})
	// No git repository at all: the configured checkout accepts itself.
	report := verifyReportArgs(t, session, map[string]any{"id": id, "dir": repoDir})
	if report.Dir != canonical(t, repoDir) || !report.OK {
		t.Errorf("report = %+v, want a pass in the canonical checkout %q", report, canonical(t, repoDir))
	}
}

func TestVerifyRefusesAChangedAcceptanceIDBeforeRunning(t *testing.T) {
	session, repoDir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{
		"title":      "Moved contract",
		"type":       "feature",
		"acceptance": "- Leaves a trace.\n  verify: /bin/sh -c 'echo ran > trace'\n",
	})
	stale := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "acceptance": "- Leaves a trace.\n  verify: /bin/sh -c 'echo changed > trace'\n"})
	current := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	if stale == current {
		t.Fatal("editing the command did not move the identity")
	}

	refused := callTool(t, session, "ticket_verify", map[string]any{"id": id, "acceptance_id": stale})
	text := errorText(t, refused, "ticket_verify with a stale acceptance_id")
	for _, want := range []string{"acceptance criteria", "changed", stale, current} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal = %s, want it to name %q", text, want)
		}
	}
	if _, err := os.Stat(filepath.Join(repoDir, "trace")); !os.IsNotExist(err) {
		t.Errorf("a refused verify ran its command: %v", err)
	}
	if shown := showTicket(t, session, id); shown["test_results"] != nil {
		t.Errorf("a refused verify recorded results: %v", shown["test_results"])
	}
	if status := callTool(t, session, "ticket_verify_status", map[string]any{"id": id}); !status.IsError {
		t.Errorf("a refused verify started a job: %v", status.Content)
	}

	// The current identity runs.
	if report := verifyReportArgs(t, session, map[string]any{"id": id, "acceptance_id": current}); !report.OK {
		t.Errorf("report = %+v, want a pass under the current identity", report)
	}
}

func TestVerifyDoesNotRecordAContractThatChangedMidRun(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Moving target", "acceptance": latchedVerify})
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "test_results": "prior verification record"})
	ran := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	beginCancelledVerifyRequest(t, session, id, dir)
	jobID := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})["verification_id"].(string)

	// The contract moves under the running command.
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "acceptance": "- Something else.\n  verify: /bin/echo other\n"})
	now := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	releaseVerify(t, dir)

	terminal := callObject(t, session, "ticket_verify_status", map[string]any{"verification_id": jobID})
	if terminal["state"] != "completed" {
		t.Fatalf("terminal = %#v", terminal)
	}
	raw, _ := json.Marshal(terminal["report"])
	var report ticket.VerifyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	// The results themselves are what ran, against the contract that ran.
	if !report.OK || report.AcceptanceID != ran || report.Results[0].Output != "exact-output" {
		t.Errorf("report = %+v, want the run's own results under %s", report, ran)
	}
	for _, want := range []string{"changed during verification", ran, now, "not recorded"} {
		if !strings.Contains(report.RecordError, want) {
			t.Errorf("record_error = %q, want it to say %q", report.RecordError, want)
		}
	}
	if shown := showTicket(t, session, id); shown["test_results"] != "prior verification record" {
		t.Errorf("a run against the old contract was recorded under the new one: %v", shown["test_results"])
	}
}

func TestVerifyDirRefusals(t *testing.T) {
	session, repoDir := verifyServer(t)
	worktree := verifyWorktree(t, repoDir)
	base := filepath.Dir(repoDir)
	unrelated := filepath.Join(base, "unrelated")
	plain := filepath.Join(base, "plain")
	sub := filepath.Join(worktree, "sub")
	for _, d := range []string{unrelated, plain, sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=tk@test", "-c", "user.name=tk", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", unrelated}, argv...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", argv, err, out)
		}
	}
	// An alias inside the checkout that lands outside the repository.
	escape := filepath.Join(repoDir, "escape")
	if err := os.Symlink(unrelated, escape); err != nil {
		t.Fatal(err)
	}
	id := createTicketID(t, session, map[string]any{
		"title":      "Refused directories",
		"type":       "feature",
		"acceptance": "- Leaves a trace.\n  verify: /bin/sh -c 'echo ran > trace'\n",
	})

	for _, tc := range []struct {
		name string
		dir  any
		want string
	}{
		{"unrelated repository", unrelated, "another repository"},
		{"missing", filepath.Join(base, "missing"), "not an existing directory"},
		{"subdirectory", sub, "not a worktree root"},
		{"symlink escape", escape, "another repository"},
		{"not a git worktree", plain, "not a git worktree"},
		{"empty", "", "dir is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused := callTool(t, session, "ticket_verify", map[string]any{"id": id, "dir": tc.dir})
			if text := errorText(t, refused, "ticket_verify dir "+tc.name); !strings.Contains(text, tc.want) {
				t.Errorf("refusal = %s, want it to say %q", text, tc.want)
			}
		})
	}
	for _, d := range []string{repoDir, worktree, unrelated, plain, sub} {
		if _, err := os.Stat(filepath.Join(d, "trace")); !os.IsNotExist(err) {
			t.Errorf("a refused verify ran its command in %s: %v", d, err)
		}
	}
	if shown := showTicket(t, session, id); shown["test_results"] != nil {
		t.Errorf("a refused verify recorded results: %v", shown["test_results"])
	}
}

func TestVerifyDirKeepsTheProjectsPolicy(t *testing.T) {
	// The worktree moves where the commands run; the allow-list and the bound
	// are still the project's, from machine-local config.
	session, repoDir := verifyServer(t)
	worktree := verifyWorktree(t, repoDir)
	setVerifyTimeout(t, "200ms")
	id := createTicketID(t, session, map[string]any{
		"title": "Policy in a worktree",
		"type":  "feature",
		"acceptance": "- Not permitted.\n  verify: /usr/bin/true\n" +
			"- Slow.\n  verify: /bin/sh -c 'sleep 3'\n" +
			"- No shell.\n  verify: /bin/echo a; /bin/cat marker\n",
	})
	report := verifyReportArgs(t, session, map[string]any{"id": id, "dir": worktree})
	if report.Results[0].Status != string(ticket.VerifyRefused) || !strings.Contains(report.Results[0].Output, "verify_allow") {
		t.Errorf("allow-list did not apply in the worktree: %+v", report.Results[0])
	}
	if report.Results[1].Status != string(ticket.VerifyFail) || !strings.Contains(report.Results[1].Output, "timed out after 200ms") {
		t.Errorf("project timeout did not apply in the worktree: %+v", report.Results[1])
	}
	if report.Results[2].Status != string(ticket.VerifyPass) || report.Results[2].Output != "a; /bin/cat marker" {
		t.Errorf("shell semantics appeared in the worktree: %+v", report.Results[2])
	}
}

func TestVerifyIDOnlyCallIsUnchanged(t *testing.T) {
	session, repoDir := verifyServer(t)
	if err := os.WriteFile(filepath.Join(repoDir, "marker"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := createTicketID(t, session, map[string]any{
		"title":      "Plain call",
		"type":       "feature",
		"acceptance": "- Reads the marker.\n  verify: /bin/cat marker\n",
	})
	result := callTool(t, session, "ticket_verify", map[string]any{"id": id})
	if result.IsError {
		t.Fatalf("ticket_verify error: %v", result.Content)
	}
	var quick map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &quick); err != nil {
		t.Fatal(err)
	}
	// Quick-path shape: the report itself, not the job envelope; the configured
	// checkout as registered; the identity present; no candidate key.
	if quick["state"] != nil || quick["verification_id"] != nil {
		t.Errorf("ID-only quick call returned the job envelope: %v", quick)
	}
	if quick["dir"] != repoDir || quick["ok"] != true {
		t.Errorf("report = %v, want ok in %q", quick, repoDir)
	}
	if _, present := quick["candidate"]; present {
		t.Errorf("candidate present with none supplied: %v", quick)
	}
	if id, _ := quick["acceptance_id"].(string); !strings.HasPrefix(id, "sha256:") {
		t.Errorf("acceptance_id = %v, want the identity", quick["acceptance_id"])
	}
	record, _ := showTicket(t, session, id)["test_results"].(string)
	if !strings.HasPrefix(record, "verify ") || !strings.Contains(record, "\ncheckout "+repoDir+"; acceptance sha256:") || strings.Contains(record, "candidate") {
		t.Errorf("record = %q, want the counts line, the checkout and identity, and no candidate", record)
	}
}

// beginLatchedVerify starts a latched verification with the given arguments
// and returns once its command is running, abandoning the client request the
// way a timed-out caller would.
func beginLatchedVerify(t *testing.T, session *mcp.ClientSession, dir string, args map[string]any) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_verify", Arguments: args})
		done <- err
	}()
	waitVerifyFile(t, dir, "starts")
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected canceled client request")
	}
}

// joinsActiveVerify reports whether a repeated start joined the active job —
// blocking past the short deadline — rather than returning a refusal.
func joinsActiveVerify(t *testing.T, session *mcp.ClientSession, args map[string]any) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_verify", Arguments: args})
	if err != nil {
		return true
	}
	if !result.IsError {
		t.Fatalf("repeated start returned a result while the job was still running: %v", result.Content)
	}
	return false
}

func TestVerifyRepeatedStartJoinsOnlyTheSameInputs(t *testing.T) {
	session, repoDir := verifyServer(t)
	worktree := verifyWorktree(t, repoDir)
	id := createTicketID(t, session, map[string]any{"title": "Overlapping starts", "acceptance": latchedVerify})
	acceptanceID := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	beginLatchedVerify(t, session, repoDir, map[string]any{"id": id, "candidate": "one"})
	jobID := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})["verification_id"].(string)

	// Same directory, contract and candidate: joins.
	if !joinsActiveVerify(t, session, map[string]any{"id": id, "candidate": "one", "acceptance_id": acceptanceID}) {
		t.Error("a start with the same inputs did not join the active run")
	}
	// A different directory or candidate is a different verification: refused,
	// naming the active run, and nothing starts.
	for name, args := range map[string]map[string]any{
		"dir":          {"id": id, "dir": worktree, "candidate": "one"},
		"candidate":    {"id": id, "candidate": "two"},
		"no candidate": {"id": id},
	} {
		if joinsActiveVerify(t, session, args) {
			t.Errorf("%s: a start with different inputs joined the active run", name)
			continue
		}
		refused := callTool(t, session, "ticket_verify", args)
		text := errorText(t, refused, name)
		for _, want := range []string{"already has a verification running in " + repoDir, acceptanceID, `candidate "one"`} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: refusal = %s, want it to name %q", name, text, want)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, "starts")); !os.IsNotExist(err) {
		t.Errorf("a refused start ran in the worktree: %v", err)
	}

	// The contract moves under the run: a start against the new contract —
	// with or without its acceptance_id — is not the run in flight.
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "acceptance": latchedVerify + "- Added.\n  verify: /bin/echo added\n"})
	current := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)
	for name, args := range map[string]map[string]any{
		"current acceptance_id": {"id": id, "candidate": "one", "acceptance_id": current},
		"id only":               {"id": id, "candidate": "one"},
	} {
		if joinsActiveVerify(t, session, args) {
			t.Errorf("%s: a start against a changed contract joined the active run", name)
			continue
		}
		if text := errorText(t, callTool(t, session, "ticket_verify", args), name); !strings.Contains(text, "acceptance "+acceptanceID) {
			t.Errorf("%s: refusal = %s, want it to name the active run's contract", name, text)
		}
	}

	releaseVerify(t, repoDir)
	terminal := callObject(t, session, "ticket_verify_status", map[string]any{"verification_id": jobID})
	if terminal["state"] != "completed" {
		t.Fatalf("terminal = %#v", terminal)
	}
	starts, _ := os.ReadFile(filepath.Join(repoDir, "starts"))
	if string(starts) != "start\n" {
		t.Fatalf("refused starts ran the command: %q", starts)
	}
}
