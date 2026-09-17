package mcp_test

import (
	"context"
	"encoding/json"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestVerifyRequestCancellationDoesNotCancelCommands(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{
		"title":      "Cancellation boundary",
		"acceptance": "- Finishes after request cancellation.\n  verify: /bin/sh -c 'echo started > started; sleep 0.4; echo finished > finished'\n- Still runs.\n  verify: /bin/echo second\n",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_verify", Arguments: map[string]any{"id": id}})
		callDone <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("verify command did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-callDone; err == nil {
		t.Fatal("canceled client call unexpectedly succeeded")
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		status := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})
		if status["state"] == "completed" {
			break
		}
		if status["state"] != "running" && status["state"] != "recording" {
			t.Fatalf("unexpected status: %#v", status)
		}
		if time.Now().After(deadline) {
			t.Fatal("verification did not finish")
		}
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "ticket_show", Arguments: map[string]any{"id": id}})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "2 pass, 0 fail") {
		t.Fatalf("request cancellation prevented complete verification: %s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "finished")); err != nil {
		t.Fatalf("command did not finish: %v", err)
	}
}

// A filesystem latch keeps the command alive without depending on a particular
// scheduler speed; releasing it proves the MCP calls did not kill the process.
const latchedVerify = "- Latched command.\n  verify: /bin/sh -c 'echo start >> starts; while [ ! -f release ]; do sleep 0.02; done; echo exact-output'\n"

func waitVerifyFile(t *testing.T, dir, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("command did not create %s", name)
}

func releaseVerify(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func beginCancelledVerifyRequest(t *testing.T, session *mcp.ClientSession, id, dir string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ticket_verify", Arguments: map[string]any{"id": id}})
		done <- err
	}()
	waitVerifyFile(t, dir, "starts")
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected canceled client request")
	}
}

func TestVerifyLongRunHandoffAndRetrieval(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Long verification", "acceptance": latchedVerify})
	started := time.Now()
	pending := callObject(t, session, "ticket_verify", map[string]any{"id": id})
	if elapsed := time.Since(started); elapsed < 9*time.Second || elapsed > 15*time.Second {
		t.Fatalf("handoff took %v, want bounded 10s wait", elapsed)
	}
	if pending["state"] != "running" || pending["report"] != nil {
		t.Fatalf("pending = %#v", pending)
	}
	jobID, ok := pending["verification_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("missing verification_id: %#v", pending)
	}
	// A request deadline on both the duplicate start and a poll must neither
	// duplicate nor stop the running command.
	for _, name := range []string{"ticket_verify", "ticket_verify_status"} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{"id": id}})
		cancel()
		if err == nil {
			t.Fatalf("%s unexpectedly returned before deadline", name)
		}
	}
	releaseVerify(t, dir)
	terminal := callObject(t, session, "ticket_verify_status", map[string]any{"verification_id": jobID})
	if terminal["state"] != "completed" {
		t.Fatalf("terminal = %#v", terminal)
	}
	raw, err := json.Marshal(terminal["report"])
	if err != nil {
		t.Fatal(err)
	}
	var report ticket.VerifyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.ID != id || report.Dir != dir || !report.OK || report.Summary.Pass != 1 || len(report.Results) != 1 || report.Results[0].Output != "exact-output" {
		t.Fatalf("report = %+v", report)
	}
	for range 2 {
		again := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})
		if !reflect.DeepEqual(again, terminal) {
			t.Fatalf("retrieval changed: %#v vs %#v", again, terminal)
		}
	}
	starts, err := os.ReadFile(filepath.Join(dir, "starts"))
	if err != nil || string(starts) != "start\n" {
		t.Fatalf("duplicate execution: %q, %v", starts, err)
	}
	shown := showTicket(t, session, id)
	if !strings.Contains(shown["test_results"].(string), "1 pass, 0 fail, 0 refused, 0 unverified") {
		t.Fatalf("not recorded: %#v", shown)
	}
}

func TestVerifyLostStartResponseCanBeRecoveredAfterCompletion(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Lost response", "acceptance": latchedVerify})
	beginCancelledVerifyRequest(t, session, id, dir)
	releaseVerify(t, dir)
	result := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})
	if result["state"] != "completed" {
		t.Fatalf("recovered = %#v", result)
	}
	if again := callObject(t, session, "ticket_verify_status", map[string]any{"id": id}); !reflect.DeepEqual(result, again) {
		t.Fatalf("unstable result: %#v", again)
	}
	starts, _ := os.ReadFile(filepath.Join(dir, "starts"))
	if string(starts) != "start\n" {
		t.Fatalf("recovery reran command: %q", starts)
	}
}

