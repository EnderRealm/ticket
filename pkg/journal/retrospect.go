package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/EnderRealm/ticket/v7/internal/state"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// loomBinary is the knowledge miner the watch cycle hands a closed ticket to.
// tk does not depend on it: it is looked up on PATH per cycle, and a machine
// without it journals exactly as before.
const loomBinary = "loom"

// maxRetrospectsPerCycle bounds how many runs one cycle starts. Each is an LLM
// extraction, and the pending set is not bounded by the cycle — a synced store
// arrives with a batch of closes, and the missing-loom path deliberately keeps
// its closes pending until loom is installed. Whatever is left over carries no
// marker and fires on later cycles, seconds apart, for free.
const maxRetrospectsPerCycle = 4

// retrospectStderrLimit bounds what one child's stderr costs. The capture is
// held for the life of the run and goes into a log line on failure, so neither
// the daemon's memory nor its log is sized by the child.
const retrospectStderrLimit = 2048

// retrospectMarker is one processed close. FiredAt is when the marker was
// written rather than when the child exited — the run is fire-and-forget, so
// there is no completion to record — and on a seeded row it is when the flag was
// first observed.
//
// Idempotency is keyed on the ticket ID alone, deliberately: a ticket reopened
// and closed again never fires a second retrospect. Keying on the close instead
// would mine the same sessions twice for what is usually one piece of work, and
// a missed second run costs one ticket's candidates where a duplicate spends
// another extraction and files the same ones again.
type retrospectMarker struct {
	TicketID string `json:"ticket_id"`
	FiredAt  string `json:"fired_at"`
}

// runRetrospects fires `loom retrospect <project>/<id>` for tickets that read
// done or closed and have no marker yet, up to budget of them, and reports how
// many it started, how many it seeded, and what it wants said. It is
// best-effort throughout: every failure is a warning the caller logs, never an
// error that would cost the cycle its journal or its auto-closes.
//
// Scanning the store rather than watching the close loop is what makes the
// trigger cover every path — a manual `tk edit`, the TUI, an MCP write, and the
// cycle's own auto-close, which by now reads done like any other.
func runRetrospects(projectName string, store ticket.Store, budget int) (fired, seeded int, warnings []string) {
	// The project half of the namespaced ID reaches argv too, and it has the same
	// provenance as the ticket half: a key of the shared config.yaml, which
	// arrives over that file's git remote. project.ValidName gates registration
	// only — it admits a leading dash and non-printables, and a loaded config is
	// never re-checked — so the name is held to a plain ID's shape here. Nothing
	// is written, so a repaired config fires these closes later.
	if !plainTicketID(projectName) {
		return 0, 0, []string{fmt.Sprintf("retrospect: skipped project %q: not a plain project name", projectName)}
	}

	path, err := state.RetrospectLogPath(projectName)
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("retrospect: %v", err)}
	}

	release, locked, err := lockRetrospects(path)
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("retrospect: lock markers: %v", err)}
	}
	if !locked {
		// Another journal loop holds the window and is doing this project's scan.
		// Silently, not as a warning: a machine running `tk watch` beside a
		// `tk serve` per MCP client would log it on every tick of every loser.
		return 0, 0, nil
	}
	defer release()

	processed, seen, err := loadRetrospects(path)
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("retrospect: read markers: %v", err)}
	}

	tickets, err := store.List()
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("retrospect: list tickets: %v", err)}
	}

	var pending []string
	for _, t := range tickets {
		// An epic's done is derived from its children, each of which fires a
		// retrospect of its own, so mining the epic would re-mine the same
		// sessions under a ticket nobody worked on directly.
		if t.Type == ticket.TypeEpic {
			continue
		}
		if t.Status != ticket.StatusDone && t.Status != ticket.StatusClosed {
			continue
		}
		if _, done := processed[t.ID]; done {
			continue
		}
		pending = append(pending, t.ID)
	}
	sort.Strings(pending)

	// First cycle with the flag on: record the store's existing closes without
	// firing. Every retrospect is an LLM extraction, and enabling the flag on a
	// store with years of done tickets would otherwise launch one per ticket at
	// once. Only closes after enablement are mined.
	if !seen {
		if err := seedRetrospects(path, pending); err != nil {
			return 0, 0, []string{fmt.Sprintf("retrospect: seed markers: %v", err)}
		}
		return 0, len(pending), []string{fmt.Sprintf("retrospect: seeded %d closed ticket(s); only closes from now on are mined", len(pending))}
	}

	if len(pending) == 0 {
		return 0, 0, nil
	}

	loomPath, err := exec.LookPath(loomBinary)
	if err != nil {
		// Nothing is recorded, so these closes fire on the first cycle after loom
		// is installed rather than being lost to a machine that never had it.
		return 0, 0, []string{fmt.Sprintf("retrospect: %s not found; %d retrospect(s) pending", loomBinary, len(pending))}
	}

	for i, id := range pending {
		if fired >= budget {
			warnings = append(warnings, fmt.Sprintf("retrospect: fired %d in this pass; %d deferred to the next cycle", fired, len(pending)-i))
			break
		}

		// The ID reaches a child's argv out of ticket-file YAML, which arrives over
		// the store's git remote. Only a plain ticket ID is handed over: a path
		// component names something the project does not own, a leading dash is a
		// flag to whatever parses loom's arguments, and a non-printable byte
		// belongs in no ID. No marker is written, so a store that is repaired
		// fires later.
		if !plainTicketID(id) {
			warnings = append(warnings, fmt.Sprintf("retrospect: skipped %q: not a plain ticket ID", id))
			continue
		}

		// The marker goes down before the process starts, so a crash between the
		// two loses a retrospect rather than repeating one: a duplicate spends a
		// second extraction and files the same candidates again, where a miss
		// costs one ticket's candidates.
		if err := appendRetrospects(path, id); err != nil {
			warnings = append(warnings, fmt.Sprintf("retrospect: record %s: %v", id, err))
			continue
		}

		nsID := ticket.FormatNamespacedID(projectName, id)
		cmd := exec.Command(loomPath, "retrospect", nsID)
		stderr := &cappedBuffer{}
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			warnings = append(warnings, fmt.Sprintf("retrospect: start %s: %v", nsID, err))
			continue
		}
		fired++

		// Reaped off the cycle: a retrospect runs an LLM extraction, and waiting
		// on it here would stall the watcher for as long as that takes. The daemon
		// outlives the goroutine; a `--once` run exits first and the child's status
		// goes unlogged, which costs a log line and nothing else.
		//
		// Straight to the log rather than to the cycle's warnings: the child
		// outlives the cycle that started it, so there is no result left to append
		// to, and every caller of a cycle is a daemon that owns the log.
		go func() {
			if err := cmd.Wait(); err != nil {
				log.Printf("retrospect %s: %v: %s", nsID, err, strings.TrimSpace(stderr.String()))
			}
		}()
	}

	return fired, 0, warnings
}

