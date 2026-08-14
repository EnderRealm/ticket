package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
)

// mustBuildSpawnCommand builds the spawn command for an id the ID gate is
// expected to accept.
func mustBuildSpawnCommand(t *testing.T, template, dir, id, project, title string) string {
	t.Helper()
	cmd, err := buildSpawnCommand(template, dir, id, project, title)
	if err != nil {
		t.Fatalf("buildSpawnCommand(%q): %v", id, err)
	}
	return cmd
}

func TestBuildSpawnCommandSubstitutes(t *testing.T) {
	got := mustBuildSpawnCommand(t, "foo {id} {dir}", "/some/dir", "project/tk-x", "project", "Title")
	want := "foo project/tk-x /some/dir"
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestBuildSpawnCommandSubstitutesProjectAndTitle(t *testing.T) {
	got := mustBuildSpawnCommand(t, "{project}: {title}", "/some/dir", "project/tk-x", "myproj", "My Title")
	want := "myproj: My Title"
	if got != want {
		t.Errorf("buildSpawnCommand = %q, want %q", got, want)
	}
}

func TestBuildSpawnCommandDefault(t *testing.T) {
	got := mustBuildSpawnCommand(t, "", "/some/dir", "project/tk-x", "project", "Title")
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

func TestBuildSpawnCommandDefaultMakesTitleStick(t *testing.T) {
	// The AppleScript `set name` is clobbered by the shell's preexec hook and by
	// claude's own title updates. The default must reassert the title from inside
	// the shell (printf OSC-0, after preexec) and disable claude's title updater.
	// The disable var must be `export`ed, not prefixed inline: `claude` is often
	// an alias (e.g. `tabset ...; command claude`), and an inline `VAR=1 claude`
	// prefix would bind to the alias's first command instead of claude.
	got := mustBuildSpawnCommand(t, "", "/some/dir", "project/tk-x", "project", "Title")
	for _, want := range []string{`printf`, `]0;`, `%s`, "export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("default spawn command must make the title stick, missing %q; got: %s", want, got)
		}
	}
}

func TestBuildSpawnCommandDefaultWhenWhitespace(t *testing.T) {
	got := mustBuildSpawnCommand(t, "   ", "/some/dir", "project/tk-x", "project", "Title")
	if !strings.Contains(got, "iTerm") {
		t.Errorf("whitespace template should fall back to default, got %q", got)
	}
}

func TestSpawnWindowTitle(t *testing.T) {
	// PROJECT is uppercased; the id contributes only its 4-char suffix (the full
	// slug duplicates the title text).
	got := spawnWindowTitle("ticket", "ticket/tk-ui-set-a6d2", "Some title here")
	want := "TICKET -- a6d2 -- Some title here"
	if got != want {
		t.Errorf("spawnWindowTitle = %q, want %q", got, want)
	}
}

func TestSpawnWindowTitleTruncatesTo20Runes(t *testing.T) {
	got := spawnWindowTitle("proj", "proj/tk-abcd", "0123456789abcdefghijKLMNOP")
	want := "PROJ -- abcd -- 0123456789abcdefghij"
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
	cmd := mustBuildSpawnCommand(t, "", "/some/dir", "ticket/tk-ui-set-a6d2", "ticket", "'tk ui' set title of iterm window")
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
	cmd := mustBuildSpawnCommand(t, "", "/Users/me/My Project", "project/tk-x", "project", "Title")
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

func TestBuildSpawnCommandRefusesDirWithQuotingCharacters(t *testing.T) {
	// {dir} lands inside the default template's innermost quoting, so a single
	// quote closes it and the rest parses as shell. The path is never repaired —
	// a rewritten path names a different directory — so it refuses instead.
	bad := []string{
		`/Users/me/it's`,
		"/Users/me/`id`",
		"/Users/me/$(id)",
		`/Users/me/say "hi"`,
		`/Users/me/back\slash`,
		"/Users/me/bang!",
		"/Users/me/bell\x07",
		"/Users/me/\u202egpj.exe", // bidi override: Cf, like the id gate refuses
	}
	for _, dir := range bad {
		cmd, err := buildSpawnCommand("", dir, "proj/tk-x", "proj", "Title")
		if err == nil {
			t.Errorf("buildSpawnCommand accepted dir %q: %s", dir, cmd)
		}
		if cmd != "" {
			t.Errorf("buildSpawnCommand(%q) returned a command alongside its error: %s", dir, cmd)
		}
	}
}

func TestBuildSpawnCommandKeepsPathWithSpacesVerbatim(t *testing.T) {
	// Spaces are the common case on macOS: the default template quotes them, and
	// the refusal must not touch them or alter the path in any way.
	dir := "/Users/x/My Projects/repo"
	cmd := mustBuildSpawnCommand(t, "", dir, "proj/tk-x", "proj", "Title")
	if !strings.Contains(cmd, dir) {
		t.Errorf("dir %q must reach the command verbatim, got: %s", dir, cmd)
	}
}

func TestBuildSpawnCommandRefusesProjectWithQuotingCharacters(t *testing.T) {
	// A project name is bounded by project.ValidName, which rules on path
	// joining and not on shell syntax, so a basename-derived name can carry a
	// quote into the same `sh -c` string.
	cmd, err := buildSpawnCommand("", "/some/dir", "proj/tk-x", "it's", "Title")
	if err == nil {
		t.Errorf("buildSpawnCommand accepted a project name with a quote: %s", cmd)
	}
}

func TestValidSpawnID(t *testing.T) {
	ok := []string{"tk-ui-set-a6d2", "ticket/tk-ui-set-a6d2", "ghostwheel/g-101.2", "a/b", "7-up-a6d2", "g_101/x_y"}
	// Anything GenerateID emits must pass: slugifyTitle keeps every Unicode
	// letter, so a German, Japanese or Russian title is ordinary output.
	for _, title := range []string{"Ticket über Größe", "日本語のチケット", "Проверка тикета"} {
		id := ticket.GenerateID(title)
		ok = append(ok, id, "proj/"+id)
	}
	for _, id := range ok {
		if !validSpawnID(id) {
			t.Errorf("validSpawnID(%q) = false, want true", id)
		}
	}
	bad := []string{
		"x'; touch /tmp/pwned; echo '", // closes the outer `-e '...'` quoting
		"proj/$(id)",                   // survives to the interactive shell
		"proj/`id`",
		"proj/a b",
		"proj/a;b",
		"proj/a\nb",
		"a/b/c", // one namespace separator only
		"/tk-x",
		"tk-x/",
		"",
		"tk-\u202egpj.exe", // bidi override: category Cf, not a letter
		"tk-x\u2066y-a6d2", // bidi isolate: same
		"tk-x\x07y",        // control character
		"..",               // dot-only segment: `.` is for hand-named ids, not traversal
		"../x",
		"proj/..",
		".",
		// A leading `-` or `_` is outside GenerateID's output — every slug opens
		// with a letter or digit — and a leading `-` reads as an option token to
		// a template that interpolates {id} in argument position.
		"-rf",
		"--flag",
		"-",
		"_x",
		"proj/-x",
	}
	for _, id := range bad {
		if validSpawnID(id) {
			t.Errorf("validSpawnID(%q) = true, want false", id)
		}
	}
}

// TestSpawnRefusalPreventsInjectedCommand runs both halves of the gate against
// the same injection: a ticket ID carrying a single quote closes the default
// template's `-e '...'` chunk and runs arbitrary shell in the outer `sh -c`.
// The first half bypasses the gate and asserts the injected `touch` DOES fire —
// that is the vulnerability, and without it the second half's assertion would
// hold even if buildSpawnCommand were deleted. The second half runs the same id
// through the gate and asserts its sentinel is never created. The template is
// byte-identical to the default except for the leading program, so every
// quoting layer is the real one, and the payload only touches a file under
// t.TempDir().
func TestSpawnRefusalPreventsInjectedCommand(t *testing.T) {
	dir := t.TempDir()
	template := strings.Replace(defaultSpawnTemplate, "osascript", "true", 1)
	inject := func(sentinel string) string { return "x'; touch " + sentinel + "; echo '" }

	// Negative control: ungated, the injection executes. The gate lives inside
	// buildSpawnCommand, so the unvalidated string is substituted here by hand —
	// the same replacement over the same template, minus the refusal — rather
	// than via a production seam that would let a caller skip the check.
	ungated := filepath.Join(dir, "PWNED-UNGATED")
	raw := strings.NewReplacer(
		"{dir}", dir,
		"{id}", inject(ungated),
		"{wtitle}", spawnWindowTitle("proj", inject(ungated), "Title"),
	).Replace(template)
	exec.Command("sh", "-c", raw).Run()
	if _, err := os.Stat(ungated); err != nil {
		t.Fatalf("negative control did not fire (%v): the injection no longer reaches the shell, so the gate assertion below proves nothing", err)
	}

	// Gated: buildSpawnCommand refuses the same id, so there is nothing to run.
	gated := filepath.Join(dir, "PWNED-GATED")
	cmd, err := buildSpawnCommand(template, dir, inject(gated), "proj", "Title")
	if err == nil {
		exec.Command("sh", "-c", cmd).Run()
		t.Errorf("buildSpawnCommand accepted an injected id: %s", cmd)
	}
	if _, err := os.Stat(gated); err == nil {
		t.Fatalf("injected command executed: %s exists", gated)
	}
}

func TestSpawnWorkRefusalNamesID(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	id := "x'; touch " + sentinel + "; echo '"
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", strings.Replace(defaultSpawnTemplate, "osascript", "true", 1), dir, false)

	msg := a.spawnWork(&ticket.Ticket{ID: id, Title: "Title"})()
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("spawnWork returned %T, want statusMsg", msg)
	}
	if !strings.Contains(string(status), "refusing to spawn") || !strings.Contains(string(status), id) {
		t.Errorf("refusal must name the offending ID, got %q", string(status))
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("injected command executed: %s exists", sentinel)
	}
}

// TestSpawnRefusalPreventsInjectedDir is the {dir} half of the gate, built like
// the {id} half above: the negative control proves the injection reaches the
// shell ungated, so the gated assertion below measures the refusal rather than a
// template that no longer interpolates the value.
func TestSpawnRefusalPreventsInjectedDir(t *testing.T) {
	dir := t.TempDir()
	template := strings.Replace(defaultSpawnTemplate, "osascript", "true", 1)
	inject := func(sentinel string) string { return dir + "'; touch " + sentinel + "; echo '" }

	ungated := filepath.Join(dir, "PWNED-UNGATED")
	raw := strings.NewReplacer(
		"{dir}", inject(ungated),
		"{id}", "proj/tk-x",
		"{wtitle}", spawnWindowTitle("proj", "proj/tk-x", "Title"),
	).Replace(template)
	exec.Command("sh", "-c", raw).Run()
	if _, err := os.Stat(ungated); err != nil {
		t.Fatalf("negative control did not fire (%v): the injection no longer reaches the shell, so the gate assertion below proves nothing", err)
	}

	gated := filepath.Join(dir, "PWNED-GATED")
	cmd, err := buildSpawnCommand(template, inject(gated), "proj/tk-x", "proj", "Title")
	if err == nil {
		exec.Command("sh", "-c", cmd).Run()
		t.Errorf("buildSpawnCommand accepted an injected dir: %s", cmd)
	}
	if _, err := os.Stat(gated); err == nil {
		t.Fatalf("injected command executed: %s exists", gated)
	}
}

func TestSpawnWorkRefusalNamesDir(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "PWNED")
	workDir := dir + "'; touch " + sentinel + "; echo '"
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", strings.Replace(defaultSpawnTemplate, "osascript", "true", 1), workDir, false)

	msg := a.spawnWork(&ticket.Ticket{ID: "proj/tk-ui-set-a6d2", Title: "Title"})()
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("spawnWork returned %T, want statusMsg", msg)
	}
	if !strings.Contains(string(status), "refusing to spawn") || !strings.Contains(string(status), "working directory") {
		t.Errorf("refusal must name the working directory as the reason, got %q", string(status))
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Fatalf("injected command executed: %s exists", sentinel)
	}
}

