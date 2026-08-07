package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify <id>",
	Short: "Run the verify commands declared in a ticket's acceptance criteria",
	Long:  "Run the verify commands declared in a ticket's acceptance criteria and record the results on the ticket.",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerify,
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	// A failing criterion is an expected outcome, not a usage error: report it
	// once (via Execute) rather than with cobra's usage dump and duplicate line.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	store := TicketStore()
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}

	criteria := ticket.ParseCriteria(ticket.AcceptanceCriteria(t.Body))
	if len(criteria) == 0 {
		return fmt.Errorf("%s has no acceptance criteria", t.ID)
	}

	dir := verifyWorkDir()
	// The allow-list comes from machine-local config only — there is no flag for
	// it, so nothing in a ticket or the synced store can widen what runs.
	allow, allowErr := project.VerifyAllow()
	results, err := ticket.RunVerify(cmd.Context(), criteria, dir, allow, allowErr)
	if err != nil {
		return err
	}
	report := ticket.NewVerifyReport(t.ID, dir, results)

	// Record after the run so a store failure degrades to a warning instead of
	// discarding the results.
	t.Body = ticket.UpdateSection(t.Body, "Test Results", ticket.FormatVerifyRecord(results, time.Now().UTC()))
	if err := store.Update(t); err != nil {
		report.RecordError = err.Error()
	}

	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("verifying %s in %s\n", t.ID, dir)
		for _, r := range results {
			switch r.Status {
			case ticket.VerifyUnverified:
				fmt.Printf("UNVERIFIED %s\n", r.Criterion.Text)
				continue
			case ticket.VerifyRefused:
				fmt.Printf("REFUSED %s\n", r.Criterion.Text)
			default:
				fmt.Printf("%s (exit %d) %s\n", strings.ToUpper(string(r.Status)), r.ExitCode, r.Criterion.Text)
			}
			if r.Status != ticket.VerifyPass && r.Output != "" {
				for _, line := range strings.Split(r.Output, "\n") {
					fmt.Printf("  %s\n", line)
				}
			}
		}
		fmt.Printf("%d pass, %d fail, %d refused, %d unverified\n",
			report.Summary.Pass, report.Summary.Fail, report.Summary.Refused, report.Summary.Unverified)
	}

	if report.RecordError != "" && !jsonOutput {
		fmt.Fprintf(os.Stderr, "warning: failed to record verify results: %s\n", report.RecordError)
	}

	// Only the non-zero counts appear, so a refusal with nothing failing does not
	// report "0 ... failed" alongside it.
	var problems []string
	if report.Summary.Fail > 0 {
		problems = append(problems, fmt.Sprintf("%d failed", report.Summary.Fail))
	}
	if report.Summary.Refused > 0 {
		problems = append(problems, fmt.Sprintf("%d refused", report.Summary.Refused))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s of %d criteria", strings.Join(problems, " and "), len(criteria))
	}
	return nil
}

// verifyWorkDir returns the directory verify commands run in: the configured
// path of the project the working directory (or --repo) belongs to, falling
// back to that directory itself. Only a config-sourced project name is trusted
// — ResolveName's git-remote and dirname inference can name a project the
// directory isn't a checkout of, which would run commands in the wrong repo.
func verifyWorkDir() string {
	dir := mustGetwd()
	if repoFlag != "" {
		if abs, err := filepath.Abs(repoFlag); err == nil {
			dir = abs
		}
	}
	cfg, err := project.Load()
	if err != nil {
		return dir
	}
	name, source := project.ResolveName(cfg, dir, "")
	if source != "config" {
		return dir
	}
	if p, ok := cfg.Projects[name]; ok && p.Path != "" {
		return p.Path
	}
	return dir
}
