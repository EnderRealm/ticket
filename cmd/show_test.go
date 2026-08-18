package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestShowSanitizesStoredContentWithoutChangingJSON(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewFileStore(dir)
	hostileTitle := "Déjà 日本語 \x1b[31mred\x1b[0m \u202erepaint"
	tk := &ticket.Ticket{
		ID:      "ts-safe-0001",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   hostileTitle,
		Body:    "Description \x1b[2Jclear \u2066hidden\u2069\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}

	out := captureShow(t, store, tk.ID, false)
	for _, r := range []rune{'\x1b', '\u202e', '\u2066', '\u2069'} {
		if strings.ContainsRune(out, r) {
			t.Errorf("human output contains control or format character %U:\n%q", r, out)
		}
	}
	for _, want := range []string{"Déjà", "日本語", "Description", "�[31mred�[0m", "�[2Jclear"} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%q", want, out)
		}
	}

	stored, err := store.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	data, err := json.Marshal(toTicketJSON(stored))
	if err != nil {
		t.Fatalf("Marshal JSON: %v", err)
	}
	var decoded ticketJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal JSON: %v", err)
	}
	if decoded.Title != hostileTitle {
		t.Errorf("JSON title = %q, want stored bytes %q", decoded.Title, hostileTitle)
	}
}

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
	store := ticket.NewProjectFileStore(dir, "proj")

	parents := map[string]string{
		"sh-child-bare": "sh-epic-0001",
		"sh-child-ns":   "proj/sh-epic-0001",
	}
	for _, id := range []string{"sh-epic-0001", "sh-child-bare", "sh-child-ns"} {
		typ := ticket.TypeFeature
		status := ticket.StatusOpen
		if id == "sh-epic-0001" {
			typ = ticket.TypeEpic
			status = ticket.StatusBacklog
		}
		tk := &ticket.Ticket{
			ID:      id,
			Status:  status,
			Type:    typ,
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
	store := ticket.NewProjectFileStore(dir, "proj")

	for _, id := range []string{"sp-epic-0001", "sp-child-0002"} {
		tk := &ticket.Ticket{
			ID:      id,
			Status:  ticket.StatusBacklog,
			Type:    ticket.TypeEpic,
			Created: time.Now(),
			Title:   "Item " + id,
			Body:    "\n",
		}
		if id == "sp-child-0002" {
			tk.Type = ticket.TypeFeature
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

func TestShowResolvesNamespacedDepsAndLinks(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewProjectFileStore(dir, "proj")

	createShowTicket(t, store, "sn-blocker-0001", ticket.StatusOpen)
	createShowTicket(t, store, "sn-bare-0002", ticket.StatusReady)
	createShowTicket(t, store, "sn-linked-0003", ticket.StatusDone)
	createShowTicket(t, store, "sn-donedep-0004", ticket.StatusDone)

	subject := &ticket.Ticket{
		ID:      "sn-subject-0005",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Subject",
		Deps:    []string{"proj/sn-blocker-0001", "sn-bare-0002", "proj/sn-donedep-0004"},
		Links:   []string{"proj/sn-linked-0003"},
		Body:    "\n",
	}
	if err := store.Create(subject); err != nil {
		t.Fatalf("Create subject: %v", err)
	}

	out := captureShow(t, store, "sn-subject-0005", false)

	want := []string{
		// A namespaced dep renders with the target's real status and title.
		"- proj/sn-blocker-0001 [open] Item sn-blocker-0001",
		// A bare dep renders exactly as before.
		"- sn-bare-0002 [ready] Item sn-bare-0002",
		// A namespaced link renders with the target's real status and title.
		"- proj/sn-linked-0003 [done] Item sn-linked-0003",
	}
	for _, w := range want {
		if !contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
	// A namespaced dep that is done resolves, so it is not a blocker at all.
	idx := strings.Index(out, "## Blockers")
	if idx < 0 {
		t.Fatalf("show output missing Blockers section:\n%s", out)
	}
	if contains(out[idx:], "sn-donedep-0004") {
		t.Errorf("done namespaced dep listed as a blocker:\n%s", out[idx:])
	}
	if contains(out, "[unknown]") {
		t.Errorf("output has an unresolved reference:\n%s", out)
	}
}

func TestShowRendersDanglingReferenceUnknown(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewProjectFileStore(dir, "proj")

	createShowTicket(t, store, "sg-present-0001", ticket.StatusOpen)
	subject := &ticket.Ticket{
		ID:      "sg-subject-0002",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Subject",
		// Nothing named here exists: an absent bare half, and a bare half this
		// store holds under a prefix naming another project.
		Deps:  []string{"proj/sg-missing-0003"},
		Links: []string{"other/sg-present-0001"},
		Body:  "\n",
	}
	if err := store.Create(subject); err != nil {
		t.Fatalf("Create subject: %v", err)
	}

	out := captureShow(t, store, "sg-subject-0002", false)

	for _, w := range []string{
		"- proj/sg-missing-0003 [unknown]",
		"- other/sg-present-0001 [unknown]",
	} {
		if !contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestShowListsNamespacedDependentAsBlocking(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewProjectFileStore(dir, "proj")

	createShowTicket(t, store, "sb-subject-0001", ticket.StatusOpen)
	dependent := &ticket.Ticket{
		ID:      "sb-dependent-0002",
		Status:  ticket.StatusReady,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Dependent",
		Deps:    []string{"proj/sb-subject-0001"},
		Body:    "\n",
	}
	if err := store.Create(dependent); err != nil {
		t.Fatalf("Create dependent: %v", err)
	}

	out := captureShow(t, store, "sb-subject-0001", false)
	idx := strings.Index(out, "## Blocking")
	if idx < 0 {
		t.Fatalf("show output missing Blocking section:\n%s", out)
	}
	if !contains(out[idx:], "- sb-dependent-0002 [ready] Dependent") {
		t.Errorf("Blocking section missing the namespaced dependent:\n%s", out[idx:])
	}
}

func TestShowLeavesAmbiguousReferenceUnknown(t *testing.T) {
	dir := t.TempDir()
	store := ticket.NewProjectFileStore(dir, "proj")

	createShowTicket(t, store, "sa-target-0001", ticket.StatusOpen)
	// A second ticket sharing the bare half, which only a file the store did not
	// write can hold — its ID is namespaced, so it is not this project's
	// sa-target-0001 and matching the bare half would be a guess between the two.
	other := &ticket.Ticket{
		ID:      "other/sa-target-0001",
		Status:  ticket.StatusDone,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Foreign namesake",
		Body:    "\n",
	}
	data, err := ticket.Serialize(other)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sa-foreign-0002.md"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	subject := &ticket.Ticket{
		ID:      "sa-subject-0003",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Subject",
		Deps:    []string{"proj/sa-target-0001"},
		Body:    "\n",
	}
	if err := store.Create(subject); err != nil {
		t.Fatalf("Create subject: %v", err)
	}

	out := captureShow(t, store, "sa-subject-0003", false)
	if !contains(out, "- proj/sa-target-0001 [unknown]") {
		t.Errorf("ambiguous reference did not stay unknown:\n%s", out)
	}
}

func createShowTicket(t *testing.T, store *ticket.FileStore, id string, status ticket.Status) {
	t.Helper()
	tk := &ticket.Ticket{
		ID:      id,
		Status:  status,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Item " + id,
		Body:    "\n",
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create %s: %v", id, err)
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
