package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// discovery.go finds worktrees and branch-only entries and orders them for
// display. Ported from v1's discover_worktrees / discover_branches / discover_all.

// rootWorktreePattern matches a plain scr-NN root worktree, e.g. /home/jaz/code/notable/scr-01.
var rootWorktreePattern = regexp.MustCompile(`/code/notable/scr-(\d{2})/?$`)

// legacyRootWorktreeNames are suffixed scr-NN checkouts that predate the plain scr-NN layout.
var legacyRootWorktreeNames = map[string]struct{}{
	"scr-02-backend":       {},
	"scr-03-web":           {},
	"scr-04-design-system": {},
	"scr-05-packages":      {},
	"scr-06-misc":          {},
	"scr-07-misc":          {},
	"scr-09-misc":          {},
	"scr-10-misc":          {},
}

// CategorizePath decides whether a worktree path is a root or a nested worktree.
func CategorizePath(path string) WorktreeKind {
	if _, found := getPermanentWorktreeLabel(path); found {
		return KindRootWorktree
	}
	return KindNestedWorktree
}

// getPermanentWorktreeLabel returns the fixed index for a permanent worktree path.
func getPermanentWorktreeLabel(path string) (string, bool) {
	trimmed := strings.TrimSuffix(path, "/")
	if strings.HasSuffix(trimmed, "/code/notable/vivaa") {
		return "v", true
	}
	if match := rootWorktreePattern.FindStringSubmatch(trimmed); match != nil {
		return strings.TrimPrefix(match[1], "0"), true
	}
	parent, name := filepath.Split(trimmed)
	if _, found := legacyRootWorktreeNames[name]; found && strings.HasSuffix(parent, "/code/notable/") {
		return strings.TrimPrefix(name[len("scr-"):len("scr-NN")], "0"), true
	}
	return "", false
}

// PermanentWorktreeLabel returns the fixed index shown to the left of a permanent
// (root) worktree row: the NN from a scr-NN checkout, "v" for vivaa, "" otherwise.
func PermanentWorktreeLabel(worktree WorktreeInfo) string {
	if worktree.Kind != KindRootWorktree {
		return ""
	}
	label, _ := getPermanentWorktreeLabel(worktree.Path)
	return label
}

// newWorktreeInfoDefaults returns a WorktreeInfo with every string-union field
// set to its "none/unknown" member, so callers only override what they know.
func newWorktreeInfoDefaults() WorktreeInfo {
	return WorktreeInfo{
		Kind:             KindNestedWorktree,
		Role:             RoleLinked,
		Shape:            ShapeNormal,
		HeadState:        HeadOnBranch,
		LockState:        LockUnlocked,
		Cleanliness:      CleanlinessUnknown,
		PullRequestState: PullRequestNone,
		ReviewDecision:   ReviewNone,
		MergeState:       MergeUnknown,
		PullRequestLoad:  LoadIdle,
		ComposeStatus:    ComposeNotConfigured,
		PiState:          PiStateNone,
	}
}

// ParseWorktreePorcelain parses `git worktree list --porcelain` into records.
func ParseWorktreePorcelain(porcelain string) []WorktreeInfo {
	worktrees := []WorktreeInfo{}
	current := newWorktreeInfoDefaults()
	haveCurrent := false

	flush := func() {
		if haveCurrent && current.Path != "" {
			current.Kind = CategorizePath(current.Path)
			worktrees = append(worktrees, current)
		}
		current = newWorktreeInfoDefaults()
		haveCurrent = false
	}

	for _, line := range strings.Split(porcelain, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			haveCurrent = true
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			current.Shape = ShapeBare
		case line == "detached":
			current.HeadState = HeadDetached
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.LockState = LockLocked
			current.LockReason = strings.TrimSpace(strings.TrimPrefix(line, "locked"))
		}
	}
	flush()
	return worktrees
}

