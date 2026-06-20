package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSpawnCommandSubstitutes(t *testing.T) {
	got := buildSpawnCommand("foo {id} {dir}", "/some/dir", "project/tk-x", "project", "Title")
	want := "foo project/tk-x /some/dir"
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestBuildSpawnCommandSubstitutesProjectAndTitle(t *testing.T) {
	got := buildSpawnCommand("{project}: {title}", "/some/dir", "project/tk-x", "myproj", "My Title")
	want := "myproj: My Title"
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestBuildSpawnCommandDefault(t *testing.T) {
	got := buildSpawnCommand("", "/some/dir", "project/tk-x", "project", "Title")
	// "create window"/"write text" guard against the regression where the
	// iTerm `command "..."` form exec'd the string without a shell, so the
	// && pipeline never ran and the window closed instantly. write text runs
	// the command in the window's live interactive shell.
	for _, want := range []string{"/some/dir", "project/tk-x", "iTerm", "claude", "create window", "write text", "set name"} {
		if !strings.Contains(got, want) {
			t.Errorf("default spawn command %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "profile command") {
		t.Errorf("default must not use the inline `command` form (closes the window); got: %s", got)
	}
}

func TestBuildSpawnCommandDefaultWhenWhitespace(t *testing.T) {
	got := buildSpawnCommand("   ", "/some/dir", "project/tk-x", "project", "Title")
	if !strings.Contains(got, "iTerm") {
		t.Errorf("whitespace template should fall back to default, got %q", got)
	}
}

func TestSpawnWindowTitle(t *testing.T) {
	got := spawnWindowTitle("ticket", "ticket/tk-ui-set-a6d2", "Some title here")
	want := "ticket -- tk-ui-set-a6d2 -- Some title here"
	if got != want {
		t.Errorf("spawnWindowTitle = %q, want %q", got, want)
	}
}

func TestSpawnWindowTitleTruncatesTo20Runes(t *testing.T) {
	got := spawnWindowTitle("ticket", "ticket/tk-x", "0123456789abcdefghijKLMNOP")
	want := "ticket -- tk-x -- 0123456789abcdefghij"
	if got != want {
		t.Errorf("spawnWindowTitle = %q, want %q", got, want)
	}
}

func TestSpawnWindowTitleSanitizesQuotes(t *testing.T) {
	got := spawnWindowTitle("proj", "proj/tk-x", `a'b"c\d`)
	if strings.ContainsAny(got, "'\"\\") {
		t.Errorf("spawnWindowTitle %q must not contain ' \" or \\", got)
	}
}

func TestBuildSpawnCommandDefaultQuotesTitleWithSingleQuote(t *testing.T) {
	// Regression: a ticket title that starts with a single quote must not
	// break the osascript/AppleScript quoting layers in the default template.
	cmd := buildSpawnCommand("", "/some/dir", "ticket/tk-ui-set-a6d2", "ticket", "'tk ui' set title of iterm window")
	check := exec.Command("sh", "-n")
	check.Stdin = strings.NewReader(cmd)
	if out, err := check.CombinedOutput(); err != nil {
		t.Errorf("default spawn command is not valid shell syntax: %v\n%s\ncmd: %s", err, out, cmd)
	}
	if !strings.Contains(cmd, "set name") {
		t.Errorf("default spawn command should set the window name, got: %s", cmd)
	}
}

// TestBuildSpawnCommandDefaultQuotesPathWithSpaces guards the nested-quoting
// fix: the default iTerm template must produce a syntactically valid sh command
// even when the project path contains spaces (common on macOS). `sh -n` parses
// without executing.
func TestBuildSpawnCommandDefaultQuotesPathWithSpaces(t *testing.T) {
	cmd := buildSpawnCommand("", "/Users/me/My Project", "project/tk-x", "project", "Title")
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

func TestNewStoresWorkDir(t *testing.T) {
	// New stores the workDir resolved by the caller; spawn uses it verbatim.
	a := New(filepath.Join(t.TempDir(), ".tickets"), "v0", "", "/repo/root")
	if a.workDir != "/repo/root" {
		t.Errorf("workDir = %q, want /repo/root", a.workDir)
	}
}
