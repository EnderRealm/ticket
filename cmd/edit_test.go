package cmd

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

// editStore creates a central-store project holding one open ticket on a
// branch.
func editStore(t *testing.T, id string) *ticket.FileStore {
	t.Helper()
	store := centralStore(t, "ed-edit")
	tk := &ticket.Ticket{
		ID:      id,
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Branch:  "feature-x",
		Created: time.Now(),
		Title:   "Editable ticket",
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store
}

// runEditWith runs the edit command against a fresh flag set, since flag
// values on the shared editCmd persist across calls.
func runEditWith(t *testing.T, id string, flags ...string) error {
	t.Helper()
	cmd := &cobra.Command{}
	registerEditFlags(cmd)
	f := cmd.Flags()
	for i := 0; i+1 < len(flags); i += 2 {
		if err := f.Set(flags[i], flags[i+1]); err != nil {
			t.Fatalf("set %s: %v", flags[i], err)
		}
	}
	return runEdit(cmd, []string{id})
}

func TestEditSetsAndRemovesOutputs(t *testing.T) {
	store := editStore(t, "ed-1234")

	if err := runEditWith(t, "ed-1234", "output", "branch=feature-x", "output", "commit=abc1234"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-1234")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "feature-x" || tk.Outputs["commit"] != "abc1234" {
		t.Fatalf("Outputs = %v, want branch and commit set", tk.Outputs)
	}

	if err := runEditWith(t, "ed-1234", "output", "commit="); err != nil {
		t.Fatalf("runEdit remove: %v", err)
	}
	tk, err = store.Get("ed-1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tk.Outputs["commit"]; ok {
		t.Errorf("Outputs[commit] still present: %v", tk.Outputs)
	}
	if tk.Outputs["branch"] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", tk.Outputs["branch"])
	}
}

func TestEditRejectsInvalidOutput(t *testing.T) {
	editStore(t, "ed-bad")

	if err := runEditWith(t, "ed-bad", "output", "noequals"); err == nil {
		t.Error("expected error for --output without key=value")
	}
	if err := runEditWith(t, "ed-bad", "output", "bad key=value"); err == nil {
		t.Error("expected error for invalid output key")
	}
	if err := runEditWith(t, "ed-bad", "output", "commit=has:colon"); err == nil {
		t.Error("expected error for invalid output value")
	}
}

func TestEditToDonePopulatesBranchOutput(t *testing.T) {
	store := editStore(t, "ed-done")

	if err := runEditWith(t, "ed-done", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-done")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "feature-x" {
		t.Errorf("Outputs[branch] = %q, want feature-x", tk.Outputs["branch"])
	}
}

