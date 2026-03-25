package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/EnderRealm/ticket/internal/project"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure tk for first use",
	RunE:  runSetup,
}

func init() {
	setupCmd.Flags().String("central-root", "", "path to central ticket store")
	rootCmd.AddCommand(setupCmd)
}

func runSetup(cmd *cobra.Command, args []string) error {
	centralRoot, _ := cmd.Flags().GetString("central-root")

	if centralRoot == "" {
		// Interactive mode
		reader := bufio.NewReader(os.Stdin)
		fmt.Println("tk setup")
		fmt.Println()
		fmt.Println("Where should the central ticket store be located?")
		fmt.Println("This is the directory that holds tickets for all projects.")
		fmt.Println()
		fmt.Print("Central store path: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading input: %w", err)
		}
		centralRoot = strings.TrimSpace(line)
	}

	if centralRoot == "" {
		return fmt.Errorf("central store path is required")
	}

	// Expand ~ if present
	if strings.HasPrefix(centralRoot, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		centralRoot = filepath.Join(home, centralRoot[2:])
	}

	// Make absolute
	abs, err := filepath.Abs(centralRoot)
	if err != nil {
		return err
	}
	centralRoot = abs

	// Create directory if needed
	if err := os.MkdirAll(centralRoot, 0o755); err != nil {
		return fmt.Errorf("creating central store directory: %w", err)
	}

	// Load existing config to preserve project entries
	cfg, _ := project.Load()
	cfg.CentralRoot = centralRoot

	if err := project.Save(cfg); err != nil {
		return err
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(map[string]any{
			"central_root": centralRoot,
			"config_path":  configPath(),
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Println()
	fmt.Println("── Setup complete ──")
	fmt.Println()
	fmt.Printf("  central store: %s\n", centralRoot)
	fmt.Printf("  config:        %s\n", configPath())
	fmt.Println()
	fmt.Println("Next: run `tk init` from each project directory to register it.")
	fmt.Println()

	return nil
}

func configPath() string {
	path, err := project.ConfigPath()
	if err != nil {
		return "~/.ticket/config.yaml"
	}
	return path
}
