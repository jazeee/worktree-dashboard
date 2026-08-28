package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// model.go is the Bubble Tea Elm loop: DashboardModel holds all state, Update
// handles keys / window resize / tier messages, and View renders. The table is
// rendered by hand (lipgloss is ANSI-aware, so per-cell colors survive width
// padding and truncation) rather than via bubbles/table.

// FocusTarget replaces v1's search-box focus flag: keys route to the table or the
// search field depending on this.
type FocusTarget string

const (
	FocusTable  FocusTarget = "Table"
	FocusSearch FocusTarget = "Search"
)

// columnSpec is one summary-table column. Widths match v1's SUMMARY_COLUMNS.
type columnSpec struct {
	title string
	width int
}

var summaryColumns = []columnSpec{
	{"Branch", 40},
	{"↑↓", 9},
	{"Files", 6},
	{"State", 10},
	{"Review", 12},
	{"Claude", 22},
	{"Recommendation", 14},
}

// cellSeparator sits between rendered table columns.
const cellSeparator = " "

// DashboardModel is the whole application state.
type DashboardModel struct {
	repositoryRoot string

	worktrees    []WorktreeInfo
	indexByKey   map[string]int
	visibleKeys  []string
	currentKey   string
	cursorIndex  int
	scrollOffset int

	searchPattern string
	focus         FocusTarget
	search        textinput.Model

	spinnerFrame      int
	layoutOrientation LayoutOrientation
	terminalWidth     int
	terminalHeight    int

	worktreePoll    PollActivity
	pullRequestPoll PollActivity
	lifecyclePhase  LifecyclePhase
	mouseCapture    MouseCapture

	deletedKeys map[string]time.Time

	dialog DialogState

	notification         string
	notificationSeverity NotificationSeverity

	lastWorktreePollMillis    int64
	lastPullRequestPollMillis int64
	pollSkipCount             int
}

// NewDashboardModel builds the initial model for a repository root.
func NewDashboardModel(repositoryRoot string) DashboardModel {
	searchField := textinput.New()
	searchField.Placeholder = "search branch / claude name / PR number (regex) — Enter to keep, Esc to clear"
	searchField.CharLimit = 200

	return DashboardModel{
		repositoryRoot:    repositoryRoot,
		indexByKey:        map[string]int{},
		deletedKeys:       map[string]time.Time{},
		focus:             FocusTable,
		search:            searchField,
		layoutOrientation: OrientationSideBySide,
		worktreePoll:      PollIdle,
		pullRequestPoll:   PollIdle,
		lifecyclePhase:    LifecycleRunning,
		mouseCapture:      MouseCaptureOn,
		dialog:            NoDialog(),
	}
}

// Init kicks off discovery, the live tiers, and a start trace line.
func (dashboard DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		discoverWorktreesCommand(dashboard.repositoryRoot),
		scheduleSpinnerTick(),
		scheduleClaudePollTick(),
		scheduleWorktreePollTick(),
		schedulePullRequestPollTick(),
		scheduleTraceTick(),
		dashboard.traceCommand("start"),
	)
}

// traceCommand writes one trace line as a side effect and produces no message.
func (dashboard DashboardModel) traceCommand(event string) tea.Cmd {
	snapshot := TraceSnapshot{
		Event:                 event,
		ItemCount:             len(dashboard.worktrees),
		WorktreePollMillis:    dashboard.lastWorktreePollMillis,
		PullRequestPollMillis: dashboard.lastPullRequestPollMillis,
		SkipCount:             dashboard.pollSkipCount,
	}
	return func() tea.Msg {
		AppendTraceLine(snapshot)
		return nil
	}
}

// Update is the single point where state changes; Bubble Tea repaints only when
// it returns a changed model, so idle ticks that change nothing cost no redraw.
func (dashboard DashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.WindowSizeMsg:
		return dashboard.handleWindowSize(typed)
	case tea.KeyMsg:
		return dashboard.handleKey(typed)
	case tea.MouseMsg:
		return dashboard.handleMouse(typed)

	case worktreesDiscoveredMsg:
		return dashboard.handleWorktreesDiscovered(typed)
	case discoveryFailedMsg:
		dashboard.setNotification(typed.message, SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	case statusesCollectedMsg:
		return dashboard.handleStatusesCollected(typed)
	case worktreesResolvedMsg:
		return dashboard.handleWorktreesResolved(typed)
	case pullRequestsFetchedMsg:
		return dashboard.handlePullRequestsFetched(typed)
	case claudeRefreshedMsg:
		return dashboard.handleClaudeRefreshed(typed)

	case spinnerTickMsg:
		return dashboard.handleSpinnerTick()
	case claudePollTickMsg:
		return dashboard, tea.Batch(refreshClaudeStatesCommand(dashboard.worktrees), scheduleClaudePollTick())
	case worktreePollTickMsg:
		return dashboard.handleWorktreePollTick()
	case pullRequestPollTickMsg:
		return dashboard.handlePullRequestPollTick()
	case traceTickMsg:
		return dashboard, tea.Batch(dashboard.traceCommand("tick"), scheduleTraceTick())

	case actionResultMsg:
		dashboard.setNotification(typed.text, typed.severity)
		return dashboard, dashboard.notificationClearCommand()
	case worktreeCreatedMsg:
		return dashboard.handleWorktreeCreated(typed)
	case rowDeletedMsg:
		return dashboard.handleRowDeleted(typed)
	case clearNotificationMsg:
		dashboard.notification = ""
		return dashboard, nil
	}
	return dashboard, nil
}

