package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Report tickets whose parent breaks the one-level epic hierarchy",
	Long: "Report tickets whose parent breaks the one-level epic hierarchy: a parent that is not an epic, " +
		"a parent that does not resolve, a parent in another project, an epic that has a parent, or a parent cycle. " +
		"Each parent is resolved within the project that owns the ticket, so the report matches what a write would accept. " +
		"Read-only — nothing is rewritten.",
	Args: cobra.NoArgs,
	RunE: runAudit,
}

func init() {
	auditCmd.Flags().String("project", "", "limit to a single project")
	rootCmd.AddCommand(auditCmd)
}

func runAudit(cmd *cobra.Command, args []string) error {
	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("audit requires a configured central store: %w", err)
	}
	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))

	audit, err := ticket.AuditParents(store)
	if err != nil {
		return err
	}

	if proj, _ := cmd.Flags().GetString("project"); proj != "" {
		cfg, err := project.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if _, ok := cfg.Projects[proj]; !ok {
			return fmt.Errorf("project %q not found in config", proj)
		}
		var filtered []ticket.ParentViolation
		for _, v := range audit.Violations {
			if p, _ := ticket.ParseNamespacedID(v.ID); p == proj {
				filtered = append(filtered, v)
			}
		}
		audit.Violations = filtered
		var skipped []ticket.ProjectSkip
		for _, s := range audit.Skipped {
			if s.Project == proj {
				skipped = append(skipped, s)
			}
		}
		audit.Skipped = skipped
	}

	if jsonOutput {
		if audit.Violations == nil {
			audit.Violations = []ticket.ParentViolation{}
		}
		data, err := json.MarshalIndent(audit, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(audit.Violations) == 0 {
		fmt.Println("No parent violations.")
	} else {
		for _, v := range audit.Violations {
			fmt.Printf("%s  %s  parent: %s  (%s)\n", v.ID, v.Kind, v.Parent, v.Detail)
		}
		fmt.Printf("\n%d ticket(s) violate the one-level epic hierarchy — clear or repoint each parent\n", len(audit.Violations))
	}
	// A project the audit could not read is named rather than dropped: without
	// it, "No parent violations." would speak for a store never fully read.
	if len(audit.Skipped) > 0 {
		fmt.Printf("\nwarning: %d project(s) could not be read, so this report is incomplete:\n", len(audit.Skipped))
		for _, s := range audit.Skipped {
			fmt.Printf("  %s: %s\n", s.Project, s.Error)
		}
	}
	return nil
}
