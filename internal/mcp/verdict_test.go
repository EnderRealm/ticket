package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	verdictHead  = "0123456789abcdef0123456789abcdef01234567"
	verdictOther = "fedcba9876543210fedcba9876543210fedcba98"
)

func recordVerdict(t *testing.T, session *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verdict_record",
		Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func currentVerdict(t *testing.T, session *mcp.ClientSession, id, head string) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ticket_verdict_current",
		Arguments: map[string]any{"id": id, "head": head},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("ticket_verdict_current error: %v", result.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	return out
}

func TestVerdictRecordThenCurrent(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict record"})

	result := recordVerdict(t, session, map[string]any{
		"id":       id,
		"sha":      verdictHead,
		"class":    "test-verified",
		"role":     "worker",
		"evidence": "go test ./...",
	})
	if result.IsError {
		t.Fatalf("ticket_verdict_record error: %v", result.Content)
	}

	out := currentVerdict(t, session, id, verdictHead)
	current, _ := out["current"].(map[string]any)
	if current == nil {
		t.Fatalf("current = %v, want the recorded row", out["current"])
	}
	if current["class"] != "test-verified" || current["sha"] != verdictHead {
		t.Errorf("current = %v, want the test-verified row at head", current)
	}
	if passes, _ := out["passes"].(bool); !passes {
		t.Error("passes = false, want true for test-verified at head")
	}
	if stale, _ := out["stale"].([]any); len(stale) != 0 {
		t.Errorf("stale = %v, want none", stale)
	}
}

func TestVerdictRecordRefusesUnknownClass(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict class"})

	result := recordVerdict(t, session, map[string]any{
		"id":       id,
		"sha":      verdictHead,
		"class":    "passed",
		"role":     "verifier",
		"evidence": "review session",
	})
	if !result.IsError {
		t.Fatal("ticket_verdict_record accepted an unknown class")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, class := range []string{"live-verified", "test-verified", "type-check-only", "verifier-blocked", "verifier-failed"} {
		if !strings.Contains(text, class) {
			t.Errorf("error %q does not name %q", text, class)
		}
	}
}

func TestVerdictVerifierSupersedesWorker(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict roles"})

	for _, args := range []map[string]any{
		{"id": id, "sha": verdictHead, "class": "test-verified", "role": "worker", "evidence": "self-report", "source": "worker-1"},
		{"id": id, "sha": verdictHead, "class": "verifier-failed", "role": "verifier", "evidence": "review session", "source": "verifier-1"},
	} {
		if result := recordVerdict(t, session, args); result.IsError {
			t.Fatalf("ticket_verdict_record error: %v", result.Content)
		}
	}

	out := currentVerdict(t, session, id, verdictHead)
	current, _ := out["current"].(map[string]any)
	if current == nil || current["role"] != "verifier" || current["by"] != "verifier-1" {
		t.Fatalf("current = %v, want the verifier row", out["current"])
	}
	if passes, _ := out["passes"].(bool); passes {
		t.Error("passes = true, want false: the verifier row failed")
	}
}

func TestVerdictAtOtherHeadIsStale(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict staleness"})

	if result := recordVerdict(t, session, map[string]any{
		"id":       id,
		"sha":      verdictOther,
		"class":    "live-verified",
		"role":     "verifier",
		"evidence": "manual run",
	}); result.IsError {
		t.Fatalf("ticket_verdict_record error: %v", result.Content)
	}

	out := currentVerdict(t, session, id, verdictHead)
	if out["current"] != nil {
		t.Errorf("current = %v, want null", out["current"])
	}
	if passes, _ := out["passes"].(bool); passes {
		t.Error("passes = true, want false with no verdict at head")
	}
	stale, _ := out["stale"].([]any)
	if len(stale) != 1 {
		t.Fatalf("stale = %v, want the row at the other sha", out["stale"])
	}
	row, _ := stale[0].(map[string]any)
	if row["sha"] != verdictOther {
		t.Errorf("stale row sha = %v, want %q", row["sha"], verdictOther)
	}
}

func TestVerdictBlockedIsNotAPass(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict blocked"})

	if result := recordVerdict(t, session, map[string]any{
		"id":       id,
		"sha":      verdictHead,
		"class":    "verifier-blocked",
		"role":     "verifier",
		"evidence": "sandbox denied the test run",
	}); result.IsError {
		t.Fatalf("ticket_verdict_record error: %v", result.Content)
	}

	out := currentVerdict(t, session, id, verdictHead)
	current, _ := out["current"].(map[string]any)
	if current == nil || current["class"] != "verifier-blocked" {
		t.Fatalf("current = %v, want the verifier-blocked row", out["current"])
	}
	if passes, _ := out["passes"].(bool); passes {
		t.Error("passes = true, want false: verifier-blocked is a verifier that could not run")
	}
}

func TestShowIncludesVerdicts(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Verdict in show"})

	if result := recordVerdict(t, session, map[string]any{
		"id":       id,
		"sha":      verdictHead,
		"class":    "type-check-only",
		"role":     "worker",
		"evidence": "go vet ./...",
	}); result.IsError {
		t.Fatalf("ticket_verdict_record error: %v", result.Content)
	}

	out := showResult(t, session, map[string]any{"id": id})
	verdicts, _ := out["verdicts"].([]any)
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %v, want one row", out["verdicts"])
	}
	row, _ := verdicts[0].(map[string]any)
	if row["class"] != "type-check-only" || row["evidence"] != "go vet ./..." {
		t.Errorf("row = %v, want the recorded type-check-only row", row)
	}
}
