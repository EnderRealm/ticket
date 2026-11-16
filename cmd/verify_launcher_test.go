package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/pflag"
)

// setLauncher parses words as --launcher flags, the way a command line would.
// The cleanup empties the array and clears Changed: restoring DefValue through
// Set would append the literal "[]" to a string array.
func setLauncher(t *testing.T, args ...string) {
	t.Helper()
	f := verifyCmd.Flags().Lookup("launcher")
	if f == nil {
		t.Fatal("no --launcher flag")
	}
	t.Cleanup(func() {
		f.Value.(pflag.SliceValue).Replace(nil)
		f.Changed = false
	})
	if err := verifyCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
}

// launcherArgs is a /bin/sh launcher that records the argv it is handed in
// argvFile and then execs it. Its words include -c, so the space-separated
// form is shown to take a dash-led value.
func launcherArgs(argvFile string) (args, words []string) {
	words = []string{"/bin/sh", "-c", `printf '%s\n' "$@" > "$0"; exec "$@"`, argvFile}
	args = []string{"--launcher", words[0], "--launcher", words[1], "--launcher=" + words[2], "--launcher", words[3]}
	return args, words
}

const launcherBody = "Description.\n\n## Acceptance Criteria\n\n- Echoes.\n  verify: /bin/echo launched\n"

func TestVerifyLauncherPrefixesTheCommand(t *testing.T) {
	verifyStore(t, "vf-launch", launcherBody)
	argvFile := filepath.Join(t.TempDir(), "argv")
	args, words := launcherArgs(argvFile)

	t.Run("run", func(t *testing.T) {
		setLauncher(t, args...)
		got, code := verifyCriterionJSON(t, "vf-launch", 1, false)
		if got.Status != string(ticket.VerifyPass) || code != 0 || got.Output != "launched" {
			t.Fatalf("run = %+v exit %d, want the command to pass through the launcher", got, code)
		}
		handed, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatalf("the launcher did not run: %v", err)
		}
		if want := "/bin/echo\nlaunched\n"; string(handed) != want {
			t.Errorf("launcher was handed %q, want %q", handed, want)
		}
	})

	t.Run("check", func(t *testing.T) {
		setLauncher(t, args...)
		got, code := verifyCriterionJSON(t, "vf-launch", 1, true)
		want := append(append([]string{}, words...), "/bin/echo", "launched")
		if got.Status != string(ticket.VerifyRunnable) || code != 0 || !reflect.DeepEqual(got.Argv, want) {
			t.Errorf("--check = %+v exit %d, want runnable with argv %q", got, code, want)
		}

		setCriterion(t, 1)
		setVerifyFlag(t, "check", "true")
		out, err := captureVerify(t, "vf-launch")
		if err != nil {
			t.Fatalf("--check: %v", err)
		}
		if line := fmt.Sprintf("  argv: %q\n", want); !strings.Contains(out, line) {
			t.Errorf("--check text output missing %q:\n%s", line, out)
		}
	})

	t.Run("no launcher", func(t *testing.T) {
		got, _ := verifyCriterionJSON(t, "vf-launch", 1, true)
		if want := []string{"/bin/echo", "launched"}; !reflect.DeepEqual(got.Argv, want) {
			t.Errorf("--check argv without --launcher = %q, want %q", got.Argv, want)
		}
	})
}

func TestVerifyLauncherRejectsAnEmptyWord(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "sentinel.txt")
	store := verifyStore(t, "vf-launch-empty", sentinelBody(sentinel))

	setLauncher(t, "--launcher", "/bin/sh", "--launcher", "")
	_, err := captureVerify(t, "vf-launch-empty")
	assertVerifyRefusedBeforeRunning(t, store, "vf-launch-empty", sentinel, err, "--launcher")
}

// A launcher comes from the command line alone: a ticket field naming one is
// ignored, and the command runs as its own argv. The field's launcher would
// fail the run, so a pass shows it was not applied.
func TestVerifyLauncherIsNotReadFromTheTicket(t *testing.T) {
	store := verifyStore(t, "vf-launch-plain", "Description.\n")
	if err := store.Create(&ticket.Ticket{
		ID:      "vf-launch-field",
		Status:  ticket.StatusOpen,
		Type:    ticket.TypeFeature,
		Created: time.Now(),
		Title:   "Names a launcher",
		Body:    launcherBody,
		Extra:   map[string]string{"launcher": "/bin/sh -c 'exit 1'"},
	}); err != nil {
		t.Fatal(err)
	}

	got, code := verifyCriterionJSON(t, "vf-launch-field", 1, false)
	if got.Status != string(ticket.VerifyPass) || code != 0 {
		t.Fatalf("run = %+v exit %d, want a pass", got, code)
	}
	check, _ := verifyCriterionJSON(t, "vf-launch-field", 1, true)
	if want := []string{"/bin/echo", "launched"}; !reflect.DeepEqual(check.Argv, want) {
		t.Errorf("--check argv = %q, want the command's own %q", check.Argv, want)
	}
}

func TestHelpDocumentsVerifyLauncher(t *testing.T) {
	help := renderHelp(t, verifyCmd)
	for _, want := range []string{"--launcher stringArray", "verify_allow", "SIGTERM", "--check"} {
		if !strings.Contains(help, want) {
			t.Errorf("verify help missing %q:\n%s", want, help)
		}
	}
}