// ---- Window + key routing ----

func (dashboard DashboardModel) handleWindowSize(size tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	dashboard.terminalWidth = size.Width
	dashboard.terminalHeight = size.Height
	if size.Width < NarrowWidthThreshold {
		dashboard.layoutOrientation = OrientationStacked
	} else {
		dashboard.layoutOrientation = OrientationSideBySide
	}
	dashboard.clampScroll()
	return dashboard, nil
}

// handleMouse selects a row on left-click and scrolls the cursor on the wheel.
// Motion events (reported while a button is held) change nothing and are ignored.
func (dashboard DashboardModel) handleMouse(mouse tea.MouseMsg) (tea.Model, tea.Cmd) {
	if dashboard.dialog.IsOpen() {
		return dashboard, nil
	}
	switch mouse.Button {
	case tea.MouseButtonWheelUp:
		dashboard.moveCursor(-1)
		return dashboard, nil
	case tea.MouseButtonWheelDown:
		dashboard.moveCursor(1)
		return dashboard, nil
	case tea.MouseButtonLeft:
		if mouse.Action != tea.MouseActionPress {
			return dashboard, nil
		}
		return dashboard.selectRowAtPoint(mouse.X, mouse.Y)
	}
	return dashboard, nil
}

// toggleMouseCapture flips mouse interception on or off. Turning it off releases
// the mouse to the terminal so the detail pane's text can be selected and copied
// with the native drag-select; turning it back on restores click-to-select and
// wheel scrolling.
func (dashboard DashboardModel) toggleMouseCapture() (tea.Model, tea.Cmd) {
	if dashboard.mouseCapture == MouseCaptureOn {
		dashboard.mouseCapture = MouseCaptureOff
		dashboard.setNotification("mouse released — drag to select text; press m to re-enable", SeverityWarning)
		return dashboard, tea.Batch(tea.DisableMouse, dashboard.notificationClearCommand())
	}
	dashboard.mouseCapture = MouseCaptureOn
	dashboard.setNotification("mouse capture on", SeverityInfo)
	return dashboard, tea.Batch(tea.EnableMouseCellMotion, dashboard.notificationClearCommand())
}

// selectRowAtPoint moves the cursor to the worktree row under a click, if the
// click landed on a data row of the table (not the column header, the empty pad
// below the rows, or — in the side-by-side layout — the detail pane).
func (dashboard DashboardModel) selectRowAtPoint(pointX int, pointY int) (tea.Model, tea.Cmd) {
	if dashboard.layoutOrientation == OrientationSideBySide && pointX >= summaryTableWidth() {
		return dashboard, nil
	}
	if pointY < 1 {
		// Row 0 is the column header.
		return dashboard, nil
	}
	rowOffset := pointY - 1
	if rowOffset >= dashboard.tableBodyHeight() {
		return dashboard, nil
	}
	target := dashboard.scrollOffset + rowOffset
	if target >= len(dashboard.visibleKeys) {
		return dashboard, nil
	}
	dashboard.moveCursorTo(target)
	return dashboard, nil
}

// summaryTableWidth is the rendered width of the summary columns plus the
// separators between them — the click-target boundary of the table panel.
func summaryTableWidth() int {
	total := 0
	for index, column := range summaryColumns {
		if index > 0 {
			total += len(cellSeparator)
		}
		total += column.width
	}
	return total
}

func (dashboard DashboardModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if dashboard.dialog.IsOpen() {
		return dashboard.handleDialogKey(key)
	}
	if dashboard.focus == FocusSearch {
		return dashboard.handleSearchKey(key)
	}
	return dashboard.handleBrowsingKey(key)
}

