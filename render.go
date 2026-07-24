package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// render.go turns a WorktreeInfo into styled strings for the table cells and the
// detail pane. Ported from v1's format_* / build_* helpers; Rich markup becomes
// lipgloss styles from theme.go.

// highlightBackgroundSequences returns the ANSI open/reset pair lipgloss emits
// for the selection background under the active color profile. A \x00 sentinel is
// styled so the wrapping open and reset sequences can be split back out.
func highlightBackgroundSequences() (openSequence string, resetSequence string) {
	probe := lipgloss.NewStyle().Background(colorHighlight).Render("\x00")
	sentinel := strings.IndexByte(probe, 0)
	if sentinel < 0 {
		return "", ""
	}
	return probe[:sentinel], probe[sentinel+1:]
}

// spanBackgroundAcrossResets lays a background across a pre-colored row: it
// re-applies the open sequence after every embedded reset so the background
// survives each cell's own reset, while the cells keep their foreground colors.
func spanBackgroundAcrossResets(row string, openSequence string, resetSequence string) string {
	if openSequence == "" {
		return row
	}
	spanned := strings.ReplaceAll(row, resetSequence, resetSequence+openSequence)
	return openSequence + spanned + resetSequence
}

// HighlightSelectedRow applies the subtle selection background across a whole
// rendered table row, keeping the per-cell foreground colors intact.
func HighlightSelectedRow(row string) string {
	openSequence, resetSequence := highlightBackgroundSequences()
	return spanBackgroundAcrossResets(row, openSequence, resetSequence)
}

// CurrentSpinnerFrame returns the braille glyph for the given animation frame.
func CurrentSpinnerFrame(frame int) string {
	return string(SpinnerFrames[frame%len(SpinnerFrames)])
}

// DetermineBlinkPhase toggles roughly every 0.5s off wall-clock time, so the
// waiting-glyph blink stays steady regardless of the spinner tick rate.
func DetermineBlinkPhase(now time.Time) BlinkPhase {
	if (now.UnixMilli()/500)%2 == 0 {
		return BlinkVisible
	}
	return BlinkHidden
}

// DetermineDeletionEligibility reports whether a row is safe to flag "Deletable":
// a nested worktree or branch whose PR is merged, with no ahead commits or dirty
// files.
func DetermineDeletionEligibility(worktree WorktreeInfo) DeletionEligibility {
	if worktree.LockState == LockLocked {
		return EligibilityNotDeletable
	}
	if worktree.Kind != KindNestedWorktree && worktree.Kind != KindBranchOnly {
		return EligibilityNotDeletable
	}
	if worktree.Cleanliness == CleanlinessUnknown {
		return EligibilityNotDeletable
	}
	if worktree.PullRequestState != PullRequestMerged {
		return EligibilityNotDeletable
	}
	if worktree.AheadCount == 0 && worktree.DirtyFileCount == 0 {
		return EligibilityDeletable
	}
	return EligibilityNotDeletable
}

// isDeletable is a small predicate over the eligibility union for cell styling.
func isDeletable(worktree WorktreeInfo) bool {
	return DetermineDeletionEligibility(worktree) == EligibilityDeletable
}

// ClaudeStateGlyph returns the colored glyph for a Claude state.
func ClaudeStateGlyph(state ClaudeState, frame int, blink BlinkPhase) string {
	switch state {
	case ClaudeStateWorking:
		return styleWorking.Render(CurrentSpinnerFrame(frame))
	case ClaudeStateWaiting:
		if blink == BlinkVisible {
			return styleAttention.Render("!")
		}
		return styleAttention.Render(" ")
	case ClaudeStateIdle:
		return styleIdle.Render("○")
	default:
		return styleDim.Render("○")
	}
}

// FormatClaudeCell renders the Claude column for a row.
func FormatClaudeCell(worktree WorktreeInfo, now time.Time, frame int) string {
	if worktree.ClaudeState == ClaudeStateNone && worktree.ClaudeSessionIdentifier == "" {
		return styleDim.Render("—")
	}
	displayState := worktree.ClaudeState
	if displayState == ClaudeStateNone {
		displayState = ClaudeStateIdle
	}
	label := worktree.ClaudeSessionName
	if label == "" {
		label = string(displayState)
	}
	liveness := DetermineClaudeLiveness(worktree, now)
	if liveness == LivenessStale {
		return styleDim.Render("○ " + label + " (stale)")
	}
	blink := DetermineBlinkPhase(now)
	glyph := ClaudeStateGlyph(displayState, frame, blink)
	if displayState == ClaudeStateWaiting {
		return glyph + styleAttention.Render(" "+label)
	}
	return glyph + " " + label
}

// pullRequestStateWord returns the lowercase display word for a PR state.
func pullRequestStateWord(state PullRequestState) string {
	switch state {
	case PullRequestOpen:
		return "open"
	case PullRequestDraft:
		return "draft"
	case PullRequestMerged:
		return "merged"
	case PullRequestClosed:
		return "closed"
	case PullRequestUnknown:
		return "unknown"
	default:
		return ""
	}
}

