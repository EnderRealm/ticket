package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// implementationFixture registers project A (vf-impl-a) and project B
// (vf-impl-b) with distinguishable checkouts — each holds a marker naming its
// project — and returns a store over A's tickets plus both checkout paths.
// A's verify_timeout is unreadable, so a run that took A's bound refuses
// every command while one that took B's runs.
func implementationFixture(t *testing.T) (store *ticket.FileStore, dirA, dirB string) {
	t.Helper()
	verifyStore(t, "vf-anchor", mixedCriteriaBody)
	dirA, dirB = t.TempDir(), t.TempDir()
	for dir, name := range map[string]string{dirA: "alpha", dirB: "beta"} {
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store = registerCentralProject(t, "vf-impl-a", dirA)
	registerCentralProject(t, "vf-impl-b", dirB)
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Projects["vf-impl-a"]
	p.VerifyTimeout = "soon"
	cfg.UpsertProject("vf-impl-a", p)
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}
	selectProject(t, "vf-impl-a")
	return store, dirA, dirB
}

func mkImplementationTicket(t *testing.T, store *ticket.FileStore, id, implementation, body string) {
	t.Helper()
	if err := store.Create(&ticket.Ticket{
		ID:      id,
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Implemented elsewhere",
		Body:    body,
		Extra:   map[string]string{ticket.ImplementationField: implementation},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestVerifyImplementationRunsInTheAssociatedCheckout(t *testing.T) {
	store, dirA, dirB := implementationFixture(t)
	mkImplementationTicket(t, store, "vf-elsewhere", "vf-impl-b",
		"Description.\n\n## Acceptance Criteria\n\n- Reads the marker.\n  verify: /bin/sh -c 'cat marker; touch ran'\n")
	jsonOutput = true
	defer func() { jsonOutput = false }()

	out, err := captureVerify(t, "vf-elsewhere")
	if err != nil {
		t.Fatalf("verify in the implementation checkout should pass: %v\n%s", err, out)
	}
	var report ticket.VerifyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if report.Dir != dirB || report.Implementation != "vf-impl-b" {
		t.Errorf("report dir, implementation = %q, %q; want B's checkout %q and vf-impl-b", report.Dir, report.Implementation, dirB)
	}
	if !report.OK || report.Results[0].Output != "beta" {
		t.Errorf("report = %+v, want a pass reading B's marker under B's bound", report)
	}
	if _, err := os.Stat(filepath.Join(dirA, "ran")); err == nil {
		t.Error("the command ran in A's checkout")
	}
	tk, err := store.Get("vf-elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if want := "checkout " + dirB + "; implementation vf-impl-b; acceptance " + report.AcceptanceID; !contains(tk.Body, want) {
		t.Errorf("record lacks %q:\n%s", want, tk.Body)
	}
}

func TestVerifyImplementationThatDoesNotResolveIsRefused(t *testing.T) {
	store, dirA, _ := implementationFixture(t)
	for _, tc := range []struct {
		id, implementation, want string
	}{
		{"vf-unregistered", "vf-impl-none", `implementation "vf-impl-none": project "vf-impl-none" has no checkout registered on this machine`},
		{"vf-path", dirA, "is not a project name"},
	} {
		sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
		mkImplementationTicket(t, store, tc.id, tc.implementation, sentinelBody(sentinel))
		_, err := captureVerify(t, tc.id)
		assertVerifyRefusedBeforeRunning(t, store, tc.id, sentinel, err, tc.want)
	}
}

// Root has no repository whatever the ticket says: implementation naming a
// registered project with a real checkout does not make a Root ticket run.
func TestVerifyImplementationDoesNotMakeRootRunnable(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	anchor := verifyStore(t, "vf-fixture", mixedCriteriaBody)
	root := verifyRoot(anchor)
	registerCentralProject(t, "vf-impl-b", t.TempDir())
	if err := os.WriteFile(ticket.CatalogPath(root), []byte("required_features: [root-namespace]\nnamespaces:\n  _root: {kind: root}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootDir := filepath.Join(root, "tickets", project.RootNamespace)
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := ticket.NewProjectFileStore(rootDir, project.RootNamespace)
	mkImplementationTicket(t, store, "vf-root-impl", "vf-impl-b", sentinelBody(sentinel))
	selectProject(t, project.RootNamespace)

	_, err := captureVerify(t, "vf-root-impl")
	assertVerifyRefusedBeforeRunning(t, store, "vf-root-impl", sentinel, err, "Root has no repository")
}
