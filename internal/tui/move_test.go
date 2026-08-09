package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// TestHandleMoveLandsInTheTargetsCentralProject holds the TUI to the same target
// resolution as `tk move`: the picker and the command line both take a repo
// path, and a ticket moved from either has to land where that repo's own tk
// reads it.
func TestHandleMoveLandsInTheTargetsCentralProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	srcRepo := filepath.Join(home, "tui-from")
	dstRepo := filepath.Join(home, "tui-to")
	for _, dir := range []string{srcRepo, dstRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"tui-from": {Path: srcRepo, Store: "central"},
			"tui-to":   {Path: dstRepo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	srcDir := filepath.Join(centralRoot, "tickets", "tui-from")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := ticket.NewProjectFileStore(srcDir, "tui-from")
	if err := src.Create(&ticket.Ticket{
		ID: "tui-move-0001", Status: ticket.StatusReady, Type: ticket.TypeFeature,
		Priority: 2, Created: time.Now(), Title: "Moved from the TUI", Body: "\n",
		Deps: []string{}, Links: []string{},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	a := New(srcDir, "tui-from", "v0", "", srcRepo, false)
	msg := a.handleMove("tui-move-0001", dstRepo)()
	if status, ok := msg.(statusMsg); ok && strings.Contains(string(status), "error:") {
		t.Fatalf("handleMove: %s", status)
	}

	landed, err := filepath.Glob(filepath.Join(centralRoot, "tickets", "tui-to", "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(landed) != 1 {
		t.Errorf("destination project holds %v, want the moved ticket", landed)
	}
	if _, err := os.Stat(filepath.Join(dstRepo, ".tickets")); !os.IsNotExist(err) {
		t.Errorf("a stray .tickets/ was created in %s: %v", dstRepo, err)
	}
}

// The CLI warns on stderr when the move lands in an unregistered central
// project; in the alt screen that write corrupts the frame, so the TUI carries
// the same sentence on the status line.
func TestHandleMoveWarnsWhenTheTargetProjectIsUnregistered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	centralRoot := filepath.Join(home, "central")
	srcRepo := filepath.Join(home, "tui-reg")
	dstRepo := filepath.Join(home, "tui-stray")
	for _, dir := range []string{srcRepo, dstRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"tui-reg": {Path: srcRepo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(centralRoot, "tickets", "tui-stray"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	srcDir := filepath.Join(centralRoot, "tickets", "tui-reg")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := ticket.NewProjectFileStore(srcDir, "tui-reg")
	if err := src.Create(&ticket.Ticket{
		ID: "tui-stray-0001", Status: ticket.StatusReady, Type: ticket.TypeFeature,
		Priority: 2, Created: time.Now(), Title: "Moved into an unregistered project", Body: "\n",
		Deps: []string{}, Links: []string{},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	a := New(srcDir, "tui-reg", "v0", "", srcRepo, false)
	status := statusLine(t, a.handleMove("tui-stray-0001", dstRepo))
	if strings.Contains(status, "error:") {
		t.Fatalf("handleMove: %s", status)
	}
	for _, want := range []string{"Moved", "tui-stray", "not registered", "tk init"} {
		if !strings.Contains(status, want) {
			t.Errorf("status line = %q, want to contain %q", status, want)
		}
	}
}
