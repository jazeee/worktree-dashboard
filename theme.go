package main

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
)

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

	// Working styles cycle under the spinner so an in-progress pi session pulses
	// and catches the eye across a full screen of rows.
	stylesWorking = buildPulseStyles(0x5c3e00, 0xb87c00, 8)

	// Done styles for an idle pi session, one per PiDoneFadeStep: the green block
	// fades to black as the finished turn goes unread.
	stylesDone = buildBlockStyles([]lipgloss.Color{"#005f00", "#004000", "#002800", "#001200"})

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

// buildPulseStyles interpolates a triangle wave between two packed RGB values:
// steps shades up, then back down without repeating the endpoints.
func buildPulseStyles(low int, high int, steps int) []lipgloss.Style {
	backgrounds := make([]lipgloss.Color, 0, 2*steps-2)
	for step := 0; step < steps; step++ {
		backgrounds = append(backgrounds, blendColor(low, high, float64(step)/float64(steps-1)))
	}
	for step := steps - 2; step > 0; step-- {
		backgrounds = append(backgrounds, backgrounds[step])
	}
	return buildBlockStyles(backgrounds)
}

// blendColor mixes two packed 0xRRGGBB values, with ratio 0 giving low and 1 high.
func blendColor(low int, high int, ratio float64) lipgloss.Color {
	channel := func(shift uint) int {
		lowChannel := float64((low >> shift) & 0xff)
		highChannel := float64((high >> shift) & 0xff)
		return int(math.Round(lowChannel + (highChannel-lowChannel)*ratio))
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", channel(16), channel(8), channel(0)))
}

func buildBlockStyles(backgrounds []lipgloss.Color) []lipgloss.Style {
	styles := make([]lipgloss.Style, 0, len(backgrounds))
	for _, background := range backgrounds {
		styles = append(styles, lipgloss.NewStyle().Foreground(colorWhite).Background(background).Bold(true))
	}
	return styles
}
