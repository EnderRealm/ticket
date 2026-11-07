package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

func TestMovePartialFailurePrintsWhatLanded(t *testing.T) {
	// The move is not rolled back. When the source close fails after the
	// target copy is written, the error names the orphaned copy and the source
	// ticket left open — that pair is what reconciling needs — and stdout
	// reports no move, since none completed.
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only file mode this test relies on")
	}
	src, targetRepo, targetDir := movePair(t)
	srcDir := src.Dir

	mkMoveTicket(t, src, "mv-leaf-0001", ticket.TypeFeature, ticket.StatusOpen, "")

	// The close fails after the target copy is written.
	leafFile := filepath.Join(srcDir, "mv-leaf-0001.md")
	if err := os.Chmod(leafFile, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(leafFile, 0o644) })

	stdout, stderr, moveErr := captureMove(t, "mv-leaf-0001", targetRepo)
	if moveErr == nil {
		t.Fatal("runMove succeeded, want a failure closing the read-only source")
	}

	dstTickets, err := ticket.NewProjectFileStore(targetDir, "mv-dst").List()
	if err != nil {
		t.Fatalf("List target: %v", err)
	}
	if len(dstTickets) != 1 {
		t.Fatalf("target holds %d tickets, want the orphaned copy", len(dstTickets))
	}
	// The destination is a central project, so what the error names is the
	// namespaced form of the ID on disk.
	orphanID := ticket.FormatNamespacedID("mv-dst", dstTickets[0].ID)

	if contains(stdout, "Moved") {
		t.Errorf("stdout reports a move that did not complete:\n%s", stdout)
	}
	if contains(stderr, "Move failed partway") {
		t.Errorf("no move completed, so there is no partial move to banner:\n%s", stderr)
	}
	if !contains(moveErr.Error(), orphanID) {
		t.Errorf("error %q does not name %s, the target copy left behind", moveErr, orphanID)
	}
	if !contains(moveErr.Error(), "mv-leaf-0001") {
		t.Errorf("error %q does not name the source ticket left open", moveErr)
	}
}

// A recursive move is refused as a whole: the flag is still accepted so an
// old invocation fails with the reason rather than as an unknown flag.
func TestMoveRefusesRecursive(t *testing.T) {
	src, targetRepo, targetDir := movePair(t)
	mkMoveTicket(t, src, "mv-epic-0001", ticket.TypeEpic, ticket.StatusBacklog, "")
	mkMoveTicket(t, src, "mv-child-0002", ticket.TypeFeature, ticket.StatusOpen, "mv-epic-0001")

	f := moveCmd.Flags()
	if err := f.Set("recursive", "true"); err != nil {
		t.Fatalf("set recursive: %v", err)
	}
	defer func() { _ = f.Set("recursive", "false") }()

	stdout, _, err := captureMove(t, "mv-epic-0001", targetRepo)
	if err == nil || !contains(err.Error(), "recursive moves are refused") {
		t.Fatalf("runMove = %v, want the recursive refusal", err)
	}
	if contains(stdout, "Moved") {
		t.Errorf("stdout reports a move that did not happen:\n%s", stdout)
	}
	if landed, _ := filepath.Glob(filepath.Join(targetDir, "*.md")); len(landed) != 0 {
		t.Errorf("target holds %v, want nothing", landed)
	}
}

// An unregistered central project is one MultiStore.Create refuses to write to
// and `tk ls` warns about, so a move into it — the one write that lands in
// another repo's store — has to say so too. Only a config entry naming the path
// reaches it: that entry is what makes the directory the project's.
func TestMoveWarnsWhenTheTargetProjectIsUnregistered(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, &project.ProjectConfig{})
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

// With no config entry naming the path nothing owns the directory, and a central
// directory that merely shares its basename does not change that.
func TestMoveRefusesAnUnregisteredDirectoryWithNoConfigEntry(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, true, nil)
	src := registerCentralProject(t, "mv-src", mustGetwd())
	mkMoveTicket(t, src, "mv-stray-0001", ticket.TypeFeature, ticket.StatusReady, "")

	_, _, err := captureMove(t, "mv-stray-0001", strayRepo)
	if err == nil || !contains(err.Error(), "no ticket store found for") {
		t.Fatalf("runMove = %v, want the no-store refusal", err)
	}
	if landed, _ := filepath.Glob(filepath.Join(strayDir, "*.md")); len(landed) != 0 {
		t.Errorf("stray directory holds %v, want nothing", landed)
	}
}

