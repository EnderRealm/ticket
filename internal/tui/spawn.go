package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode"

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
// interactive shell handles paths containing spaces. {wtitle} and {title} are
// pre-sanitized in Go (no quotes, backslashes, `$`, backticks or `!`) so they
// embed in the AppleScript strings without escaping and survive the inner
// shell's expansion and history expansion.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so such paths need a custom spawn_command. Spaces — the
// common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm"' -e 'set w to (create window with default profile)' -e 'tell current session of w to set name to "{wtitle}"' -e 'tell current session of w to write text "cd '"'"'{dir}'"'"' && printf \"\\033]0;%s\\007\" \"{wtitle}\" && export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 && claude \"/work {id}\""' -e 'end tell'`

// buildSpawnCommand substitutes the placeholders into the template (or the
// default when template is empty) and returns the shell command to run.
// Placeholders: {dir}, {id} (namespaced, used verbatim by /work), {project},
// {title} (sanitized like {wtitle}), and {wtitle} (the
// "PROJECT -- ID4 -- TITLE" window name).
//
// {id} and {title} come from the central store, a git repo other machines push
// to, so both are untrusted here. {project} and {dir} stay raw (the single-quote
// limitation on {dir} above still applies): {project} comes from the merged
// config, whose local half is the machine owner's own file or — under
// TK_STORE_ROOT — the override root's, and {dir} is either that config's
// recorded path for the project or, when it records none, the repo the store was
// resolved from — `--repo` if given, else the working directory. Nothing
// git-synced reaches either, since mergeConfigs takes `path` from the local half
// alone. Whether {dir} should be sanitized regardless is open:
// ticket/spawn-command-interpolates-5425. The template itself is not merged:
// project.SpawnCommand reads it from ~/.ticket/config.yaml alone, because it is
// the string handed to `sh -c`.
//
// {title} is sanitized rather than refused because titles are free text —
// refusing to spawn over an apostrophe would be absurd — while an id has a
// generated shape, so an id outside it returns an error and no command.
// The refusal lives here, at the shell boundary, rather than in the caller: a
// second spawn path must not be able to reach the interpolation without it.
//
// Sanitizing removes what would break a quoting layer, not everything that is
// shell syntax: `;`, `&`, `|` and `>` survive in a title, so a template must
// still quote {title} where it interpolates it, exactly as it must {dir}.
//
// Quoting means shell quoting, which in a `write text` payload is not the
// quotes you see. The default's outer "..." is consumed by AppleScript; what
// reaches the inner shell is the escaped \"...\", which is why {wtitle} is safe
// there and a title dropped into the visible quotes would not be. The default
// template only uses {wtitle}.
func buildSpawnCommand(template, dir, id, project, title string) (string, error) {
	if !validSpawnID(id) {
		// Short by necessity: this reaches the TUI as a statusMsg, which
		// footerView renders as exactly one reserved row. The shape rule lives
		// in the README rather than here, where it would wrap the frame.
		return "", fmt.Errorf("ticket ID %q is not a plain ID; see spawn_command in the README", id)
	}
	if strings.TrimSpace(template) == "" {
		template = defaultSpawnTemplate
	}
	r := strings.NewReplacer(
		"{dir}", dir,
		"{id}", id,
		"{project}", project,
		"{title}", sanitizeSpawnText(title),
		"{wtitle}", spawnWindowTitle(project, id, title),
	)
	return r.Replace(template), nil
}

