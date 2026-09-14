package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultSpawnTemplate opens a new iTerm window, names it {wtitle}, cds to
// {dir}, and runs the Claude session on {command} — `/work <id>` from the `w`
// key, `/capture <idea>` from `c`. macOS/iTerm-specific.
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
// interactive shell handles paths containing spaces. {wtitle}, {title} and the
// idea half of a capture {command} are pre-sanitized in Go (no quotes,
// backslashes, `$`, backticks or `!`) so they embed in the AppleScript strings
// without escaping and survive the inner shell's expansion and history
// expansion; the id half of a work {command} passes the {id} shape gate.
//
// Limitation: a project path containing a literal single quote can't be
// escaped inside the `osascript -e '...'` wrapper (that layer is itself
// single-quoted), so buildSpawnCommand refuses such a {dir} rather than
// emitting a command whose quoting it closes. The refusal sits at the shell
// boundary, so a custom spawn_command does not lift it — such a path has to be
// renamed. Spaces — the common case on macOS — work.
const defaultSpawnTemplate = `osascript -e 'tell application "iTerm"' -e 'set w to (create window with default profile)' -e 'tell current session of w to set name to "{wtitle}"' -e 'tell current session of w to write text "cd '"'"'{dir}'"'"' && printf \"\\033]0;%s\\007\" \"{wtitle}\" && export CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1 && claude \"{command}\""' -e 'end tell'`

// buildSpawnCommand substitutes the placeholders into the template (or the
// default when template is empty) and returns the shell command to run.
// Placeholders: {dir}, {id} (namespaced, used verbatim by /work), {project},
// {title} (sanitized like {wtitle}), {wtitle} (the "PROJECT -- ID4 -- TITLE"
// window name) and {command} (`/work <id>`, the slash command the session
// opens on). {command} is what lets one template serve both keys: a custom
// template written before it existed hard-codes `/work {id}` and carries no
// {command}, so nothing fires and it keeps working unchanged; only
// buildCaptureCommand needs the placeholder present.
//
// {id} and {title} come from the central store, a git repo other machines push
// to, so both are untrusted here. {project} and {dir} do not arrive that way —
// mergeConfigs takes `path` from the local half alone, so {dir} is the machine
// owner's own recorded path for the ticket's project (project.ExecutionDir,
// which spawnWork asks for the ticket's namespace and which refuses Root and a
// project with no path recorded). Provenance is not the bound it
// was read as, though. Neither value's character set is checked anywhere
// upstream: project.ValidName rules on path joining, refusing a separator and
// the dot segments and nothing else, and a name it passes may come from a git
// remote or a directory basename rather than from a config the owner wrote. So
// both are refused here on the characters {title} is sanitized of, since {dir}
// lands inside the default template's innermost quoting ('"'"'), where one
// single quote closes the layer and the rest parses as shell. Neither is ever
// rewritten to fit: replacing a character in a path changes which directory `cd`
// reaches, so the value passes verbatim or not at all. The template itself is
// not merged: project.SpawnCommand reads it from ~/.ticket/config.yaml alone,
// because it is the string handed to `sh -c`.
//
// {title} is sanitized rather than refused because titles are free text —
// refusing to spawn over an apostrophe would be absurd — while an id has a
// generated shape and a path or project name carrying a quote is as odd as an id
// doing so, so all three return an error and no command when they fall outside
// what interpolates safely. The refusals live here, at the shell boundary,
// rather than in the caller: a second spawn path must not be able to reach the
// interpolation without them.
//
// Sanitizing removes what would break a quoting layer, not everything that is
// shell syntax: `;`, `&`, `|` and `>` survive in a title, so a template must
// still quote {title} where it interpolates it, exactly as it must {dir}.
//
// Quoting means shell quoting, which in a `write text` payload is not the
// quotes you see. The default's outer "..." is consumed by AppleScript; what
// reaches the inner shell is the escaped \"...\", which is why {wtitle} is safe
// there and a title dropped into the visible quotes would not be. The default
// template only uses {wtitle} and {command}, both inside the escaped quotes.
func buildSpawnCommand(template, dir, id, project, title string) (string, error) {
	if !validSpawnID(id) {
		// Short by necessity: this reaches the TUI as a statusMsg, which
		// footerView renders as exactly one reserved row. The shape rule lives
		// in the README rather than here, where it would wrap the frame.
		return "", fmt.Errorf("ticket ID %q is not a plain ID; see spawn_command in the README", id)
	}
	if err := checkSpawnTarget(dir, project); err != nil {
		return "", err
	}
	return interpolateSpawn(spawnTemplate(template), dir, id, project, sanitizeSpawnText(title), "/work "+id, spawnWindowTitle(project, id, title)), nil
}

