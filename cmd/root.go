package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/pkg/ticket"
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
  backlog [filters]          List tickets in the backlog
  ready [filters]            Tickets with all deps resolved and parent in_progress
  blocked [filters]          Tickets with unresolved deps
  done [--limit=N] [filters]    Recently done (default limit: 20)

Creating & Editing:
  create [title] [options]   Create ticket (interactive if no title)
  edit <id> [options]        Update ticket fields
  add-note <id> [text]       Append timestamped note (stdin if no text)
  delete <id> [id...]        Delete ticket(s)

Pipeline:
  advance <id> [--to <stage>] [--force]  Advance ticket to next pipeline stage
  skip <id> --to <stage> --reason '...'  Skip to a later stage with justification
  revert <id> --to <stage> --reason '..' Revert to an earlier stage with justification
  review <id> --approve|--reject         Record review verdict on current stage
  log <id>                               Show stage transition and review history
  pipeline [--stage <stage>]             Show tickets grouped by pipeline stage
  inbox                                  Show tickets needing human attention
  next                                   Show per-project next actions
  migrate [--dry-run]                    Migrate legacy tickets to stage pipeline

Dependencies & Links:
  dep <id> <dep-id>          Add dependency (id depends on dep-id)
  undep <id> <dep-id>        Remove dependency
  dep tree [--full] <id>     Show dependency tree
  dep cycle                  Find cycles in open tickets
  link <id> <id> [id...]     Link tickets (symmetric)
  unlink <id> <target-id>    Remove link

Query (JSON):
  query [jq-filter]          Output all tickets as JSONL (one JSON object per line)

  The optional filter is passed to jq's select() automatically.
  Do NOT wrap your filter in select() — just provide the expression.
  Always use single quotes for the filter to avoid bash issues with ! and ".

  tk query                                        # all tickets as JSONL
  tk query '.stage == "triage"'                    # filter by field
  tk query '.type == "bug" and .priority <= 1'    # compound filter
  tk query '.title | test("deploy"; "i")'         # regex search

  JSON fields: id, stage, type, priority, title, description,
    design, acceptance_criteria, deps[], links[], tags[],
    created, assignee, parent, notes, external_ref, review, risk,
    plus any custom extra fields (flattened to top level)
  Body sections (## Heading) become snake_case fields.

Analytics:
  stats                      Project health at a glance
  timeline [--weeks=N]       Tickets closed by week (default: 4 weeks)

Setup:
  init [--store central|local] [--project <name>] [--yes] [--json]
                               Initialize ticket storage for this project
  sync                         Sync ticket changes to git (stage, commit, push)

Interactive:
  ui                         Interactive ticket browser (TUI)
  serve                      Start MCP server on stdio

Other:
  workflow                   Ticket workflow guide (types, stages, conventions)

Filter flags for ls:
  --stage=X          backlog | triage | spec | design | implement | test | verify | done
  -t, --type=X       bug | feature | task | epic | chore
  -P, --priority=X   0 (critical) through 4 (backlog)
  -a, --assignee=X   Filter by assignee
  -T, --tag=X        Filter by tag
  --parent=X         Children of ticket X
  --group-by=X       Group by: workflow | pipeline | type | priority
  --flat             Flat list (no grouping)

Filter flags for ready, blocked, done:
  -a, --assignee=X   Filter by assignee
  -T, --tag=X        Filter by tag
  ready also accepts: --open (skip parent hierarchy checks)

Create & edit options:
  -d, --description    Description text
  --design             Design notes
  --acceptance         Acceptance criteria
  -t, --type           bug | feature | task | epic | chore [default: task]
  -p, --priority       0-4, 0=highest [default: 2]
  --stage              Pipeline stage (edit only)
  --review             Review state: pending | approved | rejected (edit only)
  --risk               Risk level: low | normal | high | critical (edit only)
  --title              New title (edit only)
  -a, --assignee       Assignee
  --parent             Parent ticket ID
  --tags               Comma-separated (e.g., --tags ui,backend)
  --external-ref       External reference (e.g., gh-123)
  --branch             Git branch name (edit only)
  --set key=value      Set extra field (repeatable, blank value removes)

Stages: backlog → triage → spec → design → design-review → implement → code-review → test → verify → done
  Pipelines are type-dependent and risk-dependent.
  Default pipelines (low risk) omit review stages.
  Normal/high/critical add design-review and code-review stages.

Global flags:
  --repo <path>    Operate on a different repo (walks up to find .tickets/)
  --json           Output in JSON format

Partial ID matching: 'tk show 5c4' matches 'nw-5c46'
Tickets stored as markdown in .tickets/`

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

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().StringVar(&repoFlag, "repo", "", "path to repo root (walks up to find .tickets/)")
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Println(helpText)
	})
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
		fmt.Fprintf(os.Stderr, "Error: no .tickets/ directory found under %s\n", abs)
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
	cfg, err := project.Load()
	if err != nil {
		return "", false
	}
	cwd := mustGetwd()
	name, _ := project.ResolveName(cfg, cwd, "")
	if name == "" {
		return "", false
	}
	p, ok := cfg.Projects[name]
	if !ok || p.Store != "central" {
		return "", false
	}
	dir, err := project.CentralProjectDir(name)
	if err != nil {
		return "", false
	}
	return dir, true
}

func mustGetwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
