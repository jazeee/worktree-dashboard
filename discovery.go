package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// discovery.go finds worktrees and branch-only entries and orders them for
// display. Ported from v1's discover_worktrees / discover_branches / discover_all.

// rootWorktreePattern matches a root worktree path in this user's layout, e.g.
// /home/jaz/code/notable/scr-01 or scr-06-misc. Ported verbatim from v1.
var rootWorktreePattern = regexp.MustCompile(`/code/notable/scr-\d{2}(?:-[^/]+)?/?$`)

// CategorizePath decides whether a worktree path is a root or a nested worktree.
func CategorizePath(path string) WorktreeKind {
	if rootWorktreePattern.MatchString(path) || strings.HasSuffix(path, "/notable/vivaa") {
		return KindRootWorktree
	}
	return KindNestedWorktree
}

// permanentIndexPattern captures the NN in a .../notable/scr-NN[-suffix] root
// worktree path so it can be shown as that worktree's fixed index.
var permanentIndexPattern = regexp.MustCompile(`/notable/scr-(\d+)(?:-[^/]+)?/?$`)

// PermanentWorktreeLabel returns the fixed index shown to the left of a permanent
// (root) worktree row: the number from a scr-NN checkout, "2" for the vivaa
// checkout (a unique case), and "" for any non-permanent row.
func PermanentWorktreeLabel(worktree WorktreeInfo) string {
	if worktree.Kind != KindRootWorktree {
		return ""
	}
	if strings.HasSuffix(strings.TrimSuffix(worktree.Path, "/"), "/notable/vivaa") {
		return "2"
	}
	match := permanentIndexPattern.FindStringSubmatch(worktree.Path)
	if match == nil {
		return ""
	}
	number, conversionError := strconv.Atoi(match[1])
	if conversionError != nil {
		return ""
	}
	return strconv.Itoa(number)
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
