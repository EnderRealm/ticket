package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestMovePartialFailurePrintsWhatLanded(t *testing.T) {
	// The move is not rolled back. The completed moves are the command's
	// result and stay on stdout; the failure banner is a diagnostic and goes
	// to stderr, so piping stdout still yields only moved IDs.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	src, targetRepo, targetDir := movePair(t)
	srcDir := src.Dir

	mkMoveTicket(t, src, "mv-epic-0001", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkMoveTicket(t, src, "mv-child-0002", ticket.TypeFeature, ticket.StatusOpen, "mv-epic-0001")

	// The child's close fails after its target copy is written.
	childFile := filepath.Join(srcDir, "mv-child-0002.md")
	if err := os.Chmod(childFile, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(childFile, 0o644) })

	f := moveCmd.Flags()
	if err := f.Set("recursive", "true"); err != nil {
		t.Fatalf("set recursive: %v", err)
	}
	defer func() { _ = f.Set("recursive", "false") }()

	stdout, stderr, moveErr := captureMove(t, "mv-epic-0001", targetRepo)
	if moveErr == nil {
		t.Fatal("runMove succeeded, want a failure closing the read-only child")
	}

	dstTickets, err := ticket.NewProjectFileStore(targetDir, "mv-dst").List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	// The destination is a central project, so what the command reports and
	// what its error names are the namespaced forms of the IDs on disk.
	var movedID, orphanID string
	for _, dt := range dstTickets {
		switch dt.Title {
		case "Item mv-epic-0001":
			movedID = ticket.FormatNamespacedID("mv-dst", dt.ID)
		case "Item mv-child-0002":
			orphanID = ticket.FormatNamespacedID("mv-dst", dt.ID)
		}
	}
	if movedID == "" || orphanID == "" {
		t.Fatalf("target holds %d tickets, want the epic copy and the orphaned child copy", len(dstTickets))
	}

	if !contains(stdout, "Moved mv-epic-0001 -> "+movedID) {
		t.Errorf("stdout does not report the completed move:\n%s", stdout)
	}
	if contains(stdout, "Move failed partway") {
		t.Errorf("failure banner belongs on stderr, found on stdout:\n%s", stdout)
	}
	if !contains(stderr, "Move failed partway: the ticket above is") {
		t.Errorf("stderr does not carry the failure banner:\n%s", stderr)
	}
	if !contains(moveErr.Error(), orphanID) {
		t.Errorf("error %q does not name %s, the target copy left behind", moveErr, orphanID)
	}
	if !contains(moveErr.Error(), "mv-child-0002") {
		t.Errorf("error %q does not name the source ticket left open", moveErr)
	}
}

// An unregistered central project is one MultiStore.Create refuses to write to
// and `tk ls` warns about, so a move into it — the one write that lands in
// another repo's store — has to say so too.
func TestMoveWarnsWhenTheTargetProjectIsUnregistered(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, nil)
	src := registerCentralProject(t, "mv-src", mustGetwd())
	mkMoveTicket(t, src, "mv-stray-0001", ticket.TypeFeature, ticket.StatusReady, "")

	_, stderr, err := captureMove(t, "mv-stray-0001", strayRepo)
	if err != nil {
		t.Fatalf("runMove: %v", err)
	}
	landed, err := filepath.Glob(filepath.Join(strayDir, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(landed) != 1 {
		t.Fatalf("unregistered project holds %v, want the moved ticket", landed)
	}
	for _, want := range []string{"cs-stray", "not registered", "tk init"} {
		if !contains(stderr, want) {
			t.Errorf("stderr = %q, want to contain %q", stderr, want)
		}
	}
}

// The route a user actually hits: a relative target typed from inside a
// registered repo becomes a path under that repo, which prefix-matches back to
// the repo's own project. However the target resolves, a move into the store the
// ticket is already in is refused and nothing is written.
func TestMoveRefusesATargetResolvingToTheSourceProject(t *testing.T) {
	home := setupTestHome(t)
	centralRoot := filepath.Join(home, "central")
	fromRepo := filepath.Join(home, "mv", "from")
	toRepo := filepath.Join(home, "mv", "to")
	for _, dir := range []string{fromRepo, toRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	cfg := project.Config{
		CentralRoot: centralRoot,
		Projects: map[string]project.ProjectConfig{
			"mv-from": {Path: fromRepo, Store: "central"},
			"mv-to":   {Path: toRepo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	srcDir := filepath.Join(centralRoot, "tickets", "mv-from")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := ticket.NewProjectFileStore(srcDir, "mv-from")
	mkMoveTicket(t, src, "mv-self-0001", ticket.TypeFeature, ticket.StatusOpen, "")

	oldDir, _ := os.Getwd()
	if err := os.Chdir(fromRepo); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(oldDir)

	stdout, stderr, err := captureMove(t, "mv-self-0001", "to")
	if err == nil {
		t.Fatal("runMove succeeded, want a refusal")
	}
	for _, want := range []string{"mv-from", "no move performed"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if contains(stdout, "Moved") {
		t.Errorf("stdout reports a move that did not happen:\n%s", stdout)
	}
	if contains(stderr, "Move failed partway") {
		t.Errorf("a refusal is not a partial move:\n%s", stderr)
	}

	orig, err := src.Get("mv-self-0001")
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if orig.Status != ticket.StatusOpen {
		t.Errorf("source status = %q, want %q — nothing was moved", orig.Status, ticket.StatusOpen)
	}
	landed, err := filepath.Glob(filepath.Join(srcDir, "*.md"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(landed) != 1 {
		t.Errorf("source project holds %v, want only the original", landed)
	}
}

// movePair registers the test process's working directory as the source
// project and a second repo as the destination, both central. Returns the
// source store the command resolves through TicketStore(), the destination
// repo path `tk move` takes as its target, and that project's ticket dir.
func movePair(t *testing.T) (*ticket.FileStore, string, string) {
	t.Helper()
	src := centralStore(t, "mv-src")
	dstRepo := filepath.Join(t.TempDir(), "mv-dst")
	if err := os.MkdirAll(dstRepo, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dst := registerCentralProject(t, "mv-dst", dstRepo)
	return src, dstRepo, dst.Dir
}

func mkMoveTicket(t *testing.T, store *ticket.FileStore, id string, typ ticket.TicketType, status ticket.Status, parent string) {
	t.Helper()
	tk := &ticket.Ticket{
		ID: id, Status: status, Type: typ, Priority: 2, Parent: parent,
		Created: time.Now(), Title: "Item " + id, Body: "\n",
		Deps: []string{}, Links: []string{},
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func captureMove(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW

	err := runMove(moveCmd, args)

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr

	out, _ := io.ReadAll(outR)
	errOut, _ := io.ReadAll(errR)
	return string(out), string(errOut), err
}
