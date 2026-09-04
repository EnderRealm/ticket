package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify <id>",
	Short: "Run the verify commands declared in a ticket's acceptance criteria",
	Long:  "Run the verify commands declared in a ticket's acceptance criteria and record the results on the ticket.",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerify,
}

var (
	verifyDir       string
	verifyCriterion int
	verifyNoRecord  bool
)

// verifyRefusedExit is the exit code a single-criterion run reports for a
// refused or unverified criterion. The integer is weft's anchor-stage
// convention (AnchorCommand.gradedRefusalExitCode: "graded and refused, do not
// relaunch") adopted rather than invented, so one documented code replaces a
// wrapper script per harness. Neither status may exit 0: a harness grades the
// run on the exit code, and a criterion that never ran is not one that passed.
const verifyRefusedExit = 20

func init() {
	f := verifyCmd.Flags()
	f.StringVar(&verifyDir, "dir", "", "run the commands in this directory instead of the project's")
	f.IntVar(&verifyCriterion, "criterion", 0, "run only criterion n (1-based): exit 0 pass, 1 fail, 20 refused or unverified")
	f.BoolVar(&verifyNoRecord, "no-record", false, "skip writing the Test Results section")
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

	// --criterion selects by position in ParseCriteria order, the order --json
	// reports, so a harness re-running one check names the index it read there.
	single := cmd.Flags().Changed("criterion")
	if single {
		if verifyCriterion < 1 || verifyCriterion > len(criteria) {
			return fmt.Errorf("--criterion %d is out of range: %s has %d criteria", verifyCriterion, t.ID, len(criteria))
		}
		criteria = criteria[verifyCriterion-1 : verifyCriterion]
	}

	dir := verifyWorkDir()
	if cmd.Flags().Changed("dir") {
		// Checked before anything runs, so a mistyped directory is a usage error
		// rather than every criterion failing in the wrong tree. Keyed on the flag
		// being passed, not on a non-empty value: a harness that computed an empty
		// path must get the refusal, not a silent run in the configured tree.
		info, statErr := os.Stat(verifyDir)
		if statErr != nil {
			return fmt.Errorf("--dir %q: not an existing directory: %w", verifyDir, statErr)
		}
		if !info.IsDir() {
			return fmt.Errorf("--dir %q: not an existing directory", verifyDir)
		}
		dir = verifyDir
	}
	// The allow-list comes from machine-local config only — there is no flag for
	// it, so nothing in a ticket or the synced store can widen what runs.
	allow, allowErr := project.VerifyAllow()
	results, err := ticket.RunVerify(cmd.Context(), criteria, dir, allow, allowErr)
	if err != nil {
		return err
	}
	report := ticket.NewVerifyReport(t.ID, dir, results)

	// Record after the run so a store failure degrades to a warning instead of
	// discarding the results. Through Mutate, because a verify run is long
	// enough for the ticket to have been edited meanwhile: the record lands in
	// the body as it stands now rather than in the copy read before the run.
	if !verifyNoRecord {
		record := ticket.FormatVerifyRecord(results, time.Now().UTC())
		if _, err := ticket.Mutate(store, t.ID, func(t *ticket.Ticket) error {
			t.Body = ticket.UpdateSection(t.Body, "Test Results", record)
			return nil
		}); err != nil {
			report.RecordError = err.Error()
		}
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

	if single {
		switch res := results[0]; res.Status {
		case ticket.VerifyPass:
			return nil
		case ticket.VerifyFail:
			return &exitError{code: 1, err: fmt.Errorf("criterion %d failed (exit %d)", verifyCriterion, res.ExitCode)}
		default:
			return &exitError{code: verifyRefusedExit, err: fmt.Errorf("criterion %d %s", verifyCriterion, res.Status)}
		}
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
	cfg, err := project.Load()
	if repoFlag != "" {
		repo := repoFlag
		if err == nil {
			if path, ok := project.ConfiguredRepoPath(cfg, repo); ok {
				repo = path
			}
		}
		if abs, err := filepath.Abs(repo); err == nil {
			dir = abs
		}
	}
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
