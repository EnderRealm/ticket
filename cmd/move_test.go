package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestMovePartialFailurePrintsWhatLanded(t *testing.T) {
	// The move is not rolled back. The completed moves are the command's
	// result and stay on stdout; the failure banner is a diagnostic and goes
	// to stderr, so piping stdout still yields only moved IDs.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	srcDir := t.TempDir()
	t.Setenv("TICKETS_DIR", srcDir)
	src := ticket.NewFileStore(srcDir)

	targetRepo := t.TempDir()
	targetDir := filepath.Join(targetRepo, ".tickets")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	mkMoveTicket(t, src, "mv-epic-0001", ticket.TypeEpic, "")
	mkMoveTicket(t, src, "mv-child-0002", ticket.TypeFeature, "mv-epic-0001")

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

	dstTickets, err := ticket.NewFileStore(targetDir).List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	var movedID, orphanID string
	for _, dt := range dstTickets {
		switch dt.Title {
		case "Item mv-epic-0001":
			movedID = dt.ID
		case "Item mv-child-0002":
			orphanID = dt.ID
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

func mkMoveTicket(t *testing.T, store *ticket.FileStore, id string, typ ticket.TicketType, parent string) {
	t.Helper()
	tk := &ticket.Ticket{
		ID: id, Status: ticket.StatusOpen, Type: typ, Priority: 2, Parent: parent,
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
