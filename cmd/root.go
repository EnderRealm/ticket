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

// helpText is the root help page: a command index only. Each command's
// behaviour lives in its own Long and Example, printed by tk <command> --help,
// and policy prose lives in docs/ and the README.
var helpText = `tk - ticket management CLI

Usage: tk <command> [args]      tk <command> --help for details

Viewing:
  show <id>...           Display ticket details
  ls, list               List tickets (done/closed hidden; --all shows them)
  frontier               Ready tickets whose deps are testing, done or closed
  search <query>         Search tickets by relevance
  query [jq-filter]      All tickets as JSONL, optionally filtered
  audit [cause]          Summarise store findings by cause

Creating & Editing:
  create [title]         Create a ticket
  edit <id>              Update ticket fields
  edit --acceptance <t>  Set a body section (also --design, --test-results)
  add-note <id> [text]   Append a timestamped note (stdin if no text)
  move <id> <project>    Move an isolated leaf ticket to another project
  delete <id>...         Delete tickets

Dependencies & Links:
  dep <id> <dep-id>      Add dependency (--cargo names what flows)
  undep <id> <dep-id>    Remove dependency
  dep tree <id>          Show dependency tree
  link <id> <id>...      Link tickets (symmetric)
  unlink <id> <id>       Remove link

Verification:
  verify <id>            Run acceptance-criteria verify commands
  verify --policy        Print the verify_allow and verify_timeout in force
  verify --launcher <w>  Exec each verify command through an argv prefix

Setup & Server:
  init                   Initialize tk and register a project
  status                 System status and project overview
  sync                   Sync ticket changes to git
  serve                  Start MCP server on stdio
  activate               Activate Root and cross-project parents

Journal:
  watch start|stop|status|logs   Background git commit watcher
  recompute              Rebuild commit journal from git history

Global flags:
  --project <ns>         Operate on a central-store namespace (_root for ideas)
  --repo <name|path>     Operate on a registered project or repo
  --json                 JSON output

Statuses: backlog -> ready -> open -> testing -> done; closed = won't fix.
testing = landed, waiting on a person's test; its dependents are not blocked.
An epic's status is derived from its children; see tk edit --help.
--workflow-kind generic marks work no run can do (a person's step, an event);
frontier and ready work leave it out. See tk create --help.
IDs match on prefix: 'tk show 5c4' matches 'nw-5c46'.`

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
	rootCmd.PersistentFlags().StringVar(&projectFlag, "project", "", "namespace in the central store to operate on (_root for Root); no repo is resolved, an unknown name is refused, conflicts with --repo")
	// The root prints only the index; every other command keeps cobra's
	// default help, so its own Long, Example and flags are what it prints.
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd != rootCmd {
			defaultHelp(cmd, args)
			return
		}
		fmt.Fprintln(cmd.OutOrStdout(), helpText)
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
		// A config that exists but cannot be read or parsed is not an
		// unconfigured machine; name the file and the error instead of
		// sending the reader to `tk init`.
		if _, ok, _ := project.StoreRootOverride(); !ok {
			if err := project.LocalConfigError(); err != nil {
				cmd.SilenceUsage = true
				return err
			}
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
	dir, name, err := resolveTicketsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return dir, name
}

// resolveTicketsDir resolves the tickets directory, the project it holds, and
// the error a repo owning no central project is.
//
// The rules live in ticket.ResolveStoreForRepo, which MCP ticket_create's repo
// argument reads through as well, and `tk move`'s destination opens its project
// through the same store resolution: a write cannot land tickets in a directory
// this one would never read back, and a config that fails to load is named as
// itself rather than sent to `tk init`.
func resolveTicketsDir() (string, string, error) {
	if projectFlag != "" {
		if repoFlag != "" {
			return "", "", fmt.Errorf("--project and --repo are both selectors; pass one")
		}
		store, err := ticket.ResolveStoreForNamespace(projectFlag)
		if err != nil {
			return "", "", err
		}
		return store.Dir, store.Project, nil
	}
	repo, err := repoDir()
	if err != nil {
		return "", "", err
	}
	store, unregistered, err := ticket.ResolveStoreForRepo(repo)
	if err != nil {
		return "", "", err
	}
	if unregistered {
		fmt.Fprintln(os.Stderr, ticket.UnregisteredWarning(store))
	}
	return store.Dir, store.Project, nil
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
// is absolute because it is what the store resolves from.
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
