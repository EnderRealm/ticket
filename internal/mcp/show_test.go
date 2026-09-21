package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createWithNotes(t *testing.T, session *mcp.ClientSession, title string, noteCount int) string {
	t.Helper()
	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "ticket_create",
		Arguments: map[string]any{"title": title},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tk map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &tk)
	id, _ := tk["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}
	for i := 0; i < noteCount; i++ {
		if _, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "ticket_add_note",
			Arguments: map[string]any{
				"id":   id,
				"text": fmt.Sprintf("note %d", i),
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func showResult(t *testing.T, session *mcp.ClientSession, args map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_show",
		Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("show failed: %v", result.Content)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out)
	return out
}

func TestShow_DefaultCapsNotesToTwenty(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes cap", 30)

	out := showResult(t, session, map[string]any{"id": id})

	notes, _ := out["notes"].([]any)
	if len(notes) != 20 {
		t.Errorf("default notes returned = %d, want 20", len(notes))
	}
	if total, _ := out["notes_total"].(float64); int(total) != 30 {
		t.Errorf("notes_total = %v, want 30", out["notes_total"])
	}
	if shown, _ := out["notes_shown"].(float64); int(shown) != 20 {
		t.Errorf("notes_shown = %v, want 20", out["notes_shown"])
	}
	// Default window is newest-first; the last visible note should be the newest.
	last, _ := notes[len(notes)-1].(map[string]any)
	if last["text"] != "note 29" {
		t.Errorf("newest note in default window = %v, want %q", last["text"], "note 29")
	}
	first, _ := notes[0].(map[string]any)
	if first["text"] != "note 10" {
		t.Errorf("oldest note in default window = %v, want %q", first["text"], "note 10")
	}
}

func TestShow_NotesLimitZeroReturnsAll(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes all", 25)

	out := showResult(t, session, map[string]any{
		"id":          id,
		"notes_limit": 0,
	})

	notes, _ := out["notes"].([]any)
	if len(notes) != 25 {
		t.Errorf("notes returned = %d, want 25", len(notes))
	}
}

func TestShow_MetadataOnlySkipsNotes(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Metadata only", 5)

	out := showResult(t, session, map[string]any{
		"id":            id,
		"metadata_only": true,
	})

	if _, ok := out["notes"]; ok {
		t.Errorf("notes should be omitted with metadata_only, got %v", out["notes"])
	}
	if total, _ := out["notes_total"].(float64); int(total) != 5 {
		t.Errorf("notes_total = %v, want 5", out["notes_total"])
	}
	if shown, _ := out["notes_shown"].(float64); int(shown) != 0 {
		t.Errorf("notes_shown = %v, want 0", out["notes_shown"])
	}
}

func TestShow_NotesOffsetPagesBack(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "Notes paging", 30)

	// Skip the newest 20, ask for next 5.
	out := showResult(t, session, map[string]any{
		"id":           id,
		"notes_limit":  5,
		"notes_offset": 20,
	})

	notes, _ := out["notes"].([]any)
	if len(notes) != 5 {
		t.Fatalf("notes returned = %d, want 5", len(notes))
	}
	first, _ := notes[0].(map[string]any)
	last, _ := notes[len(notes)-1].(map[string]any)
	// After skipping 20 from newest (indices 10..29), next 5 are indices 5..9.
	if first["text"] != "note 5" {
		t.Errorf("first paged note = %v, want %q", first["text"], "note 5")
	}
	if last["text"] != "note 9" {
		t.Errorf("last paged note = %v, want %q", last["text"], "note 9")
	}
}

func TestShow_MetadataOnlyAsString(t *testing.T) {
	session := testServer(t)
	id := createWithNotes(t, session, "metadata only string", 3)

	out := showResult(t, session, map[string]any{
		"id":            id,
		"metadata_only": "true",
	})
	if _, ok := out["notes"]; ok {
		t.Errorf("notes should be omitted with metadata_only=\"true\"")
	}
}

const bareAcceptance = "- Something happens.\n"

const checkedAcceptance = "- Something happens.\n  verify: /bin/true\n"

// createFlagged creates a ticket whose only acceptance criterion carries no
// check, which is the one finding the write path warns about and stores.
func createFlagged(t *testing.T, session *mcp.ClientSession, args map[string]any) string {
	t.Helper()
	full := map[string]any{"title": "Flagged", "description": "A description.", "acceptance": bareAcceptance}
	for k, v := range args {
		full[k] = v
	}
	return createTicketID(t, session, full)
}

// auditContent is the audit.content list of a show response, failing the test
// when the response carries no audit.
func auditContent(t *testing.T, out map[string]any) []any {
	t.Helper()
	audit, ok := out["audit"].(map[string]any)
	if !ok {
		t.Fatalf("show carries no audit: %v", out)
	}
	content, _ := audit["content"].([]any)
	return content
}

// plantParent rewrites a stored ticket's frontmatter to name parent, which the
// write path refuses for a non-epic parent.
func plantParent(t *testing.T, path, parent string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "---\n", "---\nparent: "+parent+"\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShow_AuditReportsBareAcceptance(t *testing.T) {
	session := testServer(t)
	id := createFlagged(t, session, nil)

	content := auditContent(t, showResult(t, session, map[string]any{"id": id}))
	if len(content) != 1 {
		t.Fatalf("audit.content = %v, want one entry", content)
	}
	entry, _ := content[0].(map[string]any)
	if entry["kind"] != "bare-acceptance" || entry["bare"] != 1.0 || entry["id"] != id {
		t.Errorf("audit.content[0] = %v, want kind bare-acceptance, bare 1, id %s", entry, id)
	}
}

func TestShow_AuditOmittedWhenClean(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Clean", "description": "A description.", "acceptance": checkedAcceptance})

	out := showResult(t, session, map[string]any{"id": id})
	if _, present := out["audit"]; present {
		t.Errorf("a clean ticket carries audit: %v", out["audit"])
	}
}

func TestShow_AuditSurvivesMetadataOnly(t *testing.T) {
	session := testServer(t)
	id := createFlagged(t, session, nil)

	out := showResult(t, session, map[string]any{"id": id, "metadata_only": true})
	if _, present := out["notes"]; present {
		t.Errorf("notes should be omitted with metadata_only, got %v", out["notes"])
	}
	if content := auditContent(t, out); len(content) != 1 {
		t.Errorf("audit.content under metadata_only = %v, want one entry", content)
	}
}

func TestShow_AuditSurvivesTerminalStatus(t *testing.T) {
	session := testServer(t)
	id := createFlagged(t, session, nil)

	for _, status := range []string{"done", "closed"} {
		editStatus(t, session, id, status)
		out := showResult(t, session, map[string]any{"id": id})
		if out["status"] != status {
			t.Fatalf("status = %v, want %s", out["status"], status)
		}
		if content := auditContent(t, out); len(content) != 1 {
			t.Errorf("audit.content at %s = %v, want one entry", status, content)
		}
	}
}

func TestShow_AuditReportsParentNotEpic(t *testing.T) {
	session, dir := testServerDir(t)
	parent := createTicketID(t, session, map[string]any{"title": "Not an epic", "type": "feature"})
	child := createTicketID(t, session, map[string]any{"title": "Child", "type": "feature"})
	plantParent(t, filepath.Join(dir, child+".md"), parent)

	out := showResult(t, session, map[string]any{"id": child})
	if issue, _ := out["relationship_issue"].(string); issue == "" {
		t.Errorf("relationship_issue absent from a leaf whose parent is not an epic: %v", out)
	}
	audit, _ := out["audit"].(map[string]any)
	violation, _ := audit["parent"].(map[string]any)
	if violation["kind"] != "parent-not-epic" {
		t.Fatalf("audit.parent = %v, want kind parent-not-epic", violation)
	}
	if detail, _ := violation["detail"].(string); detail == "" {
		t.Errorf("audit.parent carries no detail: %v", violation)
	}
}

// auditJSON marshals a finding the way `tk audit --json` does and reads it back
// as the generic map a show response decodes to.
func auditJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestShow_AuditMatchesAuditJSONShape(t *testing.T) {
	session, dir := testServerDir(t)
	flagged := createFlagged(t, session, nil)
	parent := createTicketID(t, session, map[string]any{"title": "Not an epic", "type": "feature"})
	child := createTicketID(t, session, map[string]any{"title": "Child", "type": "feature"})
	plantParent(t, filepath.Join(dir, child+".md"), parent)

	report, err := ticket.Audit(ticket.NewFileStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Content) != 1 || len(report.Violations) != 1 {
		t.Fatalf("audit report = %+v, want one content issue and one violation", report)
	}

	content := auditContent(t, showResult(t, session, map[string]any{"id": flagged}))
	if !reflect.DeepEqual(content[0], auditJSON(t, report.Content[0])) {
		t.Errorf("show audit.content[0] = %v, tk audit --json reports %v", content[0], auditJSON(t, report.Content[0]))
	}

	audit, _ := showResult(t, session, map[string]any{"id": child})["audit"].(map[string]any)
	if !reflect.DeepEqual(audit["parent"], auditJSON(t, report.Violations[0])) {
		t.Errorf("show audit.parent = %v, tk audit --json reports %v", audit["parent"], auditJSON(t, report.Violations[0]))
	}
}

var statusLine = regexp.MustCompile(`(?m)^status: .*$`)

// plantStatus rewrites a stored ticket's frontmatter status, which for an epic
// the write path would derive from the children instead.
func plantStatus(t *testing.T, path, status string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, statusLine.ReplaceAll(raw, []byte("status: "+status)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestShow_AuditReportsEpicStatusDrift(t *testing.T) {
	session, dir := testServerDir(t)
	epic := createTicketID(t, session, map[string]any{"title": "Epic", "type": "epic"})
	createTicketID(t, session, map[string]any{"title": "Child", "type": "feature", "parent": epic})
	plantStatus(t, filepath.Join(dir, epic+".md"), "done")

	report, err := ticket.Audit(ticket.NewFileStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.EpicStatus) != 1 {
		t.Fatalf("audit report = %+v, want one epic status drift", report)
	}

	audit, _ := showResult(t, session, map[string]any{"id": epic})["audit"].(map[string]any)
	drift, _ := audit["epic_status"].(map[string]any)
	if drift["kind"] != string(ticket.EpicDriftStale) {
		t.Fatalf("audit.epic_status = %v, want kind %s", drift, ticket.EpicDriftStale)
	}
	if !reflect.DeepEqual(audit["epic_status"], auditJSON(t, report.EpicStatus[0])) {
		t.Errorf("show audit.epic_status = %v, tk audit --json reports %v", audit["epic_status"], auditJSON(t, report.EpicStatus[0]))
	}
}

func TestShow_AuditLeavesExistingFieldsInPlace(t *testing.T) {
	session := testServer(t)
	id := createFlagged(t, session, nil)

	out := showResult(t, session, map[string]any{"id": id})
	for _, field := range []string{"id", "status", "notes_total", "notes_shown", "namespace", "precondition"} {
		if _, present := out[field]; !present {
			t.Errorf("show no longer carries %s: %v", field, out)
		}
	}
}

func TestShow_AuditNamespacesIDsOnACentralStore(t *testing.T) {
	session, _ := testCentralServer(t, "proj")
	id := createFlagged(t, session, map[string]any{"project": "proj"})

	content := auditContent(t, showResult(t, session, map[string]any{"id": id}))
	if len(content) != 1 {
		t.Fatalf("audit.content = %v, want one entry", content)
	}
	entry, _ := content[0].(map[string]any)
	if !strings.HasPrefix(id, "proj/") || entry["id"] != id {
		t.Errorf("audit.content[0].id = %v, want the namespaced %s", entry["id"], id)
	}
}
