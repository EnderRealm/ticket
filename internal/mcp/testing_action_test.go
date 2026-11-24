package mcp_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	landedSHA   = "0123456789abcdef0123456789abcdef01234567"
	relandedSHA = "fedcba9876543210fedcba9876543210fedcba98"
)

const testedAcceptance = "- Builds.\n  verify: go build ./...\n- Wraps.\n  test: narrow the window\n- Scrolls.\n  test: scroll it"

// action applies one transition and returns the ticket the response carries.
func action(t *testing.T, session *mcp.ClientSession, id, actionID, transition string, more map[string]any) map[string]any {
	t.Helper()
	tk, receipt, replayed := actionResponse(t, applyAction(t, session, actionArgs(id, actionID, transition, more)), transition)
	if replayed || receipt["outcome"] != tk["status"] {
		t.Errorf("%s: replayed %v, receipt %v, status %v", transition, replayed, receipt, tk["status"])
	}
	return tk
}

func actionArgs(id, actionID, transition string, more map[string]any) map[string]any {
	args := map[string]any{"id": id, "action_id": actionID, "note": "note " + actionID, "transition": transition}
	for k, v := range more {
		args[k] = v
	}
	return args
}

func TestApplyActionLandPassFailRetest(t *testing.T) {
	session := testServer(t)
	tested := createTicketID(t, session, map[string]any{"title": "Tested", "acceptance": testedAcceptance})
	untested := createTicketID(t, session, map[string]any{"title": "Untested", "acceptance": "- Builds."})

	land := map[string]any{"commit": landedSHA}
	if tk := action(t, session, untested, "land", "land", land); tk["status"] != "done" || tk["outputs"].(map[string]any)["commit"] != landedSHA {
		t.Errorf("land with no test: criterion = %v at %v, want done", tk["status"], tk["outputs"])
	}
	tk := action(t, session, tested, "land", "land", land)
	if tk["status"] != "testing" || tk["outputs"].(map[string]any)["commit"] != landedSHA || tk["last_test"] != nil {
		t.Errorf("land with a test: criterion = %v, want testing", tk)
	}
	_, receipt, replayed := actionResponse(t, applyAction(t, session, actionArgs(tested, "land", "land", land)), "replay")
	if !replayed || receipt["commit"] != landedSHA {
		t.Errorf("land replay = %v %v", replayed, receipt)
	}

	tk = action(t, session, tested, "pass", "pass", nil)
	last, _ := tk["last_test"].(map[string]any)
	if tk["status"] != "done" || tk["closed"] == nil || last["result"] != "passed" || last["by"] != "test" || last["commit"] != landedSHA || last["checks"] != nil || last["note"] != nil {
		t.Errorf("pass = %v, last test %v", tk["status"], last)
	}
	tk = action(t, session, tested, "undo", "retest", nil)
	if tk["status"] != "testing" || tk["closed"] != nil || tk["last_test"] != nil {
		t.Errorf("retest = %v", tk)
	}
	// A client that sends its integers as strings is coerced.
	if tk = action(t, session, tested, "fail", "fail", map[string]any{"checks": []any{"2"}}); tk["status"] != "ready" {
		t.Errorf("fail = %v", tk["status"])
	}
	tk = action(t, session, tested, "reland", "land", map[string]any{"commit": relandedSHA})
	if tk["status"] != "testing" || tk["outputs"].(map[string]any)["commit"] != relandedSHA {
		t.Errorf("re-landed = %v at %v", tk["status"], tk["outputs"])
	}

	for _, args := range []map[string]any{
		actionArgs(untested, "x", "pass", nil),
		actionArgs(untested, "x", "fail", map[string]any{"checks": []any{1}}),
		actionArgs(untested, "x", "retest", nil),
		actionArgs(tested, "x", "retest", nil),
	} {
		if text := errorText(t, applyAction(t, session, args), args["transition"].(string)); !strings.HasPrefix(text, "transition not permitted: ") {
			t.Errorf("%v refused with %q", args, text)
		}
	}
	for _, args := range []map[string]any{
		actionArgs(tested, "x", "land", nil),
		actionArgs(tested, "x", "pass", map[string]any{"commit": landedSHA}),
		actionArgs(tested, "x", "pass", map[string]any{"checks": []any{1}}),
		actionArgs(tested, "x", "fail", map[string]any{"checks": []any{1, 1}}),
		actionArgs(tested, "x", "fail", map[string]any{"checks": []any{3}}),
	} {
		if text := errorText(t, applyAction(t, session, args), "a malformed request"); strings.HasPrefix(text, "transition not permitted: ") {
			t.Errorf("%v refused with %q", args, text)
		}
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "ticket_apply_action" {
			continue
		}
		props := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		transition := props["transition"].(map[string]any)["description"].(string)
		for _, name := range []ticket.Transition{ticket.TransitionLand, ticket.TransitionPass, ticket.TransitionFail, ticket.TransitionRetest} {
			if !strings.Contains(transition, string(name)+" (") {
				t.Errorf("the transition argument does not describe %s: %s", name, transition)
			}
			if !strings.Contains(tool.Description, "`"+string(name)+"`") {
				t.Errorf("the tool does not describe %s: %s", name, tool.Description)
			}
		}
		for _, arg := range []string{"commit", "checks"} {
			if d, _ := props[arg].(map[string]any)["description"].(string); d == "" {
				t.Errorf("%s has no description", arg)
			}
		}
		return
	}
	t.Error("ticket_apply_action is not registered")
}

