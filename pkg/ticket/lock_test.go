package ticket

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTicketLockExcludesASecondHolder pins the assumption the concurrency tests
// below rest on: flock belongs to the open file description, so two
// descriptors in one process exclude each other exactly as two processes do.
// Without that, every goroutine-based test here would pass vacuously.
func TestTicketLockExcludesASecondHolder(t *testing.T) {
	store, _ := testStore(t)

	release, err := store.lockTicket("held-0001")
	if err != nil {
		t.Fatalf("lockTicket: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		r, err := store.lockTicket("held-0001")
		if err == nil {
			r()
		}
		second <- err
	}()

	select {
	case <-second:
		t.Fatal("a second holder took the lock while the first held it")
	case <-time.After(200 * time.Millisecond):
	}

	release()
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("second holder: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second holder never acquired the lock after it was released")
	}
}

// TestMutateConcurrentAppendsKeepEveryNote is the lost-update reproduction:
// every writer appends to the same ticket, and every note has to survive.
// Against the read-modify-write this replaced, three or four of twenty did.
func TestMutateConcurrentAppendsKeepEveryNote(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("race-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	const writers = 20
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = Mutate(store, "race-0001", func(t *Ticket) error {
				t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: fmt.Sprintf("note %d", i)})
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	final, err := store.Get("race-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	texts := map[string]bool{}
	for _, n := range final.Notes {
		texts[n.Text] = true
	}
	if len(texts) != writers {
		t.Errorf("%d distinct notes survived, want %d", len(texts), writers)
	}

	// The atomic replace leaves nothing behind: the temp files are renamed onto
	// the target, and every error path removes its own.
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("store holds %d files after the writes, want only the ticket", len(entries))
	}
}

// TestMutateDisjointTicketsAllSucceed covers the other half: concurrent writers
// against different tickets never contend, since each takes only its own lock.
func TestMutateDisjointTicketsAllSucceed(t *testing.T) {
	store, _ := testStore(t)

	const tickets = 10
	const notesEach = 5
	for i := 0; i < tickets; i++ {
		if err := store.Create(sampleTicket(fmt.Sprintf("wide-%04d", i))); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, tickets*notesEach)
	for i := 0; i < tickets; i++ {
		for j := 0; j < notesEach; j++ {
			wg.Add(1)
			go func(i, j int) {
				defer wg.Done()
				_, errs[i*notesEach+j] = Mutate(store, fmt.Sprintf("wide-%04d", i), func(t *Ticket) error {
					t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: fmt.Sprintf("note %d", j)})
					return nil
				})
			}(i, j)
		}
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	for i := 0; i < tickets; i++ {
		id := fmt.Sprintf("wide-%04d", i)
		got, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if len(got.Notes) != notesEach {
			t.Errorf("%s has %d notes, want %d", id, len(got.Notes), notesEach)
		}
	}
}