func TestSpawnWorkAcceptsApostropheTitle(t *testing.T) {
	// Titles are free text: an apostrophe is sanitized, never a refusal.
	dir := t.TempDir()
	a := New(filepath.Join(dir, ".tickets"), "proj", "v0", "true {id} {title}", dir, false)

	msg := a.spawnWork(&ticket.Ticket{ID: "proj/tk-ui-set-a6d2", Title: "'tk ui' set title"})()
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("spawnWork returned %T, want statusMsg", msg)
	}
	if !strings.HasPrefix(string(status), "Launching /work ") {
		t.Errorf("expected a launch, got %q", string(status))
	}
}

func TestBuildSpawnCommandSanitizesTitleExpansions(t *testing.T) {
	// A `$(...)` or backtick in a title passes the outer `sh -c` single quotes
	// untouched and then expands in the interactive shell `write text` types
	// into, so neither may reach the payload. The template holds nothing but the
	// two title placeholders, so the assertion covers only what interpolation
	// contributed and not whatever the default template itself may come to
	// contain.
	cmd := mustBuildSpawnCommand(t, "true {title} {wtitle}", "/some/dir", "proj/tk-x", "proj", "fix $(touch /tmp/x) and `id`")
	for _, bad := range []string{"$", "`"} {
		if strings.Contains(cmd, bad) {
			t.Errorf("title expansion %q reached the spawn command: %s", bad, cmd)
		}
	}
}

