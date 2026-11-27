package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestMarkAndUnmarkCommands(t *testing.T) {
	t.Setenv(ticket.SourceEnv, "weft")
	store := centralStore(t, "mk")
	mkCentral(t, store, "mk-step", ticket.TypeFeature, ticket.StatusReady, "")
	mkCentral(t, store, "mk-code", ticket.TypeFeature, ticket.StatusReady, "")
	if err := runEditWith(t, "mk-step", "workflow-kind", "generic"); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"mk-step", "done"},
		{"mk-step", "done", "--note", " "},
		{"mk-step", "later", "--note", "x"},
	} {
		if _, err := runLanding(t, "mark", args...); err == nil {
			t.Errorf("mark %v was accepted", args)
		}
	}
	if _, err := runLanding(t, "mark", "mk-code", "done", "--note", "x"); !errors.Is(err, ticket.ErrTransitionNotPermitted) {
		t.Errorf("marking a coding ticket = %v", err)
	}
	if tk := getTicket(t, store, "mk-step"); tk.Status != ticket.StatusReady || len(tk.Notes) != 0 {
		t.Fatalf("a refused mark left %s with notes %v", tk.Status, tk.Notes)
	}

	if out := mustLanding(t, "mark", "mk-step", "not-needed", "--note", "Shipped another way."); out != "mk-step is closed\n" {
		t.Errorf("mark printed %q", out)
	}
	host, _ := os.Hostname()
	row := captureQuery(t)["mk-step"]["last_mark"].(map[string]any)
	if row["result"] != "not_needed" || row["note"] != "Shipped another way." || row["by"] != "weft" || row["host"] != host || row["prior"] != "ready" || row["at"] == "" {
		t.Errorf("query last_mark = %v", row)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var report struct {
		Status   string         `json:"status"`
		LastMark map[string]any `json:"last_mark"`
	}
	if err := json.Unmarshal([]byte(mustLanding(t, "unmark", "mk-step")), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "ready" || report.LastMark != nil {
		t.Errorf("unmark report = %+v", report)
	}
	if _, err := runLanding(t, "unmark", "mk-step"); !errors.Is(err, ticket.ErrTransitionNotPermitted) {
		t.Errorf("a second unmark = %v", err)
	}
}
