package cmd

import (
	"fmt"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Print ticket workflow guide",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("# Ticket Workflow Guide")
		fmt.Println()
		fmt.Println("## Ticket Types")
		fmt.Println("- task: Default type for work items")
		fmt.Println("- epic: Container for related tasks, provides hierarchy")
		fmt.Println("- feature: New functionality")
		fmt.Println("- bug: Defect fix")
		fmt.Println("- chore: Maintenance work")
		fmt.Println()
		fmt.Println("## Stage Pipelines (default)")
		fmt.Print(ticket.PipelineDescription())
		fmt.Println()
		fmt.Println("Risk levels: low, normal, high, critical")
		fmt.Println("  Risk determines pipeline variant. Normal/high/critical add review stages.")
		fmt.Println("  Use `tk pipeline` to see current ticket distribution.")
		fmt.Println()
		fmt.Println("## Gate Checks")
		fmt.Println("Stage transitions enforce prerequisites:")
		fmt.Println("- Structural gates: checked automatically (description exists, review approved, etc.)")
		fmt.Println("- Agentic gates: requirements returned to the caller for evaluation")
		fmt.Println()
		fmt.Println("## Readiness Rules (tk ready)")
		fmt.Println("A ticket appears in `tk ready` when:")
		fmt.Println("1. Stage is not done")
		fmt.Println("2. All dependencies (deps) are at done stage")
		fmt.Println("3. Parent chain is not done (use --open to bypass)")
		fmt.Println()
		fmt.Println("## Stage Propagation")
		fmt.Println("When a child advances to done, the system checks siblings:")
		fmt.Println("- All siblings at done -> parent advances to done")
		fmt.Println("- All siblings at test or later -> parent advances to test")
		fmt.Println()
		fmt.Println("## Working Conventions")
		fmt.Println("1. Use `tk advance <id>` to move through the pipeline")
		fmt.Println("2. Use `tk review <id> --approve` to approve stage gates")
		fmt.Println("3. Create child tasks under epics with --parent")
		fmt.Println("4. Use `tk ready` to see what's available to work on")
		fmt.Println()
		fmt.Println("## Commit Format")
		fmt.Println("[ticket-id] Imperative description of change")
		fmt.Println("Example: [t-abc1] Add session refresh logic")
	},
}

func init() {
	rootCmd.AddCommand(workflowCmd)
}
