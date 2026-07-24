package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ansiPattern strips SGR/CSI escapes so view assertions test text, not styling.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripAnsi(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

// dashboard_test.go covers the pure logic: parsers, derivations, tombstone
// filtering, terminal-command building, and search matching. The subprocess and
// TUI layers are exercised by the live run, not here.

func TestCategorizePath(test *testing.T) {
	cases := []struct {
		path string
		want WorktreeKind
	}{
		{"/home/jaz/code/notable/scr-01", KindRootWorktree},
		{"/home/jaz/code/notable/scr-06-misc", KindRootWorktree},
		{"/home/jaz/code/notable/vivaa", KindRootWorktree},
		{"/home/jaz/code/notable/scr-01/worktrees/feature", KindNestedWorktree},
		{"/tmp/some/other/checkout", KindNestedWorktree},
	}
	for _, testCase := range cases {
		if got := CategorizePath(testCase.path); got != testCase.want {
			test.Errorf("CategorizePath(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func TestPermanentWorktreeLabel(test *testing.T) {
	cases := []struct {
		path string
		kind WorktreeKind
		want string
	}{
		{"/home/jaz/code/notable/scr-01", KindRootWorktree, "1"},
		{"/home/jaz/code/notable/scr-06-misc", KindRootWorktree, "6"},
		{"/home/jaz/code/notable/scr-12", KindRootWorktree, "12"},
		{"/home/jaz/code/notable/vivaa", KindRootWorktree, "0"},
		{"/home/jaz/code/notable/vivaa/", KindRootWorktree, "0"},
		{"/home/jaz/code/notable/scr-01/worktrees/feature", KindNestedWorktree, ""},
		{"/tmp/some/other/checkout", KindNestedWorktree, ""},
	}
	for _, testCase := range cases {
		worktree := WorktreeInfo{Path: testCase.path, Kind: testCase.kind}
		if got := PermanentWorktreeLabel(worktree); got != testCase.want {
			test.Errorf("PermanentWorktreeLabel(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func TestParseWorktreePorcelain(test *testing.T) {
	porcelain := "worktree /home/jaz/code/notable/scr-01\n" +
		"HEAD abc123\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /home/jaz/code/notable/scr-01/worktrees/feature\n" +
		"HEAD def456\n" +
		"detached\n" +
		"\n"
	worktrees := ParseWorktreePorcelain(porcelain)
	if len(worktrees) != 2 {
		test.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}
	if worktrees[0].Branch != "main" || worktrees[0].Head != "abc123" {
		test.Errorf("first worktree parsed wrong: %+v", worktrees[0])
	}
	if worktrees[0].Kind != KindRootWorktree {
		test.Errorf("first worktree kind = %q, want RootWorktree", worktrees[0].Kind)
	}
	if worktrees[1].HeadState != HeadDetached {
		test.Errorf("second worktree should be detached, got %q", worktrees[1].HeadState)
	}
	if worktrees[1].Kind != KindNestedWorktree {
		test.Errorf("second worktree kind = %q, want NestedWorktree", worktrees[1].Kind)
	}
}

func TestParseStatusV2(test *testing.T) {
	output := "# branch.oid abc\n" +
		"# branch.head feature\n" +
		"# branch.upstream origin/feature\n" +
		"# branch.ab +3 -2\n" +
		"1 .M N... 100644 100644 100644 aaa bbb file1.go\n" +
		"? untracked.txt\n"
	worktree := newWorktreeInfoDefaults()
	ParseStatusV2(&worktree, output)
	if worktree.AheadCount != 3 || worktree.BehindCount != 2 {
		test.Errorf("ahead/behind = %d/%d, want 3/2", worktree.AheadCount, worktree.BehindCount)
	}
	if worktree.Upstream != "origin/feature" {
		test.Errorf("upstream = %q, want origin/feature", worktree.Upstream)
	}
	if worktree.DirtyFileCount != 2 {
		test.Errorf("dirty = %d, want 2", worktree.DirtyFileCount)
	}
	if worktree.Cleanliness != CleanlinessDirty {
		test.Errorf("cleanliness = %q, want Dirty", worktree.Cleanliness)
	}
}

func TestParseStatusV2Clean(test *testing.T) {
	output := "# branch.ab +0 -0\n"
	worktree := newWorktreeInfoDefaults()
	ParseStatusV2(&worktree, output)
	if worktree.Cleanliness != CleanlinessClean {
		test.Errorf("cleanliness = %q, want Clean", worktree.Cleanliness)
	}
}

func TestComposeProjectNameFor(test *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/a/b/My_Feature", "my_feature"},
		{"/a/b/1234-Foo Bar", "1234-foo-bar"},
		{"/a/b/--edge--", "edge"},
	}
	for _, testCase := range cases {
		if got := ComposeProjectNameFor(testCase.path); got != testCase.want {
			test.Errorf("ComposeProjectNameFor(%q) = %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

func TestReadClaudeSessionState(test *testing.T) {
	directory := test.TempDir()
	contents := "id=session-42\nstate=working\nname=my task\nupdated=1700000000\n"
	if writeError := os.WriteFile(filepath.Join(directory, ClaudeSessionStateFileName), []byte(contents), 0o644); writeError != nil {
		test.Fatalf("write temp state: %v", writeError)
	}
	record := ReadClaudeSessionState(directory)
	if record.SessionIdentifier != "session-42" {
		test.Errorf("id = %q, want session-42", record.SessionIdentifier)
	}
	if record.State != ClaudeStateWorking {
		test.Errorf("state = %q, want working", record.State)
	}
	if record.SessionName != "my task" {
		test.Errorf("name = %q, want 'my task'", record.SessionName)
	}
	if record.UpdatedAt != time.Unix(1700000000, 0) {
		test.Errorf("updated = %v, want 1700000000", record.UpdatedAt)
	}
}

func TestReadClaudeSessionStateMissing(test *testing.T) {
	record := ReadClaudeSessionState(test.TempDir())
	if record.State != ClaudeStateNone {
		test.Errorf("missing file should yield None state, got %q", record.State)
	}
}

func TestDetermineDeletionEligibility(test *testing.T) {
	base := func() WorktreeInfo {
		worktree := newWorktreeInfoDefaults()
		worktree.Kind = KindNestedWorktree
		worktree.Cleanliness = CleanlinessClean
		worktree.PullRequestState = PullRequestMerged
		return worktree
	}
	if got := DetermineDeletionEligibility(base()); got != EligibilityDeletable {
		test.Errorf("merged clean nested should be Deletable, got %q", got)
	}

	dirty := base()
	dirty.DirtyFileCount = 1
	if got := DetermineDeletionEligibility(dirty); got != EligibilityNotDeletable {
		test.Errorf("dirty should be NotDeletable, got %q", got)
	}

	ahead := base()
	ahead.AheadCount = 1
	if got := DetermineDeletionEligibility(ahead); got != EligibilityNotDeletable {
		test.Errorf("ahead should be NotDeletable, got %q", got)
	}

	notMerged := base()
	notMerged.PullRequestState = PullRequestOpen
	if got := DetermineDeletionEligibility(notMerged); got != EligibilityNotDeletable {
		test.Errorf("open PR should be NotDeletable, got %q", got)
	}

	root := base()
	root.Kind = KindRootWorktree
	if got := DetermineDeletionEligibility(root); got != EligibilityNotDeletable {
		test.Errorf("root worktree should be NotDeletable, got %q", got)
	}
}

func TestDetermineClaudeLiveness(test *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		name      string
		state     ClaudeState
		updatedAt time.Time
		want      ClaudeLiveness
	}{
		{"idle is inactive", ClaudeStateIdle, now, LivenessInactive},
		{"working recent is active", ClaudeStateWorking, now.Add(-time.Minute), LivenessActive},
		{"working old is stale", ClaudeStateWorking, now.Add(-3 * time.Hour), LivenessStale},
		{"working no timestamp is active", ClaudeStateWorking, time.Time{}, LivenessActive},
		{"waiting recent is active", ClaudeStateWaiting, now.Add(-time.Second), LivenessActive},
	}
	for _, testCase := range cases {
		worktree := newWorktreeInfoDefaults()
		worktree.ClaudeState = testCase.state
		worktree.ClaudeStateUpdatedAt = testCase.updatedAt
		if got := DetermineClaudeLiveness(worktree, now); got != testCase.want {
			test.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestInterpretPullRequestState(test *testing.T) {
	cases := []struct {
		rawState string
		isDraft  bool
		want     PullRequestState
	}{
		{"OPEN", false, PullRequestOpen},
		{"OPEN", true, PullRequestDraft},
		{"MERGED", false, PullRequestMerged},
		{"CLOSED", false, PullRequestClosed},
		{"", false, PullRequestNone},
		{"SOMETHING", false, PullRequestUnknown},
	}
	for _, testCase := range cases {
		if got := interpretPullRequestState(testCase.rawState, testCase.isDraft); got != testCase.want {
			test.Errorf("interpretPullRequestState(%q,%v) = %q, want %q", testCase.rawState, testCase.isDraft, got, testCase.want)
		}
	}
}

func TestBuildTerminalCommand(test *testing.T) {
	cases := []struct {
		terminal string
		inner    []string
		want     []string
	}{
		{"wezterm", nil, []string{"wezterm", "start", "--cwd", "/dir"}},
		{"wezterm", []string{"claude", "x"}, []string{"wezterm", "start", "--cwd", "/dir", "--", "claude", "x"}},
		{"kitty", []string{"claude"}, []string{"kitty", "--directory", "/dir", "claude"}},
		{"gnome-terminal", []string{"claude"}, []string{"gnome-terminal", "--working-directory=/dir", "--", "claude"}},
		{"xterm", []string{"claude"}, []string{"xterm", "-e", "claude"}},
	}
	for _, testCase := range cases {
		got := BuildTerminalCommand(testCase.terminal, "/dir", testCase.inner)
		if !reflect.DeepEqual(got, testCase.want) {
			test.Errorf("BuildTerminalCommand(%q) = %v, want %v", testCase.terminal, got, testCase.want)
		}
	}
}

func TestFilterTombstoned(test *testing.T) {
	dashboard := NewDashboardModel("/repo")
	deletedKey := "wt:/repo/worktrees/gone"
	dashboard.deletedKeys[deletedKey] = time.Now().Add(TombstoneWindow)

	gone := newWorktreeInfoDefaults()
	gone.Path = "/repo/worktrees/gone"
	kept := newWorktreeInfoDefaults()
	kept.Path = "/repo/worktrees/here"

	result := dashboard.filterTombstoned([]WorktreeInfo{gone, kept})
	if len(result) != 1 || result[0].Path != "/repo/worktrees/here" {
		test.Fatalf("tombstoned row should be dropped, got %+v", result)
	}
	// The tombstone stays while the deleted row is still reported by discovery.
	if _, present := dashboard.deletedKeys[deletedKey]; !present {
		test.Errorf("tombstone should persist while the row is still present")
	}

	// Once the deleted row no longer appears, its tombstone clears.
	dashboard.filterTombstoned([]WorktreeInfo{kept})
	if _, present := dashboard.deletedKeys[deletedKey]; present {
		test.Errorf("tombstone should clear once the row is gone")
	}
}

func TestMatchesSearch(test *testing.T) {
	dashboard := NewDashboardModel("/repo")
	worktree := newWorktreeInfoDefaults()
	worktree.Branch = "jaz-feature-login"
	worktree.ClaudeSessionName = "Fix the bug"

	dashboard.searchPattern = ""
	if !dashboard.matchesSearch(worktree) {
		test.Errorf("empty pattern should match everything")
	}
	dashboard.searchPattern = "LOGIN"
	if !dashboard.matchesSearch(worktree) {
		test.Errorf("case-insensitive branch match failed")
	}
	dashboard.searchPattern = "fix.*bug"
	if !dashboard.matchesSearch(worktree) {
		test.Errorf("regex over claude name failed")
	}
	dashboard.searchPattern = "nomatch"
	if dashboard.matchesSearch(worktree) {
		test.Errorf("unrelated pattern should not match")
	}
	dashboard.searchPattern = "[" // invalid regex → literal fallback
	if dashboard.matchesSearch(worktree) {
		test.Errorf("invalid regex literal '[' should not match this row")
	}
	worktree.Branch = "weird[branch"
	if !dashboard.matchesSearch(worktree) {
		test.Errorf("invalid regex should fall back to literal substring match")
	}
}

func TestDetermineBlinkPhase(test *testing.T) {
	if DetermineBlinkPhase(time.UnixMilli(0)) != BlinkVisible {
		test.Errorf("millis 0 should be Visible")
	}
	if DetermineBlinkPhase(time.UnixMilli(500)) != BlinkHidden {
		test.Errorf("millis 500 should be Hidden")
	}
	if DetermineBlinkPhase(time.UnixMilli(1000)) != BlinkVisible {
		test.Errorf("millis 1000 should be Visible")
	}
}

// sampleWorktrees builds a nested worktree and a branch-only row for view tests.
func sampleWorktrees() []WorktreeInfo {
	nested := newWorktreeInfoDefaults()
	nested.Path = "/repo/worktrees/login"
	nested.Branch = "jaz-feature-login"
	nested.Kind = KindNestedWorktree
	nested.Cleanliness = CleanlinessClean

	branchOnly := newWorktreeInfoDefaults()
	branchOnly.Branch = "jaz-orphan-branch"
	branchOnly.Kind = KindBranchOnly
	branchOnly.Cleanliness = CleanlinessClean

	return []WorktreeInfo{nested, branchOnly}
}

func TestViewRendersRowsAndDetail(test *testing.T) {
	model := NewDashboardModel("/repo")

	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	view := stripAnsi(dashboard.View())
	for _, needle := range []string{"Branch", "jaz-feature-login", "jaz-orphan-branch", "Path"} {
		if !contains(view, needle) {
			test.Errorf("view missing %q\n---\n%s", needle, view)
		}
	}
	if dashboard.currentKey != "wt:/repo/worktrees/login" {
		test.Errorf("cursor should start on first row, got %q", dashboard.currentKey)
	}
}

func TestMouseClickSelectsRow(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	// Row 0 is the column header; the first data row is at Y=1, the second at Y=2.
	clicked, _ := dashboard.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 2})
	dashboard = clicked.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("click on second row should select it, got %q", dashboard.currentKey)
	}

	// A click in the detail pane (past the table width) must not change selection.
	deep, _ := dashboard.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 500, Y: 1})
	dashboard = deep.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("click in detail pane should not move cursor, got %q", dashboard.currentKey)
	}

	// A click on the column header row must not change selection.
	header, _ := dashboard.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 0})
	dashboard = header.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("click on header row should not move cursor, got %q", dashboard.currentKey)
	}
}

func TestMouseWheelMovesCursor(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	down, _ := dashboard.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	dashboard = down.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("wheel down should move to second row, got %q", dashboard.currentKey)
	}
	up, _ := dashboard.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	dashboard = up.(DashboardModel)
	if dashboard.currentKey != "wt:/repo/worktrees/login" {
		test.Errorf("wheel up should move back to first row, got %q", dashboard.currentKey)
	}
}

func TestNavigationMovesCursor(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})

	moved, _ := populated.(DashboardModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	dashboard := moved.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("down should move cursor to second row, got %q", dashboard.currentKey)
	}
}