func TestFailRequiresNoteAndReturnsToReady(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Tested", "acceptance": testedAcceptance})
	action(t, session, id, "land", "land", map[string]any{"commit": landedSHA})

	for _, more := range []map[string]any{
		{"checks": []any{1}, "note": ""},
		{"checks": []any{1}, "note": " \n"},
		{"checks": []any{}},
		nil,
	} {
		errorText(t, applyAction(t, session, actionArgs(id, "fail", "fail", more)), "a fail with no note or no check")
	}
	if shown := showTicket(t, session, id); shown["status"] != "testing" || shown["last_test"] != nil || len(notesOf(shown)) != 1 {
		t.Errorf("a refused fail left %v", shown)
	}

	before := time.Now().UTC().Truncate(time.Second)
	action(t, session, id, "fail", "fail", map[string]any{"checks": []any{2, 1}, "note": "Clipped.\n---\nAnd stuck.", "source": "phone"})
	for _, args := range []map[string]any{{"id": id}, {"id": id, "metadata_only": true}, {"id": id, "omit_notes": true}} {
		shown := callObject(t, session, "ticket_show", args)
		last, _ := shown["last_test"].(map[string]any)
		if shown["status"] != "ready" || last == nil {
			t.Fatalf("show %v = %v, last test %v; want ready with one", args, shown["status"], last)
		}
		at, err := time.Parse(time.RFC3339, last["at"].(string))
		if err != nil || at.Before(before) || at.After(time.Now()) {
			t.Errorf("at = %v, %v", last["at"], err)
		}
		delete(last, "at")
		want := map[string]any{
			"result": "failed", "note": "Clipped.\n---\nAnd stuck.", "by": "phone", "commit": landedSHA,
			"checks": []any{map[string]any{"index": 1.0, "text": "Wraps."}, map[string]any{"index": 2.0, "text": "Scrolls."}},
		}
		if !reflect.DeepEqual(last, want) {
			t.Errorf("last_test = %v, want %v", last, want)
		}
	}
	if notes := notesOf(showTicket(t, session, id)); len(notes) != 2 || notes[1].(map[string]any)["text"] != "Clipped.\n---\nAnd stuck." {
		t.Errorf("notes = %v, want the fail's note added", notes)
	}
}
