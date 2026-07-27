package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// actions.go holds the tea.Cmd constructors for user-triggered actions (open PR,
// editor, terminal, Claude; copy; create; delete). Each runs off the UI goroutine
// and returns a single result message. Ported from v1's action_* handlers.

// worktreeNameSanitizer strips characters not allowed in a branch/worktree name.
var worktreeNameSanitizer = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// whitespaceRun collapses runs of whitespace to a single '-' in a typed name.
var whitespaceRun = regexp.MustCompile(`\s+`)

// openPullRequestCommand opens the row's PR URL in the browser.
func openPullRequestCommand(pullRequestUrl string) tea.Cmd {
	return func() tea.Msg {
		if pullRequestUrl == "" {
			return actionResultMsg{text: "No PR URL for this worktree", severity: SeverityWarning}
		}
		if openError := OpenUrl(pullRequestUrl); openError != nil {
			return actionResultMsg{text: "Failed to open PR: " + openError.Error(), severity: SeverityError}
		}
		return actionResultMsg{text: "Opening " + pullRequestUrl, severity: SeverityInfo}
	}
}

// openInEditorCommand opens the worktree in VS Code.
func openInEditorCommand(worktreePath string) tea.Cmd {
	return func() tea.Msg {
		if worktreePath == "" {
			return actionResultMsg{text: "No worktree path for this branch", severity: SeverityWarning}
		}
		if launchError := LaunchDetachedProcess([]string{"code", worktreePath}, ""); launchError != nil {
			return actionResultMsg{text: "`code` command not found on PATH", severity: SeverityError}
		}
		return actionResultMsg{text: "Opening in VS Code: " + worktreePath, severity: SeverityInfo}
	}
}

// openInTerminalCommand opens a terminal in the worktree.
func openInTerminalCommand(worktreePath string) tea.Cmd {
	return func() tea.Msg {
		if worktreePath == "" {
			return actionResultMsg{text: "No worktree path for this branch", severity: SeverityWarning}
		}
		argv, terminalName, found := SelectTerminalCommand(worktreePath, nil)
		if !found {
			return actionResultMsg{text: noTerminalMessage(), severity: SeverityError}
		}
		if launchError := LaunchTerminalWindow(argv, terminalName, worktreePath); launchError != nil {
			return actionResultMsg{text: "Failed to launch " + terminalName + ": " + launchError.Error(), severity: SeverityError}
		}
		return actionResultMsg{text: "Opening in " + terminalName + ": " + worktreePath, severity: SeverityInfo}
	}
}

// openClaudeCommand opens a terminal running the Claude launcher in the worktree.
// resumeWord is "resuming" or "fresh", chosen by the caller from the row's state.
func openClaudeCommand(worktreePath string, resumeWord string) tea.Cmd {
	return func() tea.Msg {
		terminalName, failure := openClaudeInTerminal(worktreePath)
		if failure != "" {
			return actionResultMsg{text: failure, severity: SeverityError}
		}
		return actionResultMsg{
			text:     fmt.Sprintf("Opening Claude (%s) in %s: %s", resumeWord, terminalName, worktreePath),
			severity: SeverityInfo,
		}
	}
}

// openClaudeInTerminal launches the Claude session script in a terminal at path.
// On success it returns the terminal name and an empty failure; otherwise the
// terminal name is empty and failure describes what went wrong.
func openClaudeInTerminal(worktreePath string) (terminalName string, failure string) {
	launcher := ClaudeSessionLauncherPath()
	if !isRegularFile(launcher) {
		return "", "Launcher not found: " + launcher
	}
	launchCommand := []string{launcher, worktreePath}
	argv, chosenTerminal, found := SelectTerminalCommand(worktreePath, launchCommand)
	if !found {
		return "", noTerminalMessage()
	}
	if launchError := LaunchTerminalWindow(argv, chosenTerminal, worktreePath); launchError != nil {
		return "", "Failed to launch " + chosenTerminal + ": " + launchError.Error()
	}
	return chosenTerminal, ""
}

// noTerminalMessage matches v1's guidance when no terminal emulator is found.
func noTerminalMessage() string {
	return "No supported terminal emulator found on PATH (set $TERMINAL or install one of: " +
		strings.Join(terminalPreference, ", ") + ")"
}

// copyPathCommand copies the worktree path to the system clipboard.
func copyPathCommand(worktreePath string) tea.Cmd {
	return func() tea.Msg {
		if worktreePath == "" {
			return actionResultMsg{text: "No worktree path for this branch", severity: SeverityWarning}
		}
		toolName, copyError := CopyToSystemClipboard(worktreePath)
		if copyError != nil {
			return actionResultMsg{text: "Copy failed: " + copyError.Error(), severity: SeverityError}
		}
		return actionResultMsg{text: "Copied via " + toolName + ": " + worktreePath, severity: SeverityInfo}
	}
}

// createBranchWorktreeCommand checks an existing branch out into a new worktree
// under the primary worktree's worktrees/<branch>, then opens Claude in it.
func createBranchWorktreeCommand(repositoryRoot string, branch string) tea.Cmd {
	return func() tea.Msg {
		base := GetFirstWorktreePath(repositoryRoot)
		if base == "" {
			return worktreeCreatedMsg{failure: "Could not determine primary worktree"}
		}
		directoryName := sanitizeSegment(branch, "branch")
		worktreePath := filepath.Join(base, "worktrees", directoryName)
		addResult := RunCommand(60*time.Second, base, "git", "worktree", "add", worktreePath, branch)
		if !addResult.Succeeded() {
			return worktreeCreatedMsg{failure: "git worktree add failed: " + strings.TrimSpace(addResult.Stderr)}
		}
		return worktreeCreatedMsg{note: creationNote(branch, worktreePath)}
	}
}