// A config entry names a checkout, not a store: with no central directory behind
// it there is nothing to land in, and none is minted.
func TestMoveRefusesAConfigEntryWithNoCentralDir(t *testing.T) {
	strayDir, strayRepo := unregisteredCentral(t, false, &project.ProjectConfig{})
	src := registerCentralProject(t, "mv-src", mustGetwd())
	mkMoveTicket(t, src, "mv-stray-0001", ticket.TypeFeature, ticket.StatusReady, "")

	_, _, err := captureMove(t, "mv-stray-0001", strayRepo)
	if err == nil || !contains(err.Error(), "no ticket store found for") {
		t.Fatalf("runMove = %v, want the no-store refusal", err)
	}
	if _, err := os.Stat(strayDir); !os.IsNotExist(err) {
		t.Errorf("a store was created at %s: %v", strayDir, err)
	}
}

// A name is a registration, so the bare name of an unregistered project is not
// one even though its path form still reaches the directory.
func TestMoveRefusesTheBareNameOfAnUnregisteredProject(t *testing.T) {
	strayDir, _ := unregisteredCentral(t, true, &project.ProjectConfig{})
	src := registerCentralProject(t, "mv-src", mustGetwd())
	mkMoveTicket(t, src, "mv-stray-0001", ticket.TypeFeature, ticket.StatusReady, "")

	_, _, err := captureMove(t, "mv-stray-0001", "cs-stray")
	if err == nil || !contains(err.Error(), "not a registered project") {
		t.Fatalf("runMove = %v, want the unregistered-name refusal", err)
	}
	if landed, _ := filepath.Glob(filepath.Join(strayDir, "*.md")); len(landed) != 0 {
		t.Errorf("stray directory holds %v, want nothing", landed)
	}
}

