package ticket

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/internal/state"
)

// mutationSandbox points HOME at a temp tree — os.UserHomeDir honours it, so
// the mutation log lands there — and clears the two variables that would
// redirect the log or re-attribute it, so a test starts from the default
// resolution whatever the developer's shell exports.
func mutationSandbox(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	unsetEnv(t, project.StoreRootEnv)
	unsetEnv(t, SourceEnv)
}

// unsetEnv removes a variable for the test, restoring it afterwards: t.Setenv
// registers the restore, and the Unsetenv leaves it absent rather than empty —
// an empty TK_STORE_ROOT is a set-but-unusable value, not an unset one.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	os.Unsetenv(key)
}

func readMutations(t *testing.T, proj string) []MutationEntry {
	t.Helper()
	path, err := state.MutationLogPath(proj)
	if err != nil {
		t.Fatalf("mutation log path: %v", err)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("open mutation log: %v", err)
	}
	defer f.Close()

	var entries []MutationEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e MutationEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("mutation log line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read mutation log: %v", err)
	}
	return entries
}

func TestMutationLogRecordsEveryOperation(t *testing.T) {
	mutationSandbox(t)
	store := NewProjectFileStore(t.TempDir(), "alpha")

	main := sampleTicket("m-0001")
	if err := store.Create(main); err != nil {
		t.Fatalf("Create: %v", err)
	}
	dep := sampleTicket("d-0001")
	if err := store.Create(dep); err != nil {
		t.Fatalf("Create dep: %v", err)
	}

	main.Status = StatusOpen
	if err := store.Update(main); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, err := Mutate(store, "m-0001", func(t *Ticket) error {
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "a note"})
		return nil
	}); err != nil {
		t.Fatalf("Mutate note: %v", err)
	}
	if _, err := Mutate(store, "m-0001", func(t *Ticket) error {
		if err := AddDep(t, "d-0001"); err != nil {
			return err
		}
		return SetDepCargo(t, "d-0001", "the schema")
	}); err != nil {
		t.Fatalf("Mutate dep: %v", err)
	}
	if _, err := Mutate(store, "m-0001", func(t *Ticket) error {
		AddLink(t, dep)
		return nil
	}); err != nil {
		t.Fatalf("Mutate link: %v", err)
	}
	if err := store.Delete("m-0001"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := []MutationEntry{
		{TicketID: "m-0001", Operation: MutationCreate},
		{TicketID: "d-0001", Operation: MutationCreate},
		{TicketID: "m-0001", Operation: MutationEdit, FieldsChanged: []string{"status"}},
		{TicketID: "m-0001", Operation: MutationAddNote, FieldsChanged: []string{"notes"}},
		{TicketID: "m-0001", Operation: MutationDep, FieldsChanged: []string{"deps", "dep_cargo"}},
		{TicketID: "m-0001", Operation: MutationLink, FieldsChanged: []string{"links"}},
		{TicketID: "m-0001", Operation: MutationDelete},
	}
	got := readMutations(t, "alpha")
	if len(got) != len(want) {
		t.Fatalf("logged %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].TicketID != w.TicketID || got[i].Operation != w.Operation {
			t.Errorf("entry %d = %s/%s, want %s/%s", i, got[i].TicketID, got[i].Operation, w.TicketID, w.Operation)
		}
		if !slices.Equal(got[i].FieldsChanged, w.FieldsChanged) {
			t.Errorf("entry %d fields_changed = %v, want %v", i, got[i].FieldsChanged, w.FieldsChanged)
		}
		if got[i].Timestamp.IsZero() {
			t.Errorf("entry %d has no timestamp", i)
		}
	}
}

func TestMutationLogIgnoresAnEpicsDerivedStatus(t *testing.T) {
	mutationSandbox(t)
	store := NewProjectFileStore(t.TempDir(), "alpha")

	epic := sampleTicket("e-0001")
	epic.Type = TypeEpic
	epic.Status = StatusBacklog
	if err := store.Create(epic); err != nil {
		t.Fatalf("Create epic: %v", err)
	}
	// The child makes the epic derive open while its file still holds backlog,
	// which is the state a note has to be recorded as a note through.
	child := sampleTicket("c-0001")
	child.Status = StatusOpen
	child.Parent = "e-0001"
	if err := store.Create(child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	if _, err := Mutate(store, "e-0001", func(t *Ticket) error {
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "a note"})
		return nil
	}); err != nil {
		t.Fatalf("Mutate note: %v", err)
	}

	entries := readMutations(t, "alpha")
	got := entries[len(entries)-1]
	if got.Operation != MutationAddNote {
		t.Errorf("operation = %s, want %s", got.Operation, MutationAddNote)
	}
	if !slices.Equal(got.FieldsChanged, []string{fieldNotes}) {
		t.Errorf("fields_changed = %v, want [%s]", got.FieldsChanged, fieldNotes)
	}
}

