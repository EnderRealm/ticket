package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
	repoFlag   string
)

var helpText = `tk - ticket management CLI

Usage: tk <command> [args]

Viewing:
  show <id> [--metadata]     Display ticket details
  ls|list [filters]          List tickets (default: workflow grouped)
  frontier [--project=NAME]  List ready tickets with all deps done/closed (central store)
  search <query>             Search tickets by relevance (best matches first)
  audit [--project=NAME]     Report invalid parents, and epics whose stored status is not read (central store)
  verify <id>                Run the ticket's acceptance-criteria verify commands

  A criterion declares its check on an indented continuation line:

    - Frontier excludes blocked tickets.
      verify: go test ./pkg/ticket -run TestFrontier
    - Docs updated.

  Each command runs in the project directory (120s timeout), execed as
  argv and never through a shell: quotes group arguments, everything else
  (; | && $() backticks ~) is literal text. A command runs only if its
  program exactly matches an entry in verify_allow in ~/.ticket/config.yaml;
  anything else is refused without running. Only that machine-local file
  grants permission — the shared central-store config, ticket content and
  MCP arguments cannot, so an agent can never widen the list itself. An
  empty verify_allow refuses everything, and so does a local config that
  cannot be parsed.
  Default: go, make, cargo, pytest. Listing a program trusts whoever
  writes verify lines with everything that program can do — including
  "go run pkg@version", "go install pkg@version", "cargo install <crate>"
  and "make -f <file>", which run code from outside the repo. Shells and
  interpreters are absent: swift is not in the default because
  "swift -e <code>" runs arbitrary Swift, so a Swift project adds swift
  to verify_allow itself to get "swift test" back.
  Criteria with no command are reported unverified. Results are recorded
  in the ticket's Test Results section; exit is non-zero on any failure
  or refusal.

Creating & Editing:
  create [title] [options]   Create ticket
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)
  move <id> <repo-path>      Move a ticket to another repo's ticket store
    -r, --recursive          Move the ticket and all its descendants

  The target resolves to the project that repo owns in the central store,
  else to a .tickets/ the repo owns. A repo that resolves to neither is
  refused rather than having a store created for it. The moved
  ticket gets a new ID in the destination project, and the original is closed
  with a note — so list moved tickets with 'tk ls --status=closed'.

Dependencies & Links:
  dep <id> <dep-id>          Add dependency (id depends on dep-id)
    --cargo "<what flows>"   Name what concretely flows across the edge ("" clears)
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree (marks edges with no cargo)
  link <id> <id> [id...]     Link tickets (symmetric)
  unlink <id> <target-id>    Remove link

Query (JSON):
  query [jq-filter]          Output all tickets as JSONL (one JSON object per line)

  The optional filter is passed to jq's select() automatically.
  Do NOT wrap your filter in select() — just provide the expression.
  Always use single quotes for the filter to avoid bash issues with ! and ".

  tk query                                        # all tickets as JSONL
  tk query '.status == "open"'                     # filter by field
  tk query '.type == "bug" and .priority <= 1'    # compound filter
  tk query '.title | test("deploy"; "i")'         # regex search

  JSON fields: id, status, type, priority, title, description,
    design, acceptance_criteria, deps[], links[], tags[],
    created, parent, notes, external_ref,
    plus any custom extra fields (flattened to top level)
  Body sections (## Heading) become snake_case fields.

Setup:
  init [--project <name>] [--central-root <path>] [--yes]
                               Initialize tk and register a project
  sync                         Sync ticket changes to git
  status                       Show tk system status and project overview

Interactive:
  ui                         Interactive ticket browser (TUI)
  serve                      Start MCP server on stdio

Journal:
  watch start [--interval=5s]  Start background git commit watcher
  watch stop                   Stop the background watcher
  watch status                 Show watcher status
  watch logs [-n 50]           Show watcher log output
  recompute [--project=NAME]   Rebuild commit journal from git history

Filter flags for ls:
  --status=X         backlog | ready | open | done | closed
  -t, --type=X       bug | feature | epic
  -P, --priority=X   0 (critical) through 4 (backlog)
  -T, --tag=X        Filter by tag
  --field=key=value  Filter by extra field (substring match)
  --parent=X         Children of ticket X
  --group-by=X       Group by: workflow | type | priority
  --flat             Flat list (no grouping)

Create & edit options:
  -d, --description    Description text
  -t, --type           bug | feature | epic [default: feature]
  -p, --priority       0-4, 0=highest [default: 2]
  --status             Ticket status (edit only)
  --title              New title (edit only)
  --parent             Parent epic ID (an epic in the same project; an epic
                       itself cannot have a parent)
  --tags               Comma-separated (e.g., --tags ui,backend)
  --external-ref       External reference (e.g., gh-123)
  --branch             Git branch name (edit only)
  --set key=value      Set extra field (repeatable, blank value removes)
  --output key=value   Set output value, e.g. branch/commit/artifact
                       (edit only, repeatable, blank value removes)

Statuses: backlog, ready, open, done, closed
  An epic's status is derived from its children, never set: no children reads
  backlog, any child open reads open, all children done reads done, and all
  terminal with one of them closed reads closed — an epic whose work was
  abandoned did not complete. An epic never reads ready, and its completion
  date is its last child's. Setting a status by hand is refused — change the
  children, or set the epic closed to abandon it, which records the intent
  (abandoned: true) and closes its children too, naming them in the line that
  reports the edit; setting any other status on an abandoned epic takes that
  back. The same applies to a status set alongside --type epic. Statuses
  stored on epics before this were left in place and are ignored, not
  migrated; tk audit lists every epic that now reads a different one, and the
  ones it reports as stored-closed should be re-recorded before those epics
  are edited — but only where the file predates the change, since a later
  write of an epic leaves the same shape behind.

Global flags:
  --repo <path>    Operate on a different repo
  --json           Output in JSON format

Environment:
  TK_STORE_ROOT <abs path>
    Resolve the whole store against this root instead of the configured
    central_root: tickets in <root>/tickets, shared config <root>/config.yaml,
    local config <root>/.ticket/config.yaml. Nothing reads or writes the
    configured store or ~/.ticket/config.yaml, and tk serve starts without a
    local config.
    No sync, commit, push or journal watch runs: tk serve starts neither loop,
    and tk sync, tk watch and tk recompute refuse to run — sync would commit and
    push the git repo enclosing the sandbox, and the journal (recompute rebuilds
    it; watch appends to it, alongside its pid file and log) stays under HOME,
    which the override does not move. That is the one store path HOME still
    decides. tk init still registers a project, since central writes to an
    unregistered one are refused, but skips the store's git bootstrap.
    verify_allow and spawn_command are the settings it does not move: both are
    always read from ~/.ticket/config.yaml, because each decides code that runs
    as you — the allow-list by permitting a program, spawn_command by being the
    string handed to sh -c. So a store root can neither widen the allow-list nor
    supply a spawn template, and a root with no config falls back to your own.
    That bounds what a store root can supply, not a caller who also controls the
    environment: whoever can set TK_STORE_ROOT can generally set HOME too.
    A value that is not an absolute path is an error on any command, before any
    store is resolved — there is no fall-back to the configured store. The empty
    string is such a value, not an absent one.
    It covers tk's own store resolution and nothing wider. Outside it, under
    separate controls: ticket_create's "repo" argument, which resolves a
    caller-supplied path before any config lookup; and a verify command that
    verify_allow permits — "go run pkg@version" and "make -f <file>" run code
    from outside the repo, which can reach any path.

Partial ID matching: 'tk show 5c4' matches 'nw-5c46'
Run 'tk init' to configure and register a project.`