func TestEditToDoneKeepsExistingBranchOutput(t *testing.T) {
	store := editStore(t, "ed-keep")

	if err := runEditWith(t, "ed-keep", "output", "branch=manual-branch"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	if err := runEditWith(t, "ed-keep", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-keep")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Outputs["branch"] != "manual-branch" {
		t.Errorf("Outputs[branch] = %q, want manual-branch", tk.Outputs["branch"])
	}
}

func TestEditPromotesToEpic(t *testing.T) {
	// `tk edit --type epic` sends the status the ticket was read with alongside
	// the new type. That status was never chosen for an epic, so promotion has
	// to stay one ordinary command rather than needing --status backlog with it.
	store := editStore(t, "ed-epic")

	if err := runEditWith(t, "ed-epic", "type", "epic"); err != nil {
		t.Fatalf("promoting a ticket to an epic should not be refused: %v", err)
	}
	tk, err := store.Get("ed-epic")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Type != ticket.TypeEpic {
		t.Errorf("type = %q, want %q", tk.Type, ticket.TypeEpic)
	}
	if tk.Status != ticket.StatusBacklog {
		t.Errorf("status = %q, want %q — a childless epic derives backlog", tk.Status, ticket.StatusBacklog)
	}
}

func TestEditPromotesToEpicAndCloses(t *testing.T) {
	// `tk edit --type epic --status closed` sets the status explicitly, so it is
	// the abandon it looks like: recorded, not dropped along with the promotion.
	store := editStore(t, "ed-promote-close")

	if err := runEditWith(t, "ed-promote-close", "type", "epic", "status", "closed"); err != nil {
		t.Fatalf("promoting a ticket and abandoning it should be accepted: %v", err)
	}
	tk, err := store.Get("ed-promote-close")
	if err != nil {
		t.Fatal(err)
	}
	if !tk.Abandoned {
		t.Error("a closed set with the promotion recorded no abandon intent")
	}
	if tk.Status != ticket.StatusClosed {
		t.Errorf("status = %q, want %q", tk.Status, ticket.StatusClosed)
	}
}

func TestEditPromotingToEpicRefusesAnotherStatus(t *testing.T) {
	store := editStore(t, "ed-promote-ready")

	err := runEditWith(t, "ed-promote-ready", "type", "epic", "status", "ready")
	if err == nil {
		t.Fatal("expected a status set alongside the promotion to be refused, got nil")
	}
	if !contains(err.Error(), "derived from its children") {
		t.Errorf("error should say an epic's status is derived, got: %v", err)
	}
	tk, getErr := store.Get("ed-promote-ready")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if tk.Type == ticket.TypeEpic {
		t.Error("the refused edit promoted the ticket anyway")
	}
}

func TestEditReportsTheChildrenAnAbandonClosed(t *testing.T) {
	// Closing an epic writes its children too, so the line reporting the edit
	// names them — the failure case already did, and success said nothing.
	store := centralStore(t, "ed-abandon")
	if err := store.Create(&ticket.Ticket{
		ID: "ed-epic-0001", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "An epic", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(&ticket.Ticket{
		ID: "ed-child-0002", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "ed-epic-0001",
		Created: time.Now(), Title: "A child", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	out := captureEdit(t, "ed-epic-0001", "status", "closed")
	if !contains(out, "ed-child-0002") || !contains(out, "closed 1 child ticket(s)") {
		t.Errorf("edit should report the children it closed:\n%s", out)
	}

	// An edit that closes nothing reports nothing extra.
	out = captureEdit(t, "ed-child-0002", "title", "Renamed")
	if contains(out, "child ticket(s)") {
		t.Errorf("an edit that closed nothing reported a cascade:\n%s", out)
	}
}

func captureEdit(t *testing.T, id string, flags ...string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runEditWith(t, id, flags...)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}

func TestShowRendersOutputs(t *testing.T) {
	store := editStore(t, "ed-show")

	if err := runEditWith(t, "ed-show", "output", "branch=feature-x", "output", "commit=abc1234"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	out := captureShow(t, store, "ed-show", false)
	for _, want := range []string{
		"outputs:\n  branch: feature-x\n  commit: abc1234\n",
		"## Outputs\n\n- branch: feature-x\n- commit: abc1234\n",
	} {
		if !contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestEditSetsBodySections(t *testing.T) {
	store := editStore(t, "ed-sections")
	if err := runEditWith(t, "ed-sections", "description", "Why it matters."); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	// Backdate the stored stamp so the section edit's own write is observable
	// at the format's one-second precision.
	path := filepath.Join(store.Dir, "ed-sections.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale := regexp.MustCompile(`(?m)^updated: .*$`).ReplaceAll(data, []byte("updated: 2020-01-01T00:00:00Z"))
	if err := os.WriteFile(path, stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if before, err := store.Get("ed-sections"); err != nil || before.Updated.Year() != 2020 {
		t.Fatalf("backdating did not take: updated = %v, err = %v", before.Updated, err)
	}

	acceptance := "- It works.\n  verify: go test ./cmd/"
	if err := runEditWith(t, "ed-sections",
		"acceptance", acceptance,
		"design", "Reuse UpdateSection.",
		"test-results", "all green"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err := store.Get("ed-sections")
	if err != nil {
		t.Fatal(err)
	}
	desc, design, acc, results := ticket.BodySections(tk.Body)
	if desc != "Why it matters." {
		t.Errorf("description = %q, want it kept", desc)
	}
	if design != "Reuse UpdateSection." {
		t.Errorf("design = %q", design)
	}
	if acc != acceptance {
		t.Errorf("acceptance = %q, want %q", acc, acceptance)
	}
	if results != "all green" {
		t.Errorf("test results = %q", results)
	}
	if !contains(tk.Body, "## Acceptance Criteria\n") {
		t.Errorf("body has no ## Acceptance Criteria heading:\n%s", tk.Body)
	}
	if criteria := ticket.ParseCriteria(ticket.AcceptanceCriteria(tk.Body)); len(criteria) != 1 || criteria[0].Command != "go test ./cmd/" {
		t.Errorf("criteria = %+v, want the one written", criteria)
	}
	if !tk.Updated.After(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("updated = %v, want the edit to stamp it", tk.Updated)
	}

	// A second acceptance edit replaces the section rather than adding one.
	if err := runEditWith(t, "ed-sections", "acceptance", "- Replaced.\n  unverifiable: by hand"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	tk, err = store.Get("ed-sections")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, acc, _ := ticket.BodySections(tk.Body); acc != "- Replaced.\n  unverifiable: by hand" {
		t.Errorf("acceptance after replace = %q", acc)
	}
	if n := strings.Count(tk.Body, "## Acceptance"); n != 1 {
		t.Errorf("body carries %d acceptance headings, want 1:\n%s", n, tk.Body)
	}
}

func TestEditAcceptanceDerivesParentStatus(t *testing.T) {
	// The section edit rides the same SaveEdit write as a status change, so an
	// epic's only child finishing with its contract groomed finishes the epic.
	store := centralStore(t, "ed-propagate")
	if err := store.Create(&ticket.Ticket{
		ID: "ed-epic-0003", Status: ticket.StatusBacklog, Type: ticket.TypeEpic,
		Created: time.Now(), Title: "An epic", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(&ticket.Ticket{
		ID: "ed-child-0004", Status: ticket.StatusOpen, Type: ticket.TypeFeature, Parent: "ed-epic-0003",
		Created: time.Now(), Title: "A child", Body: "\n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := runEditWith(t, "ed-child-0004", "acceptance", "- Done.\n  unverifiable: by hand", "status", "done"); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	epic, err := store.Get("ed-epic-0003")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Status != ticket.StatusDone {
		t.Errorf("epic status = %q, want %q", epic.Status, ticket.StatusDone)
	}
}

func TestEditWarnsOnUnparsedAcceptance(t *testing.T) {
	editStore(t, "ed-warn")

	stderr := captureEditStderr(t, "ed-warn", "acceptance", "1. Numbered.\n   verify: go test ./...")
	if !contains(stderr, ticket.UnparsedAcceptanceWarning("ed-warn")) {
		t.Errorf("stderr missing the unparsed-acceptance warning:\n%s", stderr)
	}

	stderr = captureEditStderr(t, "ed-warn", "acceptance", "- Shell.\n  verify: cd x && true")
	if !contains(stderr, "shell syntax outside quotes") {
		t.Errorf("stderr missing the shell-syntax warning:\n%s", stderr)
	}

	// An edit that writes neither section repeats nothing about one it did not touch.
	stderr = captureEditStderr(t, "ed-warn", "design", "unrelated")
	if stderr != "" {
		t.Errorf("a design edit warned:\n%s", stderr)
	}
}

func captureEditStderr(t *testing.T, id string, flags ...string) string {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	os.Stdout, os.Stderr = devnull, w

	err = runEditWith(t, id, flags...)

	w.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr

	if err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}

func TestHelpDocumentsEditSections(t *testing.T) {
	help := renderHelp(t, editCmd)
	for _, flag := range []string{"--acceptance", "--design", "--test-results"} {
		if !contains(help, flag+" string") {
			t.Errorf("edit help lists no %s flag:\n%s", flag, help)
		}
	}
	root := renderHelp(t, rootCmd)
	if !contains(root, "edit --acceptance <t>") {
		t.Errorf("root help has no index line for edit's section flags:\n%s", root)
	}
}
