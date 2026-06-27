package tui

import (
	"os/exec"
	"strings"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultSpawnTemplate opens a new iTerm window, names it {wtitle}, cds to
// {dir}, and runs the work session. macOS/iTerm-specific.
//
// It creates the window with the default profile (which starts a normal
// interactive shell), sets the session name to {wtitle}, and then `write text`s
// the command into that live session — rather than iTerm's `... command "..."`
// form, which exec's the string as a single process WITHOUT a shell, so a `&&`
// pipeline never runs and the window closes immediately. `write text` runs the
// full pipeline in the window's interactive shell, so `claude` resolves on PATH
// and the window stays open. Passed as a multi-statement `osascript -e ... -e
// ...` script.
//
// Making the title stick. The AppleScript `set name` alone flashes and is then
// clobbered by two writers: (1) the shell's prompt — oh-my-zsh and similar set
// the title from a preexec hook to the running command the instant it starts;
// (2) Claude Code, which updates the terminal title continuously as it runs. So
// the command itself reasserts the title with a `printf` OSC-0 escape AFTER the
// preexec hook has fired (it runs mid-pipeline, so it wins), and disables
// claude's own title updates via CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1. With both
// overwriters handled, the OSC title persists for the session. The title is
// passed as a `%s` argument (not interpolated into the format) so a literal `%`
// in a title is safe.
//
// The env var is `export`ed as its own statement rather than prefixed inline
// (`VAR=1 claude ...`): `claude` is frequently a shell alias (e.g. `tabset ...;
// command claude`), and an inline prefix would bind to the alias's first
// command, never reaching the real `claude`. Exporting puts it in the shell
// environment that the aliased `command claude` inherits.
//
// Quoting: the whole string runs via `sh -c`; each AppleScript statement is in
// its own `-e '...'` single-quoted arg; the `write text`/`set name` arguments
// are double-quoted AppleScript strings with inner quotes escaped (\") and inner
// backslashes doubled (\\033 → the literal \033 the shell's printf needs); and
// {dir} is wrapped in shell single quotes (via the '"'"' idiom) so the
// interactive shell handles paths containing spaces. {wtitle} is pre-sanitized
// in Go (no quotes/backslashes) so it embeds in the AppleScript strings without
// escaping.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so such paths need a custom spawn_command. Spaces — the
// common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm"' -e 'set w to (create window with default profile)' -e 'tell current session of w to set name to "{wtitle}"' -e 'tell current session of w to write text "cd '"'"'{dir}'"'"' && printf \"\\033]0;%s\\007\" \"{wtitle}\" && export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 && claude \"/work {id}\""' -e 'end tell'`

// buildSpawnCommand substitutes the placeholders into the template (or the
// default when template is empty) and returns the shell command to run.
// Placeholders: {dir}, {id} (namespaced, used verbatim by /work), {project},
// {title} (raw — caller-quoted, like {dir}), and {wtitle} (the pre-sanitized
// "PROJECT -- ID4 -- TITLE" window name).
func buildSpawnCommand(template, dir, id, project, title string) string {
	if strings.TrimSpace(template) == "" {
		template = defaultSpawnTemplate
	}
	r := strings.NewReplacer(
		"{dir}", dir,
		"{id}", id,
		"{project}", project,
		"{title}", title,
		"{wtitle}", spawnWindowTitle(project, id, title),
	)
	return r.Replace(template)
}

// spawnWindowTitle builds the "PROJECT -- ID4 -- TITLE" window name: the
// uppercased project, the ticket's short id suffix (the 4-char hash after the
// last `-`, via IDSuffix — the full id slug just echoes the title, so it's
// redundant here), and the title truncated to the first 20 runes. The whole
// string is sanitized of characters that would break the osascript/AppleScript
// quoting layers, so it embeds without escaping.
func spawnWindowTitle(project, id, title string) string {
	r := []rune(title)
	if len(r) > 20 {
		r = r[:20]
	}
	return sanitizeWindowTitle(strings.ToUpper(project) + " -- " + IDSuffix(id) + " -- " + string(r))
}

// sanitizeWindowTitle replaces characters that would break the shell
// single-quote or AppleScript double-quote layers ('"\\) and control
// characters with spaces, so the result embeds safely.
func sanitizeWindowTitle(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\'', '"', '\\':
			return ' '
		}
		if r < ' ' {
			return ' '
		}
		return r
	}, s)
}

// spawnWork launches a new terminal session in the project working directory
// running `/work <id>`. The command runs detached so the TUI is never blocked.
func (a App) spawnWork(t *ticket.Ticket) tea.Cmd {
	cmd := buildSpawnCommand(a.spawnCommand, a.workDir, t.ID, a.projectName, t.Title)
	return func() tea.Msg {
		if err := exec.Command("sh", "-c", cmd).Start(); err != nil {
			return statusMsg("error: " + err.Error())
		}
		return statusMsg("Launching /work " + t.ID + "…")
	}
}
