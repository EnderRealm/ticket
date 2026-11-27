package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

const (
	landSHA   = "0123456789abcdef0123456789abcdef01234567"
	relandSHA = "fedcba9876543210fedcba9876543210fedcba98"
)

// landStore creates a central-store project holding an open ticket with two
// test: criteria and one with none.
func landStore(t *testing.T) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "ld")
	mkCentral(t, store, "ld-tested", ticket.TypeFeature, ticket.StatusOpen, "")
	mkCentral(t, store, "ld-untested", ticket.TypeFeature, ticket.StatusOpen, "")
	if err := runEditWith(t, "ld-tested", "acceptance", "- Builds.\n  verify: true\n- Wraps.\n  test: narrow it\n- Scrolls.\n  test: scroll it"); err != nil {
		t.Fatal(err)
	}
	return store
}

// runLanding runs one of the landing, test or mark commands against a fresh
// flag set parsed from args, returning what it printed.
func runLanding(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{}
	run := map[string]func(*cobra.Command, []string) error{"land": runLand, "pass": runPass, "fail": runFail, "retest": runRetest, "mark": runMark, "unmark": runUnmark}[name]
	switch name {
	case "land":
		registerLandFlags(cmd)
	case "fail":
		registerFailFlags(cmd)
	default:
		registerTestFlags(cmd)
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := run(cmd, cmd.Flags().Args())
	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	return string(out), err
}

func mustLanding(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := runLanding(t, name, args...)
	if err != nil {
		t.Fatalf("tk %s %v: %v", name, args, err)
	}
	return out
}

func getTicket(t *testing.T, store *ticket.FileStore, id string) *ticket.Ticket {
	t.Helper()
	tk, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestLandChoosesTestingOrDone(t *testing.T) {
	store := landStore(t)
	for id, want := range map[string]ticket.Status{"ld-tested": ticket.StatusTesting, "ld-untested": ticket.StatusDone} {
		if out := mustLanding(t, "land", id, "--commit", landSHA); out != id+" is "+string(want)+"\n" {
			t.Errorf("land %s printed %q, want %s", id, out, want)
		}
		tk := getTicket(t, store, id)
		if tk.Status != want || tk.Outputs["commit"] != landSHA || len(tk.Notes) != 1 || !strings.Contains(tk.Notes[0].Text, landSHA) {
			t.Errorf("%s landed %s at %v with notes %v", id, tk.Status, tk.Outputs, tk.Notes)
		}
	}
	if _, err := runLanding(t, "land", "ld-tested"); err == nil {
		t.Error("land without --commit was accepted")
	}
}

func TestPassFailAndRetestCommands(t *testing.T) {
	t.Setenv(ticket.SourceEnv, "weft")
	store := landStore(t)
	mustLanding(t, "land", "ld-tested", "--commit", landSHA)

	for _, args := range [][]string{
		{"ld-tested", "--check", "1"},
		{"ld-tested", "--note", "Clipped."},
		{"ld-tested", "--note", "Clipped.", "--check", "3"},
		{"ld-tested", "--note", "Clipped.", "--check", "1", "--check", "1"},
	} {
		if _, err := runLanding(t, "fail", args...); err == nil {
			t.Errorf("fail %v was accepted", args)
		}
	}
	if tk := getTicket(t, store, "ld-tested"); tk.Status != ticket.StatusTesting || len(tk.Notes) != 1 {
		t.Fatalf("a refused fail left %s with notes %v", tk.Status, tk.Notes)
	}

	if out := mustLanding(t, "fail", "ld-tested", "--check", "2", "--check", "1", "--note", "Clipped."); out != "ld-tested is ready\n" {
		t.Errorf("fail printed %q", out)
	}
	tk := getTicket(t, store, "ld-tested")
	want := []ticket.TestCheck{{Index: 1, Text: "Wraps."}, {Index: 2, Text: "Scrolls."}}
	if last := ticket.LastTest(tk); tk.Status != ticket.StatusReady || last.Note != "Clipped." || last.By != "weft" || !reflect.DeepEqual(last.Checks, want) {
		t.Errorf("failed to %s with %+v", tk.Status, ticket.LastTest(tk))
	}
	if row := captureQuery(t)["ld-tested"]["last_test"]; row.(map[string]any)["note"] != "Clipped." || len(row.(map[string]any)["checks"].([]any)) != 2 {
		t.Errorf("query last_test = %v", row)
	}

	mustLanding(t, "retest", "ld-tested")
	if tk = getTicket(t, store, "ld-tested"); tk.Status != ticket.StatusTesting || ticket.LastTest(tk) != nil {
		t.Errorf("retest left %s with %+v", tk.Status, ticket.LastTest(tk))
	}
	if _, err := runLanding(t, "retest", "ld-tested"); !errors.Is(err, ticket.ErrTransitionNotPermitted) {
		t.Errorf("a second retest = %v", err)
	}

	mustLanding(t, "fail", "ld-tested", "--check", "1", "--note", "Clipped again.")
	mustLanding(t, "land", "ld-tested", "--commit", relandSHA)
	mustLanding(t, "pass", "ld-tested", "--note", "Good now.")
	if tk = getTicket(t, store, "ld-tested"); tk.Status != ticket.StatusDone || tk.ClosedAt().IsZero() || tk.Outputs["commit"] != relandSHA {
		t.Errorf("passed to %s at %v, closed %v", tk.Status, tk.Outputs, tk.ClosedAt())
	}
	if _, err := runLanding(t, "pass", "ld-untested"); !errors.Is(err, ticket.ErrTransitionNotPermitted) {
		t.Errorf("passing an open ticket = %v", err)
	}
}

func TestLandingCommandsReplayByActionID(t *testing.T) {
	store := landStore(t)
	args := []string{"ld-tested", "--commit", landSHA, "--action-id", "run-1-land"}
	mustLanding(t, "land", args...)
	mustLanding(t, "pass", "ld-tested")
	if out := mustLanding(t, "land", args...); out != "ld-tested is done (already recorded as testing)\n" {
		t.Errorf("replay printed %q", out)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()
	var report struct {
		ID       string               `json:"id"`
		Status   string               `json:"status"`
		LastTest ticket.TestRow       `json:"last_test"`
		Receipt  ticket.ActionReceipt `json:"receipt"`
		Replayed bool                 `json:"replayed"`
	}
	if err := json.Unmarshal([]byte(mustLanding(t, "land", args...)), &report); err != nil {
		t.Fatal(err)
	}
	if report.ID != "ld-tested" || report.Status != "done" || report.LastTest.Result != ticket.TestPassed || !report.Replayed || report.Receipt.ID != "run-1-land" || report.Receipt.Commit != landSHA {
		t.Errorf("report = %+v", report)
	}
	if tk := getTicket(t, store, "ld-tested"); len(tk.Actions) != 2 || len(tk.Notes) != 2 {
		t.Errorf("replays wrote: %d receipts, %d notes", len(tk.Actions), len(tk.Notes))
	}
}
