package tui

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// cwdApp is `tk ui` over ns as cmd/ui.go builds it: execDir over the loaded
// config, and — when dir is not "", i.e. the board was resolved from the
// working directory dir rather than from --project or --repo — the checkout
// offer armed over that same config.
func (f globalFixture) cwdApp(t *testing.T, ns, spawnCommand, dir string) App {
	t.Helper()
	cfg, err := project.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	execDir := func(n string) (string, error) { return project.ExecutionDir(cfg, n) }
	workDir, _ := execDir(ns)
	a := New(f.ticketsDir(ns), ns, "v0", spawnCommand, workDir, false, execDir)
	if dir != "" {
		a = a.OfferCheckout(&cfg, dir)
	}
	return f.reload(t, a)
}

// configBytes is the local and shared config files as they stand on disk.
func (f globalFixture) configBytes(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	local, err := os.ReadFile(filepath.Join(home, ".ticket", "config.yaml"))
	if err != nil {
		t.Fatalf("read local config: %v", err)
	}
	shared, err := os.ReadFile(filepath.Join(f.root, "config.yaml"))
	if err != nil {
		t.Fatalf("read shared config: %v", err)
	}
	return string(local) + "\n---shared---\n" + string(shared)
}

// mainCheckout is a git repository with one commit, canonical, so it compares
// against what RecordableCheckout records.
func mainCheckout(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "-c", "user.email=tk@test", "-c", "user.name=tk", "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}

// waitFor reads path until the spawned command has written it.
func waitFor(t *testing.T, path string) string {
	t.Helper()
	for i := 0; i < 50; i++ {
		if got, _ := os.ReadFile(path); len(got) > 0 {
			return strings.TrimSpace(string(got))
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the spawn never wrote %s", path)
	return ""
}

// AC1: ticket is store: central with no local path; from its main checkout,
// w asks to record it, y writes it to the local config alone and the spawn
// runs there. Keys other than the answers do nothing while the prompt is open.
func TestWorkOffersToRecordTheCheckoutAndSpawnsOnYes(t *testing.T) {
	f := newGlobalFixture(t)
	repo := mainCheckout(t)
	out := filepath.Join(t.TempDir(), "pwd")
	sharedBefore, err := os.ReadFile(filepath.Join(f.root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	a := onTab(t, f.cwdApp(t, "ticket", "pwd -P > "+out, repo), tabInbox, "e-0007")
	before := f.configBytes(t)
	a = press(t, a, "w")
	if a.record == nil {
		t.Fatalf("w opened no record prompt; status = %q", a.status)
	}
	if footer, _ := a.footerView(); !strings.Contains(footer, "ticket has no local path") || !strings.Contains(footer, repo) {
		t.Errorf("footer = %q, want the prompt naming the project and %s", footer, repo)
	}
	a = press(t, a, "x")
	if a.record == nil {
		t.Fatalf("x closed the record prompt")
	}
	if got, err := f.store("ticket").Get("e-0007"); err != nil || got.Status != ticket.StatusReady {
		t.Errorf("x leaked through the prompt: e-0007 = %+v, %v", got, err)
	}
	if after := f.configBytes(t); after != before {
		t.Fatalf("the config changed before the answer:\n%s", after)
	}

	a = press(t, a, "y")
	if a.record != nil {
		t.Errorf("y left the prompt open")
	}
	if strings.Contains(a.status, "refusing") {
		t.Fatalf("spawn refused after recording: %s", a.status)
	}
	if got := waitFor(t, out); got != repo {
		t.Errorf("spawn ran in %q, want the recorded checkout %q", got, repo)
	}
	cfg, err := project.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Projects["ticket"].Path; got != repo {
		t.Errorf("ticket's local path = %q, want %q", got, repo)
	}
	if got := cfg.Projects["warp"].Path; got != f.warpRepo {
		t.Errorf("warp's local path = %q, want it kept as %q", got, f.warpRepo)
	}
	sharedAfter, err := os.ReadFile(filepath.Join(f.root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sharedAfter) != string(sharedBefore) {
		t.Errorf("the shared config changed:\n%s", sharedAfter)
	}

	// The recorded path holds for the next spawn: no second prompt.
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	a = press(t, a, "w")
	if a.record != nil {
		t.Errorf("w prompted again after the path was recorded")
	}
	if got := waitFor(t, out); got != repo {
		t.Errorf("second spawn ran in %q, want %q", got, repo)
	}
}

// AC1: n and esc write nothing and leave the spawn refused — from the list,
// and from a detail, where the prompt renders in the overlay frame.
func TestWorkRecordPromptDeclinedWritesNothing(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		for _, view := range []string{"list", "detail"} {
			fromDetail := view == "detail"
			t.Run(key+" from the "+view, func(t *testing.T) {
				f := newGlobalFixture(t)
				repo := mainCheckout(t)
				marker := filepath.Join(t.TempDir(), "spawned")
				a := onTab(t, f.cwdApp(t, "ticket", "touch "+marker, repo), tabInbox, "e-0007")
				if fromDetail {
					a = press(t, a, "enter")
					if a.overlay != overlayDetail {
						t.Fatalf("enter opened overlay %v, want e-0007's detail", a.overlay)
					}
				}
				before := f.configBytes(t)
				a = press(t, a, "w")
				if a.record == nil {
					t.Fatalf("w opened no record prompt; status = %q", a.status)
				}
				if fromDetail && !strings.Contains(a.View(), "ticket has no local path") {
					t.Errorf("the detail frame does not show the prompt:\n%s", a.View())
				}
				a = press(t, a, key)
				if a.record != nil {
					t.Errorf("%s left the prompt open", key)
				}
				assertRefused(t, a, marker, `project "ticket" has no local path in ~/.ticket/config.yaml`)
				if after := f.configBytes(t); after != before {
					t.Errorf("%s wrote the config:\n%s", key, after)
				}
			})
		}
	}
}

// AC2: everywhere the offer's conditions do not all hold, w refuses with no
// prompt and the config is untouched.
func TestWorkDoesNotOfferToRecordOutsideItsConditions(t *testing.T) {
	type setup struct {
		dir    string // the working directory offered; "" for --project/--repo
		cfg    func(project.Config) project.Config
		pick   func(*testing.T, App) App
		reason string
	}
	onE0007 := func(t *testing.T, a App) App { return onTab(t, a, tabInbox, "e-0007") }
	// From e-0007's parent epic detail, the child qid of another namespace.
	throughEpic := func(qid string) func(*testing.T, App) App {
		return func(t *testing.T, a App) App {
			a = onE0007(t, a)
			a = press(t, a, "u")
			a = press(t, a, "enter")
			return pickChild(t, a, qid)
		}
	}
	pathless := `project "ticket" has no local path in ~/.ticket/config.yaml`

	cases := map[string]func(t *testing.T, repo string) setup{
		"--project or --repo": func(t *testing.T, repo string) setup {
			return setup{dir: "", pick: onE0007, reason: pathless}
		},
		"subdirectory": func(t *testing.T, repo string) setup {
			sub := filepath.Join(repo, "sub")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			return setup{dir: sub, pick: onE0007, reason: pathless}
		},
		"linked worktree": func(t *testing.T, repo string) setup {
			wt := filepath.Join(t.TempDir(), "wt")
			git(t, repo, "worktree", "add", "-q", wt)
			return setup{dir: wt, pick: onE0007, reason: pathless}
		},
		"foreign child": func(t *testing.T, repo string) setup {
			// warp pathless too, so the foreign child is itself a project the
			// offer would cover from its own board.
			return setup{dir: repo, cfg: func(c project.Config) project.Config {
				c.Projects["warp"] = project.ProjectConfig{Store: "central"}
				return c
			}, pick: throughEpic("warp/a-0002"), reason: `project "warp" has no local path`}
		},
		"Root ticket": func(t *testing.T, repo string) setup {
			return setup{dir: repo, pick: throughEpic("_root/idea-0006"), reason: "Root has no repository"}
		},
		"not store: central": func(t *testing.T, repo string) setup {
			return setup{dir: repo, cfg: func(c project.Config) project.Config {
				c.Projects["ticket"] = project.ProjectConfig{}
				return c
			}, pick: onE0007, reason: `project "ticket" has no checkout registered on this machine`}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGlobalFixture(t)
			repo := mainCheckout(t)
			s := build(t, repo)
			if s.cfg != nil {
				cfg := f.cfg
				cfg.Projects = maps.Clone(f.cfg.Projects)
				if err := project.Save(s.cfg(cfg)); err != nil {
					t.Fatalf("Save: %v", err)
				}
			}
			marker := filepath.Join(t.TempDir(), "spawned")
			a := s.pick(t, f.cwdApp(t, "ticket", "touch "+marker, s.dir))
			before := f.configBytes(t)
			a = press(t, a, "w")
			if a.record != nil {
				t.Fatalf("w offered to record %s", a.record.dir)
			}
			assertRefused(t, a, marker, s.reason)
			if after := f.configBytes(t); after != before {
				t.Errorf("the config changed:\n%s", after)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("the spawn command ran: %s exists", marker)
			}
		})
	}
}