func TestSanitizeSpawnTextCoversExpansions(t *testing.T) {
	// `!` is here with the quoting breakers because history expansion runs in the
	// interactive shell before parsing and double quotes do not suppress it: an
	// unmatched event aborts the typed line, killing the spawn silently.
	got := sanitizeSpawnText("a'b\"c\\d$e`f\x07g!h")
	want := "a b c d e f g h"
	if got != want {
		t.Errorf("sanitizeSpawnText = %q, want %q", got, want)
	}
}

func TestSanitizeSpawnTextCoversControlAndFormat(t *testing.T) {
	// C0, TAB, DEL, C1 and the Cf format characters all repaint or misrender an
	// operator's terminal, so every one becomes a space — including ZWJ, whose
	// legitimate uses lose nothing in a 20-rune window title.
	for _, r := range []rune{'\x00', '\x07', '\t', '\n', '\x1b', '\x7f', '\u0080', '\u009b', '\u200d', '\u200c', '\u202e', '\u2066', '\ufeff'} {
		if got := sanitizeSpawnText("a" + string(r) + "b"); got != "a b" {
			t.Errorf("sanitizeSpawnText(%U) = %q, want %q", r, got, "a b")
		}
	}
	if got := sanitizeSpawnText("übergröße 日本語"); got != "übergröße 日本語" {
		t.Errorf("sanitizeSpawnText mangled ordinary text: %q", got)
	}
}

func TestNewStoresWorkDir(t *testing.T) {
	// New stores the workDir resolved by the caller; spawn uses it verbatim.
	a := New(filepath.Join(t.TempDir(), ".tickets"), "", "v0", "", "/repo/root", false)
	if a.workDir != "/repo/root" {
		t.Errorf("workDir = %q, want /repo/root", a.workDir)
	}
}

func TestNewScopesStoreToProject(t *testing.T) {
	// The project the caller resolved must reach the store, or TUI writes stop
	// resolving namespaced parent/dep/link IDs from a central-store project.
	a := New(t.TempDir(), "proj", "v0", "", "/repo/root", false)
	if a.store.Project != "proj" {
		t.Errorf("store.Project = %q, want %q", a.store.Project, "proj")
	}
}
