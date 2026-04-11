package ticket

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMoveTicketPreservesAllFields(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := &FileStore{Dir: srcDir}
	dst := &FileStore{Dir: dstDir}

	original := &Ticket{
		ID:            "test-ticket-1234",
		Status:        StatusReady,
		Type:          TypeFeature,
		Priority:      1,
		Tags:          []string{"frontend", "urgent"},
		ExternalRef:   "GH-42",
		Branch:        "feature/foo",
		Created:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Title:         "Test ticket with all fields",
		Body:          "Some body text.",
		Notes:         []Note{{Timestamp: time.Now().UTC(), Text: "initial note"}},
		Deps:          []string{},
		Links:         []string{},
	}

	if err := src.Create(original); err != nil {
		t.Fatalf("create source ticket: %v", err)
	}

	results, err := MoveTicket(src, dst, original.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	newID := results[0].NewID

	// Read the moved ticket from dst.
	moved, err := dst.Get(newID)
	if err != nil {
		t.Fatalf("get moved ticket: %v", err)
	}

	// Fields that should be preserved as-is.
	if moved.Type != TypeFeature {
		t.Errorf("Type: got %q, want %q", moved.Type, TypeFeature)
	}
	if moved.Priority != 1 {
		t.Errorf("Priority: got %d, want 1", moved.Priority)
	}
	if moved.ExternalRef != "GH-42" {
		t.Errorf("ExternalRef: got %q, want %q", moved.ExternalRef, "GH-42")
	}
	if moved.Branch != "feature/foo" {
		t.Errorf("Branch: got %q, want %q", moved.Branch, "feature/foo")
	}
	if len(moved.Tags) != 2 || moved.Tags[0] != "frontend" || moved.Tags[1] != "urgent" {
		t.Errorf("Tags: got %v, want [frontend urgent]", moved.Tags)
	}
	// Fields that should be reset.
	if moved.Status != StatusBacklog {
		t.Errorf("Status: got %q, want %q (should reset to backlog)", moved.Status, StatusBacklog)
	}

	// Should have provenance note.
	foundProvenance := false
	for _, n := range moved.Notes {
		if len(n.Text) > 10 && n.Text[:10] == "Moved from" {
			foundProvenance = true
		}
	}
	if !foundProvenance {
		t.Error("missing provenance note on moved ticket")
	}

	// Original should be done.
	orig, err := src.Get(original.ID)
	if err != nil {
		t.Fatalf("get original: %v", err)
	}
	if orig.Status != StatusDone {
		t.Errorf("original status: got %q, want %q", orig.Status, StatusDone)
	}
}

func TestMoveTicketCreatesFileInBothDirs(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := &FileStore{Dir: srcDir}
	dst := &FileStore{Dir: dstDir}

	original := &Ticket{
		ID:       "iso-test-abcd",
		Status:   StatusReady,
		Type:     TypeFeature,
		Priority: 2,
		Created:  time.Now().UTC(),
		Title:    "Isolation test",
		Body:     "",
		Tags:     []string{"alpha"},
		Deps:     []string{},
		Links:    []string{},
		Notes:    []Note{{Timestamp: time.Now().UTC(), Text: "original note"}},
	}

	if err := src.Create(original); err != nil {
		t.Fatalf("create: %v", err)
	}

	results, err := MoveTicket(src, dst, original.ID, false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}

	// Verify one file exists in each directory.
	dstFiles, _ := filepath.Glob(filepath.Join(dstDir, "*.md"))
	srcFiles, _ := filepath.Glob(filepath.Join(srcDir, "*.md"))
	if len(dstFiles) != 1 || len(srcFiles) != 1 {
		t.Errorf("expected 1 file in each dir, got dst=%d src=%d", len(dstFiles), len(srcFiles))
	}

	// Verify dst ticket has the tag from original.
	moved, err := dst.Get(results[0].NewID)
	if err != nil {
		t.Fatalf("get moved: %v", err)
	}
	if len(moved.Tags) != 1 || moved.Tags[0] != "alpha" {
		t.Errorf("Tags: got %v, want [alpha]", moved.Tags)
	}
}