// However the target is spelled, a move into the store the ticket is already in
// is refused and nothing is written.
func TestMoveRefusesATargetResolvingToTheSourceProject(t *testing.T) {
	for _, target := range []string{"mv-from", "real path", "."} {
		t.Run(target, func(t *testing.T) {
			f := newMoveFixture(t)
			if target == "real path" {
				target = f.fromRepo
			}
			stdout, stderr, err := captureMove(t, moveProbe, target)
			if err == nil {
				t.Fatal("runMove succeeded, want a refusal")
			}
			for _, want := range []string{"mv-from", "no move performed"} {
				if !contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if contains(stderr, "Move failed partway") {
				t.Errorf("a refusal is not a partial move:\n%s", stderr)
			}
			f.assertNothingMoved(t, stdout)
		})
	}
}

// The target's shape picks the rule. Each of these either used to resolve by a
// fallback — the basename of a path nothing checked, a git remote, the prefix of
// a registered repo — or joined a bare name onto the working directory.
func TestMoveRefusesATargetThatOwnsNoStore(t *testing.T) {
	const noStore = "no ticket store found for"
	const noDir = "no such directory"
	const notRegistered = "not a registered project"
	const initHint = "tk init"
	// The path in the message can carry the project's name too, so these match
	// the phrase that names it.
	const inMvTo = `checkout of project "mv-to"`
	const inMvFrom = `checkout of project "mv-from"`
	cases := []struct {
		name   string
		target func(t *testing.T, f moveFixture) string
		want   []string
		reject []string
	}{
		{
			name:   "missing absolute path sharing a project's basename",
			target: func(*testing.T, moveFixture) string { return "/no/such/place/mv-to" },
			want:   []string{noDir, "/no/such/place/mv-to"},
		},
		{
			name:   "missing relative path",
			target: func(*testing.T, moveFixture) string { return "./mv-to" },
			want:   []string{noDir, "mv-to"},
		},
		{
			name: "real directory sharing a project's basename",
			target: func(t *testing.T, f moveFixture) string {
				return mkdir(t, f.home, "decoy", "mv-to")
			},
			want: []string{noStore, filepath.Join("decoy", "mv-to"), initHint},
		},
		{
			name: "subdirectory of the source repo named for a project",
			target: func(t *testing.T, f moveFixture) string {
				mkdir(t, f.fromRepo, "mv-to")
				return "./mv-to"
			},
			want:   []string{noStore, inMvFrom},
			reject: []string{initHint},
		},
		{
			name: "subdirectory of a registered repo",
			target: func(t *testing.T, f moveFixture) string {
				return mkdir(t, f.toRepo, "sub")
			},
			want:   []string{noStore, inMvTo},
			reject: []string{initHint},
		},
		{
			name: "subdirectory of a registered repo spelled in another case",
			target: func(t *testing.T, f moveFixture) string {
				mkdir(t, f.toRepo, "sub")
				return filepath.Join(caseVariant(t, f), "sub")
			},
			want:   []string{noStore, inMvTo},
			reject: []string{initHint},
		},
		{
			name: "linked worktree of a registered repo",
			target: func(t *testing.T, f moveFixture) string {
				return linkedWorktree(t, f)
			},
			want:   []string{noStore, inMvTo},
			reject: []string{initHint},
		},
		{
			name: "subdirectory of a linked worktree",
			target: func(t *testing.T, f moveFixture) string {
				return mkdir(t, linkedWorktree(t, f), "sub")
			},
			want:   []string{noStore, inMvTo},
			reject: []string{initHint},
		},
		{
			name: "repo whose git remote names a project",
			target: func(t *testing.T, f moveFixture) string {
				repo := mkdir(t, f.home, "elsewhere")
				runGit(t, repo, "init")
				runGit(t, repo, "remote", "add", "origin", "https://example.com/org/mv-to.git")
				return repo
			},
			want: []string{noStore, initHint},
		},
		{
			name:   "parent of the source repo",
			target: func(*testing.T, moveFixture) string { return ".." },
			want:   []string{noStore, initHint},
		},
		{
			name: "file",
			target: func(t *testing.T, f moveFixture) string {
				file := filepath.Join(f.toRepo, "file")
				if err := os.WriteFile(file, nil, 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return file
			},
			want: []string{"is not a directory"},
		},
		{
			name:   "bare name matching no registered project",
			target: func(*testing.T, moveFixture) string { return "mv-none" },
			want:   []string{notRegistered, "mv-none", "./mv-none"},
		},
		{
			name: "bare name of a directory here that is no project",
			target: func(t *testing.T, f moveFixture) string {
				mkdir(t, f.fromRepo, "mv-dir")
				return "mv-dir"
			},
			want: []string{notRegistered, "./mv-dir"},
		},
		{
			name:   "empty target",
			target: func(*testing.T, moveFixture) string { return "" },
			want:   []string{"move target is empty"},
			reject: []string{notRegistered},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFixture(t)
			stdout, _, err := captureMove(t, moveProbe, tc.target(t, f))
			if err == nil {
				t.Fatal("runMove succeeded, want a refusal")
			}
			for _, want := range tc.want {
				if !contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			for _, reject := range tc.reject {
				if contains(err.Error(), reject) {
					t.Errorf("error %q contains %q", err, reject)
				}
			}
			f.assertNothingMoved(t, stdout)
		})
	}
}

// The refusal may only name a project the bare form would accept, so a checkout
// whose project is not registered gets the plain refusal.
func TestMoveDoesNotNameAnUnregisteredEnclosingProject(t *testing.T) {
	_, strayRepo := unregisteredCentral(t, true, &project.ProjectConfig{})
	src := registerCentralProject(t, "mv-src", mustGetwd())
	mkMoveTicket(t, src, "mv-stray-0001", ticket.TypeFeature, ticket.StatusReady, "")

	_, _, err := captureMove(t, "mv-stray-0001", mkdir(t, strayRepo, "sub"))
	if err == nil || !contains(err.Error(), "no ticket store found for") {
		t.Fatalf("runMove = %v, want the no-store refusal", err)
	}
	if contains(err.Error(), "name the project") {
		t.Errorf("error %q points at a project the bare form refuses", err)
	}
}

func TestMoveLandsInTheProjectTheTargetNames(t *testing.T) {
	cases := []struct {
		name   string
		target func(t *testing.T, f moveFixture) string
	}{
		{
			name:   "bare project name from inside another repo",
			target: func(*testing.T, moveFixture) string { return "mv-to" },
		},
		{
			// The ambiguity the shape rule settles: a bare name is never joined
			// onto the working directory.
			name: "bare project name beside a directory of that name",
			target: func(t *testing.T, f moveFixture) string {
				mkdir(t, f.fromRepo, "mv-to")
				return "mv-to"
			},
		},
		{
			name:   "registered repo's real path",
			target: func(_ *testing.T, f moveFixture) string { return f.toRepo },
		},
		{
			name:   "relative path to the registered repo",
			target: func(*testing.T, moveFixture) string { return "../mv-to" },
		},
		{
			name: "symlink to the registered checkout",
			target: func(t *testing.T, f moveFixture) string {
				link := filepath.Join(f.home, "link")
				if err := os.Symlink(f.toRepo, link); err != nil {
					t.Fatalf("Symlink: %v", err)
				}
				return link
			},
		},
		{
			// EvalSymlinks does not fold case, so only stat identity sees this as
			// the registered directory.
			name: "registered repo spelled in another case",
			target: func(t *testing.T, f moveFixture) string {
				return caseVariant(t, f)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMoveFixture(t)
			stdout, _, err := captureMove(t, moveProbe, tc.target(t, f))
			if err != nil {
				t.Fatalf("runMove: %v", err)
			}
			if !contains(stdout, "-> mv-to/") {
				t.Errorf("stdout = %q, want the new ID in mv-to", stdout)
			}
			landed, err := filepath.Glob(filepath.Join(f.dst.Dir, "*.md"))
			if err != nil {
				t.Fatalf("Glob: %v", err)
			}
			if len(landed) != 1 {
				t.Errorf("mv-to holds %v, want the moved ticket", landed)
			}
			orig, err := f.src.Get(moveProbe)
			if err != nil {
				t.Fatalf("Get source: %v", err)
			}
			if orig.Status != ticket.StatusClosed {
				t.Errorf("source status = %q, want %q", orig.Status, ticket.StatusClosed)
			}
		})
	}
}

// Config is a map, so two entries claiming one path would otherwise leave the
// destination to iteration order. The error names both, in a stable order.
func TestMoveRefusesAPathTwoProjectsClaim(t *testing.T) {
	f := newMoveFixture(t)
	registerCentralProject(t, "mv-alias", f.toRepo)

	stdout, _, err := captureMove(t, moveProbe, f.toRepo)
	if err == nil || !contains(err.Error(), "mv-alias, mv-to") {
		t.Fatalf("runMove = %v, want a refusal naming both projects", err)
	}
	f.assertNothingMoved(t, stdout)
}

// A registered project needs neither a checkout on this machine nor, having never
// held a ticket, a directory: a fresh clone of the store arrives without one.
func TestMoveByNameNeedsNoCheckoutOrProjectDir(t *testing.T) {
	f := newMoveFixture(t)
	far := registerCentralProject(t, "mv-far", "")
	if err := os.Remove(far.Dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, _, err := captureMove(t, moveProbe, "mv-far"); err != nil {
		t.Fatalf("runMove: %v", err)
	}
	if landed, _ := filepath.Glob(filepath.Join(far.Dir, "*.md")); len(landed) != 1 {
		t.Errorf("mv-far holds %v, want the moved ticket", landed)
	}
	if landed, _ := filepath.Glob(filepath.Join(f.dst.Dir, "*.md")); len(landed) != 0 {
		t.Errorf("mv-to holds %v, want nothing", landed)
	}
}

// A name reaches the same store guard a path does: a project directory that is
// not one is refused rather than written through.
func TestMoveByNameRefusesASymlinkedProjectDir(t *testing.T) {
	f := newMoveFixture(t)
	outside := t.TempDir()
	if err := os.Remove(f.dst.Dir); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := os.Symlink(outside, f.dst.Dir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	stdout, _, err := captureMove(t, moveProbe, "mv-to")
	if err == nil || !contains(err.Error(), "refusing to write outside the store") {
		t.Fatalf("runMove = %v, want the project-directory refusal", err)
	}
	f.assertNothingMoved(t, stdout)
	if landed, _ := filepath.Glob(filepath.Join(outside, "*.md")); len(landed) != 0 {
		t.Errorf("wrote through the link: %v", landed)
	}
}

// moveProbe is the ticket newMoveFixture leaves open in the source project.
const moveProbe = "mv-probe-0001"

// moveFixture is two registered repos whose directories carry their projects'
// names, so a basename fallback would find a real project, with the test
// standing inside the source repo.
type moveFixture struct {
	home, fromRepo, toRepo string
	src, dst               *ticket.FileStore
}

func newMoveFixture(t *testing.T) moveFixture {
	t.Helper()
	// Canonical, as a real checkout's path is: on macOS the temp dir sits behind a
	// /var symlink, which would hide a prefix match on a path that does not exist.
	home, err := filepath.EvalSymlinks(setupTestHome(t))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	fromRepo := mkdir(t, home, "mv", "mv-from")
	toRepo := mkdir(t, home, "mv", "mv-to")
	cfg := project.Config{
		CentralRoot: filepath.Join(home, "central"),
		Projects: map[string]project.ProjectConfig{
			"mv-from": {Path: fromRepo, Store: "central"},
			"mv-to":   {Path: toRepo, Store: "central"},
		},
	}
	if err := project.Save(cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	f := moveFixture{
		home:     home,
		fromRepo: fromRepo,
		toRepo:   toRepo,
		src:      ticket.NewProjectFileStore(mkdir(t, cfg.CentralRoot, "tickets", "mv-from"), "mv-from"),
		dst:      ticket.NewProjectFileStore(mkdir(t, cfg.CentralRoot, "tickets", "mv-to"), "mv-to"),
	}
	mkMoveTicket(t, f.src, moveProbe, ticket.TypeFeature, ticket.StatusOpen, "")
	t.Chdir(fromRepo)
	return f
}

// assertNothingMoved checks a refused move left both stores as they were.
func (f moveFixture) assertNothingMoved(t *testing.T, stdout string) {
	t.Helper()
	if contains(stdout, "Moved") {
		t.Errorf("stdout reports a move that did not happen:\n%s", stdout)
	}
	orig, err := f.src.Get(moveProbe)
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if orig.Status != ticket.StatusOpen {
		t.Errorf("source status = %q, want %q — nothing was moved", orig.Status, ticket.StatusOpen)
	}
	if landed, _ := filepath.Glob(filepath.Join(f.src.Dir, "*.md")); len(landed) != 1 {
		t.Errorf("source project holds %v, want only the original", landed)
	}
	if landed, _ := filepath.Glob(filepath.Join(f.dst.Dir, "*.md")); len(landed) != 0 {
		t.Errorf("destination project holds %v, want nothing", landed)
	}
}

// caseVariant spells the destination repo in another case, skipping where that
// names a different directory.
func caseVariant(t *testing.T, f moveFixture) string {
	t.Helper()
	variant := filepath.Join(filepath.Dir(f.toRepo), "MV-TO")
	recorded, err := os.Stat(f.toRepo)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	spelled, err := os.Stat(variant)
	if err != nil || !os.SameFile(recorded, spelled) {
		t.Skip("case-sensitive filesystem: the two spellings name different directories")
	}
	return variant
}

// linkedWorktree makes the destination repo a git repo and adds a linked worktree
// outside it, under a directory carrying the project's name.
func linkedWorktree(t *testing.T, f moveFixture) string {
	t.Helper()
	runGit(t, f.toRepo, "init")
	runGit(t, f.toRepo, "config", "user.email", "test@test.com")
	runGit(t, f.toRepo, "config", "user.name", "test")
	runGit(t, f.toRepo, "commit", "--allow-empty", "-m", "root")
	worktree := filepath.Join(f.home, "worktrees", "mv-to")
	runGit(t, f.toRepo, "worktree", "add", worktree)
	return worktree
}

func mkdir(t *testing.T, elem ...string) string {
	t.Helper()
	dir := filepath.Join(elem...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return dir
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
