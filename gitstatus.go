package main

import (
	"strconv"
	"strings"
)

// gitstatus.go collects per-row dirty/ahead/behind/upstream and commit history.
// Ported from v1's collect_git_status / _parse_status_v2 / _collect_commit_log.

// commitLogFieldSeparator is the unit-separator delimiter for `git log` fields —
// it never appears in commit text, so splitting on it is safe even when a
// subject contains "|", spaces, or newlines.
const commitLogFieldSeparator = "\x1f"

// commitLogPretty is the --pretty format producing short-hash, subject, relative.
const commitLogPretty = "--pretty=%h" + commitLogFieldSeparator + "%s" + commitLogFieldSeparator + "%cr"

// ParseStatusV2 fills dirty/ahead/behind/upstream from
// `git status --porcelain=v2 --branch`. Header lines begin with '#'; every other
// non-empty line is a changed or untracked entry.
func ParseStatusV2(worktree *WorktreeInfo, output string) {
	dirtyCount := 0
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			switch {
			case strings.HasPrefix(line, "# branch.ab "):
				for _, part := range strings.Fields(line)[2:] {
					if strings.HasPrefix(part, "+") {
						worktree.AheadCount = parseIntOrZero(part[1:])
					} else if strings.HasPrefix(part, "-") {
						worktree.BehindCount = parseIntOrZero(part[1:])
					}
				}
			case strings.HasPrefix(line, "# branch.upstream "):
				worktree.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "# branch.upstream "))
			}
			continue
		}
		dirtyCount++
	}
	worktree.DirtyFileCount = dirtyCount
	if dirtyCount == 0 {
		worktree.Cleanliness = CleanlinessClean
	} else {
		worktree.Cleanliness = CleanlinessDirty
	}
}

// CollectCommitLog runs one `git log -5` to populate both the latest-commit
// summary and the recent-commits list. An empty ref means the worktree's HEAD.
func CollectCommitLog(worktree *WorktreeInfo, workingDirectory string, ref string) {
	arguments := []string{"log", "-5", commitLogPretty}
	if ref != "" {
		arguments = append(arguments, ref)
	}
	result := RunCommand(DefaultCommandTimeout, workingDirectory, "git", arguments...)
	if !result.Succeeded() {
		return
	}
	recent := []string{}
	for index, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.Split(line, commitLogFieldSeparator)
		if len(parts) != 3 {
			continue
		}
		shortHash, subject, relative := parts[0], parts[1], parts[2]
		if index == 0 {
			worktree.LastCommitSubject = subject
			worktree.LastCommitRelative = relative
		}
		recent = append(recent, shortHash+" "+subject+" ("+relative+")")
	}
	worktree.RecentCommits = recent
}

// CollectGitStatus fills a worktree's git state in place.
func CollectGitStatus(worktree *WorktreeInfo) {
	if worktree.Shape == ShapeBare {
		worktree.Cleanliness = CleanlinessClean
		return
	}
	if worktree.Path == "" {
		collectBranchOnlyStatus(worktree)
		return
	}
	workingDirectory := worktree.Path
	result := RunCommand(DefaultCommandTimeout, workingDirectory, "git", "status", "--porcelain=v2", "--branch")
	if !result.Succeeded() {
		worktree.CollectionError = "git status: " + strings.TrimSpace(result.Stderr)
		return
	}
	ParseStatusV2(worktree, result.Stdout)
	CollectCommitLog(worktree, workingDirectory, "")
}

// collectBranchOnlyStatus fills state for a branch that has no worktree, using the
// repository root as the working directory.
func collectBranchOnlyStatus(worktree *WorktreeInfo) {
	if worktree.RepositoryRoot == "" || worktree.Branch == "" {
		return
	}
	workingDirectory := worktree.RepositoryRoot
	worktree.Cleanliness = CleanlinessClean
	worktree.DirtyFileCount = 0

	if worktree.Upstream != "" {
		result := RunCommand(
			DefaultCommandTimeout, workingDirectory,
			"git", "rev-list", "--left-right", "--count",
			worktree.Upstream+"..."+worktree.Branch,
		)
		if result.Succeeded() {
			parts := strings.Fields(strings.TrimSpace(result.Stdout))
			if len(parts) == 2 {
				worktree.BehindCount = parseIntOrZero(parts[0])
				worktree.AheadCount = parseIntOrZero(parts[1])
			}
		}
	}
	CollectCommitLog(worktree, workingDirectory, worktree.Branch)
}

// parseIntOrZero parses a base-10 integer, returning 0 on any error.
func parseIntOrZero(text string) int {
	value, parseError := strconv.Atoi(strings.TrimSpace(text))
	if parseError != nil {
		return 0
	}
	return value
}
