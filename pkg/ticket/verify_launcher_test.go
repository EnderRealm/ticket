package ticket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// standInLauncherEnv makes the test binary, re-execed, act as standInLauncher
// instead of running tests; TestMain checks it.
const standInLauncherEnv = "TK_TEST_LAUNCHER"

// standInLauncher plays sudo with no shell job control, which /bin/sh offers
// portably only with a controlling terminal: it starts argv in a process group
// of its own, writes its pid and the child's to pidFile suffixed with the
// child's program name, relays SIGTERM to the child and waits for it. The
// suffix keeps the context probe, launched through it before the command, from
// standing in for the command.
func standInLauncher(pidFile string, argv []string) int {
	pidFile += "." + filepath.Base(argv[0])
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Renamed into place so a reader never sees half the pids.
	tmp := pidFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d %d\n", os.Getpid(), cmd.Process.Pid)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else if err := os.Rename(tmp, pidFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	go func() {
		<-term
		cmd.Process.Signal(syscall.SIGTERM)
	}()
	cmd.Wait()
	return 0
}

// writeLauncher writes a stand-in launcher script into a temp dir and returns
// its path. The path is in no allow-list, so a run through it also shows the
// prefix is never screened.
func writeLauncher(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "launcher")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// waitGone polls until pid no longer exists. A killed child is a zombie until
// whoever inherits it reaps it, so a single kill(pid, 0) right after the run
// could still see it.
func waitGone(t *testing.T, pid int, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// readPids reads the whitespace-separated pids a stand-in launcher wrote,
// waiting for the file to be complete.
func readPids(t *testing.T, path string, n int) []int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if fields := strings.Fields(string(data)); err == nil && len(fields) == n {
			pids := make([]int, n)
			for i, f := range fields {
				if pids[i], err = strconv.Atoi(f); err != nil {
					t.Fatalf("pid file %q: %v", data, err)
				}
			}
			return pids
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("launcher never wrote %d pids to %s", n, path)
	return nil
}

func TestRunVerifyExecsThroughLauncher(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv")
	launcher := writeLauncher(t, `out=$1; shift; printf '%s\n' "$@" > "$out"; exec "$@"`+"\n")

	criteria := []Criterion{{Text: "echoes", Command: "/bin/echo 'two words' ok"}}
	results, _, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow}, []string{launcher, argvFile})
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "two words ok" {
		t.Fatalf("result = %+v, want the command run through the launcher to pass", results[0])
	}
	got, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("launcher did not run: %v", err)
	}
	if want := "/bin/echo\ntwo words\nok\n"; string(got) != want {
		t.Errorf("launcher was handed %q, want the command's own argv %q", got, want)
	}

	// CheckVerify reports the argv a run with the same launcher execs.
	check := CheckVerify(criteria, VerifyPolicy{Allow: testAllow}, []string{launcher, argvFile})
	if want := []string{launcher, argvFile, "/bin/echo", "two words", "ok"}; !reflect.DeepEqual(check[0].Argv, want) {
		t.Errorf("check argv = %q, want %q", check[0].Argv, want)
	}
	if plain := CheckVerify(criteria, VerifyPolicy{Allow: testAllow}, nil); !reflect.DeepEqual(plain[0].Argv, []string{"/bin/echo", "two words", "ok"}) {
		t.Errorf("check argv without a launcher = %q, want the command's argv unchanged", plain[0].Argv)
	}
}

