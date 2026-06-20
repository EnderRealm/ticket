package tui

import (
	"os/exec"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultSpawnTemplate opens a new iTerm window, names it, cds to {dir}, and
// runs the work session. macOS/iTerm-specific.
//
// It creates the window with the default profile (which starts a normal
// interactive shell), sets the session name to {wtitle} so the worker is
// identifiable, and then `write text`s the command into that live session —
// rather than iTerm's `... command "..."` form, which exec's the string as a
// single process WITHOUT a shell, so a `&&` pipeline never runs and the window
// closes immediately. `write text` runs the full pipeline in the window's
// interactive shell, so `claude` resolves on PATH and the window stays open.
// Passed as a multi-statement `osascript -e ... -e ...` script.
//
// Quoting: the whole string runs via `sh -c`; each AppleScript statement is in
// its own `-e '...'` single-quoted arg; the `write text` argument and the `set
// name` argument are double-quoted AppleScript strings with the inner `claude
// "/work {id}"` quotes escaped (\"); and {dir} is wrapped in shell single
// quotes (via the '"'"' idiom) so the interactive shell handles paths
// containing spaces. {wtitle} is pre-sanitized in Go (no quotes/backslashes)
// so it embeds in the AppleScript string without escaping.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so such paths need a custom spawn_command. Spaces — the
// common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm"' -e 'set w to (create window with default profile)' -e 'tell current session of w to set name to "{wtitle}"' -e 'tell current session of w to write text "cd '"'"'{dir}'"'"' && claude \"/work {id}\""' -e 'end tell'`

// buildSpawnCommand substitutes the placeholders into the template (or the
// default when template is empty) and returns the shell command to run.
// Placeholders: {dir}, {id} (namespaced, used verbatim by /work), {project},
// {title} (raw — caller-quoted, like {dir}), and {wtitle} (the pre-sanitized
// "PROJECT -- BAREID -- TITLE" window name).
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

// bareID strips the project namespace prefix from a namespaced ticket id,
// returning everything after the first `/` (or the id unchanged if absent).
func bareID(id string) string {
	if _, after, found := strings.Cut(id, "/"); found {
		return after
	}
	return id
}

// spawnWindowTitle builds the "PROJECT -- BAREID -- TITLE" window name. The
// title is truncated to the first 20 runes and the whole string is sanitized of
// characters that would break the osascript/AppleScript quoting layers, so it
// embeds without escaping.
func spawnWindowTitle(project, id, title string) string {
	r := []rune(title)
	if len(r) > 20 {
		r = r[:20]
	}
	return sanitizeWindowTitle(project + " -- " + bareID(id) + " -- " + string(r))
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
