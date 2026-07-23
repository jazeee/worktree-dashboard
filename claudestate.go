package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// claudestate.go reads each worktree's .claude-session-state file and derives its
// liveness. Ported from v1's read_claude_session_state / attach_claude_state /
// _claude_state_is_stale.

// ClaudeSessionRecord is the parsed contents of a .claude-session-state file.
type ClaudeSessionRecord struct {
	SessionIdentifier string
	State             ClaudeState
	UpdatedAt         time.Time
	SessionName       string
}

// ReadClaudeSessionState parses <worktreePath>/.claude-session-state. Missing or
// unparsable fields come back as their zero values.
func ReadClaudeSessionState(worktreePath string) ClaudeSessionRecord {
	record := ClaudeSessionRecord{State: ClaudeStateNone}
	stateFilePath := filepath.Join(worktreePath, ClaudeSessionStateFileName)
	contents, readError := os.ReadFile(stateFilePath)
	if readError != nil {
		return record
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	record.SessionIdentifier = values["id"]
	record.State = ClaudeState(values["state"])
	record.SessionName = values["name"]
	if rawUpdated := values["updated"]; rawUpdated != "" {
		if epochSeconds, parseError := strconv.ParseInt(rawUpdated, 10, 64); parseError == nil {
			record.UpdatedAt = time.Unix(epochSeconds, 0)
		}
	}
	return record
}

// AttachClaudeState fills a worktree's Claude fields from its state file.
func AttachClaudeState(worktree *WorktreeInfo) {
	if worktree.Path == "" {
		return
	}
	record := ReadClaudeSessionState(worktree.Path)
	worktree.ClaudeSessionIdentifier = record.SessionIdentifier
	worktree.ClaudeState = record.State
	worktree.ClaudeStateUpdatedAt = record.UpdatedAt
	worktree.ClaudeSessionName = record.SessionName
}

// DetermineClaudeLiveness classifies a session as Active, Stale, or Inactive.
// Only working/waiting sessions can be Active or Stale; a record with no
// timestamp is treated as Active (never stale), matching v1.
func DetermineClaudeLiveness(worktree WorktreeInfo, now time.Time) ClaudeLiveness {
	if worktree.ClaudeState != ClaudeStateWorking && worktree.ClaudeState != ClaudeStateWaiting {
		return LivenessInactive
	}
	if worktree.ClaudeStateUpdatedAt.IsZero() {
		return LivenessActive
	}
	if now.Sub(worktree.ClaudeStateUpdatedAt) > ClaudeStaleThreshold {
		return LivenessStale
	}
	return LivenessActive
}

// isRegularFile reports whether path exists and is a regular file.
func isRegularFile(path string) bool {
	info, statError := os.Stat(path)
	if statError != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// pathExists reports whether path exists at all.
func pathExists(path string) bool {
	_, statError := os.Stat(path)
	return statError == nil
}
