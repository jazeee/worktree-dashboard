package main

import "time"

// This file defines the domain model. Following the project's naming philosophy,
// booleans are replaced by string-union types (modeled on TypeScript
// `type Thing = 'A' | 'B'`) so every state is named and self-describing.

// WorktreeKind distinguishes the three row categories the dashboard shows.
type WorktreeKind string

const (
	KindNestedWorktree WorktreeKind = "NestedWorktree"
	KindRootWorktree   WorktreeKind = "RootWorktree"
	KindBranchOnly     WorktreeKind = "BranchOnly"
)

// WorktreeRole marks the primary worktree (first in `git worktree list`) apart
// from the linked ones. Replaces v1's is_primary boolean.
type WorktreeRole string

const (
	RolePrimary WorktreeRole = "Primary"
	RoleLinked  WorktreeRole = "Linked"
)

// WorktreeShape replaces v1's is_bare boolean.
type WorktreeShape string

const (
	ShapeNormal WorktreeShape = "Normal"
	ShapeBare   WorktreeShape = "Bare"
)

// HeadState replaces v1's is_detached boolean.
type HeadState string

const (
	HeadOnBranch HeadState = "OnBranch"
	HeadDetached HeadState = "Detached"
)

// LockState reflects `git worktree`'s locked annotation: a locked worktree is one
// git refuses to prune or move until it is unlocked.
type LockState string

const (
	LockUnlocked LockState = "Unlocked"
	LockLocked   LockState = "Locked"
)

// WorkingTreeCleanliness replaces v1's tri-state is_clean (None/True/False).
type WorkingTreeCleanliness string

const (
	CleanlinessUnknown WorkingTreeCleanliness = "Unknown"
	CleanlinessClean   WorkingTreeCleanliness = "Clean"
	CleanlinessDirty   WorkingTreeCleanliness = "Dirty"
)

// PullRequestState folds v1's separate pr_state + is_draft into one value.
type PullRequestState string

const (
	PullRequestNone    PullRequestState = "None"
	PullRequestOpen    PullRequestState = "Open"
	PullRequestDraft   PullRequestState = "Draft"
	PullRequestMerged  PullRequestState = "Merged"
	PullRequestClosed  PullRequestState = "Closed"
	PullRequestUnknown PullRequestState = "Unknown"
)

// ReviewDecision mirrors gh's reviewDecision field as a named union.
type ReviewDecision string

const (
	ReviewNone             ReviewDecision = "None"
	ReviewApproved         ReviewDecision = "Approved"
	ReviewChangesRequested ReviewDecision = "ChangesRequested"
	ReviewPending          ReviewDecision = "Pending"
)

// PullRequestLoad tracks whether a `gh` fetch is in flight for a row (drives the
// State-column spinner). Replaces v1's pr_loading_state.
type PullRequestLoad string

const (
	LoadIdle       PullRequestLoad = "Idle"
	LoadInProgress PullRequestLoad = "InProgress"
)

// ComposeStatus replaces v1's compose_running boolean plus the "no script" case.
type ComposeStatus string

const (
	ComposeNotConfigured ComposeStatus = "NotConfigured"
	ComposeRunning       ComposeStatus = "Running"
	ComposeStopped       ComposeStatus = "Stopped"
)

// ClaudeState mirrors the `state=` token in a .claude-session-state file. Its
// values are the raw file tokens (an external contract) so parsing is direct.
type ClaudeState string

const (
	ClaudeStateNone    ClaudeState = ""
	ClaudeStateStart   ClaudeState = "start"
	ClaudeStateWorking ClaudeState = "working"
	ClaudeStateWaiting ClaudeState = "waiting"
	ClaudeStateIdle    ClaudeState = "idle"
	ClaudeStateEnded   ClaudeState = "ended"
)

// ClaudeLiveness is derived from ClaudeState plus the record's age. Replaces the
// is_stale boolean computed ad hoc throughout v1.
type ClaudeLiveness string

const (
	LivenessInactive ClaudeLiveness = "Inactive"
	LivenessActive   ClaudeLiveness = "Active"
	LivenessStale    ClaudeLiveness = "Stale"
)

// DeletionEligibility replaces v1's is_deletable boolean.
type DeletionEligibility string

const (
	EligibilityDeletable    DeletionEligibility = "Deletable"
	EligibilityNotDeletable DeletionEligibility = "NotDeletable"
)

// LayoutOrientation drives the responsive split-pane vs stacked layout.
type LayoutOrientation string