func TestSearchFiltersRows(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	// Open search and type a pattern matching only the orphan branch.
	opened, _ := dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	dashboard = opened.(DashboardModel)
	for _, character := range "orphan" {
		typed, _ := dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{character}})
		dashboard = typed.(DashboardModel)
	}
	if len(dashboard.visibleKeys) != 1 || dashboard.visibleKeys[0] != "br:jaz-orphan-branch" {
		test.Errorf("search should leave only the orphan branch visible, got %v", dashboard.visibleKeys)
	}
}

func TestDeleteGuardsBlockProtected(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})

	protected := newWorktreeInfoDefaults()
	protected.Branch = "develop"
	protected.Kind = KindBranchOnly
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: []WorktreeInfo{protected}})
	dashboard := populated.(DashboardModel)

	requested, _ := dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	dashboard = requested.(DashboardModel)
	if dashboard.dialog.IsOpen() {
		test.Errorf("delete of a protected branch must not open a confirm dialog")
	}
	if dashboard.notification == "" {
		test.Errorf("delete of a protected branch should surface a refusal notification")
	}
}

func TestConfirmDialogAcceptsEnter(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})

	deletable := newWorktreeInfoDefaults()
	deletable.Branch = "jaz-feature-login"
	deletable.Kind = KindBranchOnly
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: []WorktreeInfo{deletable}})
	dashboard := populated.(DashboardModel)

	requested, _ := dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	dashboard = requested.(DashboardModel)
	if dashboard.dialog.Kind != DialogConfirm {
		test.Fatalf("pressing d on a deletable row should open a confirm dialog, got %q", dashboard.dialog.Kind)
	}

	confirmed, command := dashboard.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dashboard = confirmed.(DashboardModel)
	if dashboard.dialog.IsOpen() {
		test.Errorf("enter should confirm and close the dialog")
	}
	if command == nil {
		test.Errorf("enter should confirm the deletion and return a delete command")
	}
}

