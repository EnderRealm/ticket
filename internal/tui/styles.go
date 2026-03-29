package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/EnderRealm/ticket/pkg/ticket"
)

// ─── Color Palette ──────────────────────────────────────────────────────────
//
// Semantic color names mapped to adaptive terminal colors.
// Uses lipgloss.AdaptiveColor for light/dark terminal support.

var (
	// Core palette
	colorRed     = lipgloss.Color("1")
	colorGreen   = lipgloss.Color("2")
	colorYellow  = lipgloss.Color("3")
	colorBlue    = lipgloss.Color("4")
	colorMagenta = lipgloss.Color("5")
	colorCyan    = lipgloss.Color("6")
	colorWhite   = lipgloss.Color("7")
	colorGray    = lipgloss.Color("8")
	colorBlack   = lipgloss.Color("0")

	// Surfaces
	colorSurface = lipgloss.Color("237") // dark gray background for selection
	// Semantic aliases
	colorDanger  = colorRed
	colorSuccess = colorGreen
	colorWarning = colorYellow
	colorInfo    = colorBlue
	colorAccent  = colorCyan
	colorMuted   = colorGray
)

// ─── Semantic Color Maps ────────────────────────────────────────────────────
//
// Maps from domain types to colors. Single source of truth for
// how priorities, types, stages, and reviews are colored.

var (
	StageColors = map[ticket.Stage]lipgloss.Color{
		ticket.StageBacklog:      colorGray,
		ticket.StageTriage:       colorWhite,
		ticket.StageSpec:         colorCyan,
		ticket.StageDesign:       colorMagenta,
		ticket.StageDesignReview: colorMagenta,
		ticket.StageImplement:    colorYellow,
		ticket.StageCodeReview:   colorYellow,
		ticket.StageTest:         colorBlue,
		ticket.StageVerify:       colorGreen,
		ticket.StageDone:         colorGray,
	}

	ReviewColors = map[ticket.ReviewState]lipgloss.Color{
		ticket.ReviewPending:  colorYellow,
		ticket.ReviewApproved: colorGreen,
		ticket.ReviewRejected: colorRed,
	}

	PriorityColors = map[int]lipgloss.Color{
		0: colorRed,    // critical
		1: colorYellow, // high
		2: colorWhite,  // normal
		3: colorGray,   // low
		4: colorGray,   // backlog
	}

	TypeColors = map[ticket.TicketType]lipgloss.Color{
		ticket.TypeBug:     colorRed,
		ticket.TypeFeature: colorGreen,
		ticket.TypeEpic:    colorMagenta,
		ticket.TypeTask:    colorBlue,
		ticket.TypeChore:   colorGray,
	}
)

// ─── Border Styles ──────────────────────────────────────────────────────────

var (
	BorderRounded = lipgloss.RoundedBorder()
	BorderNormal  = lipgloss.NormalBorder()
)

// ─── Component Styles ───────────────────────────────────────────────────────
//
// Reusable styles for common UI components. Named by role, not location.