func (dashboard DashboardModel) handleBrowsingKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q":
		AppendTraceLine(TraceSnapshot{Event: "shutdown", ItemCount: len(dashboard.worktrees)})
		dashboard.lifecyclePhase = LifecycleShuttingDown
		return dashboard, tea.Quit
	case "r":
		return dashboard, discoverWorktreesCommand(dashboard.repositoryRoot)
	case "/":
		dashboard.focus = FocusSearch
		dashboard.search.SetValue(dashboard.searchPattern)
		dashboard.search.Focus()
		return dashboard, textinput.Blink
	case "esc":
		if dashboard.searchPattern != "" {
			dashboard.searchPattern = ""
			dashboard.rebuildRows()
		}
		return dashboard, nil
	case "o":
		return dashboard.launchAction(func(worktree WorktreeInfo) tea.Cmd {
			return openPullRequestCommand(worktree.PullRequestUrl)
		})
	case "v":
		return dashboard.launchAction(func(worktree WorktreeInfo) tea.Cmd {
			return openInEditorCommand(worktree.Path)
		})
	case "t":
		return dashboard.handleOpenTerminal()
	case "c":
		return dashboard.handleOpenClaude()
	case "n":
		return dashboard.handleNewWorktree()
	case "y", "ctrl+c":
		return dashboard.launchAction(func(worktree WorktreeInfo) tea.Cmd {
			return copyPathCommand(worktree.Path)
		})
	case "d":
		return dashboard.handleDeleteRequest()
	case "m":
		return dashboard.toggleMouseCapture()
	case "up", "k":
		dashboard.moveCursor(-1)
		return dashboard, nil
	case "down", "j":
		dashboard.moveCursor(1)
		return dashboard, nil
	case "pgup":
		dashboard.moveCursor(-dashboard.tableBodyHeight())
		return dashboard, nil
	case "pgdown":
		dashboard.moveCursor(dashboard.tableBodyHeight())
		return dashboard, nil
	case "home", "g":
		dashboard.moveCursorTo(0)
		return dashboard, nil
	case "end", "G":
		dashboard.moveCursorTo(len(dashboard.visibleKeys) - 1)
		return dashboard, nil
	}
	return dashboard, nil
}

func (dashboard DashboardModel) handleSearchKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		dashboard.focus = FocusTable
		dashboard.search.Blur()
		return dashboard, nil
	case "esc":
		dashboard.focus = FocusTable
		dashboard.search.Blur()
		dashboard.search.SetValue("")
		if dashboard.searchPattern != "" {
			dashboard.searchPattern = ""
			dashboard.rebuildRows()
		}
		return dashboard, nil
	}
	updatedField, fieldCommand := dashboard.search.Update(key)
	dashboard.search = updatedField
	dashboard.searchPattern = dashboard.search.Value()
	dashboard.rebuildRows()
	return dashboard, fieldCommand
}

func (dashboard DashboardModel) handleDialogKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if dashboard.dialog.Kind == DialogConfirm {
		switch key.String() {
		case "y", "enter":
			intent := dashboard.dialog.Intent
			targetKey := dashboard.dialog.TargetKey
			dashboard.dialog = NoDialog()
			if intent == IntentDeleteRow {
				worktree, found := dashboard.worktreeByKey(targetKey)
				if found {
					return dashboard, performDeleteCommand(worktree, dashboard.repositoryRoot)
				}
			}
			return dashboard, nil
		case "n", "esc":
			dashboard.dialog = NoDialog()
			return dashboard, nil
		}
		return dashboard, nil
	}

	// Input dialog.
	switch key.String() {
	case "enter":
		value := dashboard.dialog.Input.Value()
		baseDirectory := dashboard.dialog.BaseDir
		intent := dashboard.dialog.Intent
		dashboard.dialog = NoDialog()
		if intent == IntentCreateWorktree && strings.TrimSpace(value) != "" {
			dashboard.setNotification("Creating worktree '"+value+"' off origin/main…", SeverityInfo)
			return dashboard, createNamedWorktreeCommand(baseDirectory, value)
		}
		return dashboard, nil
	case "esc":
		dashboard.dialog = NoDialog()
		return dashboard, nil
	}
	updatedField, fieldCommand := dashboard.dialog.Input.Update(key)
	dashboard.dialog.Input = updatedField
	return dashboard, fieldCommand
}

// ---- Action helpers ----

// launchAction runs makeCommand against the current row, if any.
func (dashboard DashboardModel) launchAction(makeCommand func(WorktreeInfo) tea.Cmd) (tea.Model, tea.Cmd) {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return dashboard, nil
	}
	return dashboard, makeCommand(worktree)
}

// handleOpenTerminal opens a terminal in the current row's worktree, creating a
// worktree first when the row is a branch with none.
func (dashboard DashboardModel) handleOpenTerminal() (tea.Model, tea.Cmd) {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return dashboard, nil
	}
	if worktree.Path == "" {
		if worktree.Branch == "" {
			dashboard.setNotification("No worktree or branch to open", SeverityWarning)
			return dashboard, dashboard.notificationClearCommand()
		}
		dashboard.setNotification("Creating worktree for '"+worktree.Branch+"'…", SeverityInfo)
		return dashboard, createBranchWorktreeCommand(dashboard.repositoryRoot, worktree.Branch, OpenerTerminal)
	}
	return dashboard, openInTerminalCommand(worktree.Path)
}

