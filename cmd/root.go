package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var (
	jsonOutput  bool
	repoFlag    string
	projectFlag string
)

var helpText = `tk - ticket management CLI

Usage: tk <command> [args]

Viewing:
  show <id> [--metadata]     Display ticket details. An epic ends with a
                             Progress section: its children counted by status
                             across every namespace and whether that count is
                             complete (the store read in full); a leaf whose
                             parent does not make it a child ends with a
                             Relationship section saying why. Children and
                             Blocking list tickets from any namespace, qualified;
                             a ticket the audit flags ends with a Findings section
  ls|list [filters]          List tickets (default: workflow grouped, done
                             and closed hidden; --all shows them)
    --all-projects           Every namespace in the central store, IDs qualified
    --parent=ID              Children of an epic. Membership is global — the
                             epic's children in any namespace — and the listing
                             is the selected project's, so a child elsewhere is
                             counted in the "children of …" line on stderr and
                             shown with --all-projects; --all keeps closed ones
  frontier [--parent=ID]     List ready tickets with all deps done/closed,
                             computed over the whole store; --parent keeps the
                             members of that epic (qualified project/id, or bare
                             with --project) and --project keeps one namespace's
                             rows without changing what is ready
  search <query>             Search tickets by relevance (best matches first)
    --all-projects           Search every namespace, IDs qualified
  audit [cause] [--project=NAME]
                             Summarise findings by cause, one line each with the count and the
                             command that lists that cause's tickets: invalid parents, epics whose
                             stored status is not read, tickets missing body content, tickets whose
                             acceptance criteria nothing can check, tickets storing a legacy Review
                             Log, files that cannot be read as tickets (exits non-zero), and files
                             whose id names another project. tk audit <cause> lists that cause's
                             tickets in full and nothing else; --json is the whole report either way
  verify <id>                Run the ticket's acceptance-criteria verify commands
    --dir <path>             Run the commands in this directory instead of the project's
    --criterion <n>          Run only criterion n (1-based): exit 0 pass, 1 fail,
                             20 refused or unverified
    --no-record              Skip writing the Test Results section

  A criterion declares its check on an indented continuation line, or
  declares that no command can exist for it:

    - Frontier excludes blocked tickets.
      verify: go test ./pkg/ticket -run TestFrontier
    - The TUI redraws cleanly at 40 columns.
      unverifiable: needs a human at a terminal.

  A criterion carrying neither line is reported on stderr by tk create
  when the ticket is written; the ticket is still created.

  Each command runs in the checkout registered on this machine for the
  ticket's own project, and only there. Eligibility is decided before
  anything runs: a Root ticket is refused (Root has no repository), so is
  a ticket in a project with no checkout registered on this machine, and
  so is one whose registered checkout is not a directory here — the
  working directory, HOME, a parent epic's project and the ticket store
  never stand in, and nothing is recorded for a refused run. --dir moves
  an eligible run and applies only after that check; it makes nothing
  eligible. Commands are execed as argv and never
  through a shell: quotes group arguments, everything else
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
  Each command is bounded by verify_timeout under the project in
  ~/.ticket/config.yaml, beside its path: a Go duration such as 300s or
  5m, defaulting to 120s when unset, and a timed-out command's output
  names the bound that was applied and whether it came from
  verify_timeout or from the default. It is machine-local like
  verify_allow — the bound is a property of the machine and the suite,
  not of a ticket that syncs to every machine — and a value that is not
  a positive duration refuses every verify command in that project
  rather than falling back to the default.
  Both settings are read from ~/.ticket/config.yaml at the start of
  every run, by this command and by tk serve alike, so an edit applies
  to the next run with no server restart. The staleness that does bite
  is a tk serve process older than the build that introduced a setting:
  a binary predating verify_timeout applies the 120s it was built with
  however current the config is, so restart the server after upgrading.
  Criteria with no command are reported unverified. Results are recorded
  in the ticket's Test Results section; exit is non-zero on any failure
  or refusal.
  --criterion and --no-record are CLI-only, and the ticket_verify MCP
  tool's dir is narrower than --dir: it accepts only the project's
  configured checkout or a linked git worktree of that repository, refuses
  anything else before running, and always records. An MCP caller's
  arguments are shaped by ticket content, and a sandboxed client with no
  shell would gain reach it does not otherwise have; a CLI caller already
  has a shell and can run anything anywhere, so --dir is used as given
  and widens nothing for it. Nothing widens verify_allow.
  ticket_criteria returns the parsed criteria with acceptance_id, the
  contract's identity; ticket_verify refuses a stale acceptance_id before
  running and does not record a run whose criteria changed meanwhile. The
  report and the Test Results record name the directory the commands ran
  in, the acceptance_id and the caller's candidate.
  MCP verification waits up to 10 seconds, then returns a job ID for
  ticket_verify_status polling. Request timeouts do not cancel commands;
  ticket_verify_cancel or session disconnect does. Status recovers a lost
  start response by ticket ID without running the commands again.

Creating & Editing:
  create [title] [options]   Create ticket. Outside a directory the config
                             registers as a project, the destination must be
                             named: --project <namespace> (--project _root for
                             an idea with no repository yet) or --repo; a
                             namespace inferred from a git remote or a
                             directory name is a read fallback, never a write
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)
  move <id> <repo-path>      Move a ticket to another repo's ticket store
                             Only an isolated leaf moves; -r/--recursive is refused

  The target resolves to the project that repo owns in the central store.
  A repo that owns none is refused rather than having a store created for it,
  and so is a target resolving to the store the ticket already lives in — that
  renames rather than moves it; re-parent within a project with
  'tk edit --parent'. The moved ticket gets a new ID in the destination
  project, and the original is closed with a note — so list moved tickets
  with 'tk ls --status=closed'. An epic left behind is not closed: its status
  is derived from the children that stayed.

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
  tk query --all-projects                         # every namespace, IDs qualified

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
  ui                         Interactive ticket browser (TUI); --project _root
                             browses Root; c refines an idea through Claude
                             (/brainstorm, then /capture), n opens the create form
  serve                      Start MCP server on stdio

Journal:
  watch start [--interval=5s]  Start background git commit watcher
  watch stop                   Stop the background watcher
  watch status                 Show watcher status
  watch logs [-n 50]           Show watcher log output
  recompute [--project=NAME]   Rebuild commit journal from git history

Filter flags for ls:
  --status=X         backlog | ready | open | done | closed
  --all              Include done and closed, which the default hides.
                     --status shows one status at a time, so this is the
                     only view of the whole board.
  -t, --type=X       bug | feature | epic
  -P, --priority=X   0 (critical) through 4 (backlog)
  -T, --tag=X        Filter by tag
  --field=key=value  Filter by extra field (substring match)
  --parent=X         Children of epic X (qualified project/id, or bare in the
                     selected project); membership is global, the listing is
                     the selected project's — see ls above
  --all-projects     Every namespace in the central store, IDs qualified;
                     conflicts with --project
  --group-by=X       Group by: workflow | type | priority
  --flat             Flat list (no grouping)

Create & edit options:
  -d, --description    Description text
  -t, --type           bug | feature | epic [default: feature]
  -p, --priority       0-4, 0=highest [default: 2]
  --status             Ticket status (edit only)
  --title              New title (edit only)
  --parent             Parent epic ID (an epic in the same project; an epic
                       itself cannot have a parent). A qualified project/id
                       names an epic in another namespace, accepted once
                       the catalog requires cross-project-parents
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
  migrated; tk audit counts every epic that now reads a different one and
  tk audit stale-status and tk audit stored-closed list them, and the ones
  it reports as stored-closed should be re-recorded before those epics are
  edited — but only where the file predates the change, since a later write
  of an epic leaves the same shape behind.

Global flags:
  --repo <name|path>  Operate on a different registered project or repo
  --project <ns>   Operate on a namespace in the central store by name; no
                   repository is resolved and none has to exist. _root is
                   always selectable (writing to it is still the catalog's
                   call); any other name must be a registered project, a
                   directory under <central_root>/tickets, or catalogued —
                   an unknown one is refused rather than conjured. A
                   selector: --project and --repo together are refused
  --json           Output in JSON format

Namespaces:
  A namespace is a directory under <central_root>/tickets/ and the first half
  of every project/id. A project is a namespace with a repository registered
  behind it; _root (Root) is the built-in namespace for ideas that have no
  repository yet, and tk init refuses to register it. <central_root>/catalog.yaml
  lists every namespace with its kind (root, project, legacy) and the
  required_features every binary sharing the store must implement; a
  .namespace marker inside each namespace directory keeps an empty one in git.
  Writes are refused before they touch the store when the catalog requires a
  feature this binary lacks, and writes to _root are refused until the catalog
  requires root-namespace. No catalog means the store predates activation:
  every read and write to an ordinary project behaves as before. Binaries
  released before this guard ignore the catalog and the markers entirely, so
  activation requires replacing every tk binary, embedded library and
  long-lived process (MCP servers, watchers) sharing the store — a marker
  alone cannot make an already-released binary refuse.

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
	rootCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "registered project name or path to repo root")
	rootCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "namespace in the central store to operate on (_root for Root); no repo is resolved")
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Println(helpText)
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if gateExempt[cmd.Name()] {
			return nil
		}
		// Before the config gate and before any store resolution: IsConfigured
		// reports a set-but-unusable override as configured, and the resolutions
		// downstream (every project.Load behind resolveTicketsDir) would fall
		// back to a store the operator did not name.
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

// exitError carries a specific process exit code out of a command. Errors that
// do not use it exit 1, as every command error did before.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

// exitCode is the process exit code for a command's error: none, the code the
// error carries, or the generic failure.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code
	}
	return 1
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}

// TicketStore returns a store over the resolved tickets directory, scoped to
// the project it serves so parent/dep/link IDs namespaced by the central store
// resolve (see FileStore.Resolve).
func TicketStore() *ticket.FileStore {
	dir, name := TicketsDirAndProject()
	return ticket.NewProjectFileStore(dir, name)
}

// TicketsDirAndProject resolves the tickets directory and the project it holds:
// the project the repo — --repo when set, else the working directory — owns in
// the central store. That is the only store tk resolves.
func TicketsDirAndProject() (string, string) {
	dir, name, _ := mustResolveTicketsDir()
	return dir, name
}

// mustResolveTicketsDir is resolveTicketsDir for the callers that cannot carry
// its error: every command below the resolution needs a store, and there is
// nothing to fall back to now that a repo either owns a central project or owns
// no store at all.
func mustResolveTicketsDir() (string, string, bool) {
	dir, name, unregistered, err := resolveTicketsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return dir, name, unregistered
}

// resolveTicketsDir resolves the tickets directory, the project it holds,
// whether that project is an unregistered central directory, and the error a
// repo owning no central project is. `tk ui` needs the unregistered flag in the
// frame — the alt screen swallows the warning the CLI commands print on stderr —
// and takes it from the resolution that decided it rather than re-deriving it
// from a second config read.
//
// The rules live in ticket.ResolveStoreForRepo, which `tk move`'s destination
// and MCP ticket_create's repo argument read through as well: one resolution, so
// a write cannot land tickets in a directory this one would never read back, and
// a config that fails to load is named as itself rather than sent to `tk init`.
func resolveTicketsDir() (string, string, bool, error) {
	if projectFlag != "" {
		if repoFlag != "" {
			return "", "", false, fmt.Errorf("--project and --repo are both selectors; pass one")
		}
		store, err := ticket.ResolveStoreForNamespace(projectFlag)
		if err != nil {
			return "", "", false, err
		}
		return store.Dir, store.Project, false, nil
	}
	repo, err := repoDir()
	if err != nil {
		return "", "", false, err
	}
	store, unregistered, err := ticket.ResolveStoreForRepo(repo)
	if err != nil {
		return "", "", false, err
	}
	if unregistered {
		fmt.Fprintln(os.Stderr, ticket.UnregisteredWarning(store))
	}
	return store.Dir, store.Project, unregistered, nil
}

// allProjectsStore is the store over every namespace, for the listings that
// take --all-projects. It conflicts with --project: one selects a namespace,
// the other refuses to, and a command handed both cannot honour either.
func allProjectsStore() (*ticket.MultiStore, error) {
	if projectFlag != "" {
		return nil, fmt.Errorf("--all-projects and --project conflict; pass one")
	}
	root, err := project.CentralStoreRoot()
	if err != nil {
		return nil, err
	}
	return ticket.NewMultiStore(filepath.Join(root, "tickets")), nil
}

// listingFor is the listing a read-only command works over: every namespace
// with --all-projects, else the selected project's.
func listingFor(cmd *cobra.Command) ([]*ticket.Ticket, error) {
	if allProjects, _ := cmd.Flags().GetBool("all-projects"); allProjects {
		ms, err := allProjectsStore()
		if err != nil {
			return nil, err
		}
		return ms.List()
	}
	return TicketStore().List()
}

// repoDir is the repo tk operates on: --repo when given, else the working
// directory. A configured project name resolves before the filesystem fallback
// so a same-named relative path cannot select a different project. The result
// is absolute because it is both what the store resolves from and the directory
// `tk ui` spawns a work session in when the project's config records no path.
func repoDir() (string, error) {
	if repoFlag == "" {
		return mustGetwd(), nil
	}
	repo := repoFlag
	cfg, err := project.Load()
	if err != nil {
		return "", fmt.Errorf("load ticket config: %w", err)
	}
	path, configured := project.ConfiguredRepoPath(cfg, repo)
	if configured {
		repo = path
	}
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("invalid --repo path: %w", err)
	}
	if !configured {
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("--repo %q is neither a registered project name nor a directory: %w", repoFlag, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("--repo %q is neither a registered project name nor a directory", repoFlag)
		}
	}
	return abs, nil
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
