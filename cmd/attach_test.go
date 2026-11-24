package cmd

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

const attachLink = "https://folio.example/api/images/"

// runAttachment runs tk attach or tk detach against a fresh flag set parsed
// from args.
func runAttachment(t *testing.T, name string, args ...string) error {
	t.Helper()
	cmd := &cobra.Command{}
	run := runAttach
	if name == "attach" {
		registerAttachFlags(cmd)
	} else {
		registerDetachFlags(cmd)
		run = runDetach
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	return run(cmd, cmd.Flags().Args())
}

func mustAttach(t *testing.T, ticketID, id, role string) {
	t.Helper()
	if err := runAttachment(t, "attach", ticketID, "--role", role, "--attachment-id", id, "--link", attachLink+id); err != nil {
		t.Fatal(err)
	}
}

func attachment(id, role string) ticket.Attachment {
	return ticket.Attachment{ID: id, Role: ticket.AttachmentRole(role), Link: attachLink + id}
}

func TestAttachmentAddReplaceRemove(t *testing.T) {
	store := centralStore(t, "at")
	mkCentral(t, store, "at-1", ticket.TypeFeature, ticket.StatusOpen, "")
	want := func(want ...ticket.Attachment) {
		t.Helper()
		if got := ticket.Attachments(getTicket(t, store, "at-1")); !reflect.DeepEqual(got, want) {
			t.Errorf("attachments = %+v, want %+v", got, want)
		}
	}

	mustAttach(t, "at-1", "a1", "after")
	want(attachment("a1", "after"))
	mustAttach(t, "at-1", "b1", "before")
	want(attachment("b1", "before"), attachment("a1", "after"))
	mustAttach(t, "at-1", "a2", "after")
	want(attachment("b1", "before"), attachment("a2", "after"))

	// An edit of another extra field leaves them alone.
	if err := runEditWith(t, "at-1", "set", "env=prod"); err != nil {
		t.Fatal(err)
	}
	want(attachment("b1", "before"), attachment("a2", "after"))

	// Twice: removing one the ticket does not hold is not an error.
	for range 2 {
		if err := runAttachment(t, "detach", "at-1", "--role", "before"); err != nil {
			t.Fatal(err)
		}
		want(attachment("a2", "after"))
	}
}

func TestAttachmentsServedByListShowQuery(t *testing.T) {
	store := centralStore(t, "at")
	mkCentral(t, store, "at-1", ticket.TypeFeature, ticket.StatusReady, "")
	mkCentral(t, store, "at-2", ticket.TypeFeature, ticket.StatusReady, "")
	mustAttach(t, "at-1", "a1", "after")
	mustAttach(t, "at-1", "b1", "before")
	if err := runEditWith(t, "at-1", "set", "env=prod"); err != nil {
		t.Fatal(err)
	}

	check := func(what string, row map[string]any, want any) {
		t.Helper()
		if !reflect.DeepEqual(row["attachments"], want) {
			t.Errorf("%s: attachments = %v, want %v", what, row["attachments"], want)
		}
		for _, key := range []string{"attachment-before", "attachment-after"} {
			if v, ok := row[key]; ok {
				t.Errorf("%s: carries the stored %s = %v", what, key, v)
			}
		}
	}
	want := []any{
		map[string]any{"id": "b1", "role": "before", "link": attachLink + "b1"},
		map[string]any{"id": "a1", "role": "after", "link": attachLink + "a1"},
	}

	rows := captureQuery(t)
	check("tk query", rows["at-1"], want)
	check("tk query with none", rows["at-2"], nil)
	if rows["at-1"]["env"] != "prod" {
		t.Errorf("env = %v, want the other extra field kept", rows["at-1"]["env"])
	}

	// A tk frontier --json row.
	data, err := json.Marshal(toTicketJSON(getTicket(t, store, "at-1")))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	check("tk frontier --json", row, want)

	if help := renderHelp(t, queryCmd); !strings.Contains(help, "attachments[]") {
		t.Errorf("tk query --help does not list attachments:\n%s", help)
	}
}

func TestAttachmentRejectsUnknownRoleOrLink(t *testing.T) {
	store := centralStore(t, "at")
	mkCentral(t, store, "at-1", ticket.TypeFeature, ticket.StatusOpen, "")
	mustAttach(t, "at-1", "b1", "before")

	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"attach", []string{"--role", "during", "--attachment-id", "x", "--link", attachLink}, "must be one of before, after"},
		{"attach", []string{"--attachment-id", "x", "--link", attachLink}, "must be one of before, after"},
		{"detach", []string{"--role", "during"}, "must be one of before, after"},
		{"attach", []string{"--role", "before", "--attachment-id", "x", "--link", "http://folio.example/x"}, "must be an https URL"},
		{"attach", []string{"--role", "before", "--attachment-id", "x"}, "must be an https URL"},
		{"attach", []string{"--role", "before", "--link", attachLink}, "attachment id must not be empty"},
	} {
		if err := runAttachment(t, c.name, append([]string{"at-1"}, c.args...)...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("tk %s %v: err = %v, want %q", c.name, c.args, err, c.want)
		}
	}
	for _, set := range []string{"attachment-before=", "attachment-after=x", "attachments=x"} {
		if err := runEditWith(t, "at-1", "set", set); err == nil || !strings.Contains(err.Error(), "is reserved") {
			t.Errorf("tk edit --set %s: err = %v, want the key refused as reserved", set, err)
		}
	}
	if got, want := ticket.Attachments(getTicket(t, store, "at-1")), []ticket.Attachment{attachment("b1", "before")}; !reflect.DeepEqual(got, want) {
		t.Errorf("attachments after the refusals = %+v, want %+v", got, want)
	}
}
