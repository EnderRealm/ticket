package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

func TestShowLocalizesTimestamps(t *testing.T) {
	origLocal := time.Local
	time.Local = time.FixedZone("TEST", -7*3600)
	defer func() { time.Local = origLocal }()

	dir := t.TempDir()
	store := ticket.NewFileStore(dir)

	// Create stamps `updated` itself at the store's write choke point, so the
	// only timestamp we control deterministically here is `created`.
	created := time.Date(2026, 5, 30, 8, 20, 32, 0, time.UTC)
	tk := &ticket.Ticket{
		ID:      "t-1234",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: created,
		Title:   "Sample",
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	defaultOut := captureShow(t, store, "t-1234", false)

	// Local wall-clock with no zone suffix.
	if !contains(defaultOut, "created: 2026-05-30T01:20:32") {
		t.Errorf("default output missing local created time:\n%s", defaultOut)
	}
	// Original UTC value must be gone, and no timestamp should carry a Z suffix.
	if contains(defaultOut, "2026-05-30T08:20:32Z") {
		t.Errorf("default output still contains UTC created time:\n%s", defaultOut)
	}
	if contains(defaultOut, "Z\n") {
		t.Errorf("default output still contains a UTC (Z-suffixed) timestamp:\n%s", defaultOut)
	}

	// Metadata mode stays UTC.
	metaOut := captureShow(t, store, "t-1234", true)
	if !contains(metaOut, "created: 2026-05-30T08:20:32Z") {
		t.Errorf("metadata output missing UTC created time:\n%s", metaOut)
	}

	// On-disk file is unchanged (still UTC).
	data, err := os.ReadFile(filepath.Join(dir, "t-1234.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !contains(string(data), "created: 2026-05-30T08:20:32Z") {
		t.Errorf("on-disk file no longer UTC:\n%s", string(data))
	}
}

func TestLocalizeTimestamps(t *testing.T) {
	origLocal := time.Local
	time.Local = time.FixedZone("TEST", -7*3600)
	defer func() { time.Local = origLocal }()

	tk := &ticket.Ticket{
		Created:   time.Date(2026, 5, 30, 8, 20, 32, 0, time.UTC),
		Updated:   time.Date(2026, 5, 30, 9, 0, 0, 0, time.UTC),
		Completed: time.Date(2026, 5, 30, 10, 15, 0, 0, time.UTC),
	}
	serialized, err := ticket.Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	out := localizeTimestamps(string(serialized), tk)

	want := []string{
		"created: 2026-05-30T01:20:32",
		"updated: 2026-05-30T02:00:00",
		"completed: 2026-05-30T03:15:00",
	}
	for _, w := range want {
		if !contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	if contains(out, "Z\n") {
		t.Errorf("output still contains a UTC (Z-suffixed) timestamp:\n%s", out)
	}
}

func TestLocalizeTimestampsZeroCompleted(t *testing.T) {
	tk := &ticket.Ticket{
		Created: time.Date(2026, 5, 30, 8, 20, 32, 0, time.UTC),
		Updated: time.Date(2026, 5, 30, 9, 0, 0, 0, time.UTC),
	}
	serialized, err := ticket.Serialize(tk)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	// Zero Completed is not written by Serialize, so localization must be a no-op
	// for it and must not panic or corrupt output.
	out := localizeTimestamps(string(serialized), tk)
	if contains(out, "completed:") {
		t.Errorf("unexpected completed field:\n%s", out)
	}
}

func TestShowListsBareAndNamespacedChildren(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewFileStore(dir)

	parents := map[string]string{
		"sh-child-bare": "sh-epic-0001",
		"sh-child-ns":   "proj/sh-epic-0001",
	}
	for _, id := range []string{"sh-epic-0001", "sh-child-bare", "sh-child-ns"} {
		tk := &ticket.Ticket{
			ID:      id,
			Status:  ticket.StatusOpen,
			Type:    ticket.TypeFeature,
			Parent:  parents[id],
			Created: time.Now(),
			Title:   "Item " + id,
			Body:    "\n",
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	out := captureShow(t, store, "sh-epic-0001", false)
	idx := strings.Index(out, "## Children")
	if idx < 0 {
		t.Fatalf("show output missing Children section:\n%s", out)
	}
	children := out[idx:]
	for _, id := range []string{"sh-child-bare", "sh-child-ns"} {
		if !contains(children, id) {
			t.Errorf("Children section missing %s:\n%s", id, children)
		}
	}
}

func TestShowAnnotatesNamespacedParent(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewFileStore(dir)

	for _, id := range []string{"sp-epic-0001", "sp-child-0002"} {
		tk := &ticket.Ticket{
			ID:      id,
			Status:  ticket.StatusOpen,
			Type:    ticket.TypeFeature,
			Created: time.Now(),
			Title:   "Item " + id,
			Body:    "\n",
		}
		if id == "sp-child-0002" {
			tk.Parent = "proj/sp-epic-0001"
		}
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	out := captureShow(t, store, "sp-child-0002", false)
	if !contains(out, "parent: proj/sp-epic-0001  # Item sp-epic-0001") {
		t.Errorf("parent line missing its title annotation:\n%s", out)
	}
}

func captureShow(t *testing.T, store *ticket.FileStore, id string, metadataOnly bool) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := showTicket(store, id, metadataOnly)

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("showTicket: %v", err)
	}

	out, _ := io.ReadAll(r)
	return string(out)
}
