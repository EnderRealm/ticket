package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
)

// ANSI escape codes for terminal color output.
// All colors match the TUI (internal/tui/pipeline.go).
const (
	ansiBold     = "\033[1m"
	ansiRed      = "\033[31m"  // TUI color "1"
	ansiGreen    = "\033[32m"  // TUI color "2"
	ansiYellow   = "\033[33m"  // TUI color "3"
	ansiBlue     = "\033[34m"  // TUI color "4"
	ansiMagenta  = "\033[35m"  // TUI color "5"
	ansiCyan     = "\033[36m"  // TUI color "6"
	ansiWhite    = "\033[37m"  // TUI color "7"
	ansiGray     = "\033[90m"  // TUI color "8"
	ansiBoldCyan = "\033[1;36m"
	ansiReset    = "\033[0m"
)

// colorEnabled is set at init time based on TTY detection and NO_COLOR.
var colorEnabled bool

func init() {
	isTTY := false
	if fi, err := os.Stdout.Stat(); err == nil {
		isTTY = fi.Mode()&os.ModeCharDevice != 0
	}
	colorEnabled = isTTY && os.Getenv("NO_COLOR") == ""
}

// colorize wraps s in the given ANSI code if color is enabled.
func colorize(s, code string) string {
	if !colorEnabled {
		return s
	}
	return code + s + ansiReset
}

// colorGroupHeader returns a bold cyan group header string.
func colorGroupHeader(s string) string {
	return colorize(s, ansiBoldCyan)
}

// colorizePriority matches TUI priorityColors: 0=red, 1=yellow, 2=white, 3/4=gray.
func colorizePriority(s string, priority int) string {
	switch priority {
	case 0:
		return colorize(s, ansiRed)
	case 1:
		return colorize(s, ansiYellow)
	case 2:
		return colorize(s, ansiWhite)
	default:
		return colorize(s, ansiGray)
	}
}

// colorizeType matches TUI typeColors: bug=red, feature=green, epic=magenta, task=blue, chore=gray.
func colorizeType(s string, t ticket.TicketType) string {
	switch t {
	case ticket.TypeBug:
		return colorize(s, ansiRed)
	case ticket.TypeFeature:
		return colorize(s, ansiGreen)
	case ticket.TypeEpic:
		return colorize(s, ansiMagenta)
	case ticket.TypeTask:
		return colorize(s, ansiBlue)
	case ticket.TypeChore:
		return colorize(s, ansiGray)
	default:
		return s
	}
}

// colorizeStage matches TUI stageColors.
func colorizeStage(s string, stage ticket.Stage) string {
	switch stage {
	case ticket.StageTriage:
		return colorize(s, ansiWhite)
	case ticket.StageSpec:
		return colorize(s, ansiCyan)
	case ticket.StageDesign, ticket.StageDesignReview:
		return colorize(s, ansiMagenta)
	case ticket.StageImplement, ticket.StageCodeReview:
		return colorize(s, ansiYellow)
	case ticket.StageTest:
		return colorize(s, ansiBlue)
	case ticket.StageVerify:
		return colorize(s, ansiGreen)
	case ticket.StageDone:
		return colorize(s, ansiGray)
	default:
		return s
	}
}

type column struct {
	header string
	value  func(*ticket.Ticket) string
	width  int
}

type tableWriter struct {
	columns []column
}

// newTableWriter creates a table writer, excluding columns whose headers
// appear in skip.
func newTableWriter(skip ...string) *tableWriter {
	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}

	allCols := []column{
		{header: "ID", value: func(t *ticket.Ticket) string { return t.ID }},
		{header: "P", value: func(t *ticket.Ticket) string { return fmt.Sprintf("P%d", t.Priority) }},
		{header: "TYPE", value: func(t *ticket.Ticket) string { return string(t.Type) }},
		{header: "STAGE", value: func(t *ticket.Ticket) string { return string(t.Stage) }},
		{header: "TITLE", value: func(t *ticket.Ticket) string {
			s := t.Title
			if n := len(t.Deps); n == 1 {
				s += " (1 dep)"
			} else if n > 1 {
				s += fmt.Sprintf(" (%d deps)", n)
			}
			return s
		}},
	}

	var cols []column
	for _, c := range allCols {
		if !skipSet[c.header] {
			cols = append(cols, c)
		}
	}
	return &tableWriter{columns: cols}
}

func (tw *tableWriter) computeWidths(tickets []*ticket.Ticket) {
	for i := range tw.columns {
		tw.columns[i].width = len(tw.columns[i].header)
	}
	for _, t := range tickets {
		for i := range tw.columns {
			if tw.columns[i].header == "TITLE" {
				continue // TITLE is last column, no padding needed
			}
			w := len(tw.columns[i].value(t))
			if w > tw.columns[i].width {
				tw.columns[i].width = w
			}
		}
	}
}

// Print computes column widths from tickets, then renders header + rows.
func (tw *tableWriter) Print(tickets []*ticket.Ticket) {
	tw.computeWidths(tickets)
	tw.PrintGroup(tickets)
}

// PrintGroup renders header + rows using pre-computed column widths.
// Call computeWidths before this to set widths from a broader ticket set.
func (tw *tableWriter) PrintGroup(tickets []*ticket.Ticket) {
	tw.printHeader()
	for _, t := range tickets {
		tw.printRow(t)
	}
}

func (tw *tableWriter) printHeader() {
	var parts []string
	for i, col := range tw.columns {
		s := col.header
		if i < len(tw.columns)-1 {
			s = fmt.Sprintf("%-*s", col.width, s)
		}
		parts = append(parts, colorize(s, ansiBold))
	}
	fmt.Println(strings.Join(parts, "  "))
}

func (tw *tableWriter) printRow(t *ticket.Ticket) {
	var parts []string
	for i, col := range tw.columns {
		val := col.value(t)
		display := val

		switch col.header {
		case "P":
			display = colorizePriority(val, t.Priority)
		case "TYPE":
			display = colorizeType(val, t.Type)
		case "STAGE":
			display = colorizeStage(val, t.Stage)
		}

		if i < len(tw.columns)-1 {
			// Pad the raw value, then replace with colorized version.
			padded := fmt.Sprintf("%-*s", col.width, val)
			if display != val {
				// Replace the raw value portion with the colorized version,
				// keeping the trailing padding.
				padding := padded[len(val):]
				display = display + padding
			} else {
				display = padded
			}
		}

		parts = append(parts, display)
	}
	fmt.Println(strings.Join(parts, "  "))
}
