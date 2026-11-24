package mcp_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const attachLink = "https://folio.example/api/images/"

func attachment(id, role string) map[string]any {
	return map[string]any{"id": id, "role": role, "link": attachLink + id}
}

func attach(t *testing.T, session *mcp.ClientSession, ticketID, id, role string) map[string]any {
	t.Helper()
	return callObject(t, session, "ticket_attach", map[string]any{"id": ticketID, "action": "add", "role": role, "attachment_id": id, "link": attachLink + id})
}

func wantAttachments(t *testing.T, what string, got map[string]any, want ...any) {
	t.Helper()
	var attachments any
	if len(want) > 0 {
		attachments = want
	}
	if !reflect.DeepEqual(got["attachments"], attachments) {
		t.Errorf("%s: attachments = %v, want %v", what, got["attachments"], attachments)
	}
	for _, key := range []string{"attachment-before", "attachment-after"} {
		if v, ok := got[key]; ok {
			t.Errorf("%s: carries the stored %s = %v", what, key, v)
		}
	}
}

func TestAttachmentAddReplaceRemove(t *testing.T) {
	session := projectServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Pictured", "set": map[string]any{"env": "prod"}})

	wantAttachments(t, "after", attach(t, session, id, "a1", "after"), attachment("a1", "after"))
	wantAttachments(t, "before", attach(t, session, id, "b1", "before"), attachment("b1", "before"), attachment("a1", "after"))
	wantAttachments(t, "replaced", attach(t, session, id, "a2", "after"), attachment("b1", "before"), attachment("a2", "after"))

	// Twice: removing one the ticket does not hold is not an error.
	for range 2 {
		tk := callObject(t, session, "ticket_attach", map[string]any{"id": id, "action": "remove", "role": "after", "source": "runner"})
		wantAttachments(t, "removed", tk, attachment("b1", "before"))
		if tk["env"] != "prod" {
			t.Errorf("env = %v, want the other extra kept", tk["env"])
		}
	}
	wantAttachments(t, "stored", callObject(t, session, "ticket_show", map[string]any{"id": id}), attachment("b1", "before"))

	if got, want := mutationSources(t), []string{"test", "test", "test", "test", "runner", "runner"}; !slices.Equal(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
}

func TestAttachmentsServedByListShowQuery(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Pictured"})
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "status": "ready"})
	bare := createTicketID(t, session, map[string]any{"title": "Nothing to see"})
	attach(t, session, id, "a1", "after")
	attach(t, session, id, "b1", "before")
	want := []any{attachment("b1", "before"), attachment("a1", "after")}

	for _, tool := range []string{"ticket_list", "ticket_ready", "ticket_frontier"} {
		seen := 0
		for _, row := range callObject(t, session, tool, map[string]any{})["tickets"].([]any) {
			row := row.(map[string]any)
			switch row["id"] {
			case id:
				seen++
				wantAttachments(t, tool, row, want...)
			case bare:
				wantAttachments(t, tool+" with none", row)
			}
		}
		if seen != 1 {
			t.Errorf("%s lists the ticket %d times", tool, seen)
		}
	}
	for what, args := range map[string]map[string]any{
		"ticket_show":               {"id": id},
		"ticket_show metadata_only": {"id": id, "metadata_only": true},
	} {
		wantAttachments(t, what, callObject(t, session, "ticket_show", args), want...)
	}
	wantAttachments(t, "ticket_show with none", callObject(t, session, "ticket_show", map[string]any{"id": bare}))
	wantAttachments(t, "ticket_add_note", callObject(t, session, "ticket_add_note", map[string]any{"id": id, "text": "n"}), want...)
}

func TestAttachmentRejectsUnknownRoleOrLink(t *testing.T) {
	session := testServer(t)
	id := createTicketID(t, session, map[string]any{"title": "Pictured"})
	attach(t, session, id, "b1", "before")

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"ticket_attach", map[string]any{"action": "add", "role": "during", "attachment_id": "x", "link": attachLink}, "must be one of before, after"},
		{"ticket_attach", map[string]any{"action": "remove", "role": "during"}, "must be one of before, after"},
		{"ticket_attach", map[string]any{"action": "add", "role": "before", "attachment_id": "x", "link": "http://folio.example/x"}, "must be an https URL"},
		{"ticket_attach", map[string]any{"action": "add", "role": "before", "attachment_id": "x"}, "must be an https URL"},
		{"ticket_attach", map[string]any{"action": "add", "role": "before", "link": attachLink}, "attachment id must not be empty"},
		{"ticket_attach", map[string]any{"action": "replace", "role": "before", "attachment_id": "x", "link": attachLink}, "action must be"},
		{"ticket_edit", map[string]any{"set": map[string]any{"attachment-before": ""}}, "is reserved"},
		{"ticket_edit", map[string]any{"set": map[string]any{"attachment-after": "x"}}, "is reserved"},
		{"ticket_edit", map[string]any{"set": map[string]any{"attachments": "x"}}, "is reserved"},
	} {
		c.args["id"] = id
		if text := errorText(t, callTool(t, session, c.tool, c.args), c.tool); !strings.Contains(text, c.want) {
			t.Errorf("%s %v refused with %q, want %q", c.tool, c.args, text, c.want)
		}
	}
	wantAttachments(t, "after the refusals", callObject(t, session, "ticket_show", map[string]any{"id": id}), attachment("b1", "before"))
}