// FormatPullRequestStateCell renders the State column, including the fetch spinner.
func FormatPullRequestStateCell(worktree WorktreeInfo, frame int) string {
	base := styleDim.Render("—")
	if worktree.PullRequestNumber != 0 {
		word := pullRequestStateWord(worktree.PullRequestState)
		base = lipglossForeground(StateColorFor(worktree.PullRequestState), word)
	}
	if worktree.PullRequestLoad != LoadInProgress {
		return base
	}
	spinner := styleAhead.Render(CurrentSpinnerFrame(frame))
	if worktree.PullRequestNumber == 0 {
		return spinner
	}
	return spinner + " " + base
}

// FormatReviewCell renders the Review column.
func FormatReviewCell(worktree WorktreeInfo) string {
	if worktree.PullRequestNumber == 0 {
		return styleDim.Render("—")
	}
	switch worktree.ReviewDecision {
	case ReviewApproved:
		return styleClean.Render("approved")
	case ReviewChangesRequested:
		return styleError.Render("changes")
	case ReviewPending:
		return styleDirty.Render("pending")
	default:
		return styleDim.Render("—")
	}
}

// FormatDirtyCell renders the Files column.
func FormatDirtyCell(worktree WorktreeInfo) string {
	if worktree.Cleanliness == CleanlinessUnknown {
		return styleDim.Render("…")
	}
	deletable := isDeletable(worktree)
	if worktree.Path == "" {
		return styleDim.Bold(deletable).Render("—")
	}
	text := "●" + strconv.Itoa(worktree.DirtyFileCount)
	if worktree.DirtyFileCount != 0 {
		return styleDirty.Bold(deletable).Render(text)
	}
	return styleDim.Bold(deletable).Render(text)
}

// FormatAheadBehindCell renders the ↑↓ column.
func FormatAheadBehindCell(worktree WorktreeInfo) string {
	if worktree.Cleanliness == CleanlinessUnknown {
		return styleDim.Render("…")
	}
	deletable := isDeletable(worktree)
	aheadStyle := styleDim
	if worktree.AheadCount != 0 {
		aheadStyle = styleAhead
	}
	behindStyle := styleDim
	if worktree.BehindCount != 0 {
		behindStyle = styleBehind
	}
	ahead := aheadStyle.Bold(deletable).Render("↑" + strconv.Itoa(worktree.AheadCount))
	behind := behindStyle.Bold(deletable).Render("↓" + strconv.Itoa(worktree.BehindCount))
	return ahead + " " + behind
}

// RenderBranchCell renders the Branch column with a kind-specific style. Nested
// worktree branches get a "* " prefix; permanent (root) worktrees get their fixed
// index as a "<n> " prefix in the same space.
func RenderBranchCell(worktree WorktreeInfo) string {
	label := worktree.Branch
	if label == "" {
		label = styleDim.Render("(detached)")
	}
	deletable := isDeletable(worktree)
	switch worktree.Kind {
	case KindNestedWorktree:
		return styleNestedBranch.Bold(deletable).Render("* " + label)
	case KindBranchOnly:
		return styleBranchOnly.Bold(deletable).Render(label)
	default:
		prefix := ""
		if indexLabel := PermanentWorktreeLabel(worktree); indexLabel != "" {
			prefix = indexLabel + " "
		}
		return styleRootBranch.Bold(deletable).Render(prefix + label)
	}
}

// FormatRecommendationCell renders the Recommendation column.
func FormatRecommendationCell(worktree WorktreeInfo) string {
	if worktree.LockState == LockLocked {
		return styleLocked.Render("locked")
	}
	if worktree.Cleanliness == CleanlinessUnknown {
		return styleDim.Render("…")
	}
	if isDeletable(worktree) {
		return styleClean.Bold(true).Render("Deletable")
	}
	return styleDim.Render("—")
}

// FormatStatusSummary renders the detail-pane one-line git summary.
func FormatStatusSummary(worktree WorktreeInfo) string {
	if worktree.Cleanliness == CleanlinessUnknown {
		return styleDim.Render("…loading")
	}
	parts := []string{}
	if worktree.Cleanliness == CleanlinessDirty {
		parts = append(parts, styleDirty.Render("●"+strconv.Itoa(worktree.DirtyFileCount)))
	}
	if worktree.AheadCount != 0 {
		parts = append(parts, styleAhead.Render("↑"+strconv.Itoa(worktree.AheadCount)))
	}
	if worktree.BehindCount != 0 {
		parts = append(parts, styleBehind.Render("↓"+strconv.Itoa(worktree.BehindCount)))
	}
	if len(parts) == 0 {
		return styleClean.Render("clean")
	}
	return strings.Join(parts, " ")
}

// FormatDockerStatus renders the detail-pane Docker line.
func FormatDockerStatus(worktree WorktreeInfo) string {
	switch worktree.ComposeStatus {
	case ComposeRunning:
		return styleClean.Render("● running") + "  project " + styleBold.Render(worktree.ComposeProjectName)
	case ComposeStopped:
		return styleDim.Render("○ not running") + "  project " + styleBold.Render(worktree.ComposeProjectName)
	default:
		return styleDim.Render("(no compose-worktree.sh in this worktree)")
	}
}

