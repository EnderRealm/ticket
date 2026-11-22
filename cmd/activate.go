package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var activateCmd = &cobra.Command{
	Use:   "activate",
	Short: "Activate Root and cross-project parents in the central store",
	Long: "Turn on the catalog features the central store's writers are gated on — root-namespace, which allows writes to _root, " +
		"and cross-project-parents, which lets a leaf name an epic in another namespace — or only those named with --feature. " +
		"Activation catalogues every namespace the inventory finds (Root as root, a registered project as project, any other directory as legacy; " +
		"an entry already in catalog.yaml keeps its kind), writes each namespace's .namespace marker, and adds the features to required_features. " +
		"catalog.yaml is edited in place, keeping its existing entries and comments, and replaced atomically. " +
		"No ticket file is read to be rewritten, no directory is moved and no registration is invented: adoption into a foreign epic is a later, explicit edit.\n\n" +
		"Everything is checked before anything is written, and any failed check refuses the whole activation, naming every cause: " +
		"a catalog that cannot be read or requires a feature this tk lacks, a requested feature this tk does not implement, no tickets directory, " +
		"a config entry binding _root or a _root directory not catalogued as Root, a catalogued namespace with no directory, " +
		"a namespace path that is not a real directory or a marker that is not a regular file, and — when cross-project-parents is being turned on — " +
		"any ticket already naming a parent in another namespace, or an unreadable file that could, since the feature would make it that epic's child without review.\n\n" +
		"--dry-run reports the plan and the refusals and writes nothing. Re-running an activated store writes nothing. " +
		"Activation is a store write like any other and lands on other machines through `tk sync`: replace every tk binary, embedded library and long-lived process sharing the store first, " +
		"because a binary released before the catalog guard ignores the markers entirely. See docs/rollout-root-cross-project.md.",
	Example: "  tk activate --dry-run\n  tk activate\n  tk activate --feature root-namespace --json",
	Args:    cobra.NoArgs,
	RunE:    runActivate,
}

func init() {
	registerActivateFlags(activateCmd)
	rootCmd.AddCommand(activateCmd)
}

func registerActivateFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "report what activation would write and refuse on, writing nothing")
	cmd.Flags().StringArray("feature", nil, "feature to require, repeatable (default: "+strings.Join(ticket.DefaultActivationFeatures, ", ")+")")
}

func runActivate(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	features, _ := cmd.Flags().GetStringArray("feature")

	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("activate requires a configured central store: %w", err)
	}
	cfg, err := project.Load()
	if err != nil {
		return fmt.Errorf("load ticket config: %w", err)
	}
	act, err := ticket.Activate(root, cfg, features, dryRun)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if jsonOutput {
		data, err := json.MarshalIndent(act, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	for _, d := range act.Inventory.Diagnostics {
		fmt.Fprintf(out, "inventory: %s\n", d)
	}
	if !act.Changed() {
		fmt.Fprintf(out, "%s is already activated for %s: nothing to write\n", root, strings.Join(featuresOrDefault(features), ", "))
		return nil
	}
	verb := "Activated"
	if act.DryRun {
		verb = "Would activate"
	}
	fmt.Fprintf(out, "%s %s\n", verb, root)
	fmt.Fprintf(out, "  required features added: %s\n", listOrNone(act.Features))
	var catalogued []string
	for _, ns := range act.Catalogued {
		catalogued = append(catalogued, fmt.Sprintf("%s (%s)", ns.Name, ns.Kind))
	}
	fmt.Fprintf(out, "  namespaces catalogued:   %s\n", listOrNone(catalogued))
	fmt.Fprintf(out, "  markers written:         %s\n", listOrNone(act.Markers))
	if act.DryRun {
		fmt.Fprintln(out, "Dry run: nothing was written.")
		return nil
	}
	fmt.Fprintln(out, "No ticket file was changed. Run `tk sync` to publish the activation to every machine sharing the store.")
	return nil
}

func featuresOrDefault(features []string) []string {
	if len(features) == 0 {
		return ticket.DefaultActivationFeatures
	}
	return features
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