// Version is set via -ldflags at build time.
var Version = "dev"

func version() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	var revision, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = ", dirty"
			}
		}
	}
	if revision == "" {
		return Version
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	return fmt.Sprintf("dev (%s%s)", revision, dirty)
}

var rootCmd = &cobra.Command{
	Use:     "tk",
	Short:   "A markdown-based ticket manager",
	Long:    helpText,
	Version: version(),
}

// Commands exempt from the config gate.
var gateExempt = map[string]bool{
	"init":    true,
	"help":    true,
	"version": true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "path to repo root (resolves via .tickets/ or central store config)")
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Println(helpText)
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if gateExempt[cmd.Name()] {
			return nil
		}
		// Before the config gate and before any store resolution: IsConfigured
		// reports a set-but-unusable override as configured, and the resolutions
		// downstream (ticketsDirFromConfigFor, and every project.Load behind it)
		// swallow the error and fall back to a store the operator did not name.
		if _, _, err := project.StoreRootOverride(); err != nil {
			cmd.SilenceUsage = true
			return err
		}
		if !project.IsConfigured() {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			fmt.Fprintln(os.Stderr, "tk is not configured. Run `tk init` to get started.")
			os.Exit(1)
		}
		return nil
	}
}

// refuseIsolatedStore returns an error naming TK_STORE_ROOT for the commands
// whose work leaves the isolated store: `tk sync` commits and pushes the git
// repo enclosing the store root, which under the override publishes a sandbox
// to whatever remote that repo has; `tk watch` runs journal cycles that
// auto-close tickets; and `tk recompute` deletes and rebuilds a project's
// commit journal from whatever the sandbox's config points at. Journal state —
// the watcher's pid file and log with it — lives under HOME, which the override
// does not move, so a sandbox naming a project the machine also has would
// rewrite the machine owner's journal for it. Refusing is what makes the
// documented "no sync, commit, push or journal watch runs" true of the whole
// CLI rather than of `tk serve` alone.
//
// `tk watch status`, `logs` and `stop` read or signal rather than write, and
// are refused all the same: they manage the machine's real watcher, and a shell
// that asked to be isolated from the machine's store has no business doing that
// either.
func refuseIsolatedStore(command string) error {
	_, isolated, err := project.StoreRootOverride()
	if err != nil {
		return err
	}
	if isolated {
		return fmt.Errorf("tk %s does not run while %s is set: it writes outside the isolated store — unset %s to run it against the configured store", command, project.StoreRootEnv, project.StoreRootEnv)
	}
	return nil
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// TicketsDir returns the directory where tickets are stored.
func TicketsDir() string {
	dir, _ := TicketsDirAndProject()
	return dir
}

// TicketStore returns a store over the resolved tickets directory, scoped to
// the project it serves so parent/dep/link IDs namespaced by the central store
// resolve (see FileStore.Resolve).
func TicketStore() *ticket.FileStore {
	dir, name := TicketsDirAndProject()
	return ticket.NewProjectFileStore(dir, name)
}

// TicketsDirAndProject resolves the tickets directory and the project it holds.
// Priority: --repo flag → TICKETS_DIR env → config lookup → walk up from CWD → fallback .tickets
// The project name is non-empty only for a central-store project dir; a local
// .tickets/ store never sees namespaced IDs and must not accept them.
func TicketsDirAndProject() (string, string) {
	dir, name, _ := resolveTicketsDir()
	return dir, name
}

// resolveTicketsDir is TicketsDirAndProject plus whether the resolved project is
// an unregistered central directory. `tk ui` needs it in the frame — the alt
// screen swallows the warning the CLI commands print on stderr — and takes it
// from the resolution that decided it rather than re-deriving it from a second
// config read.
func resolveTicketsDir() (string, string, bool) {
	if repoFlag != "" {
		abs, err := filepath.Abs(repoFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --repo path: %v\n", err)
			os.Exit(1)
		}
		if dir, ok := ticket.FindTicketsDir(abs); ok {
			return dir, "", false
		}
		// No .tickets/ dir — try central store config for this repo path.
		if dir, name, unregistered, ok := ticketsDirFromConfigFor(abs); ok {
			return dir, name, unregistered
		}
		fmt.Fprintf(os.Stderr, "Error: no ticket store found for %s\n", abs)
		os.Exit(1)
	}
	if dir := os.Getenv("TICKETS_DIR"); dir != "" {
		return dir, "", false
	}
	if dir, name, unregistered, ok := ticketsDirFromConfig(); ok {
		return dir, name, unregistered
	}
	if dir, ok := ticket.FindTicketsDir(mustGetwd()); ok {
		return dir, "", false
	}
	return ".tickets", "", false
}

func ticketsDirFromConfig() (string, string, bool, bool) {
	return ticketsDirFromConfigFor(mustGetwd())
}

// ticketsDirFromConfigFor resolves a repo path to its central ticket directory,
// the project name, whether that project is unregistered, and whether anything
// was resolved at all. The rules live in ticket.CentralStoreForRepo, which
// `tk move` resolves its destination through as well, so a move cannot land
// tickets in a directory this resolution would never read back.
func ticketsDirFromConfigFor(dir string) (ticketsDir, name string, unregistered, ok bool) {
	// Why nothing resolved does not change this path: a config that failed to
	// load and a repo with no central project both fall through to the local
	// search below. `tk move` takes the reason, where the resolution feeds a
	// write into another repo's store.
	store, unregistered, ok, _ := ticket.CentralStoreForRepo(dir)
	if !ok {
		return "", "", false, false
	}
	if unregistered {
		fmt.Fprintln(os.Stderr, ticket.UnregisteredWarning(store))
	}
	return store.Dir, store.Project, unregistered, true
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