// buildCaptureCommand is the `c` key's counterpart of buildSpawnCommand: the
// session opens on `/capture <idea>` in the project's checkout, and there is
// no ticket yet, so {id} is empty and {title} is the idea. The idea is typed
// locally, but it lands inside the template's quoting all the same, so it is
// sanitized exactly like a title — free text, never refused — while {dir} and
// {project} take the same refusals as the work path. The template must carry
// {command}: a hard-coded `/work {id}` has nowhere to put a capture, and
// substituting into it anyway would open a work session on an empty id.
func buildCaptureCommand(template, dir, project, idea string) (string, error) {
	if err := checkSpawnTarget(dir, project); err != nil {
		return "", err
	}
	template = spawnTemplate(template)
	if !strings.Contains(template, "{command}") {
		return "", errors.New("spawn_command has no {command} placeholder; see the README")
	}
	idea = sanitizeSpawnText(idea)
	return interpolateSpawn(template, dir, "", project, idea, "/capture "+idea, windowTitle(project, "capture", idea)), nil
}

// checkSpawnTarget is the {dir}/{project} half of the refusals, shared by both
// builders so a second spawn path cannot reach the interpolation without it.
func checkSpawnTarget(dir, project string) error {
	if !validSpawnLiteral(dir) {
		return fmt.Errorf("working directory %q carries shell quoting characters; see spawn_command in the README", dir)
	}
	if !validSpawnLiteral(project) {
		return fmt.Errorf("project name %q carries shell quoting characters; see spawn_command in the README", project)
	}
	return nil
}

// spawnTemplate returns the configured template, or the default when none is.
func spawnTemplate(template string) string {
	if strings.TrimSpace(template) == "" {
		return defaultSpawnTemplate
	}
	return template
}