// createNamedWorktreeCommand branches a fresh worktree off origin/main and opens
// Claude in it. rawName is the user's typed name from the input dialog.
func createNamedWorktreeCommand(baseDirectory string, rawName string) tea.Cmd {
	return func() tea.Msg {
		name := whitespaceRun.ReplaceAllString(strings.TrimSpace(rawName), "-")
		name = strings.Trim(worktreeNameSanitizer.ReplaceAllString(name, "-"), "-")
		if name == "" {
			return worktreeCreatedMsg{failure: "Invalid worktree name"}
		}
		datestamp := time.Now().Unix()
		branch := fmt.Sprintf("jaz-%d-%s", datestamp, name)
		worktreePath := filepath.Join(baseDirectory, "worktrees", fmt.Sprintf("%d-%s", datestamp, name))

		fetchResult := RunCommand(120*time.Second, baseDirectory, "git", "fetch", "origin", "main")
		if !fetchResult.Succeeded() {
			return worktreeCreatedMsg{failure: "git fetch origin main failed: " + strings.TrimSpace(fetchResult.Stderr)}
		}
		addResult := RunCommand(60*time.Second, baseDirectory, "git", "worktree", "add", "-b", branch, worktreePath, "origin/main")
		if !addResult.Succeeded() {
			return worktreeCreatedMsg{failure: "git worktree add failed: " + strings.TrimSpace(addResult.Stderr)}
		}
		return worktreeCreatedMsg{note: creationNote(branch, worktreePath)}
	}
}

// creationNote opens Claude in the new worktree and returns the success message,
// mirroring v1's _after_worktree_created.
func creationNote(branch string, worktreePath string) string {
	terminalName, failure := openClaudeInTerminal(worktreePath)
	if failure != "" {
		return "Created worktree " + branch
	}
	return "Created worktree " + branch + " · opened Claude in " + terminalName
}

// sanitizeSegment turns an arbitrary string into a safe path segment, falling
// back to fallback when nothing survives.
func sanitizeSegment(raw string, fallback string) string {
	cleaned := strings.Trim(worktreeNameSanitizer.ReplaceAllString(raw, "-"), "-")
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

// performDeleteCommand carries out a confirmed deletion: compose down, remove the
// worktree files, prune, delete the branch, and (on leftover files) copy a sudo
// command to the clipboard. Ported from v1's _perform_delete. Callers must have
// already enforced the delete guards before showing the confirm dialog.
func performDeleteCommand(worktree WorktreeInfo, repositoryRoot string) tea.Cmd {
	key := ItemKey(worktree)
	return func() tea.Msg {
		notes := []string{}
		severity := SeverityInfo
		leftoverPath := ""

		if worktree.ComposeStatus == ComposeRunning && worktree.ComposeScriptPath != "" && isRegularFile(worktree.ComposeScriptPath) {
			composeResult := RunCommand(180*time.Second, filepath.Dir(worktree.ComposeScriptPath), worktree.ComposeScriptPath, "down")
			if composeResult.Succeeded() {
				notes = append(notes, "docker compose down ("+worktree.ComposeProjectName+")")
			} else {
				detail := strings.TrimSpace(composeResult.Stderr)
				if detail == "" {
					detail = "unknown error"
				}
				notes = append(notes, "compose down failed (continuing): "+detail)
				severity = SeverityWarning
			}
		}

		if worktree.Path != "" {
			RunCommand(DefaultCommandTimeout, "", "rm", "-rf", worktree.Path)
			if pathExists(worktree.Path) {
				leftoverPath = worktree.Path
			}
			RunCommand(DefaultCommandTimeout, repositoryRoot, "git", "worktree", "prune")
			if leftoverPath == "" {
				notes = append(notes, "removed worktree "+worktree.Path)
			} else {
				notes = append(notes, "git records pruned; files remain at "+worktree.Path)
			}
		}

		if worktree.Branch != "" && !IsProtectedBranch(worktree.Branch) {
			branchResult := RunCommand(DefaultCommandTimeout, repositoryRoot, "git", "branch", "-D", worktree.Branch)
			if branchResult.Succeeded() {
				notes = append(notes, "deleted branch "+worktree.Branch)
			} else {
				notes = append(notes, "git branch -D failed: "+strings.TrimSpace(branchResult.Stderr))
				severity = SeverityWarning
			}
		}

		if leftoverPath != "" {
			sudoCommand := "sudo rm -rf " + leftoverPath
			_, copyError := CopyToSystemClipboard(sudoCommand)
			suffix := ""
			if copyError == nil {
				suffix = " (copied to clipboard)"
			}
			return rowDeletedMsg{
				key:          key,
				notification: "Some files need sudo to remove: " + sudoCommand + suffix,
				severity:     SeverityWarning,
			}
		}
		return rowDeletedMsg{
			key:          key,
			notification: strings.Join(notes, " · "),
			severity:     severity,
		}
	}
}