var (
	// Text styles
	StyleBold      = lipgloss.NewStyle().Bold(true)
	StyleDim       = lipgloss.NewStyle().Foreground(colorMuted)
	StyleAccent    = lipgloss.NewStyle().Foreground(colorAccent)
	StyleDanger    = lipgloss.NewStyle().Foreground(colorDanger)
	StyleSuccess   = lipgloss.NewStyle().Foreground(colorSuccess)
	StyleWarning   = lipgloss.NewStyle().Foreground(colorWarning)
	StyleInfo      = lipgloss.NewStyle().Foreground(colorInfo)
	StyleUnderline = lipgloss.NewStyle().Underline(true)

	// Tab bar
	StyleTabActive = lipgloss.NewStyle().Bold(true).Underline(true)
	StyleTabDim    = lipgloss.NewStyle().Foreground(colorMuted)

	// List rows
	StyleRow         = lipgloss.NewStyle()
	StyleRowSelected = lipgloss.NewStyle().Bold(true).Background(colorSurface)

	// Field labels (detail view, form)
	StyleFieldKey = lipgloss.NewStyle().Bold(true).Foreground(colorInfo).Width(14)
	StyleFieldVal = lipgloss.NewStyle()

	// Section headers
	StyleSection   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	StyleTimestamp = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)

	// Input
	StyleInputLabel = lipgloss.NewStyle().Bold(true).Foreground(colorWarning)
	StyleInputText  = lipgloss.NewStyle().Foreground(colorWhite)
	StyleCursor     = lipgloss.NewStyle().Foreground(colorWarning)
	StyleTextCursor = lipgloss.NewStyle().Foreground(colorBlack).Background(colorWarning)

	// Filter / search
	StyleFilter = lipgloss.NewStyle().Foreground(colorWarning)

	// Help bar
	StyleHelp = lipgloss.NewStyle().Foreground(colorMuted)

	// Cards (pipeline)
	StyleCard         = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	StyleCardSelected = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1).
				Bold(true).Background(colorSurface)
	StyleColHeader = lipgloss.NewStyle().Bold(true).Underline(true)

	// Overlay
	StyleOverlayBorder = lipgloss.NewStyle().
				Border(BorderRounded).
				BorderForeground(colorMuted)
)

// ─── Badge Helpers ──────────────────────────────────────────────────────────
//
// Render small colored badges for priority, type, stage, and review.

// PriorityBadge renders a priority label like "P0" in the appropriate color.
func PriorityBadge(p int) string {
	c, ok := PriorityColors[p]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(priorityLabel(p))
}

func priorityLabel(p int) string {
	switch p {
	case 0:
		return "P0"
	case 1:
		return "P1"
	case 2:
		return "P2"
	case 3:
		return "P3"
	case 4:
		return "P4"
	default:
		return "P?"
	}
}

// TypeBadge renders a ticket type in the appropriate color.
func TypeBadge(t ticket.TicketType) string {
	c, ok := TypeColors[t]
	if !ok {
		c = colorWhite
	}
	label := string(t)
	if len(label) > 5 {
		label = label[:5]
	}
	return lipgloss.NewStyle().Foreground(c).Render(label)
}

// StageBadge renders a pipeline stage in the appropriate color.
func StageBadge(s ticket.Stage) string {
	c, ok := StageColors[s]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(string(s))
}

// ReviewBadge renders a review state with indicator symbol.
func ReviewBadge(r ticket.ReviewState) string {
	c, ok := ReviewColors[r]
	if !ok {
		c = colorWhite
	}
	var symbol string
	switch r {
	case ticket.ReviewPending:
		symbol = "●"
	case ticket.ReviewApproved:
		symbol = "✓"
	case ticket.ReviewRejected:
		symbol = "✗"
	default:
		symbol = "?"
	}
	return lipgloss.NewStyle().Foreground(c).Render(symbol)
}

// ─── Colorize Helpers ───────────────────────────────────────────────────────
//
// Apply domain-appropriate color to arbitrary text.

// ColorPriority renders text in the priority's color.
func ColorPriority(p int, text string) string {
	c, ok := PriorityColors[p]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// ColorType renders text in the ticket type's color.
func ColorType(t ticket.TicketType, text string) string {
	c, ok := TypeColors[t]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// ColorStage renders text in the stage's color.
func ColorStage(s ticket.Stage, text string) string {
	c, ok := StageColors[s]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// ColorReview renders text in the review state's color.
func ColorReview(r ticket.ReviewState, text string) string {
	c, ok := ReviewColors[r]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(text)
}

// ─── Layout Helpers ─────────────────────────────────────────────────────────

// ProgressBar renders a simple text progress bar: ━━━━━━━━━━ (filled/total).
func ProgressBar(done, total, width int) string {
	if total == 0 || width <= 0 {
		return ""
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	empty := width - filled
	bar := strings.Repeat("━", filled) + strings.Repeat("─", empty)

	var c lipgloss.Color
	switch {
	case done == total:
		c = colorSuccess
	case done*2 >= total:
		c = colorWarning
	default:
		c = colorMuted
	}
	return lipgloss.NewStyle().Foreground(c).Render(bar)
}