// A launcher wraps an allowed command; it never makes a refused one run, even
// when the launcher's own program is on the allow-list.
func TestRunVerifyLauncherDoesNotWidenAllowList(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	launcher := []string{"/bin/sh", "-c", `touch "$0"; exec "$@"`, marker}
	criteria := []Criterion{{Text: "not allowed", Command: "/usr/bin/true"}}

	bare, _, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow}, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	launched, _, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow}, launcher)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if launched[0].Status != VerifyRefused || !reflect.DeepEqual(launched[0], bare[0]) {
		t.Errorf("with a launcher = %+v\nwithout         = %+v\nwant the identical refusal", launched[0], bare[0])
	}
	if !strings.Contains(launched[0].Output, `"/usr/bin/true" is not in verify_allow`) {
		t.Errorf("refusal = %q, want it to name the criterion's program, not the launcher's", launched[0].Output)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the launcher ran for a refused command")
	}
	if check := CheckVerify(criteria, VerifyPolicy{Allow: testAllow}, launcher); !reflect.DeepEqual(check[0], bare[0]) {
		t.Errorf("check with a launcher = %+v, want the run's refusal %+v", check[0], bare[0])
	}
}

// The stand-in launcher plays sudo: it runs the command in a process group of
// its own, so tk's group signal cannot reach the command directly — the way
// it cannot reach a process owned by the agent uid — and relays SIGTERM to it.
// Under a SIGKILL-only cancel the launcher dies and the command outlives the
// run; it must be gone instead.
func TestRunVerifyTimeoutUnderLauncherReachesTheCommand(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(standInLauncherEnv, "1")

	type run struct {
		results []VerifyResult
		err     error
	}
	done := make(chan run, 1)
	go func() {
		policy := VerifyPolicy{Allow: []string{"/bin/sleep"}, Timeout: time.Second}
		results, _, err := RunVerify(context.Background(), []Criterion{{Text: "hangs", Command: "/bin/sleep 30"}}, t.TempDir(), policy, []string{exe, pidFile})
		done <- run{results, err}
	}()

	pids := readPids(t, pidFile+".sleep", 2)
	launcherPid, child := pids[0], pids[1]
	t.Cleanup(func() { syscall.Kill(child, syscall.SIGKILL) })
	// Without this the test would pass on a launcher whose child shares its
	// group, which a SIGKILL to the group reaches with no relay at all.
	if pgid, err := syscall.Getpgid(child); err != nil || pgid == launcherPid {
		t.Fatalf("child pgid = %d (%v), want a group other than the launcher's %d", pgid, err, launcherPid)
	}

	r := <-done
	if r.err != nil {
		t.Fatalf("RunVerify: %v", r.err)
	}
	if r.results[0].Status != VerifyFail || !strings.Contains(r.results[0].Output, "timed out after 1s") {
		t.Errorf("result = %+v, want a timeout failure", r.results[0])
	}
	if !waitGone(t, child, 5*time.Second) {
		t.Errorf("command pid %d outlived the timeout under a launcher", child)
	}
}

// A launcher that ignores SIGTERM, with a command that inherits the ignore, is
// killed by the SIGKILL that follows the grace period — group-wide, so the
// command goes with it, and before exec's own WaitDelay would give up on it.
func TestRunVerifyTimeoutUnderLauncherKillsAfterGrace(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	launcher := writeLauncher(t, `pids=$1; shift
trap '' TERM
"$@" &
echo "$!" > "$pids"
wait
`)

	timeout := 200 * time.Millisecond
	start := time.Now()
	policy := VerifyPolicy{Allow: []string{"/bin/sleep"}, Timeout: timeout}
	results, _, err := RunVerify(context.Background(), []Criterion{{Text: "hangs", Command: "/bin/sleep 30"}}, t.TempDir(), policy, []string{launcher, pidFile})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	child := readPids(t, pidFile, 1)[0]
	t.Cleanup(func() { syscall.Kill(child, syscall.SIGKILL) })

	if results[0].Status != VerifyFail {
		t.Errorf("status = %q, want fail", results[0].Status)
	}
	if elapsed < timeout+launcherKillGrace {
		t.Errorf("run ended after %s, before the grace period: SIGTERM should not have stopped it", elapsed)
	}
	if elapsed >= timeout+verifyWaitDelay {
		t.Errorf("run ended after %s, at exec's WaitDelay rather than at tk's SIGKILL", elapsed)
	}
	if !waitGone(t, child, 5*time.Second) {
		t.Errorf("command pid %d outlived the SIGKILL after the grace period", child)
	}
}
