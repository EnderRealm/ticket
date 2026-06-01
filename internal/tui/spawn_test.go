package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSpawnCommandSubstitutes(t *testing.T) {
	got := buildSpawnCommand("foo {id} {dir}", "/some/dir", "project/tk-x")
	want := "foo project/tk-x /some/dir"
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestBuildSpawnCommandDefault(t *testing.T) {
	got := buildSpawnCommand("", "/some/dir", "project/tk-x")
	for _, want := range []string{"/some/dir", "project/tk-x", "iTerm", "claude"} {
		if !strings.Contains(got, want) {
			t.Errorf("default spawn command %q missing %q", got, want)
		}
	}
}

func TestBuildSpawnCommandDefaultWhenWhitespace(t *testing.T) {
	got := buildSpawnCommand("   ", "/some/dir", "project/tk-x")
	if !strings.Contains(got, "iTerm") {
		t.Errorf("whitespace template should fall back to default, got %q", got)
	}
}

// TestBuildSpawnCommandDefaultQuotesPathWithSpaces guards the nested-quoting
// fix: the default iTerm template must produce a syntactically valid sh command
// even when the project path contains spaces (common on macOS). `sh -n` parses
// without executing.
func TestBuildSpawnCommandDefaultQuotesPathWithSpaces(t *testing.T) {
	cmd := buildSpawnCommand("", "/Users/me/My Project", "project/tk-x")
	check := exec.Command("sh", "-n")
	check.Stdin = strings.NewReader(cmd)
	if out, err := check.CombinedOutput(); err != nil {
		t.Errorf("default spawn command is not valid shell syntax: %v\n%s\ncmd: %s", err, out, cmd)
	}
	// The path must be single-quoted at the shell layer iTerm runs.
	if !strings.Contains(cmd, `'/Users/me/My Project'`) {
		t.Errorf("expected the dir to be single-quoted, got: %s", cmd)
	}
}

func TestNewWorkDirIsParentOfTicketsDir(t *testing.T) {
	dir := t.TempDir()
	ticketsDir := filepath.Join(dir, ".tickets")
	a := New(ticketsDir, "v0", "")
	absDir, _ := filepath.Abs(ticketsDir)
	if want := filepath.Dir(absDir); a.workDir != want {
		t.Errorf("workDir = %q, want %q", a.workDir, want)
	}
}
