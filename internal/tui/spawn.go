package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultSpawnTemplate opens a new iTerm window, cds to {dir}, and runs the
// work session. macOS/iTerm-specific.
//
// The whole string is executed via `sh -c`, so it contains three nested
// quoting layers: the outer single quotes belong to `osascript -e`, the
// AppleScript `command` argument is a double-quoted string, and the
// `claude "/work {id}"` invocation has its double quotes escaped (\") for that
// AppleScript string. {dir} is wrapped in single quotes so the shell iTerm
// spawns to run the command handles paths containing spaces.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so such paths need a custom spawn_command. Spaces — the
// common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm" to create window with default profile command "cd '"'"'{dir}'"'"' && claude \"/work {id}\""'`

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
