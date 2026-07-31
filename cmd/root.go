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

Creating & Editing:
  create [title] [options]   Create ticket
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)

Dependencies & Links:
  dep <id> <dep-id>          Add dependency (id depends on dep-id)
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree
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
  --parent             Parent ticket ID
  --tags               Comma-separated (e.g., --tags ui,backend)
  --external-ref       External reference (e.g., gh-123)
  --branch             Git branch name (edit only)
  --set key=value      Set extra field (repeatable, blank value removes)

Statuses: backlog, ready, open, done, closed

Global flags:
  --repo <path>    Operate on a different repo
  --json           Output in JSON format

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
		if !project.IsConfigured() {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			fmt.Fprintln(os.Stderr, "tk is not configured. Run `tk init` to get started.")
			os.Exit(1)
		}
		return nil
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// TicketsDir returns the directory where tickets are stored.
// Priority: --repo flag → TICKETS_DIR env → config lookup → walk up from CWD → fallback .tickets
func TicketsDir() string {
	if repoFlag != "" {
		abs, err := filepath.Abs(repoFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --repo path: %v\n", err)
			os.Exit(1)
		}
		if dir, ok := ticket.FindTicketsDir(abs); ok {
			return dir
		}
		// No .tickets/ dir — try central store config for this repo path.
		if dir, ok := ticketsDirFromConfigFor(abs); ok {
			return dir
		}
		fmt.Fprintf(os.Stderr, "Error: no ticket store found for %s\n", abs)
		os.Exit(1)
	}
	if dir := os.Getenv("TICKETS_DIR"); dir != "" {
		return dir
	}
	if dir, ok := ticketsDirFromConfig(); ok {
		return dir
	}
	if dir, ok := ticket.FindTicketsDir(mustGetwd()); ok {
		return dir
	}
	return ".tickets"
}

func ticketsDirFromConfig() (string, bool) {
	return ticketsDirFromConfigFor(mustGetwd())
}

func ticketsDirFromConfigFor(dir string) (string, bool) {
	cfg, err := project.Load()
	if err != nil {
		return "", false
	}
	name, _ := project.ResolveName(cfg, dir, "")
	if name == "" {
		return "", false
	}
	p, ok := cfg.Projects[name]
	if !ok || p.Store != "central" {
		return "", false
	}
	ticketsDir, err := project.CentralProjectDir(name)
	if err != nil {
		return "", false
	}
	return ticketsDir, true
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