func (dashboard DashboardModel) handleOpenClaude() (tea.Model, tea.Cmd) {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return dashboard, nil
	}
	if worktree.Path == "" {
		if worktree.Branch != "" {
			dashboard.setNotification("Creating worktree for '"+worktree.Branch+"'…", SeverityInfo)
			return dashboard, createBranchWorktreeCommand(dashboard.repositoryRoot, worktree.Branch, OpenerClaude)
		}
		dashboard.setNotification("No worktree or branch to open", SeverityWarning)
		return dashboard, dashboard.notificationClearCommand()
	}
	resumeWord := "fresh"
	if worktree.ClaudeSessionIdentifier != "" {
		resumeWord = "resuming"
	}
	return dashboard, openClaudeCommand(worktree.Path, resumeWord)
}

func (dashboard DashboardModel) handleNewWorktree() (tea.Model, tea.Cmd) {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return dashboard, nil
	}
	if worktree.Path == "" && worktree.Branch != "" {
		return dashboard, createBranchWorktreeCommand(dashboard.repositoryRoot, worktree.Branch, OpenerClaude)
	}
	baseDirectory := worktree.Path
	if baseDirectory == "" {
		baseDirectory = worktree.RepositoryRoot
	}
	if baseDirectory == "" {
		dashboard.setNotification("No directory to create a worktree from", SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	prompt := "New worktree under " + styleBold.Render(baseDirectory) + "\n(branches off origin/main):"
	dashboard.dialog = NewInputDialog(IntentCreateWorktree, prompt, "my-feature", baseDirectory)
	return dashboard, textinput.Blink
}

func (dashboard DashboardModel) handleDeleteRequest() (tea.Model, tea.Cmd) {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return dashboard, nil
	}
	if worktree.Kind == KindRootWorktree {
		dashboard.setNotification("Refusing to delete a root worktree (only nested worktrees and branches can be deleted here)", SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	if worktree.Branch != "" && IsProtectedBranch(worktree.Branch) {
		dashboard.setNotification("Refusing to delete protected branch: "+worktree.Branch, SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	if worktree.Role == RolePrimary {
		dashboard.setNotification("Refusing to delete the primary worktree", SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	if worktree.Path != "" && worktree.Path == dashboard.repositoryRoot {
		dashboard.setNotification("Refusing to delete the worktree the dashboard was launched from", SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	var prompt string
	if worktree.Kind == KindBranchOnly {
		prompt = "Delete branch?\n\n  " + styleBold.Render(worktree.Branch)
	} else {
		branchText := worktree.Branch
		if branchText == "" {
			branchText = styleDim.Render("(detached)")
		}
		prompt = "Delete worktree and branch?\n\n  " +
			styleBold.Render("worktree: ") + worktree.Path + "\n  " +
			styleBold.Render("branch:   ") + branchText
	}
	dashboard.dialog = NewConfirmDialog(IntentDeleteRow, prompt, ItemKey(worktree))
	return dashboard, nil
}

// ---- Tier message handlers ----

func (dashboard DashboardModel) handleWorktreesDiscovered(message worktreesDiscoveredMsg) (tea.Model, tea.Cmd) {
	dashboard.worktrees = dashboard.filterTombstoned(message.worktrees)
	dashboard.reindex()
	for index := range dashboard.worktrees {
		dashboard.worktrees[index].PullRequestLoad = LoadInProgress
	}
	dashboard.rebuildRows()
	return dashboard, tea.Batch(
		collectStatusesCommand(dashboard.worktrees),
		fetchPullRequestsCommand(dashboard.worktrees),
	)
}

func (dashboard DashboardModel) handleStatusesCollected(message statusesCollectedMsg) (tea.Model, tea.Cmd) {
	for _, collected := range message.worktrees {
		index, present := dashboard.indexByKey[ItemKey(collected)]
		if !present {
			continue
		}
		mergeStatusFields(&dashboard.worktrees[index], collected)
	}
	dashboard.rebuildRows()
	return dashboard, nil
}

func (dashboard DashboardModel) handleWorktreesResolved(message worktreesResolvedMsg) (tea.Model, tea.Cmd) {
	dashboard.worktreePoll = PollIdle
	dashboard.lastWorktreePollMillis = message.durationMillis
	if message.worktrees == nil {
		return dashboard, nil
	}
	fresh := dashboard.filterTombstoned(message.worktrees)
	previousByKey := map[string]WorktreeInfo{}
	for _, worktree := range dashboard.worktrees {
		previousByKey[ItemKey(worktree)] = worktree
	}
	for index := range fresh {
		previous, present := previousByKey[ItemKey(fresh[index])]
		if present {
			CarryPullRequestFields(&fresh[index], previous)
			fresh[index].PullRequestLoad = previous.PullRequestLoad
		}
	}
	newKeys := keySet(fresh)
	oldKeys := keySet(dashboard.worktrees)

	dashboard.worktrees = fresh
	dashboard.reindex()
	dashboard.rebuildRows()

	if !sameKeys(newKeys, oldKeys) {
		added := []WorktreeInfo{}
		for _, worktree := range fresh {
			if _, existed := oldKeys[ItemKey(worktree)]; !existed {
				added = append(added, worktree)
			}
		}
		if len(added) > 0 {
			for index := range dashboard.worktrees {
				if _, isAdded := newKeys[ItemKey(dashboard.worktrees[index])]; isAdded {
					if _, existed := oldKeys[ItemKey(dashboard.worktrees[index])]; !existed {
						dashboard.worktrees[index].PullRequestLoad = LoadInProgress
					}
				}
			}
			dashboard.rebuildRows()
			return dashboard, fetchPullRequestsCommand(added)
		}
	}
	return dashboard, nil
}

func (dashboard DashboardModel) handlePullRequestsFetched(message pullRequestsFetchedMsg) (tea.Model, tea.Cmd) {
	dashboard.pullRequestPoll = PollIdle
	dashboard.lastPullRequestPollMillis = message.durationMillis
	requestedSet := map[string]struct{}{}
	for _, key := range message.requested {
		requestedSet[key] = struct{}{}
	}
	for index := range dashboard.worktrees {
		key := ItemKey(dashboard.worktrees[index])
		if _, wasRequested := requestedSet[key]; !wasRequested {
			continue
		}
		dashboard.worktrees[index].PullRequestLoad = LoadIdle
		if payload, hit := message.payloads[key]; hit {
			ApplyPullRequestFields(&dashboard.worktrees[index], payload)
		}
	}
	dashboard.rebuildRows()
	return dashboard, nil
}

func (dashboard DashboardModel) handleClaudeRefreshed(message claudeRefreshedMsg) (tea.Model, tea.Cmd) {
	changed := false
	for index := range dashboard.worktrees {
		record, present := message.records[ItemKey(dashboard.worktrees[index])]
		if !present {
			continue
		}
		worktree := &dashboard.worktrees[index]
		timeSensitive := worktree.ClaudeState == ClaudeStateWorking || worktree.ClaudeState == ClaudeStateWaiting
		if worktree.ClaudeSessionIdentifier != record.SessionIdentifier ||
			worktree.ClaudeState != record.State ||
			!worktree.ClaudeStateUpdatedAt.Equal(record.UpdatedAt) ||
			worktree.ClaudeSessionName != record.SessionName {
			changed = true
		}
		worktree.ClaudeSessionIdentifier = record.SessionIdentifier
		worktree.ClaudeState = record.State
		worktree.ClaudeStateUpdatedAt = record.UpdatedAt
		worktree.ClaudeSessionName = record.SessionName
		if timeSensitive || worktree.ClaudeState == ClaudeStateWorking || worktree.ClaudeState == ClaudeStateWaiting {
			changed = true
		}
	}
	if changed {
		dashboard.rebuildRows()
	}
	return dashboard, nil
}

func (dashboard DashboardModel) handleSpinnerTick() (tea.Model, tea.Cmd) {
	now := time.Now()
	animating := false
	for _, worktree := range dashboard.worktrees {
		claudeActive := (worktree.ClaudeState == ClaudeStateWorking || worktree.ClaudeState == ClaudeStateWaiting) &&
			DetermineClaudeLiveness(worktree, now) != LivenessStale
		if claudeActive || worktree.PullRequestLoad == LoadInProgress {
			animating = true
			break
		}
	}
	if !animating {
		return dashboard, scheduleSpinnerTick()
	}
	dashboard.spinnerFrame++
	dashboard.rebuildRows()
	return dashboard, scheduleSpinnerTick()
}

func (dashboard DashboardModel) handleWorktreePollTick() (tea.Model, tea.Cmd) {
	if dashboard.worktreePoll == PollRunning {
		dashboard.pollSkipCount++
		return dashboard, scheduleWorktreePollTick()
	}
	dashboard.worktreePoll = PollRunning
	return dashboard, tea.Batch(resolveWorktreesCommand(dashboard.repositoryRoot), scheduleWorktreePollTick())
}

func (dashboard DashboardModel) handlePullRequestPollTick() (tea.Model, tea.Cmd) {
	if dashboard.pullRequestPoll == PollRunning {
		dashboard.pollSkipCount++
		return dashboard, schedulePullRequestPollTick()
	}
	if len(dashboard.worktrees) == 0 {
		return dashboard, schedulePullRequestPollTick()
	}
	dashboard.pullRequestPoll = PollRunning
	for index := range dashboard.worktrees {
		dashboard.worktrees[index].PullRequestLoad = LoadInProgress
	}
	dashboard.rebuildRows()
	return dashboard, tea.Batch(fetchPullRequestsCommand(dashboard.worktrees), schedulePullRequestPollTick())
}

func (dashboard DashboardModel) handleWorktreeCreated(message worktreeCreatedMsg) (tea.Model, tea.Cmd) {
	if message.failure != "" {
		dashboard.setNotification(message.failure, SeverityError)
		return dashboard, dashboard.notificationClearCommand()
	}
	dashboard.setNotification(message.note, SeverityInfo)
	return dashboard, tea.Batch(
		discoverWorktreesCommand(dashboard.repositoryRoot),
		dashboard.notificationClearCommand(),
	)
}

func (dashboard DashboardModel) handleRowDeleted(message rowDeletedMsg) (tea.Model, tea.Cmd) {
	dashboard.deletedKeys[message.key] = time.Now().Add(TombstoneWindow)

	// Remember where the deleted row sat in the filtered list so the cursor can
	// land on the row that takes its place (the next one), rather than jumping to
	// the top. Deleting the last row clamps down to the new last row.
	deletedPosition := -1
	for position, key := range dashboard.visibleKeys {
		if key == message.key {
			deletedPosition = position
			break
		}
	}

	remaining := dashboard.worktrees[:0]
	for _, worktree := range dashboard.worktrees {
		if ItemKey(worktree) != message.key {
			remaining = append(remaining, worktree)
		}
	}
	dashboard.worktrees = append([]WorktreeInfo(nil), remaining...)
	dashboard.reindex()
	dashboard.rebuildRows()
	if deletedPosition >= 0 {
		dashboard.moveCursorTo(deletedPosition)
	}
	if message.notification != "" {
		dashboard.setNotification(message.notification, message.severity)
	}
	return dashboard, dashboard.notificationClearCommand()
}

// ---- List / cursor helpers ----

func (dashboard *DashboardModel) reindex() {
	dashboard.indexByKey = map[string]int{}
	for index, worktree := range dashboard.worktrees {
		dashboard.indexByKey[ItemKey(worktree)] = index
	}
}

// rebuildRows recomputes the visible key list under the active search and keeps
// the cursor on the same row when it survives.
func (dashboard *DashboardModel) rebuildRows() {
	visible := []string{}
	for _, worktree := range dashboard.worktrees {
		if dashboard.matchesSearch(worktree) {
			visible = append(visible, ItemKey(worktree))
		}
	}
	dashboard.visibleKeys = visible
	dashboard.restoreCursor()
}

func (dashboard *DashboardModel) restoreCursor() {
	if len(dashboard.visibleKeys) == 0 {
		dashboard.currentKey = ""
		dashboard.cursorIndex = 0
		dashboard.scrollOffset = 0
		return
	}
	target := 0
	for position, key := range dashboard.visibleKeys {
		if key == dashboard.currentKey {
			target = position
			break
		}
	}
	dashboard.cursorIndex = target
	dashboard.currentKey = dashboard.visibleKeys[target]
	dashboard.clampScroll()
}

func (dashboard *DashboardModel) moveCursor(delta int) {
	dashboard.moveCursorTo(dashboard.cursorIndex + delta)
}

func (dashboard *DashboardModel) moveCursorTo(target int) {
	if len(dashboard.visibleKeys) == 0 {
		return
	}
	if target < 0 {
		target = 0
	}
	if target > len(dashboard.visibleKeys)-1 {
		target = len(dashboard.visibleKeys) - 1
	}
	dashboard.cursorIndex = target
	dashboard.currentKey = dashboard.visibleKeys[target]
	dashboard.clampScroll()
}

func (dashboard *DashboardModel) clampScroll() {
	height := dashboard.tableBodyHeight()
	if height <= 0 {
		dashboard.scrollOffset = 0
		return
	}
	if dashboard.cursorIndex < dashboard.scrollOffset {
		dashboard.scrollOffset = dashboard.cursorIndex
	}
	if dashboard.cursorIndex >= dashboard.scrollOffset+height {
		dashboard.scrollOffset = dashboard.cursorIndex - height + 1
	}
	if dashboard.scrollOffset < 0 {
		dashboard.scrollOffset = 0
	}
}

func (dashboard DashboardModel) currentWorktree() (WorktreeInfo, bool) {
	return dashboard.worktreeByKey(dashboard.currentKey)
}

func (dashboard DashboardModel) worktreeByKey(key string) (WorktreeInfo, bool) {
	if key == "" {
		return WorktreeInfo{}, false
	}
	index, present := dashboard.indexByKey[key]
	if !present || index >= len(dashboard.worktrees) {
		return WorktreeInfo{}, false
	}
	return dashboard.worktrees[index], true
}

func (dashboard DashboardModel) matchesSearch(worktree WorktreeInfo) bool {
	if dashboard.searchPattern == "" {
		return true
	}
	haystack := worktree.Branch + "\n" + worktree.ClaudeSessionName
	if worktree.PullRequestNumber > 0 {
		haystack += "\n#" + strconv.Itoa(worktree.PullRequestNumber) + "\n" + worktree.PullRequestTitle
	}
	compiled, compileError := regexp.Compile("(?i)" + dashboard.searchPattern)
	if compileError != nil {
		return strings.Contains(strings.ToLower(haystack), strings.ToLower(dashboard.searchPattern))
	}
	return compiled.MatchString(haystack)
}

func (dashboard *DashboardModel) setNotification(text string, severity NotificationSeverity) {
	dashboard.notification = text
	dashboard.notificationSeverity = severity
}

func (dashboard DashboardModel) notificationClearCommand() tea.Cmd {
	return scheduleClearNotification(6 * time.Second)
}

// filterTombstoned drops user-deleted rows from a fresh discovery, expiring
// tombstones once git catches up or the safety window passes. Ported from v1.
func (dashboard *DashboardModel) filterTombstoned(items []WorktreeInfo) []WorktreeInfo {
	if len(dashboard.deletedKeys) == 0 {
		return items
	}
	now := time.Now()
	for key, deadline := range dashboard.deletedKeys {
		if !deadline.After(now) {
			delete(dashboard.deletedKeys, key)
		}
	}
	if len(dashboard.deletedKeys) == 0 {
		return items
	}
	kept := []WorktreeInfo{}
	stillPresent := map[string]struct{}{}
	for _, worktree := range items {
		key := ItemKey(worktree)
		if _, tombstoned := dashboard.deletedKeys[key]; tombstoned {
			stillPresent[key] = struct{}{}
			continue
		}
		kept = append(kept, worktree)
	}
	for key := range dashboard.deletedKeys {
		if _, present := stillPresent[key]; !present {
			delete(dashboard.deletedKeys, key)
		}
	}
	return kept
}

// mergeStatusFields copies git/compose/claude fields from a freshly collected
// record onto an existing row, leaving PR fields and load state untouched.
func mergeStatusFields(destination *WorktreeInfo, source WorktreeInfo) {
	destination.Cleanliness = source.Cleanliness
	destination.DirtyFileCount = source.DirtyFileCount
	destination.AheadCount = source.AheadCount
	destination.BehindCount = source.BehindCount
	destination.Upstream = source.Upstream
	destination.LastCommitSubject = source.LastCommitSubject
	destination.LastCommitRelative = source.LastCommitRelative
	destination.RecentCommits = source.RecentCommits
	destination.ComposeScriptPath = source.ComposeScriptPath
	destination.ComposeProjectName = source.ComposeProjectName
	destination.ComposeStatus = source.ComposeStatus
	destination.ClaudeSessionIdentifier = source.ClaudeSessionIdentifier
	destination.ClaudeState = source.ClaudeState
	destination.ClaudeStateUpdatedAt = source.ClaudeStateUpdatedAt
	destination.ClaudeSessionName = source.ClaudeSessionName
	destination.CollectionError = source.CollectionError
}

func keySet(worktrees []WorktreeInfo) map[string]struct{} {
	set := map[string]struct{}{}
	for _, worktree := range worktrees {
		set[ItemKey(worktree)] = struct{}{}
	}
	return set
}

func sameKeys(left map[string]struct{}, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, present := right[key]; !present {
			return false
		}
	}
	return true
}

// ---- View ----

// contentHeight is the number of rows the body occupies: the whole terminal
// minus the counts and status lines pinned at the bottom.
func (dashboard DashboardModel) contentHeight() int {
	height := dashboard.terminalHeight - 2
	if height < 1 {
		return 1
	}
	return height
}

func (dashboard DashboardModel) tableBodyHeight() int {
	// The table also spends one row on its column header.
	bodyHeight := dashboard.contentHeight()
	if dashboard.layoutOrientation == OrientationStacked {
		// Split the body between the table and the stacked detail pane.
		bodyHeight = bodyHeight/2 - 1
	} else {
		bodyHeight = bodyHeight - 1
	}
	if bodyHeight < 1 {
		return 1
	}
	return bodyHeight
}

func (dashboard DashboardModel) View() string {
	if dashboard.terminalWidth == 0 || dashboard.terminalHeight == 0 {
		return "Loading worktree-dashboard…"
	}
	counts := dashboard.renderCounts()
	status := dashboard.renderStatusLine()

	var body string
	if dashboard.dialog.IsOpen() {
		body = RenderDialog(dashboard.dialog, dashboard.terminalWidth, dashboard.contentHeight())
	} else {
		body = dashboard.renderBody()
	}
	return strings.Join([]string{body, counts, status}, "\n")
}

// renderBody lays out the table and detail pane and pads them to fill the full
// content height so the counts and status lines sit at the very bottom and the
// detail divider spans the whole screen.
func (dashboard DashboardModel) renderBody() string {
	inner := dashboard.contentHeight()
	table := dashboard.renderTable()
	if dashboard.layoutOrientation == OrientationStacked {
		tableHeight := dashboard.tableBodyHeight() + 1 // + column header
		detailHeight := inner - tableHeight - 1        // - divider row
		if detailHeight < 1 {
			detailHeight = 1
		}
		detail := clipToHeight(dashboard.renderDetail(), detailHeight)
		divider := styleDim.Render(strings.Repeat("─", dashboard.terminalWidth))
		stacked := lipgloss.JoinVertical(lipgloss.Left, table, divider, detail)
		return lipgloss.NewStyle().Height(inner).MaxHeight(inner).Render(stacked)
	}
	detail := clipToHeight(dashboard.renderDetail(), inner)
	tableColumn := lipgloss.NewStyle().Height(inner).Render(table)
	detailColumn := styleDetailBorder.Height(inner).Render(detail)
	return lipgloss.JoinHorizontal(lipgloss.Top, tableColumn, detailColumn)
}

// renderTable renders the summary columns by hand so per-cell ANSI colors survive.
func (dashboard DashboardModel) renderTable() string {
	now := time.Now()
	headerCells := []string{}
	for _, column := range summaryColumns {
		headerCells = append(headerCells, padCell(styleHeading.Render(column.title), column.width))
	}
	lines := []string{strings.Join(headerCells, cellSeparator)}

	height := dashboard.tableBodyHeight()
	end := dashboard.scrollOffset + height
	if end > len(dashboard.visibleKeys) {
		end = len(dashboard.visibleKeys)
	}
	for position := dashboard.scrollOffset; position < end; position++ {
		key := dashboard.visibleKeys[position]
		index, present := dashboard.indexByKey[key]
		if !present {
			continue
		}
		rowLine := dashboard.renderRow(dashboard.worktrees[index], now)
		if position == dashboard.cursorIndex {
			rowLine = HighlightSelectedRow(rowLine)
		}
		lines = append(lines, rowLine)
	}
	if len(dashboard.visibleKeys) == 0 {
		lines = append(lines, styleDim.Render("  (no worktrees)"))
	}
	return strings.Join(lines, "\n")
}

func (dashboard DashboardModel) renderRow(worktree WorktreeInfo, now time.Time) string {
	cells := []string{
		padCell(RenderBranchCell(worktree), summaryColumns[0].width),
		padCell(FormatAheadBehindCell(worktree), summaryColumns[1].width),
		padCell(FormatDirtyCell(worktree), summaryColumns[2].width),
		padCell(FormatPullRequestStateCell(worktree, dashboard.spinnerFrame), summaryColumns[3].width),
		padCell(FormatReviewCell(worktree), summaryColumns[4].width),
		padCell(FormatClaudeCell(worktree, now, dashboard.spinnerFrame), summaryColumns[5].width),
		padCell(FormatRecommendationCell(worktree), summaryColumns[6].width),
	}
	return strings.Join(cells, cellSeparator)
}

func (dashboard DashboardModel) renderDetail() string {
	worktree, found := dashboard.currentWorktree()
	if !found {
		return styleDim.Render("Select a worktree to see details.")
	}
	return BuildDetailView(worktree, time.Now(), dashboard.spinnerFrame)
}

func (dashboard DashboardModel) renderCounts() string {
	nested, branchOnly, root := 0, 0, 0
	for _, worktree := range dashboard.worktrees {
		switch worktree.Kind {
		case KindNestedWorktree:
			nested++
		case KindBranchOnly:
			branchOnly++
		case KindRootWorktree:
			root++
		}
	}
	text := fmt.Sprintf("Nested: %s, Branch: %s, Base: %s",
		styleNestedBranch.Render(fmt.Sprintf("%d", nested)),
		styleBranchOnly.Render(fmt.Sprintf("%d", branchOnly)),
		styleRootBranch.Render(fmt.Sprintf("%d", root)),
	)
	if dashboard.searchPattern != "" {
		text += "    " + styleDirty.Render("/"+dashboard.searchPattern+"/") +
			fmt.Sprintf(" %s/%d", styleBold.Render(fmt.Sprintf("%d", len(dashboard.visibleKeys))), len(dashboard.worktrees))
	}
	return styleCountsBar.Render(text)
}

func (dashboard DashboardModel) renderStatusLine() string {
	if dashboard.focus == FocusSearch {
		return dashboard.search.View()
	}
	if dashboard.notification != "" {
		switch dashboard.notificationSeverity {
		case SeverityError:
			return styleError.Render(dashboard.notification)
		case SeverityWarning:
			return styleDirty.Render(dashboard.notification)
		default:
			return styleClean.Render(dashboard.notification)
		}
	}
	return styleDim.Render("q quit · r refresh · / search · o PR · v code · t term · c claude · n new · y copy · d delete · m mouse")
}

// padCell fixes a rendered cell to width, truncating with an ellipsis. lipgloss's
// width handling is ANSI-aware, so embedded color codes are preserved.
func padCell(content string, width int) string {
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true).Render(content)
}

// clipToHeight keeps at most maxLines lines of text.
func clipToHeight(text string, maxLines int) string {
	if maxLines < 1 {
		maxLines = 1
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[:maxLines], "\n")
}
