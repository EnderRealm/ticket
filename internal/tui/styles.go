package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/charmbracelet/lipgloss"
)

// emDash is the placeholder rendered when a time value is absent.
const emDash = "—"

// relDuration renders a duration as a compact relative span, e.g. "now", "5m",
// "2h", "3d", "5w", "1y". Kept short (≤4 chars typical) for narrow columns.
func relDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// shortDate renders a compact local date as MM-DD, or an em-dash for zero time.
func shortDate(t time.Time) string {
	if t.IsZero() {
		return emDash
	}
	return t.Local().Format("01-02")
}

// renderColumnHeader renders the leading-padded column header row, underlining
// the active sort column and appending an arrow for its direction.
func renderColumnHeader(cols []column, sortIdx int, dir sortDir) string {
	hdrStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	var b strings.Builder
	b.WriteString("  ")
	for i, c := range cols {
		label := c.name
		style := hdrStyle
		if i == sortIdx {
			arrow := "↑"
			if dir == desc {
				arrow = "↓"
			}
			label += arrow
			style = style.Underline(true)
		}
		rendered := style.Render(label)
		if c.width == 0 {
			b.WriteString(rendered)
		} else {
			b.WriteString(padRight(rendered, c.width))
		}
	}
	return b.String()
}

// ─── Color Palette ──────────────────────────────────────────────────────────
//
// Semantic color names mapped to adaptive terminal colors.
// Uses lipgloss.AdaptiveColor for light/dark terminal support.

var (
	// Core palette — muted 256-color values for a modern dark theme.
	colorRed     = lipgloss.Color("#d75f5f") // soft red
	colorGreen   = lipgloss.Color("#87af5f") // muted green
	colorYellow  = lipgloss.Color("#d7af5f") // warm amber
	colorBlue    = lipgloss.Color("#5f87af") // slate blue
	colorMagenta = lipgloss.Color("#af87af") // muted purple
	colorCyan    = lipgloss.Color("#5fafaf") // teal
	colorWhite   = lipgloss.Color("#d0d0d0") // soft white
	colorGray    = lipgloss.Color("#767676") // medium gray
	colorBlack   = lipgloss.Color("#303030") // near-black

	// Surfaces
	colorSurface = lipgloss.Color("#4a4a4a") // selection background — visible against dark terminals
	colorSubtle  = lipgloss.Color("#4e4e4e") // subtle borders/separators
	colorBadgeBg = lipgloss.Color("#444444") // badge background

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
	StatusColors = map[ticket.Status]lipgloss.Color{
		ticket.StatusBacklog: colorGray,
		ticket.StatusReady:   colorCyan,
		ticket.StatusOpen:    colorYellow,
		ticket.StatusDone:    colorGreen,
		ticket.StatusClosed:  colorGray,
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
	StyleTabActive = lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Underline(true).UnderlineSpaces(true)
	StyleTabDim    = lipgloss.NewStyle().Foreground(colorMuted)

	// List rows
	StyleRow         = lipgloss.NewStyle()
	StyleRowSelected = lipgloss.NewStyle().Bold(true).Background(colorSurface)

	// Selector chips (form Type/Priority/Status) — neutral, focus-aware.
	StyleChipUnselected      = lipgloss.NewStyle().Foreground(colorMuted)
	StyleChipSelected        = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	StyleChipSelectedFocused = lipgloss.NewStyle().Bold(true).Foreground(colorBlack).Background(colorAccent).Padding(0, 1)

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

// PriorityBadge renders "P0" as a colored pill badge.
func PriorityBadge(p int) string {
	return priorityBadge(p, false)
}

func priorityBadge(p int, selected bool) string {
	c, ok := PriorityColors[p]
	if !ok {
		c = colorWhite
	}
	label := priorityLabel(p)
	if selected {
		return lipgloss.NewStyle().
			Foreground(c).
			Background(colorSurface).
			Bold(true).
			Padding(0, 1).
			Render(label)
	}
	return lipgloss.NewStyle().
		Foreground(colorBlack).
		Background(c).
		Bold(true).
		Padding(0, 1).
		Render(label)
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

// TypeBadge renders a ticket type as a fixed-width colored pill.
func TypeBadge(t ticket.TicketType) string {
	return typeBadge(t, false)
}

func typeBadge(t ticket.TicketType, selected bool) string {
	c, ok := TypeColors[t]
	if !ok {
		c = colorWhite
	}
	label := fmt.Sprintf("%-5s", strings.ToUpper(shortType(t)))
	bg := colorBadgeBg
	if selected {
		bg = colorSurface
	}
	return lipgloss.NewStyle().
		Foreground(c).
		Background(bg).
		Padding(0, 1).
		Render(label)
}

// StatusBadge renders a status in the appropriate color.
func StatusBadge(s ticket.Status) string {
	c, ok := StatusColors[s]
	if !ok {
		c = colorWhite
	}
	return lipgloss.NewStyle().Foreground(c).Render(string(s))
}

// IDSuffix returns just the 4-character hex suffix of a ticket ID.
func IDSuffix(id string) string {
	if idx := strings.LastIndex(id, "-"); idx >= 0 && idx+1 < len(id) {
		return id[idx+1:]
	}
	return id
}

// ─── Layout Utilities ───────────────────────────────────────────────────────

// padRight pads a rendered string to a target visual width.
func padRight(s string, width int) string {
	return padRightBg(s, width, nil)
}

// padRightBg pads with a background style applied to the padding spaces.
func padRightBg(s string, width int, bg *lipgloss.Style) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	pad := strings.Repeat(" ", width-w)
	if bg != nil {
		pad = bg.Render(pad)
	}
	return s + pad
}

// ─── Text Helpers ───────────────────────────────────────────────────────────

// shortType returns a compact label for a ticket type.
func shortType(t ticket.TicketType) string {
	switch t {
	case ticket.TypeFeature:
		return "feat"
	case ticket.TypeBug:
		return "bug"
	case ticket.TypeEpic:
		return "epic"
	default:
		return string(t)
	}
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
