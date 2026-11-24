package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// criteriaOf returns ticket_criteria's entries for a ticket.
func criteriaOf(t *testing.T, session *mcp.ClientSession, id string) []map[string]any {
	t.Helper()
	raw, _ := json.Marshal(callObject(t, session, "ticket_criteria", map[string]any{"id": id})["criteria"])
	var criteria []map[string]any
	if err := json.Unmarshal(raw, &criteria); err != nil {
		t.Fatalf("criteria = %s: %v", raw, err)
	}
	return criteria
}

func TestCriteriaParsesTestMarker(t *testing.T) {
	session, _ := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Kinds", "acceptance": "- Runs.\n  verify: test -f x\n" +
		"- Rows fit.\n  test: run tk ls \x1b[2Jat 40 columns\n" +
		"- No text.\n  test:\n" +
		"- Reads plainly.\n"})

	criteria := criteriaOf(t, session, id)
	if len(criteria) != 4 {
		t.Fatalf("criteria = %v, want 4", criteria)
	}
	for i, want := range []struct {
		kind ticket.CriterionKind
		test any
	}{
		{ticket.CriterionVerify, nil},
		{ticket.CriterionTest, "run tk ls \ufffd[2Jat 40 columns"},
		{ticket.CriterionTest, nil},
		{ticket.CriterionJudged, nil},
	} {
		if got := criteria[i]; got["kind"] != string(want.kind) || got["test"] != want.test {
			t.Errorf("criteria[%d] = %v, want kind %s and test %v", i, got, want.kind, want.test)
		}
	}
}

func TestBareCriterionIsNotFlagged(t *testing.T) {
	session := testServer(t)
	const acceptance = "- Reads plainly.\n- Checked.\n  verify: go test ./..."

	created := callObject(t, session, "ticket_create", map[string]any{"title": "Judged", "description": "Why it matters", "acceptance": acceptance})
	edited := callObject(t, session, "ticket_edit", map[string]any{"id": created["id"], "acceptance": acceptance})
	shown := showResult(t, session, map[string]any{"id": created["id"]})
	for name, resp := range map[string]map[string]any{"create": created, "edit": edited, "show": shown} {
		for field := range resp {
			if strings.HasSuffix(field, "_warning") || strings.HasPrefix(field, "bare_") {
				t.Errorf("%s carries %s: %v", name, field, resp[field])
			}
		}
	}
	if audit, present := shown["audit"]; present {
		t.Errorf("show carries audit for a criterion with no line: %v", audit)
	}
	if shown["acceptance_criteria"] != acceptance {
		t.Errorf("stored acceptance = %v, want %q", shown["acceptance_criteria"], acceptance)
	}
}

func TestUnverifiableReadsAsJudgedByRun(t *testing.T) {
	session, _ := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Retired", "acceptance": "- Recipe recorded.\n  unverifiable: a recorded artifact\n"})

	criteria := criteriaOf(t, session, id)
	if len(criteria) != 1 || criteria[0]["kind"] != string(ticket.CriterionJudged) ||
		criteria[0]["unverifiable"] != true || criteria[0]["unverifiable_reason"] != "a recorded artifact" {
		t.Errorf("criteria = %v, want one judged criterion keeping its reason", criteria)
	}

	report := verifyReportArgs(t, session, map[string]any{"id": id})
	if got := report.Results[0]; got.Status != string(ticket.VerifyUnverified) || got.UnverifiableReason != "a recorded artifact" {
		t.Errorf("result = %+v, want unverified keeping its reason", got)
	}
}

func TestCriterionWithTwoMarkersIsReported(t *testing.T) {
	session := testServer(t)
	const both = "- Rows fit.\n  verify: go test ./cmd/\n  test: run tk ls at 40 columns"
	const single = "- Rows fit.\n  test: run tk ls at 40 columns"

	created := callObject(t, session, "ticket_create", map[string]any{"title": "Two markers", "description": "Why it matters", "acceptance": both})
	id, _ := created["id"].(string)
	want := ticket.TwoMarkersWarning(id, []string{"Rows fit."})
	if created["two_markers_warning"] != want {
		t.Errorf("create two_markers_warning = %v, want %q", created["two_markers_warning"], want)
	}

	edited := callObject(t, session, "ticket_edit", map[string]any{"id": id, "acceptance": both})
	if edited["two_markers_warning"] != want {
		t.Errorf("edit two_markers_warning = %v, want %q", edited["two_markers_warning"], want)
	}
	edited = callObject(t, session, "ticket_edit", map[string]any{"id": id, "title": "Retitled"})
	if w, present := edited["two_markers_warning"]; present {
		t.Errorf("title-only edit carried two_markers_warning %v", w)
	}

	content := auditContent(t, showResult(t, session, map[string]any{"id": id}))
	entry, _ := content[0].(map[string]any)
	if len(content) != 1 || entry["kind"] != string(ticket.ContentTwoMarkers) || entry["criteria"] != 1.0 {
		t.Errorf("audit.content = %v, want one two-markers issue", content)
	}

	edited = callObject(t, session, "ticket_edit", map[string]any{"id": id, "acceptance": single})
	if w, present := edited["two_markers_warning"]; present {
		t.Errorf("edit leaving one marker carried two_markers_warning %v", w)
	}
	if audit, present := showResult(t, session, map[string]any{"id": id})["audit"]; present {
		t.Errorf("show carries audit for a criterion with one marker: %v", audit)
	}
}

func TestVerifyReportsTestCriterionAsWaiting(t *testing.T) {
	session, _ := verifyServer(t)
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	id := createTicketID(t, session, map[string]any{"title": "Waiting", "acceptance": "- Runs.\n  verify: /bin/echo ok\n" +
		"- Rows fit.\n  test: run tk ls at 40 columns\n" +
		"- Both.\n  test: look\n  verify: /bin/sh -c 'touch " + sentinel + "'\n" +
		"- Reads plainly.\n"})

	report := verifyReportArgs(t, session, map[string]any{"id": id})
	if want := (ticket.VerifyCounts{Pass: 1, Unverified: 1, Waiting: 2}); report.Summary != want || !report.OK {
		t.Errorf("summary = %s ok = %v, want %s and ok", report.Summary, report.OK, want)
	}
	for i, want := range []struct {
		status ticket.VerifyStatus
		test   string
	}{
		{ticket.VerifyPass, ""},
		{ticket.VerifyWaiting, "run tk ls at 40 columns"},
		{ticket.VerifyWaiting, "look"},
		{ticket.VerifyUnverified, ""},
	} {
		if got := report.Results[i]; got.Status != string(want.status) || got.Test != want.test {
			t.Errorf("results[%d] = %+v, want %s with test %q", i, got, want.status, want.test)
		}
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("a test criterion ran its command: sentinel stat err = %v", err)
	}

	results, _ := showResult(t, session, map[string]any{"id": id})["test_results"].(string)
	for _, want := range []string{"1 pass, 0 fail, 0 refused, 1 unverified, 2 waiting", "- WAITING: Rows fit.", "- UNVERIFIED: Reads plainly."} {
		if !strings.Contains(results, want) {
			t.Errorf("recorded test results missing %q:\n%s", want, results)
		}
	}
}
