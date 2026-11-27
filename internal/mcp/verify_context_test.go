package mcp_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
)

// noisyFailure prints 2000 numbered lines, far past the 4KB the report keeps,
// then fails.
const noisyFailure = "- Noisy failure.\n  verify: /bin/sh -c 'i=0; while [ $i -lt 2000 ]; do echo \"line $i\"; i=$((i+1)); done; exit 3'\n- Passes.\n  verify: /bin/echo ok\n"

func noisyOutput() string {
	var b strings.Builder
	for i := range 2000 {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

// ticket_verify reports what its commands ran under and keeps a failed
// criterion's whole output in a private file the report names, which goes
// away with the session that owns the job.
func TestVerifyReportsContextAndKeepsFullOutput(t *testing.T) {
	session, _ := verifyServer(t)
	report := verifyReport(t, session, noisyFailure)

	vc := report.Context
	if vc == nil {
		t.Fatal("report has no context")
	}
	if vc.Error != "" {
		t.Fatalf("probe error: %s", vc.Error)
	}
	var nofile *ticket.VerifyLimit
	for i := range vc.Limits {
		if vc.Limits[i].Name == "nofile" {
			nofile = &vc.Limits[i]
		}
	}
	if nofile == nil || nofile.Soft == "" || nofile.Hard == "" {
		t.Errorf("limits = %+v, want nofile with soft and hard", vc.Limits)
	}
	if len(vc.Parents) == 0 || vc.Parents[0].PID != os.Getpid() {
		t.Errorf("parents = %+v, want the chain to open at the server, pid %d", vc.Parents, os.Getpid())
	}
	if vc.ControllingTerminal == nil {
		t.Error("controlling_terminal not reported")
	}

	failed, passed := report.Results[0], report.Results[1]
	if failed.Status != string(ticket.VerifyFail) || len(failed.Output) > 4200 {
		t.Fatalf("failed result = %s with %d bytes of output, want a capped failure", failed.Status, len(failed.Output))
	}
	if failed.OutputFile == "" {
		t.Fatalf("failed result names no output_file: %+v", failed)
	}
	got, err := os.ReadFile(failed.OutputFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != noisyOutput() {
		t.Errorf("output_file holds %d bytes, want the whole %d-byte output", len(got), len(noisyOutput()))
	}
	for path, want := range map[string]os.FileMode{failed.OutputFile: 0o600, filepath.Dir(failed.OutputFile): 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, want %v", path, info.Mode().Perm(), want)
		}
	}
	if passed.OutputFile != "" {
		t.Errorf("passing result names output_file %q", passed.OutputFile)
	}

	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Dir(failed.OutputFile)); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("output directory outlived the session that owned the job")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// No command ran, so the report carries no context and no file.
func TestVerifyReportHasNoContextWhenNothingRuns(t *testing.T) {
	session, _ := verifyServer(t)
	report := verifyReport(t, session, "- Judged.\n- Looked at.\n  test: the screen\n")
	if report.Context != nil {
		t.Errorf("context = %+v, want none", report.Context)
	}
}