// interpolateSpawn substitutes the placeholders in one pass. title and wtitle
// arrive sanitized; command carries whichever slash command the caller built.
func interpolateSpawn(template, dir, id, project, title, command, wtitle string) string {
	r := strings.NewReplacer(
		"{dir}", dir,
		"{id}", id,
		"{project}", project,
		"{title}", title,
		"{wtitle}", wtitle,
		"{command}", command,
	)
	return r.Replace(template)
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

// validSpawnLiteral reports whether a value can be interpolated into a spawn
// template verbatim. It refuses exactly what sanitizeSpawnText replaces — the
// characters that close a quoting layer ('"\), the two the interactive shell
// behind `write text` expands inside double quotes ($ and backtick), the `!`
// its history expansion consumes before parsing, and the control and Cf runes
// that repaint or misrender the operator's terminal — for the same reasons,
// against values that must not be altered instead.
//
// It gates {dir} and {project}, where sanitizing is the wrong repair: a path
// with a character replaced names a different directory, or none, so `cd` would
// land somewhere the operator did not choose while the spawn reported success.
// Spaces and ordinary Unicode pass, since a macOS path with spaces is the common
// case and every metacharacter that matters is ASCII punctuation. The two are
// gated together because they were exempted together and reach the same `sh -c`
// string through the same interpolation; a project name carrying one of these is
// as odd as a path carrying one, so neither is worth a rule of its own.
func validSpawnLiteral(s string) bool {
	for _, r := range s {
		switch r {
		case '\'', '"', '\\', '$', '`', '!':
			return false
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// spawnWindowTitle builds the "PROJECT -- ID4 -- TITLE" window name: the
// uppercased project, the ticket's short id suffix (the 4-char hash after the
// last `-`, via IDSuffix — the full id slug just echoes the title, so it's
// redundant here), and the title truncated to the first 20 runes.
func spawnWindowTitle(project, id, title string) string {
	return windowTitle(project, IDSuffix(id), title)
}

// windowTitle builds the "PROJECT -- MID -- TEXT" window name with text
// truncated to the first 20 runes; a capture puts the word `capture` where a
// work session puts the id suffix. The whole string is sanitized of characters
// that would break the osascript/AppleScript quoting layers, so it embeds
// without escaping.
func windowTitle(project, mid, text string) string {
	r := []rune(text)
	if len(r) > 20 {
		r = r[:20]
	}
	return sanitizeSpawnText(strings.ToUpper(project) + " -- " + mid + " -- " + string(r))
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

// spawnWork launches a new terminal session running `/work <id>` in the
// checkout registered for the ticket's own project. qid is the ticket's
// qualified ID: its namespace, not the board's, is what execDir resolves, so a
// foreign child reached through an epic's detail runs in its own repository
// and a Root ticket — which has none — is refused, as is a namespace with no
// checkout registered here. Every refusal lands before the command is built,
// so nothing is exec'd. An epic is refused too: it is a container, and the
// work is a child's. The command runs detached so the TUI is never blocked,
// and with the resolved checkout as its working directory: a template that
// does not interpolate {dir} — one that hands `/work` to a program directly —
// would otherwise start the foreign ticket's session wherever `tk ui` was
// launched, in a repository the eligibility check never approved.
func (a App) spawnWork(t *ticket.Ticket, qid string) tea.Cmd {
	if t.Type == ticket.TypeEpic {
		return refuseSpawn("an epic is not run; open a child")
	}
	ns, _ := ticket.ParseNamespacedID(qid)
	dir, err := a.execDir(ns)
	if err != nil {
		return refuseSpawn(err.Error())
	}
	cmd, err := buildSpawnCommand(a.spawnCommand, dir, qid, ns, t.Title)
	if err != nil {
		return refuseSpawn(err.Error())
	}
	return startSpawn(cmd, dir, "Launching /work "+qid+"…")
}

// spawnCapture launches a new terminal session running `/capture <idea>` in
// the checkout registered for ns — the board's project from the list, the
// ticket's own from a detail. The capture dialogue itself (duplicate check,
// why/success gate) runs in that window; the TUI only seeds it with the idea.
// The same refusals as spawnWork land before anything is exec'd: Root and an
// unregistered project through execDir, quoting in {dir}/{project} and a
// template with no {command} through buildCaptureCommand.
func (a App) spawnCapture(ns, idea string) tea.Cmd {
	dir, err := a.execDir(ns)
	if err != nil {
		return refuseSpawn(err.Error())
	}
	cmd, err := buildCaptureCommand(a.spawnCommand, dir, ns, idea)
	if err != nil {
		return refuseSpawn(err.Error())
	}
	return startSpawn(cmd, dir, "Launching /capture…")
}

func refuseSpawn(reason string) tea.Cmd {
	return func() tea.Msg { return statusMsg("error: refusing to spawn: " + reason) }
}

// startSpawn runs cmd detached, with dir as its working directory, and
// reports launched on success.
func startSpawn(cmd, dir, launched string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("sh", "-c", cmd)
		c.Dir = dir
		if err := c.Start(); err != nil {
			return statusMsg("error: " + err.Error())
		}
		return statusMsg(launched)
	}
}
