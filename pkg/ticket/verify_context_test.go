package ticket

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// limit returns the named limit from a context, failing the test without it.
func limit(t *testing.T, vc *VerifyContext, name string) VerifyLimit {
	t.Helper()
	for _, l := range vc.Limits {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("context has no %s limit: %+v", name, vc.Limits)
	return VerifyLimit{}
}

// The context is measured in a child launched as the commands are, so its
// open-file limit is the one a command sees — not the raised soft limit the
// Go runtime gives tk itself.
func TestRunVerifyReportsExecutionContext(t *testing.T) {
	dir := t.TempDir()
	criteria := []Criterion{{Text: "reports nofile", Command: "/bin/sh -c 'ulimit -S -n; ulimit -H -n'"}}
	results, vc, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow}, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if vc == nil {
		t.Fatal("no context for a run whose command ran")
	}
	if vc.Error != "" {
		t.Fatalf("probe error: %s", vc.Error)
	}
	seen := strings.Fields(results[0].Output)
	if len(seen) != 2 {
		t.Fatalf("command output = %q, want soft and hard nofile", results[0].Output)
	}
	if got := limit(t, vc, "nofile"); got.Soft != seen[0] || got.Hard != seen[1] {
		t.Errorf("context nofile = %+v, want what the command saw: soft %s hard %s", got, seen[0], seen[1])
	}
	// nproc included: bash takes -u and dash, Linux's /bin/sh, takes -p.
	for _, name := range []string{"core", "data", "fsize", "stack", "cpu", "nproc"} {
		if l := limit(t, vc, name); l.Soft == "" || l.Hard == "" || l.Soft == "unknown" || l.Hard == "unknown" {
			t.Errorf("limit %s = %+v, want soft and hard", name, l)
		}
	}
	if len(vc.Parents) == 0 || vc.Parents[0].PID != os.Getpid() || vc.Parents[0].Command == "" {
		t.Errorf("parents = %+v, want the chain to open at this process, pid %d", vc.Parents, os.Getpid())
	}
	if vc.ControllingTerminal == nil {
		t.Error("controlling_terminal not reported")
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if vc.Dir != want {
		t.Errorf("context dir = %q, want %q", vc.Dir, want)
	}

	report := NewVerifyReport("alpha/tk-1", VerifyProvenance{Dir: dir}, results)
	if report.Context != nil {
		t.Error("NewVerifyReport set a context; the caller attaches the run's")
	}
}

// A launcher can change the limits a command runs under, so the probe goes
// through it as the commands do.
func TestRunVerifyContextIsMeasuredThroughTheLauncher(t *testing.T) {
	launcher := []string{"/bin/sh", "-c", `ulimit -S -n 77; exec "$@"`, "launcher"}
	criteria := []Criterion{{Text: "nofile", Command: "/bin/sh -c 'ulimit -S -n'"}}
	results, vc, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow}, launcher)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Output != "77" {
		t.Fatalf("command saw nofile %q, want the launcher's 77", results[0].Output)
	}
	if vc == nil || limit(t, vc, "nofile").Soft != "77" {
		t.Errorf("context = %+v, want the launcher's soft nofile 77", vc)
	}
}

// Nothing ran, so there is nothing to describe and the probe is not launched.
func TestRunVerifyNoContextWhenNoCommandRuns(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "launched")
	launcher := []string{"/bin/sh", "-c", `touch "$0"; exec "$@"`, marker}
	criteria := []Criterion{
		{Text: "refused", Command: "/usr/bin/true"},
		{Text: "judged"},
		{Text: "waiting", Test: true, TestText: "look", Command: "/bin/echo never"},
	}
	_, vc, err := RunVerify(context.Background(), criteria, dir, VerifyPolicy{Allow: testAllow}, launcher)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if vc != nil {
		t.Errorf("context = %+v, want none when no command ran", vc)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the probe was launched for a run that ran nothing")
	}
}

// A probe that cannot run is reported on the context, and the commands still
// run and are graded on their own.
func TestRunVerifyProbeFailureDoesNotFailTheRun(t *testing.T) {
	// The launcher runs the criterion's program and nothing else, so the probe
	// it is handed exits 3.
	launcher := []string{"/bin/sh", "-c", `if [ "$1" = -c ]; then echo nope; exit 3; fi; exec "$0" "$@"`}
	criteria := []Criterion{{Text: "echoes", Command: "/bin/echo ok"}}
	results, vc, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow}, launcher)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	if results[0].Status != VerifyPass || results[0].Output != "ok" {
		t.Errorf("result = %+v, want the command to pass", results[0])
	}
	if vc == nil || !strings.Contains(vc.Error, "exit status 3") || !strings.Contains(vc.Error, "nope") {
		t.Errorf("context = %+v, want the probe's failure and its output in error", vc)
	}
}

// The capped Output keeps the tail; the whole output of a failure is kept
// beside it, and a pass keeps none.
func TestRunVerifyKeepsFullOutputOfAFailure(t *testing.T) {
	criteria := []Criterion{
		{Text: "noisy failure", Command: `/bin/sh -c 'i=0; while [ $i -lt 1000 ]; do echo "line $i"; i=$((i+1)); done; exit 3'`},
		{Text: "pass", Command: "/bin/echo ok"},
	}
	results, _, err := RunVerify(context.Background(), criteria, t.TempDir(), VerifyPolicy{Allow: testAllow}, nil)
	if err != nil {
		t.Fatalf("RunVerify: %v", err)
	}
	var want strings.Builder
	for i := range 1000 {
		want.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	if results[0].Status != VerifyFail || results[0].FullOutput != want.String() {
		t.Errorf("full output = %d bytes, want the whole %d", len(results[0].FullOutput), want.Len())
	}
	if !strings.HasPrefix(results[0].Output, "... ") || !strings.HasSuffix(results[0].Output, "line 999") {
		t.Errorf("output = %q, want the capped tail", results[0].Output)
	}
	if results[1].FullOutput != "" {
		t.Errorf("pass kept full output %q", results[1].FullOutput)
	}
}