func TestDeleteSelectsNextRow(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	// Cursor starts on the first row; deleting it should land on the next row.
	afterFirst, _ := dashboard.Update(rowDeletedMsg{key: "wt:/repo/worktrees/login"})
	dashboard = afterFirst.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Errorf("deleting the first row should select the next one, got %q", dashboard.currentKey)
	}
}

func TestDeleteOfLastRowSelectsPrevious(test *testing.T) {
	model := NewDashboardModel("/repo")
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	populated, _ := sized.(DashboardModel).Update(worktreesDiscoveredMsg{worktrees: sampleWorktrees()})
	dashboard := populated.(DashboardModel)

	moved, _ := dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	dashboard = moved.(DashboardModel)
	if dashboard.currentKey != "br:jaz-orphan-branch" {
		test.Fatalf("expected cursor on the second row before delete, got %q", dashboard.currentKey)
	}

	afterLast, _ := dashboard.Update(rowDeletedMsg{key: "br:jaz-orphan-branch"})
	dashboard = afterLast.(DashboardModel)
	if dashboard.currentKey != "wt:/repo/worktrees/login" {
		test.Errorf("deleting the last row should select the previous one, got %q", dashboard.currentKey)
	}
}

func TestSpanBackgroundAcrossResets(test *testing.T) {
	// Two cells, each ending in a reset, plus a plain tail.
	row := "[R]green[R] [R]yellow[R]  tail"
	spanned := spanBackgroundAcrossResets(row, "[BG]", "[R]")

	if !strings.HasPrefix(spanned, "[BG]") {
		test.Errorf("row should open with the background, got %q", spanned)
	}
	if !strings.HasSuffix(spanned, "[R]") {
		test.Errorf("row should end with a reset, got %q", spanned)
	}
	// Every reset except the single closing one must be immediately followed by
	// the background being re-applied, so no cell (or the tail) loses the highlight.
	resetTotal := strings.Count(spanned, "[R]")
	reapplied := strings.Count(spanned, "[R][BG]")
	if resetTotal-reapplied != 1 {
		test.Errorf("only the closing reset should lack a re-apply: %d resets, %d re-applies, %q", resetTotal, reapplied, spanned)
	}
	// An empty open sequence (no color profile) must leave the row untouched.
	if spanBackgroundAcrossResets(row, "", "[R]") != row {
		test.Errorf("with no open sequence the row must be returned unchanged")
	}
}

func contains(haystack string, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack string, needle string) int {
	for start := 0; start+len(needle) <= len(haystack); start++ {
		if haystack[start:start+len(needle)] == needle {
			return start
		}
	}
	return -1
}

func TestItemKey(test *testing.T) {
	worktree := newWorktreeInfoDefaults()
	worktree.Path = "/repo/wt"
	worktree.Branch = "feature"
	if ItemKey(worktree) != "wt:/repo/wt" {
		test.Errorf("worktree with path should key by path, got %q", ItemKey(worktree))
	}
	branchOnly := newWorktreeInfoDefaults()
	branchOnly.Branch = "feature"
	if ItemKey(branchOnly) != "br:feature" {
		test.Errorf("branch-only should key by branch, got %q", ItemKey(branchOnly))
	}
}
