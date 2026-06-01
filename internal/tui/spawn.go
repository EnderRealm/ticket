package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultSpawnTemplate opens a new iTerm window, cds to {dir}, and runs the
// work session. macOS/iTerm-specific.
//
// It creates the window with the default profile (which starts a normal
// interactive shell) and then `write text`s the command into that live
// session — rather than iTerm's `... command "..."` form, which exec's the
// string as a single process WITHOUT a shell, so a `&&` pipeline never runs
// and the window closes immediately. `write text` runs the full pipeline in
// the window's interactive shell, so `claude` resolves on PATH and the window
// stays open. Passed as a multi-statement `osascript -e ... -e ...` script.
//
// Quoting: the whole string runs via `sh -c`; each AppleScript statement is in
// its own `-e '...'` single-quoted arg; the `write text` argument is a
// double-quoted AppleScript string with the inner `claude "/work {id}"` quotes
// escaped (\"); and {dir} is wrapped in shell single quotes (via the '"'"'
// idiom) so the interactive shell handles paths containing spaces.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so such paths need a custom spawn_command. Spaces — the
// common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm"' -e 'set w to (create window with default profile)' -e 'tell current session of w to write text "cd '"'"'{dir}'"'"' && claude \"/work {id}\""' -e 'end tell'`

// buildSpawnCommand substitutes {dir} and {id} into the template (or the
// default when template is empty) and returns the shell command to run.
func buildSpawnCommand(template, dir, id string) string {
	if strings.TrimSpace(template) == "" {
		template = defaultSpawnTemplate
	}
	r := strings.NewReplacer("{dir}", dir, "{id}", id)
	return r.Replace(template)
}

// spawnWork launches a new terminal session in the project working directory
// running `/work <id>`. The command runs detached so the TUI is never blocked.
func (a App) spawnWork(id string) tea.Cmd {
	cmd := buildSpawnCommand(a.spawnCommand, a.workDir, id)
	return func() tea.Msg {
		if err := exec.Command("sh", "-c", cmd).Start(); err != nil {
			return statusMsg("error: " + err.Error())
		}
		return statusMsg("Launching /work " + id + "…")
	}
}