// TestLockIsPerTicketNotPerStore is the guarantee stated as a structural fact
// rather than a timing measurement: with one ticket's lock held, a write to
// another ticket still completes.
func TestLockIsPerTicketNotPerStore(t *testing.T) {
	store, _ := testStore(t)
	for _, id := range []string{"held-0001", "free-0002"} {
		if err := store.Create(sampleTicket(id)); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	release, err := store.lockTicket("held-0001")
	if err != nil {
		t.Fatalf("lockTicket: %v", err)
	}
	defer release()

	done := make(chan error, 1)
	go func() {
		_, err := Mutate(store, "free-0002", func(t *Ticket) error {
			t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "unrelated"})
			return nil
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write to an unlocked ticket: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a write to free-0002 blocked while held-0001 was locked — the lock is not per ticket")
	}
}

func TestUpdateRefusesAStaleRead(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("conf-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stale, err := store.Get("conf-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	winner, err := store.Get("conf-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	winner.Title = "Written first"
	if err := store.Update(winner); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stale.Title = "Computed from what the winner replaced"
	err = store.Update(stale)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Update of a stale read = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "conf-0001") {
		t.Errorf("error should name the ticket, got: %v", err)
	}

	got, err := store.Get("conf-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Written first" {
		t.Errorf("Title = %q, want the refused write to have left the winner's", got.Title)
	}
}

// TestUpdateTwiceFromOneRead covers the re-stamp after a write: a caller
// holding one ticket and updating it twice is not in conflict with itself.
func TestUpdateTwiceFromOneRead(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("twice-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get("twice-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Title = "First"
	if err := store.Update(got); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	got.Title = "Second"
	if err := store.Update(got); err != nil {
		t.Fatalf("second Update: %v", err)
	}
}

// TestUpdateWithoutAVersionWrites covers the compatibility escape: a ticket the
// caller built rather than read has nothing to compare against and is written
// unconditionally.
func TestUpdateWithoutAVersionWrites(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("fresh-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	built := sampleTicket("fresh-0001")
	built.Title = "Built by the caller"
	if err := store.Update(built); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := store.Get("fresh-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Built by the caller" {
		t.Errorf("Title = %q, want the unconditional write to have landed", got.Title)
	}
}

// TestUpdateRefusesAReadOnlyTicketFile covers the mode half of the atomic
// replace: renaming over a file does not consult the target's mode, so the
// refusal has to be explicit or a ticket someone protected would be replaced.
func TestUpdateRefusesAReadOnlyTicketFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("ro-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := filepath.Join(store.Dir, "ro-0001.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })

	got, err := store.Get("ro-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Title = "Should not land"
	if err := store.Update(got); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Update = %v, want fs.ErrPermission", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused write changed the file")
	}
}

// TestWritePreservesTheTargetsMode pins the other half: os.WriteFile left an
// existing file's mode alone, and a temp file plus rename publishes whatever
// the temp file carries — so a ticket stored at 0600 must not come back 0644.
func TestWritePreservesTheTargetsMode(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create(sampleTicket("mode-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := filepath.Join(store.Dir, "mode-0001.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got, err := store.Get("mode-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Title = "Rewritten"
	if err := store.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 — the write widened it", info.Mode().Perm())
	}
}

// fallbackStore implements Store and nothing else. FileStore is held in a named
// field rather than embedded, so the mutate method is not promoted and Mutate
// takes its retry-on-conflict path — the only path on which fn is re-run.
type fallbackStore struct {
	inner    *FileStore
	afterGet func()
}

func (f *fallbackStore) Get(id string) (*Ticket, error) {
	t, err := f.inner.Get(id)
	if f.afterGet != nil {
		f.afterGet()
	}
	return t, err
}
func (f *fallbackStore) List() ([]*Ticket, error) { return f.inner.List() }
func (f *fallbackStore) Create(t *Ticket) error   { return f.inner.Create(t) }
func (f *fallbackStore) Update(t *Ticket) error   { return f.inner.Update(t) }
func (f *fallbackStore) Delete(id string) error   { return f.inner.Delete(id) }

func TestMutateFallbackRetriesOnConflict(t *testing.T) {
	inner, _ := testStore(t)
	if err := inner.Create(sampleTicket("fb-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	store := &fallbackStore{inner: inner}

	// One interfering write, landing after the first read: the first attempt's
	// Update is then computed from state the store no longer holds.
	interfered := false
	store.afterGet = func() {
		if interfered {
			return
		}
		interfered = true
		other, err := inner.Get("fb-0001")
		if err != nil {
			t.Fatalf("interfering Get: %v", err)
		}
		other.Notes = append(other.Notes, Note{Timestamp: time.Now().UTC(), Text: "interloper"})
		if err := inner.Update(other); err != nil {
			t.Fatalf("interfering Update: %v", err)
		}
	}

	calls := 0
	got, err := Mutate(store, "fb-0001", func(tk *Ticket) error {
		calls++
		tk.Notes = append(tk.Notes, Note{Timestamp: time.Now().UTC(), Text: "mine"})
		return nil
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if calls != 2 {
		t.Errorf("fn ran %d times, want 2 — the conflict should have re-read and re-applied", calls)
	}

	final, err := inner.Get("fb-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	texts := map[string]bool{}
	for _, n := range final.Notes {
		texts[n.Text] = true
	}
	if !texts["interloper"] || !texts["mine"] {
		t.Errorf("notes = %v, want both writes to have survived", texts)
	}
	if len(got.Notes) != len(final.Notes) {
		t.Errorf("returned ticket has %d notes, stored has %d", len(got.Notes), len(final.Notes))
	}
}

// TestNoReaderSeesAPartialTicket is what the atomic replace buys: os.WriteFile
// truncated before writing, so a concurrent reader could observe zero bytes.
func TestNoReaderSeesAPartialTicket(t *testing.T) {
	store, _ := testStore(t)
	tk := sampleTicket("torn-0001")
	tk.Body = "\n" + strings.Repeat("filler line to make the write worth interrupting\n", 2000)
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := filepath.Join(store.Dir, "torn-0001.md")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := Mutate(store, "torn-0001", func(t *Ticket) error {
				t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: fmt.Sprintf("note %d", i)})
				return nil
			}); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()

	for i := 0; i < 500; i++ {
		data, err := os.ReadFile(path)
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d: %v", i, err)
		}
		if len(data) == 0 {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d observed a zero-length ticket file", i)
		}
		got, err := Parse(strings.NewReader(string(data)))
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d observed an unparseable ticket: %v", i, err)
		}
		if got.ID != "torn-0001" {
			close(stop)
			wg.Wait()
			t.Fatalf("read %d observed ID %q, want torn-0001", i, got.ID)
		}
	}
	close(stop)
	wg.Wait()
}

func TestMultiStoreMutate(t *testing.T) {
	ms, _ := testMultiStore(t, "proj")
	if err := ms.Create(sampleTicket("proj/multi-0001")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := Mutate(ms, "multi-0001", func(t *Ticket) error {
		if t.ID != "proj/multi-0001" {
			return fmt.Errorf("fn saw ID %q, want the namespaced one", t.ID)
		}
		t.Notes = append(t.Notes, Note{Timestamp: time.Now().UTC(), Text: "through the multistore"})
		return nil
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if got.ID != "proj/multi-0001" {
		t.Errorf("returned ID = %q, want proj/multi-0001", got.ID)
	}

	stored, err := ms.Get("proj/multi-0001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(stored.Notes) != 1 {
		t.Errorf("notes = %d, want 1", len(stored.Notes))
	}
}