// DiscoverWorktrees lists the repository's worktrees, marking the first as primary.
func DiscoverWorktrees(repositoryRoot string) ([]WorktreeInfo, error) {
	result := RunCommand(DefaultCommandTimeout, repositoryRoot, "git", "worktree", "list", "--porcelain")
	if !result.Succeeded() {
		return nil, fmt.Errorf("git worktree list failed: %s", strings.TrimSpace(result.Stderr))
	}
	worktrees := ParseWorktreePorcelain(result.Stdout)
	for index := range worktrees {
		worktrees[index].RepositoryRoot = repositoryRoot
		if index == 0 {
			worktrees[index].Role = RolePrimary
		}
	}
	return worktrees, nil
}

// GetFirstWorktreePath returns the primary worktree's path, or "" on failure.
func GetFirstWorktreePath(repositoryRoot string) string {
	result := RunCommand(DefaultCommandTimeout, repositoryRoot, "git", "worktree", "list", "--porcelain")
	if !result.Succeeded() {
		return ""
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree ")
		}
	}
	return ""
}

// DiscoverBranches lists local branches that are not already checked out in a
// worktree, as branch-only rows.
func DiscoverBranches(repositoryRoot string, branchesInWorktrees map[string]struct{}) []WorktreeInfo {
	result := RunCommand(
		DefaultCommandTimeout, repositoryRoot,
		"git", "for-each-ref",
		"--format=%(refname:short)|%(upstream:short)|%(objectname)",
		"refs/heads/",
	)
	if !result.Succeeded() {
		return nil
	}
	branches := []WorktreeInfo{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		branchName, upstream := parts[0], parts[1]
		if branchName == "" {
			continue
		}
		if _, checkedOut := branchesInWorktrees[branchName]; checkedOut {
			continue
		}
		branch := newWorktreeInfoDefaults()
		branch.Branch = branchName
		branch.Head = parts[2]
		branch.Kind = KindBranchOnly
		branch.RepositoryRoot = repositoryRoot
		branch.Upstream = upstream
		branches = append(branches, branch)
	}
	return branches
}

// kindDisplayOrder ranks the row kinds: nested worktrees, then branch-only, then
// root worktrees (matching v1's KIND_ORDER).
var kindDisplayOrder = map[WorktreeKind]int{
	KindNestedWorktree: 0,
	KindBranchOnly:     1,
	KindRootWorktree:   2,
}

// DiscoverAll returns every worktree and branch-only row (excluding `main`),
// ordered by kind, with pi session state attached.
func DiscoverAll(repositoryRoot string) ([]WorktreeInfo, error) {
	worktrees, discoverError := DiscoverWorktrees(repositoryRoot)
	if discoverError != nil {
		return nil, discoverError
	}
	checkedOut := map[string]struct{}{}
	for _, worktree := range worktrees {
		if worktree.Branch != "" {
			checkedOut[worktree.Branch] = struct{}{}
		}
	}
	branches := DiscoverBranches(repositoryRoot, checkedOut)

	combined := []WorktreeInfo{}
	for _, item := range append(worktrees, branches...) {
		if item.Branch == "main" {
			continue
		}
		combined = append(combined, item)
	}
	sort.SliceStable(combined, func(leftIndex, rightIndex int) bool {
		return kindDisplayOrder[combined[leftIndex].Kind] < kindDisplayOrder[combined[rightIndex].Kind]
	})
	for index := range combined {
		AttachPiState(&combined[index])
	}
	return combined, nil
}

// FindRepositoryRoot resolves the git top-level directory containing startDirectory.
func FindRepositoryRoot(startDirectory string) (string, error) {
	result := RunCommand(DefaultCommandTimeout, startDirectory, "git", "rev-parse", "--show-toplevel")
	if !result.Succeeded() {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = startDirectory
		}
		return "", fmt.Errorf("not inside a git repository: %s", detail)
	}
	return strings.TrimSpace(result.Stdout), nil
}
