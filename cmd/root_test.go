package cmd

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// renderHelp is what `tk <command> --help` prints for cmd: cobra resolves the
// help function through the parents, so this runs the root's dispatch.
func renderHelp(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	defer cmd.SetOut(nil)
	cmd.HelpFunc()(cmd, nil)
	return buf.String()
}

func TestRootHelpListsEveryCommand(t *testing.T) {
	help := renderHelp(t, rootCmd)
	for _, c := range rootCmd.Commands() {
		// cobra's own help and completion commands are not tk's to index.
		if !c.IsAvailableCommand() || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		entry := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(c.Name()) + `\b`)
		if !entry.MatchString(help) {
			t.Errorf("root help has no entry for %q:\n%s", c.Name(), help)
		}
	}
}

func TestRootHelpFitsBudget(t *testing.T) {
	help := strings.TrimRight(renderHelp(t, rootCmd), "\n")
	lines := strings.Split(help, "\n")
	if len(lines) > 60 {
		t.Errorf("root help is %d lines, want at most 60", len(lines))
	}
	for i, line := range lines {
		if n := utf8.RuneCountInString(line); n > 80 {
			t.Errorf("root help line %d is %d columns, want at most 80: %q", i+1, n, line)
		}
	}
}