func TestVerifyExplicitCancellationPreservesPriorRecord(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Explicit cancel", "acceptance": latchedVerify})
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "test_results": "prior verification record"})
	beginCancelledVerifyRequest(t, session, id, dir)
	stopping := callObject(t, session, "ticket_verify_cancel", map[string]any{"id": id})
	jobID := stopping["verification_id"].(string)
	terminal := callObject(t, session, "ticket_verify_status", map[string]any{"verification_id": jobID})
	if terminal["state"] != "cancelled" || terminal["report"] != nil {
		t.Fatalf("terminal = %#v", terminal)
	}
	if again := callObject(t, session, "ticket_verify_cancel", map[string]any{"verification_id": jobID}); !reflect.DeepEqual(again, terminal) {
		t.Fatalf("cancel not idempotent: %#v", again)
	}
	if shown := showTicket(t, session, id); shown["test_results"] != "prior verification record" {
		t.Fatalf("partial run changed record: %#v", shown)
	}
}

func TestVerifySessionOwnershipAndDisconnect(t *testing.T) {
	server, dir := verifyTestServer(t)
	owner := connectVerifyServer(t, server)
	other := connectVerifyServer(t, server)
	id := createTicketID(t, owner, map[string]any{"title": "Session lifetime", "acceptance": latchedVerify})
	beginCancelledVerifyRequest(t, owner, id, dir)
	for _, name := range []string{"ticket_verify_status", "ticket_verify_cancel", "ticket_verify"} {
		if result := callTool(t, other, name, map[string]any{"id": id}); !result.IsError {
			t.Fatalf("%s allowed another session: %#v", name, result)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	// Wait for cancellation cleanup by retrying a harmless new verification;
	// until the old process is stopped the server refuses the same ticket.
	callObject(t, other, "ticket_edit", map[string]any{"id": id, "acceptance": "- New session.\n  verify: /bin/echo fresh\n"})
	deadline := time.Now().Add(7 * time.Second)
	for {
		result := callTool(t, other, "ticket_verify", map[string]any{"id": id})
		if !result.IsError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disconnected job did not stop: %#v", result.Content)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if shown := showTicket(t, other, id); !strings.Contains(shown["test_results"].(string), "PASS (exit 0): New session.") {
		t.Fatalf("new run not recorded: %#v", shown)
	}
	starts, _ := os.ReadFile(filepath.Join(dir, "starts"))
	if string(starts) != "start\n" {
		t.Fatalf("unexpected old execution: %q", starts)
	}
}

func TestVerifyStatusErrorsNeverStartCommands(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Lookup errors", "acceptance": latchedVerify})
	for _, name := range []string{"ticket_verify_status", "ticket_verify_cancel"} {
		for _, args := range []map[string]any{{}, {"id": id}, {"verification_id": "missing"}, {"id": id, "verification_id": "missing"}} {
			if result := callTool(t, session, name, args); !result.IsError {
				t.Fatalf("%s %v should error", name, args)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "starts")); !os.IsNotExist(err) {
		t.Fatalf("lookup started command: %v", err)
	}
}

func TestVerifyRecordErrorRetainsActualReport(t *testing.T) {
	session, dir := verifyServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Recording failure", "acceptance": latchedVerify})
	beginCancelledVerifyRequest(t, session, id, dir)
	// Recover the job ID without waiting for command completion.
	stopping := callObject(t, session, "ticket_verify_status", map[string]any{"id": id})
	jobID := stopping["verification_id"].(string)
	callObject(t, session, "ticket_delete", map[string]any{"id": id})
	releaseVerify(t, dir)
	terminal := callObject(t, session, "ticket_verify_status", map[string]any{"verification_id": jobID})
	if terminal["state"] != "completed" {
		t.Fatalf("terminal = %#v", terminal)
	}
	report := terminal["report"].(map[string]any)
	if report["record_error"] == nil || report["ok"] != true {
		t.Fatalf("result/error lost: %#v", report)
	}
}