// plainTicketID reports whether an ID may be handed to a child process. It gates
// the project half of the namespaced ID as well, which reaches argv on the same
// terms. "." and ".." are named because each is its own filepath.Base and would
// otherwise pass as a plain component while naming a directory.
func plainTicketID(id string) bool {
	if id == "" || id == "." || id == ".." || id != filepath.Base(id) || strings.HasPrefix(id, "-") {
		return false
	}
	if !utf8.ValidString(id) {
		return false
	}
	for _, r := range id {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// cappedBuffer keeps the first retrospectStderrLimit bytes written to it and
// drops the rest. Writes are reported as complete so a child is never stopped
// by a short write on its own stderr.
type cappedBuffer struct {
	buf []byte
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := retrospectStderrLimit - len(b.buf); room > 0 {
		chunk := p
		if len(chunk) > room {
			chunk = chunk[:room]
		}
		b.buf = append(b.buf, chunk...)
	}
	return len(p), nil
}

// String is safe to call once Wait has returned: Wait waits for the goroutine
// copying the child's stderr into this buffer.
func (b *cappedBuffer) String() string { return string(b.buf) }

// lockRetrospects serialises one project's read-scan-append window across
// processes and reports whether it took the lock. More than one journal loop
// runs on a machine in practice — `tk watch run` beside a `tk serve` per MCP
// client — and without it two loops read the same close as pending and each
// start an extraction of it.
//
// Non-blocking: a loop that loses the race has nothing to wait for, because the
// holder is performing exactly the scan it wanted, and the next cycle is seconds
// away. The lock file sits beside the marker file under ~/.ticket/state, which
// is never synced, so unlike the ticket store's locks there is nothing to keep
// it out of a commit. Its scope is one host, as flock's always is.
func lockRetrospects(path string) (func(), bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	// Closing releases the lock: an flock belongs to the open file description,
	// and this descriptor is the only one holding it.
	return func() { f.Close() }, true, nil
}

// loadRetrospects reads the ticket IDs already retrospected, and whether the
// marker file exists at all — an absent file is the un-seeded state, which is
// not the same as a file holding nothing. Blank and unparseable lines are
// skipped, as in LoadJournalState: a truncated line must not re-fire the whole
// store.
func loadRetrospects(path string) (map[string]struct{}, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	processed := map[string]struct{}{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row retrospectMarker
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.TicketID == "" {
			continue
		}
		processed[row.TicketID] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, true, err
	}
	return processed, true, nil
}

// seedRetrospects records the store's existing closes as already processed. The
// file is created even with nothing to seed, or the next cycle would read the
// store as un-seeded again and swallow a close that happened after the flag went
// on.
func seedRetrospects(path string, ids []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return appendRetrospects(path, ids...)
}

func appendRetrospects(path string, ids ...string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	values := make([]any, len(ids))
	for i, id := range ids {
		values[i] = retrospectMarker{TicketID: id, FiredAt: now}
	}
	return state.AppendJSONL(path, values...)
}
