package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/journal"
	"github.com/spf13/cobra"
)

var recomputeProject string

var recomputeCmd = &cobra.Command{
	Use:   "recompute",
	Short: "Rebuild commit journal from git history",
	Long:  "Delete and rebuild commits.jsonl by scanning the full git history for bracket-ref commits.",
	RunE:  runRecompute,
}

func init() {
	recomputeCmd.Flags().StringVar(&recomputeProject, "project", "", "project name (default: resolve from CWD)")
	rootCmd.AddCommand(recomputeCmd)
}

func runRecompute(cmd *cobra.Command, args []string) error {
	if err := refuseIsolatedStore("recompute"); err != nil {
		return err
	}

	cfg, err := project.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	name := recomputeProject
	if name == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		name, _ = project.ResolveName(cfg, cwd, "")
	}
	if name == "" {
		return fmt.Errorf("cannot determine project — use --project or run from a registered project directory")
	}

	entry, ok := cfg.Projects[name]
	if !ok {
		return fmt.Errorf("project %q not found in config", name)
	}
	if entry.Path == "" {
		return fmt.Errorf("project %q has no path configured", name)
	}

	jPath, err := journal.JournalPath(name)
	if err != nil {
		return err
	}

	// Delete existing journal
	if err := os.Remove(jPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old journal: %w", err)
	}

	// Scan full git history
	commits, err := journal.CollectCommits(entry.Path, "", entry.RegisteredAt)
	if err != nil {
		return err
	}

	var entries []journal.Entry
	for _, commit := range commits {
		actions := journal.ExtractTicketActions(commit.Msg)
		if len(actions) == 0 {
			continue
		}

		files, added, removed, branch, _ := journal.GetDiffStats(entry.Path, commit.SHA)

		// Same resolution the watch cycle applies, since this rebuilds the file
		// the watch cycle appends to: a ref keyed by its namespaced form here
		// would split one ticket's history across two keys and hide the
		// recomputed half from every reader that queries by bare ID.
		resolved := journal.ResolveActions(name, actions)

		tickets := make([]string, 0, len(resolved))
		for ticketID := range resolved {
			tickets = append(tickets, ticketID)
		}
		sort.Strings(tickets)

		for _, ticketID := range tickets {
			entries = append(entries, journal.Entry{
				SHA:          commit.SHA,
				Ticket:       ticketID,
				Repo:         entry.Path,
				TS:           commit.TS,
				Msg:          commit.Msg,
				Author:       commit.Author,
				Action:       resolved[ticketID],
				FilesChanged: files,
				LinesAdded:   added,
				LinesRemoved: removed,
				Branch:       branch,
			})
		}
	}

	if err := journal.AppendEntries(name, entries); err != nil {
		return err
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]any{
			"project": name,
			"commits": len(commits),
			"entries": len(entries),
			"journal": jPath,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("recomputed %s: %d commits → %d journal entries\n", name, len(commits), len(entries))
	return nil
}
