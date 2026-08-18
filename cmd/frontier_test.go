package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// setupFrontierStore configures a central store in a temp HOME and returns a
// FileStore per project name.
func setupFrontierStore(t *testing.T, projects ...string) map[string]*ticket.FileStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	centralRoot := t.TempDir()
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects:    map[string]project.ProjectConfig{},
	}
	stores := map[string]*ticket.FileStore{}
	for _, name := range projects {
		dir := filepath.Join(centralRoot, "tickets", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg.Projects[name] = project.ProjectConfig{Path: filepath.Join("/tmp", name), Store: "central"}
		stores[name] = ticket.NewFileStore(dir)
	}
	if err := project.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return stores
}

func frontierTicket(t *testing.T, store *ticket.FileStore, id string, status ticket.Status, deps ...string) {
	t.Helper()
	tk := &ticket.Ticket{
		ID:      id,
		Status:  status,
		Type:    ticket.TypeFeature,
		Deps:    deps,
		Created: time.Now(),
		Title:   "Item " + id,
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func captureFrontier(t *testing.T, args ...string) string {
	t.Helper()
	if err := frontierCmd.Flags().Set("project", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(args); i += 2 {
		if err := frontierCmd.Flags().Set(args[i], args[i+1]); err != nil {
			t.Fatal(err)
		}
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runFrontier(frontierCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runFrontier: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}

func TestFrontierListsUnblockedReady(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	frontierTicket(t, stores["alpha"], "fr-open", ticket.StatusOpen)
	frontierTicket(t, stores["alpha"], "fr-done", ticket.StatusDone)
	frontierTicket(t, stores["alpha"], "fr-free", ticket.StatusReady)
	frontierTicket(t, stores["alpha"], "fr-satisfied", ticket.StatusReady, "fr-done")
	frontierTicket(t, stores["alpha"], "fr-blocked", ticket.StatusReady, "fr-open")

	out := captureFrontier(t)

	for _, id := range []string{"fr-free", "fr-satisfied"} {
		if !contains(out, id) {
			t.Errorf("frontier output missing %s:\n%s", id, out)
		}
	}
	for _, id := range []string{"fr-blocked", "fr-open", "fr-done"} {
		if contains(out, id) {
			t.Errorf("frontier output should exclude %s:\n%s", id, out)
		}
	}
}

func TestFrontierJSON(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	frontierTicket(t, stores["alpha"], "fr-open", ticket.StatusOpen)
	frontierTicket(t, stores["alpha"], "fr-free", ticket.StatusReady)
	frontierTicket(t, stores["alpha"], "fr-blocked", ticket.StatusReady, "fr-open")
	hostileTitle := "Déjà 日本語 \x1b[2Jclear \u202erepaint"
	free, err := stores["alpha"].Get("fr-free")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	free.Title = hostileTitle
	if err := stores["alpha"].Update(free); err != nil {
		t.Fatalf("Update: %v", err)
	}

	jsonOutput = true
	defer func() { jsonOutput = false }()

	out := captureFrontier(t)

	var result []ticketJSON
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json parse: %v\noutput: %s", err, out)
	}
	if len(result) != 1 {
		t.Fatalf("frontier --json returned %d tickets, want 1: %s", len(result), out)
	}
	if result[0].ID != "alpha/fr-free" {
		t.Errorf("id = %q, want alpha/fr-free", result[0].ID)
	}
	if result[0].Status != string(ticket.StatusReady) {
		t.Errorf("status = %q, want ready", result[0].Status)
	}
	if result[0].Title != hostileTitle {
		t.Errorf("title = %q, want stored bytes %q", result[0].Title, hostileTitle)
	}
}

func TestFrontierUnknownProject(t *testing.T) {
	stores := setupFrontierStore(t, "alpha")
	frontierTicket(t, stores["alpha"], "fr-free", ticket.StatusReady)

	if err := frontierCmd.Flags().Set("project", "nope"); err != nil {
		t.Fatal(err)
	}
	defer frontierCmd.Flags().Set("project", "")

	if err := runFrontier(frontierCmd, nil); err == nil {
		t.Error("unknown --project should error, not report an empty frontier")
	}
}

func TestFrontierProjectFilter(t *testing.T) {
	stores := setupFrontierStore(t, "alpha", "beta")
	frontierTicket(t, stores["alpha"], "fr-alpha", ticket.StatusReady)
	frontierTicket(t, stores["beta"], "fr-beta", ticket.StatusReady)

	out := captureFrontier(t, "project", "beta")

	if !contains(out, "beta/fr-beta") {
		t.Errorf("frontier --project=beta missing beta ticket:\n%s", out)
	}
	if contains(out, "fr-alpha") {
		t.Errorf("frontier --project=beta should exclude alpha ticket:\n%s", out)
	}
}