// BuildClaudeLines renders the detail-pane Claude section.
func BuildClaudeLines(worktree WorktreeInfo, now time.Time, frame int) []string {
	if worktree.ClaudeState == ClaudeStateNone && worktree.ClaudeSessionIdentifier == "" {
		return []string{
			styleHeading.Render("Claude"),
			"  " + styleDim.Render("(no recorded session)"),
		}
	}
	displayState := worktree.ClaudeState
	if displayState == ClaudeStateNone {
		displayState = ClaudeStateIdle
	}
	liveness := DetermineClaudeLiveness(worktree, now)
	blink := DetermineBlinkPhase(now)
	glyph := ClaudeStateGlyph(displayState, frame, blink)
	staleSuffix := ""
	if liveness == LivenessStale {
		staleSuffix = " " + styleDim.Render("(stale)")
	}
	waiting := displayState == ClaudeStateWaiting && liveness != LivenessStale
	statusWord := " " + string(displayState)
	if waiting {
		statusWord = styleAttention.Render(" " + string(displayState))
	}
	name := worktree.ClaudeSessionName
	if name == "" {
		name = styleDim.Render("(unnamed)")
	} else if waiting {
		name = styleAttention.Render(worktree.ClaudeSessionName)
	}
	sessionValue := worktree.ClaudeSessionIdentifier
	if sessionValue == "" {
		sessionValue = styleDim.Render("(none)")
	}
	return []string{
		styleHeading.Render("Claude"),
		"  " + styleBold.Render("Status:") + "   " + glyph + statusWord + staleSuffix,
		"  " + styleBold.Render("Name:") + "     " + name,
		"  " + styleBold.Render("Session:") + "  " + sessionValue,
	}
}

// BuildDetailView renders the full right-hand detail pane for a row.
func BuildDetailView(worktree WorktreeInfo, now time.Time, frame int) string {
	pullRequestLines := []string{"  " + styleDim.Render("(no PR found for this branch)")}
	if worktree.PullRequestNumber != 0 {
		pullRequestLines = []string{
			"  " + styleBold.Render("#"+strconv.Itoa(worktree.PullRequestNumber)) + "  " + worktree.PullRequestTitle,
			"  " + styleBold.Render("State:") + "   " + FormatPullRequestStateCell(worktree, frame),
			"  " + styleBold.Render("Review:") + "  " + FormatReviewCell(worktree),
			"  " + styleBold.Render("URL:") + "     " + worktree.PullRequestUrl,
		}
	}

	pathDisplay := worktree.Path
	if pathDisplay == "" {
		pathDisplay = styleDim.Render("(no worktree — branch only)")
	}
	filesDisplay := strconv.Itoa(worktree.DirtyFileCount)
	if worktree.Path == "" {
		filesDisplay = styleDim.Render("n/a (no worktree)")
	}
	upstreamDisplay := worktree.Upstream
	if upstreamDisplay == "" {
		upstreamDisplay = styleDim.Render("(none)")
	}
	headDisplay := worktree.Head
	if len(headDisplay) > 12 {
		headDisplay = headDisplay[:12]
	}
	if headDisplay == "" {
		headDisplay = styleDim.Render("(none)")
	}

	lines := []string{
		styleBold.Render("Branch"),
		"  " + RenderBranchCell(worktree),
		"",
	}
	lines = append(lines, BuildClaudeLines(worktree, now, frame)...)
	lines = append(lines,
		"",
		styleBold.Render("Path"),
		"  "+pathDisplay,
	)
	if worktree.LockState == LockLocked {
		lockLine := styleLocked.Render("🔒 locked")
		if worktree.LockReason != "" {
			lockLine += "  " + styleDim.Render(worktree.LockReason)
		}
		lines = append(lines, "", styleBold.Render("Lock"), "  "+lockLine)
	}
	lines = append(lines,
		"",
		styleBold.Render("Status")+"      "+FormatStatusSummary(worktree),
		styleBold.Render("Files changed")+"  "+filesDisplay,
		styleBold.Render("Ahead")+fmt.Sprintf("       %d    ", worktree.AheadCount)+styleBold.Render("Behind")+fmt.Sprintf(" %d", worktree.BehindCount),
		styleBold.Render("Upstream")+"    "+upstreamDisplay,
		styleBold.Render("HEAD")+"        "+headDisplay,
		styleBold.Render("Docker")+"      "+FormatDockerStatus(worktree),
		"",
		styleHeading.Render("Pull request"),
	)
	lines = append(lines, pullRequestLines...)
	lines = append(lines, "", styleHeading.Render("Recent commits"))
	if len(worktree.RecentCommits) > 0 {
		for _, commit := range worktree.RecentCommits {
			lines = append(lines, "  "+commit)
		}
	} else {
		lines = append(lines, "  "+styleDim.Render("(none)"))
	}
	if worktree.CollectionError != "" {
		lines = append(lines, "", styleError.Render("Error: "+worktree.CollectionError))
	}
	return strings.Join(lines, "\n")
}