const (
	OrientationSideBySide LayoutOrientation = "SideBySide"
	OrientationStacked    LayoutOrientation = "Stacked"
)

// BlinkPhase replaces v1's _blink_on boolean for the waiting-state glyph.
type BlinkPhase string

const (
	BlinkVisible BlinkPhase = "Visible"
	BlinkHidden  BlinkPhase = "Hidden"
)

// PollActivity is the overlap guard for the worktree/PR poll tiers. Replaces
// v1's poll_in_flight / pr_in_flight booleans.
type PollActivity string

const (
	PollIdle    PollActivity = "Idle"
	PollRunning PollActivity = "Running"
)

// MouseCapture reflects whether the app is intercepting mouse events. When on,
// click-to-select-row and wheel-scroll work but the terminal's own drag-to-select
// is suppressed; turning it off releases the mouse so text can be selected and
// copied with the native terminal selection.
type MouseCapture string

const (
	MouseCaptureOn  MouseCapture = "On"
	MouseCaptureOff MouseCapture = "Off"
)

// LifecyclePhase replaces v1's shutting_down boolean.
type LifecyclePhase string

const (
	LifecycleRunning      LifecyclePhase = "Running"
	LifecycleShuttingDown LifecyclePhase = "ShuttingDown"
)

// WorktreeInfo is the full per-row record: a worktree, a branch-only entry, or a
// root worktree. An empty Path means "branch with no worktree".
type WorktreeInfo struct {
	Path           string
	Branch         string
	Head           string
	Kind           WorktreeKind
	RepositoryRoot string
	Role           WorktreeRole
	Shape          WorktreeShape
	HeadState      HeadState
	LockState      LockState
	LockReason     string

	Cleanliness    WorkingTreeCleanliness
	DirtyFileCount int
	AheadCount     int
	BehindCount    int
	Upstream       string

	LastCommitSubject  string
	LastCommitRelative string
	RecentCommits      []string

	PullRequestNumber int
	PullRequestUrl    string
	PullRequestState  PullRequestState
	PullRequestTitle  string
	ReviewDecision    ReviewDecision
	PullRequestLoad   PullRequestLoad

	ComposeScriptPath  string
	ComposeProjectName string
	ComposeStatus      ComposeStatus

	ClaudeSessionIdentifier string
	ClaudeState             ClaudeState
	ClaudeStateUpdatedAt    time.Time
	ClaudeSessionName       string

	CollectionError string
}

// ItemKey is the stable identity for a row: worktrees key by path, branch-only
// entries key by branch. Used for cursor preservation, live cell updates, and
// tombstones — never a positional index.
func ItemKey(worktree WorktreeInfo) string {
	if worktree.Path != "" {
		return "wt:" + worktree.Path
	}
	return "br:" + worktree.Branch
}

// ProtectedBranches are never deletable and never filtered as "just another
// branch". Mirrors v1's PROTECTED_BRANCHES.
var ProtectedBranches = map[string]struct{}{
	"main":    {},
	"master":  {},
	"develop": {},
	"trunk":   {},
}

// IsProtectedBranch reports whether a branch name is protected.
func IsProtectedBranch(branchName string) bool {
	_, found := ProtectedBranches[branchName]
	return found
}

// Tuning constants ported from v1.
const (
	// ClaudeStaleThreshold: a working/waiting record older than this belongs to a
	// session whose terminal died before its Stop/SessionEnd hook fired.
	ClaudeStaleThreshold = 2 * time.Hour

	ClaudePollInterval      = 500 * time.Millisecond
	SpinnerTickInterval     = 125 * time.Millisecond
	WorktreePollInterval    = 30 * time.Second
	PullRequestPollInterval = 15 * time.Minute
	TraceInterval           = 60 * time.Second

	// TombstoneWindow suppresses a just-deleted row from discovery until git
	// catches up, guarding the delete race.
	TombstoneWindow = 60 * time.Second

	// NarrowWidthThreshold: below this terminal width the panes stack vertically.
	NarrowWidthThreshold = 160

	// FanOutWidth bounds concurrent git/gh subprocesses during a poll.
	FanOutWidth = 8

	// DefaultCommandTimeout applies to most git/gh/docker invocations.
	DefaultCommandTimeout = 10 * time.Second
)

// SpinnerFrames animate working/waiting Claude states and in-flight PR fetches.
var SpinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// ClaudeSessionStateFileName is the per-worktree state file written by the hooks.
const ClaudeSessionStateFileName = ".claude-session-state"