func TestMutationLogIsAppendOnly(t *testing.T) {
	mutationSandbox(t)
	store := NewProjectFileStore(t.TempDir(), "alpha")
	path, err := state.MutationLogPath("alpha")
	if err != nil {
		t.Fatal(err)
	}

	tk := sampleTicket("a-0001")
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	tk.Status = StatusOpen
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := store.Delete("a-0001"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(after) <= len(before) {
		t.Fatalf("log did not grow: %d bytes, was %d", len(after), len(before))
	}
	if string(after[:len(before)]) != string(before) {
		t.Errorf("existing lines were rewritten:\nbefore: %s\nafter:  %s", before, after[:len(before)])
	}
}

func TestMutationSourceAttribution(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		store func(dir string) Store
		want  string
	}{
		{
			name:  "defaults to human",
			store: func(dir string) Store { return NewProjectFileStore(dir, "alpha") },
			want:  SourceHuman,
		},
		{
			name:  "store source",
			store: func(dir string) Store { return WithSource(NewProjectFileStore(dir, "alpha"), "claude") },
			want:  "claude",
		},
		{
			name:  "env wins over the default",
			env:   "codex",
			store: func(dir string) Store { return NewProjectFileStore(dir, "alpha") },
			want:  "codex",
		},
		{
			name:  "env wins over the store source",
			env:   "codex",
			store: func(dir string) Store { return WithSource(NewProjectFileStore(dir, "alpha"), "claude") },
			want:  "codex",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutationSandbox(t)
			if tc.env != "" {
				t.Setenv(SourceEnv, tc.env)
			}
			if err := tc.store(t.TempDir()).Create(sampleTicket("s-0001")); err != nil {
				t.Fatalf("Create: %v", err)
			}
			got := readMutations(t, "alpha")
			if len(got) != 1 {
				t.Fatalf("logged %d entries, want 1", len(got))
			}
			if got[0].Source != tc.want {
				t.Errorf("source = %q, want %q", got[0].Source, tc.want)
			}
		})
	}
}

func TestWithSourceLeavesTheOriginalStoreAlone(t *testing.T) {
	store := NewProjectFileStore(t.TempDir(), "alpha")
	if WithSource(store, "claude") == Store(store) {
		t.Error("WithSource returned the store it was given")
	}
	if store.Source != "" {
		t.Errorf("original store Source = %q, want empty", store.Source)
	}
}

func TestMutationLogSkippedForStoreWithNoProject(t *testing.T) {
	mutationSandbox(t)
	store := NewFileStore(t.TempDir())
	if err := store.Create(sampleTicket("n-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ticket", "state")); !os.IsNotExist(err) {
		t.Errorf("a store with no project wrote state under %s (stat err: %v)", home, err)
	}
}

func TestMutationLogFollowsStoreRootOverride(t *testing.T) {
	mutationSandbox(t)
	root := t.TempDir()
	t.Setenv(project.StoreRootEnv, root)

	store := NewProjectFileStore(t.TempDir(), "alpha")
	if err := store.Create(sampleTicket("o-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "state", "alpha", "mutations.jsonl")); err != nil {
		t.Errorf("log did not land under the override root: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ticket", "state")); !os.IsNotExist(err) {
		t.Errorf("an isolated store wrote state under %s (stat err: %v)", home, err)
	}
}

func TestMutationLogRecordsBothSidesOfAMove(t *testing.T) {
	mutationSandbox(t)
	src := NewProjectFileStore(t.TempDir(), "alpha")
	dst := NewProjectFileStore(t.TempDir(), "beta")
	if err := src.Create(sampleTicket("v-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	results, err := MoveTicket(src, dst, "v-0001", false)
	if err != nil {
		t.Fatalf("MoveTicket: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("moved %d tickets, want 1", len(results))
	}
	_, newBare := ParseNamespacedID(results[0].NewID)

	srcMove := lastOp(readMutations(t, "alpha"), MutationMove)
	if srcMove == nil {
		t.Fatal("no move entry in the source project's log")
	}
	if srcMove.TicketID != "v-0001" {
		t.Errorf("source move ticket_id = %q, want %q", srcMove.TicketID, "v-0001")
	}
	dstMove := lastOp(readMutations(t, "beta"), MutationMove)
	if dstMove == nil {
		t.Fatal("no move entry in the destination project's log")
	}
	if dstMove.TicketID != newBare {
		t.Errorf("destination move ticket_id = %q, want %q", dstMove.TicketID, newBare)
	}
}

func lastOp(entries []MutationEntry, op MutationOp) *MutationEntry {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Operation == op {
			return &entries[i]
		}
	}
	return nil
}

func TestMutationStillWritesWhenTheLogCannotBe(t *testing.T) {
	mutationSandbox(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// A regular file where the project's state directory belongs: the log's
	// MkdirAll fails, and the write must not.
	stateDir := filepath.Join(home, ".ticket", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "alpha"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewProjectFileStore(t.TempDir(), "alpha")
	if err := store.Create(sampleTicket("f-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Get("f-0001"); err != nil {
		t.Errorf("ticket was not written: %v", err)
	}
}
