package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// implementationServer is verifyServer with a second project: the server's own
// project alpha (A) and beta (B), each registered on this machine with a
// checkout that is a git repository holding a linked worktree. Every one of
// the four directories carries a marker naming it, so a criterion reading the
// marker reports exactly where it ran.
type implementationDirs struct {
	alpha, alphaWT, beta, betaWT string
}

func implementationServer(t *testing.T) (*mcp.ClientSession, implementationDirs) {
	t.Helper()
	server, alpha := verifyTestServer(t)
	beta := t.TempDir()
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertProject("beta", project.ProjectConfig{Path: beta, Store: "central"})
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session := connectVerifyServer(t, server)
	dirs := implementationDirs{alpha: alpha, alphaWT: verifyWorktree(t, alpha), beta: beta, betaWT: verifyWorktree(t, beta)}
	for dir, marker := range map[string]string{dirs.alpha: "alpha", dirs.alphaWT: "alpha-wt", dirs.beta: "beta", dirs.betaWT: "beta-wt"} {
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return session, dirs
}

// markerAcceptance reads where it ran and leaves a trace there, so a test can
// tell that a directory saw no command at all.
const markerAcceptance = "- Reads the marker.\n  verify: /bin/sh -c 'cat marker; touch ran'\n"

func assertNotRanIn(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "ran")); !os.IsNotExist(err) {
			t.Errorf("a command ran in %s: %v", d, err)
		}
	}
}

func TestVerifyImplementationRunsInTheAssociatedCheckout(t *testing.T) {
	session, dirs := implementationServer(t)
	// A's bound is unreadable: had the run taken A's policy, every command
	// would be refused. The bound belongs to the checkout the commands run in.
	setVerifyTimeout(t, "soon")
	id := createTicketID(t, session, map[string]any{
		"title":      "Implemented in beta",
		"type":       "feature",
		"acceptance": markerAcceptance,
		"set":        map[string]any{ticket.ImplementationField: "beta"},
	})
	acceptanceID := callObject(t, session, "ticket_criteria", map[string]any{"id": id})["acceptance_id"].(string)

	report := verifyReportArgs(t, session, map[string]any{"id": id, "acceptance_id": acceptanceID, "candidate": "c1"})
	if report.Dir != dirs.beta || report.Implementation != "beta" {
		t.Errorf("dir, implementation = %q, %q; want B's checkout %q and beta", report.Dir, report.Implementation, dirs.beta)
	}
	if !report.OK || report.Results[0].Output != "beta" {
		t.Errorf("report = %+v, want a pass reading B's marker", report)
	}
	if report.AcceptanceID != acceptanceID || report.Candidate != "c1" {
		t.Errorf("provenance = %q %q, want %q and c1", report.AcceptanceID, report.Candidate, acceptanceID)
	}
	assertNotRanIn(t, dirs.alpha, dirs.alphaWT)
	record, _ := showTicket(t, session, id)["test_results"].(string)
	if want := "checkout " + dirs.beta + "; implementation beta; acceptance " + acceptanceID + "; candidate c1"; !strings.Contains(record, want) {
		t.Errorf("test_results lacks %q:\n%s", want, record)
	}

	// Removing the field is the absent case: A's own checkout, A's policy, and
	// no implementation named anywhere.
	callObject(t, session, "ticket_edit", map[string]any{"id": id, "set": map[string]any{ticket.ImplementationField: ""}})
	setVerifyTimeout(t, "")
	quick := callObject(t, session, "ticket_verify", map[string]any{"id": id})
	if _, present := quick["implementation"]; present {
		t.Errorf("absent-field report = %v, want no implementation key", quick)
	}
	if quick["dir"] != dirs.alpha {
		t.Errorf("absent-field run in %v, want A's checkout %q", quick["dir"], dirs.alpha)
	}
	report = verifyReportArgs(t, session, map[string]any{"id": id})
	if report.Dir != dirs.alpha || report.Results[0].Output != "alpha" {
		t.Errorf("absent-field run = %q in %q, want A's marker in %q", report.Results[0].Output, report.Dir, dirs.alpha)
	}
	record, _ = showTicket(t, session, id)["test_results"].(string)
	if !strings.Contains(record, "checkout "+dirs.alpha+"; acceptance ") || strings.Contains(record, "implementation") {
		t.Errorf("absent-field record = %q, want A's checkout and no implementation", record)
	}
}

func TestVerifyImplementationDirMustBeTheImplementationsRepository(t *testing.T) {
	session, dirs := implementationServer(t)
	id := createTicketID(t, session, map[string]any{
		"title":      "Worktree of beta",
		"type":       "feature",
		"acceptance": markerAcceptance,
		"set":        map[string]any{ticket.ImplementationField: "beta"},
	})

	report := verifyReportArgs(t, session, map[string]any{"id": id, "dir": dirs.betaWT})
	if report.Dir != canonical(t, dirs.betaWT) || report.Implementation != "beta" || report.Results[0].Output != "beta-wt" {
		t.Errorf("report = %+v, want B's worktree %q named beta", report, canonical(t, dirs.betaWT))
	}

	// A's checkout and A's worktree belong to the ticket's own repository, not
	// to the implementation: running there would be A's code recorded as
	// evidence for B.
	for name, dir := range map[string]string{"A's checkout": dirs.alpha, "A's worktree": dirs.alphaWT} {
		refused := callTool(t, session, "ticket_verify", map[string]any{"id": id, "dir": dir})
		if text := errorText(t, refused, name); !strings.Contains(text, "another repository") || !strings.Contains(text, dirs.beta) {
			t.Errorf("%s: refusal = %s, want it refused as outside B's checkout %s", name, text, dirs.beta)
		}
	}
	assertNotRanIn(t, dirs.alpha, dirs.alphaWT)
}

func TestVerifyImplementationThatDoesNotResolveIsRefused(t *testing.T) {
	session, dirs := implementationServer(t)
	gone := filepath.Join(t.TempDir(), "gone")
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertProject("delta", project.ProjectConfig{Path: gone, Store: "central"})
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, implementation, want string
	}{
		{"unregistered", "gamma", `implementation "gamma": project "gamma" has no checkout registered on this machine`},
		{"missing checkout", "delta", `implementation "delta": project "delta" checkout ` + gone + " is not a directory"},
		{"a path", dirs.beta, "is not a project name"},
		{"Root", project.RootNamespace, "Root has no repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := createTicketID(t, session, map[string]any{
				"title":      "Unresolvable " + tc.name,
				"type":       "feature",
				"acceptance": markerAcceptance,
				"set":        map[string]any{ticket.ImplementationField: tc.implementation},
			})
			for _, args := range []map[string]any{{"id": id}, {"id": id, "dir": dirs.alpha}} {
				refused := callTool(t, session, "ticket_verify", args)
				if text := errorText(t, refused, "ticket_verify "+tc.name); !strings.Contains(text, tc.want) {
					t.Errorf("refusal = %s, want it to say %q", text, tc.want)
				}
			}
			if shown := showTicket(t, session, id); shown["test_results"] != nil {
				t.Errorf("a refused verify recorded results: %v", shown["test_results"])
			}
			if status := callTool(t, session, "ticket_verify_status", map[string]any{"id": id}); !status.IsError {
				t.Errorf("a refused verify started a job: %v", status.Content)
			}
		})
	}
	assertNotRanIn(t, dirs.alpha, dirs.alphaWT, dirs.beta, dirs.betaWT)
}
