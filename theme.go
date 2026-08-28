package main

import "github.com/charmbracelet/lipgloss"

// theme.go centralizes every color and text style. v1 used Rich markup like
// "[yellow]…[/]"; here those become named lipgloss styles so the palette lives
// in one place and the rest of the code reads declaratively.

// Palette — ANSI 16 indices so the app adapts to the terminal's theme, plus one
// hex accent for the attention background (matches v1's #870000).
var (
	colorYellow = lipgloss.Color("3")
	colorCyan   = lipgloss.Color("6")
	colorGreen  = lipgloss.Color("2")
	colorRed    = lipgloss.Color("1")
	colorGrey   = lipgloss.Color("8")
	colorWhite  = lipgloss.Color("15")
	colorAccent = lipgloss.Color("12")

	attentionBackground = lipgloss.Color("#870000")
)

// colorHighlight is the subtle background laid across the selected table row. It
// adapts to the terminal so it stays a quiet, low-contrast tint on both themes.
var colorHighlight = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}

// Text styles. Named for intent, not appearance, so a palette change stays local.
var (
	styleDim     = lipgloss.NewStyle().Foreground(colorGrey)
	styleClean   = lipgloss.NewStyle().Foreground(colorGreen)
	styleDirty   = lipgloss.NewStyle().Foreground(colorYellow)
	styleAhead   = lipgloss.NewStyle().Foreground(colorCyan)
	styleBehind  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleWorking = lipgloss.NewStyle().Foreground(colorYellow)
	styleIdle    = lipgloss.NewStyle().Foreground(colorGreen)
	styleError   = lipgloss.NewStyle().Foreground(colorRed)
	styleLocked  = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleHeading = lipgloss.NewStyle().Bold(true).Underline(true)

	// Row-kind label styles.
	styleNestedBranch = lipgloss.NewStyle().Foreground(colorCyan)
	styleBranchOnly   = lipgloss.NewStyle().Foreground(colorGreen).Italic(true)
	styleRootBranch   = lipgloss.NewStyle().Foreground(colorGrey).Italic(true)

	// Attention style for a waiting pi session (inverse dark-red block).
	styleAttention = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(attentionBackground).
			Bold(true)

	// Detail-pane border and counts bar.
	styleDetailBorder = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorAccent).
				Padding(0, 2)

	styleCountsBar = lipgloss.NewStyle().Padding(0, 1)

	styleDialogBox = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2)
)

// lipglossForeground renders text in a specific foreground color. Used where the
// color is chosen at runtime (e.g. per pull-request state).
func lipglossForeground(color lipgloss.Color, text string) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

// StateColorFor maps a pull-request state to its display color, matching v1.
func StateColorFor(pullRequestState PullRequestState) lipgloss.Color {
	switch pullRequestState {
	case PullRequestOpen:
		return colorGreen
	case PullRequestDraft:
		return colorYellow
	case PullRequestMerged:
		return lipgloss.Color("5")
	case PullRequestClosed:
		return colorRed
	default:
		return colorWhite
	}
}
