package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// pistate.go reads each worktree's .claude-session-state file and derives its
// liveness. Ported from v1's read_pi_session_state / attach_pi_state /
// _pi_state_is_stale.

// PiSessionRecord is the parsed contents of a .claude-session-state file.
type PiSessionRecord struct {
	SessionIdentifier string
	State             PiState
	UpdatedAt         time.Time
	SessionName       string
}

// ReadPiSessionState parses <worktreePath>/.claude-session-state. Missing or
// unparsable fields come back as their zero values.
func ReadPiSessionState(worktreePath string) PiSessionRecord {
	record := PiSessionRecord{State: PiStateNone}
	stateFilePath := filepath.Join(worktreePath, PiSessionStateFileName)
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
	record.State = PiState(values["state"])
	record.SessionName = values["name"]
	if rawUpdated := values["updated"]; rawUpdated != "" {
		if epochSeconds, parseError := strconv.ParseInt(rawUpdated, 10, 64); parseError == nil {
			record.UpdatedAt = time.Unix(epochSeconds, 0)
		}
	}
	return record
}

// AttachPiState fills a worktree's Pi fields from its state file.
func AttachPiState(worktree *WorktreeInfo) {
	if worktree.Path == "" {
		return
	}
	record := ReadPiSessionState(worktree.Path)
	worktree.PiSessionIdentifier = record.SessionIdentifier
	worktree.PiState = record.State
	worktree.PiStateUpdatedAt = record.UpdatedAt
	worktree.PiSessionName = record.SessionName
}

// DeterminePiLiveness classifies a session as Active, Stale, or Inactive.
// Only working/waiting sessions can be Active or Stale; a record with no
// timestamp is treated as Active (never stale), matching v1.
func DeterminePiLiveness(worktree WorktreeInfo, now time.Time) PiLiveness {
	if worktree.PiState != PiStateWorking && worktree.PiState != PiStateWaiting {
		return LivenessInactive
	}
	if worktree.PiStateUpdatedAt.IsZero() {
		return LivenessActive
	}
	if now.Sub(worktree.PiStateUpdatedAt) > PiStaleThreshold {
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
