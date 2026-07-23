package main

import "encoding/json"

// pullrequest.go queries `gh` for a branch's pull request and normalizes the
// result into the domain's named unions. Ported from v1's fetch_pr_fields /
// apply_pr_fields / carry_pr_fields.

// pullRequestPayload mirrors the JSON shape returned by `gh pr view --json`.
type pullRequestPayload struct {
	Number         int    `json:"number"`
	Url            string `json:"url"`
	State          string `json:"state"`
	ReviewDecision string `json:"reviewDecision"`
	Title          string `json:"title"`
	IsDraft        bool   `json:"isDraft"`
}

// FetchPullRequest queries gh for the PR on the worktree's branch. The bool
// reports whether a PR payload was obtained (false = no branch/PR, gh failed, or
// unparsable output).
func FetchPullRequest(worktree WorktreeInfo) (pullRequestPayload, bool) {
	if worktree.Branch == "" || worktree.Shape == ShapeBare || worktree.HeadState == HeadDetached {
		return pullRequestPayload{}, false
	}
	workingDirectory := worktree.Path
	if workingDirectory == "" {
		workingDirectory = worktree.RepositoryRoot
	}
	if workingDirectory == "" {
		return pullRequestPayload{}, false
	}
	result := RunCommand(
		DefaultCommandTimeout, workingDirectory,
		"gh", "pr", "view", worktree.Branch,
		"--json", "number,url,state,reviewDecision,title,isDraft",
	)
	if !result.Succeeded() {
		return pullRequestPayload{}, false
	}
	var payload pullRequestPayload
	if json.Unmarshal([]byte(result.Stdout), &payload) != nil {
		return pullRequestPayload{}, false
	}
	return payload, true
}

// interpretPullRequestState folds gh's state + isDraft into one named union.
func interpretPullRequestState(rawState string, isDraft bool) PullRequestState {
	switch rawState {
	case "OPEN":
		if isDraft {
			return PullRequestDraft
		}
		return PullRequestOpen
	case "MERGED":
		return PullRequestMerged
	case "CLOSED":
		return PullRequestClosed
	case "":
		return PullRequestNone
	default:
		return PullRequestUnknown
	}
}

// interpretReviewDecision maps gh's reviewDecision string to a named union.
func interpretReviewDecision(rawDecision string) ReviewDecision {
	switch rawDecision {
	case "APPROVED":
		return ReviewApproved
	case "CHANGES_REQUESTED":
		return ReviewChangesRequested
	case "REVIEW_REQUIRED":
		return ReviewPending
	default:
		return ReviewNone
	}
}

// ApplyPullRequestFields copies a fetched payload onto a worktree in place.
func ApplyPullRequestFields(worktree *WorktreeInfo, payload pullRequestPayload) {
	worktree.PullRequestNumber = payload.Number
	worktree.PullRequestUrl = payload.Url
	worktree.PullRequestState = interpretPullRequestState(payload.State, payload.IsDraft)
	worktree.PullRequestTitle = payload.Title
	worktree.ReviewDecision = interpretReviewDecision(payload.ReviewDecision)
}

// CarryPullRequestFields copies PR state from a prior record onto a freshly
// discovered one, so the fast git poll doesn't blank PR data that only the slow
// PR poll refreshes.
func CarryPullRequestFields(destination *WorktreeInfo, source WorktreeInfo) {
	destination.PullRequestNumber = source.PullRequestNumber
	destination.PullRequestUrl = source.PullRequestUrl
	destination.PullRequestState = source.PullRequestState
	destination.PullRequestTitle = source.PullRequestTitle
	destination.ReviewDecision = source.ReviewDecision
}