// validSpawnID reports whether id can be interpolated into a spawn template
// without carrying shell syntax into it. IDs are filenames in the central
// store, which other machines push to, so a pushed name like `x'; rm -rf ~; #`
// would close the `-e '...'` quoting of the default template and run in the
// outer `sh -c`; `$(...)` and backticks survive that layer literally and then
// evaluate in the interactive shell that `write text` types them into.
//
// The accepted shape is a non-empty segment whose first rune is a letter or a
// digit and whose rest is letters, digits, `.`, `-` or `_`, plus at most one `/`
// for the project namespace. GenerateID itself only ever emits letters, digits
// and `-`, and slugifyTitle builds each word from letters and digits, so a
// generated id always opens with one; `.` and `_` are here for pre-existing
// hand-named ids such as `ghostwheel/g-101.2`, which are ordinary store contents
// and must keep spawning. The leading-rune rule keeps a pushed `-rf` or
// `--flag` out of a template that interpolates {id} in argument position, where
// it would read as an option token rather than a ticket. It also refuses any
// segment opening with `.` — `.`, `..` and `../x` among them, so none is handed
// to /work as a ticket to pick up, and a dotfile-shaped id like `.hidden-a6d2`
// with it.
//
// The letter/digit test is by Unicode category, not an ASCII range, because
// slugifyTitle keeps any unicode.IsLetter rune — "Ticket über Größe" generates
// `ticket-über-größe-7cd2` — and a control that refuses what tk itself writes is
// no control at all. The categories cost nothing here: every shell and
// AppleScript metacharacter is ASCII punctuation, and IsLetter is false for
// control characters and for the Cf format characters (U+202E and the bidi
// isolates), so all of those stay refused.
//
// Passing the id as an argv element instead is not available: it is not a
// command argument but a fragment of an AppleScript string literal that a
// second shell re-parses after the exec, and the template is user-configurable
// so its nesting is unknown. Validating at load time is also rejected — a
// ticket that fails to load is a ticket that silently vanishes. So the check
// sits at the shell boundary: only the spawn is refused, and the ticket stays
// usable everywhere else.
func validSpawnID(id string) bool {
	parts := strings.Split(id, "/")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i, r := range part {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
			if i == 0 || (r != '.' && r != '-' && r != '_') {
				return false
			}
		}
	}
	return true
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
	return sanitizeSpawnText(strings.ToUpper(project) + " -- " + IDSuffix(id) + " -- " + string(r))
}

// sanitizeSpawnText replaces characters that would break the shell single-quote
// or AppleScript double-quote layers ('"\\) with spaces, so the result embeds
// safely. `$` and backtick go too: they pass the outer `sh -c` untouched inside
// its single quotes, but the default template's payload lands in a
// double-quoted string that the interactive shell behind `write text` expands.
// `!` goes with them: the interactive zsh/bash that `write text` types the line
// into runs history expansion before parsing, and double quotes do not suppress
// it, so a `!` in a title either aborts the whole line (no matching event — a
// silently dead spawn) or splices history text in to be re-parsed.
//
// Control characters — C0, DEL and C1, all three via unicode.IsControl — and
// the Cf format characters go as well, matching ticket.SanitizeControl against
// the same threat: a title is untrusted content that lands in a window title and
// on the operator's terminal, where a raw escape sequence repaints what they
// read and U+202E RIGHT-TO-LEFT OVERRIDE makes the command that runs render as
// something else.
//
// TAB is not exempt here, where SanitizeControl exempts it: this text is
// embedded in an AppleScript string literal and in a title truncated to 20
// runes, neither of which has anything for a tab stop to align, so mapping it to
// a space loses nothing a caller wanted. ZWJ and ZWNJ are Cf and legitimate in
// Persian, Hindi and emoji sequences; they go too, matching SanitizeControl's
// tradeoff rather than opening a per-rune allowlist over a display string.
func sanitizeSpawnText(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\'', '"', '\\', '$', '`', '!':
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
}

// spawnWork launches a new terminal session in the project working directory
// running `/work <id>`. The command runs detached so the TUI is never blocked.
func (a App) spawnWork(t *ticket.Ticket) tea.Cmd {
	cmd, err := buildSpawnCommand(a.spawnCommand, a.workDir, t.ID, a.projectName, t.Title)
	if err != nil {
		return func() tea.Msg {
			return statusMsg("error: refusing to spawn: " + err.Error())
		}
	}
	return func() tea.Msg {
		if err := exec.Command("sh", "-c", cmd).Start(); err != nil {
			return statusMsg("error: " + err.Error())
		}
		return statusMsg("Launching /work " + t.ID + "…")
	}
}
