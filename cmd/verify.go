package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify <id> | --policy",
	Short: "Run the verify commands declared in a ticket's acceptance criteria",
	Long: `Run the verify commands declared in a ticket's acceptance criteria and record
the results in the ticket's Test Results section, each failing criterion
followed by its indented output.

A criterion declares its check on an indented continuation line, or declares
that no command can exist for it:

  - Frontier excludes blocked tickets.
    verify: go test ./pkg/ticket -run TestFrontier
  - tk ls output stays readable at 40 columns.
    unverifiable: needs a human at a terminal.

A criterion carrying neither line is reported on stderr by tk create when the
ticket is written; the ticket is still created. A criterion with no command is
reported unverified.

Commands run in the checkout registered on this machine for the ticket's
project, or for the project its implementation field names
(tk edit <id> --set implementation=<project>). A Root ticket, a project with no
checkout registered here and a checkout that is not a directory are refused
before anything runs, and nothing is recorded. --dir moves an eligible run; it
makes nothing eligible.

Commands are execed as argv, never through a shell, and run only if their
program exactly matches an entry in verify_allow in ~/.ticket/config.yaml
(default: go, make, cargo, pytest). Each is bounded by the project's
verify_timeout (default 120s). Output is capped to its last 4KB.

--launcher execs each command through an argv prefix, one word per use, so a
harness can run it as another user while tk reads the store and verify_allow
as itself:

  tk verify 5c4 --criterion 1 --no-record --launcher sudo --launcher=-n \
    --launcher=-u --launcher weftagent1 --launcher=-H --launcher env --launcher=-i

The prefix is trusted like --dir and checked against nothing; verify_allow is
still checked against the criterion's own program, so a prefix cannot make a
refused command run. There is no config key or MCP argument for it. On
timeout the launcher's process group is sent SIGTERM, which sudo relays, then
SIGKILL 2s later. With --check, a runnable criterion reports the argv a run
would exec, prefix included.

tk verify --policy, with no ticket id, prints the verify_allow list and the
resolved project's verify_timeout as a run would enforce them right now;
--json for a script. An unreadable config is reported as the error with no
list, never as the defaults.

Exit status: 0 when nothing failed or was refused, 1 otherwise. With
--criterion: 0 pass, 1 fail, 20 refused or unverified.

The verify policy -- eligibility, the implementation field, verify_allow and
what listing a program trusts, verify_timeout, the output cap, and the
ticket_verify MCP tool's narrower dir -- is in docs/verify.md
(github.com/EnderRealm/ticket).`,
	Example: `  tk verify 5c4
  tk verify 5c4 --dir /path/to/worktree --criterion 1 --no-record
  tk verify 5c4 --criterion 2 --check
  tk verify 5c4 --criterion 1 --check --launcher sudo --launcher=-n --launcher=-u --launcher weftagent1
  tk verify --policy --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runVerify,
}

var (
	verifyDir       string
	verifyCriterion int
	verifyNoRecord  bool
	verifyCheck     bool
	verifyPolicy    bool
	verifyLauncher  []string
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
	f.BoolVar(&verifyCheck, "check", false, "with --criterion, report whether it would be refused without running or recording it: exit 0 runnable, 20 refused or unverified")
	f.StringArrayVar(&verifyLauncher, "launcher", nil, "exec each command through this argv prefix, one word per use (repeat it; a word may start with -); trusted like --dir, never checked against verify_allow")
	f.BoolVar(&verifyPolicy, "policy", false, "print the verify_allow list and the project's verify_timeout this machine would enforce, without a ticket (--json for a script)")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	// A failing criterion is an expected outcome, not a usage error: report it
	// once (via Execute) rather than with cobra's usage dump and duplicate line.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if verifyPolicy {
		if len(args) != 0 {
			return fmt.Errorf("--policy takes no ticket id")
		}
		return reportVerifyPolicy()
	}
	if len(args) != 1 {
		return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
	}

	store := TicketStore()
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}

	// Eligibility before anything runs: the ticket's namespace is the store's,
	// and only a registered checkout of that project — or of the project its
	// implementation field names — is somewhere its commands may execute. Root
	// has none, an unregistered project has none here, and a checkout that is
	// gone is not stood in for by the process's working directory or by the
	// ticket's own checkout. --dir moves an eligible run; it does not make one
	// eligible.
	cfg, err := project.Load()
	if err != nil {
		return fmt.Errorf("load ticket config: %w", err)
	}
	implementation := t.Extra[ticket.ImplementationField]
	execProject, dir, err := project.VerifyTarget(cfg, store.Project, implementation)
	if err != nil {
		return err
	}

	criteria := ticket.ParseCriteria(ticket.AcceptanceCriteria(t.Body))
	if len(criteria) == 0 {
		return fmt.Errorf("%s has no acceptance criteria", t.ID)
	}
	// The identity is of the whole contract, taken before --criterion narrows
	// the run: a single-criterion record still says which contract it belongs to.
	acceptanceID := ticket.CriteriaIdentity(criteria)

	// --criterion selects by position in ParseCriteria order, the order --json
	// reports, so a harness re-running one check names the index it read there.
	single := cmd.Flags().Changed("criterion")
	if verifyCheck && !single {
		return fmt.Errorf("--check requires --criterion")
	}
	if single {
		if verifyCriterion < 1 || verifyCriterion > len(criteria) {
			return fmt.Errorf("--criterion %d is out of range: %s has %d criteria", verifyCriterion, t.ID, len(criteria))
		}
		criteria = criteria[verifyCriterion-1 : verifyCriterion]
	}

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
	// An empty word is a harness's computed value gone missing, not an argv
	// element sudo or env take; refused before anything runs, as --dir "" is.
	for _, word := range verifyLauncher {
		if word == "" {
			return fmt.Errorf("--launcher: an empty word is not a launcher argument")
		}
	}
	// The allow-list and the timeout both come from machine-local config only —
	// there is no flag for either, so nothing in a ticket or the synced store can
	// widen what runs or how long it may run. --dir moves the directory alone:
	// the bound still belongs to the project whose checkout the run resolved to.
	// --launcher wraps how an allowed command is execed; the allow-list is still
	// checked against the command's own program.
	allow, allowErr := project.VerifyAllow()
	timeout, timeoutErr := project.VerifyTimeout(cfg, execProject)
	policy := ticket.VerifyPolicy{Allow: allow, AllowErr: allowErr, Timeout: timeout, TimeoutErr: timeoutErr}
	if verifyCheck {
		return reportVerifyCheck(t.ID, ticket.CheckVerify(criteria, policy, verifyLauncher), dir, implementation, acceptanceID)
	}
	// Verification children have their own process group. Forward CLI shutdown
	// through the runner's context and drain that group before leaving.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	results, err := ticket.RunVerify(ctx, criteria, dir, policy, verifyLauncher)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	prov := ticket.VerifyProvenance{Dir: dir, Implementation: implementation, AcceptanceID: acceptanceID}
	report := ticket.NewVerifyReport(t.ID, prov, results)

	// Record after the run so a store failure degrades to a warning instead of
	// discarding the results. RecordVerify lands the record in the body as it
	// stands now rather than in the copy read before the run, and refuses to
	// land it at all if the criteria changed meanwhile.
	if !verifyNoRecord {
		record := ticket.FormatVerifyRecord(results, prov, time.Now().UTC())
		if err := ticket.RecordVerify(store, t.ID, record, acceptanceID); err != nil {
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
		if implementation != "" {
			fmt.Printf("verifying %s in %s (implementation %s)\n", t.ID, dir, ticket.SanitizeControl(implementation))
		} else {
			fmt.Printf("verifying %s in %s\n", t.ID, dir)
		}
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

// reportVerifyCheck prints a --check result and maps it to the exit code: a
// runnable criterion exits 0, and a refused or unverified one exits
// verifyRefusedExit, the code a real run of it would give. Nothing ran, so
// there is no record to write.
func reportVerifyCheck(id string, results []ticket.VerifyResult, dir, implementation, acceptanceID string) error {
	res := results[0]
	if jsonOutput {
		prov := ticket.VerifyProvenance{Dir: dir, Implementation: implementation, AcceptanceID: acceptanceID}
		data, err := json.MarshalIndent(ticket.NewVerifyReport(id, prov, results), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		fmt.Printf("%s %s\n", strings.ToUpper(string(res.Status)), res.Criterion.Text)
		if res.Argv != nil {
			fmt.Printf("  argv: %q\n", res.Argv)
		}
		if res.Output != "" {
			for _, line := range strings.Split(res.Output, "\n") {
				fmt.Printf("  %s\n", line)
			}
		}
	}
	if res.Status == ticket.VerifyRunnable {
		return nil
	}
	return &exitError{code: verifyRefusedExit, err: fmt.Errorf("criterion %d %s", verifyCriterion, res.Status)}
}

// reportVerifyPolicy prints what a verify run would enforce right now for the
// project the repo resolves to, through the same project.VerifyAllow and
// project.VerifyTimeout RunVerify is handed. An unreadable config is the error,
// with no list, exactly as RunVerify refuses on it.
func reportVerifyPolicy() error {
	// A project that does not resolve still leaves the allow-list answerable,
	// and a config that does not load is exactly what the allow half reports, so
	// a resolution failure is the timeout's error rather than the command's.
	_, name, resolveErr := resolveTicketsDir()
	cfg, cfgErr := project.Load()
	report := ticket.VerifyPolicyReport{
		VerifyAllowReport:   ticket.NewVerifyAllowReport(),
		VerifyTimeoutReport: ticket.NewVerifyTimeoutReport(cfg, cfgErr, name),
	}
	if resolveErr != nil {
		report.VerifyTimeoutReport = ticket.VerifyTimeoutReport{TimeoutError: resolveErr.Error()}
	}
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if report.Allow != nil {
		fmt.Printf("verify_allow: %s\n", strings.Join(*report.Allow, ", "))
	} else {
		fmt.Printf("verify_allow: refused: %s\n", ticket.SanitizeControl(report.AllowError))
	}
	if report.Timeout != "" {
		fmt.Printf("verify_timeout (%s): %s\n", name, report.Timeout)
	} else {
		fmt.Printf("verify_timeout (%s): refused: %s\n", name, ticket.SanitizeControl(report.TimeoutError))
	}
	return nil
}
