package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// compose.go reports each worktree's docker-compose status. Ported from v1's
// list_running_compose_projects / compose_project_name_for / find_compose_script
// / attach_compose_info.

// composeNameSanitizer matches characters not allowed in a compose project name.
var composeNameSanitizer = regexp.MustCompile(`[^a-z0-9_-]`)

// ComposeProjectNameFor mirrors compose-worktree.sh: lowercase the basename,
// replace disallowed characters with '-', and trim surrounding '-'.
func ComposeProjectNameFor(path string) string {
	baseName := strings.ToLower(filepath.Base(path))
	sanitized := composeNameSanitizer.ReplaceAllString(baseName, "-")
	return strings.Trim(sanitized, "-")
}

// FindComposeScript returns the path to a worktree's compose-worktree.sh, or ""
// when it has none.
func FindComposeScript(worktreePath string) string {
	candidate := filepath.Join(worktreePath, "backend", "compose-worktree.sh")
	if isRegularFile(candidate) {
		return candidate
	}
	return ""
}

// composeListEntry mirrors one element of `docker compose ls --format json`.
type composeListEntry struct {
	Name string `json:"Name"`
}

// ListRunningComposeProjects returns the set of running compose project names.
func ListRunningComposeProjects() map[string]struct{} {
	running := map[string]struct{}{}
	result := RunCommand(DefaultCommandTimeout, "", "docker", "compose", "ls", "--format", "json")
	if !result.Succeeded() || strings.TrimSpace(result.Stdout) == "" {
		return running
	}
	var entries []composeListEntry
	if json.Unmarshal([]byte(result.Stdout), &entries) != nil {
		return running
	}
	for _, entry := range entries {
		if entry.Name != "" {
			running[entry.Name] = struct{}{}
		}
	}
	return running
}

// AttachComposeStatus fills a worktree's compose fields from the running set.
func AttachComposeStatus(worktree *WorktreeInfo, runningProjects map[string]struct{}) {
	if worktree.Path == "" {
		return
	}
	scriptPath := FindComposeScript(worktree.Path)
	if scriptPath == "" {
		worktree.ComposeStatus = ComposeNotConfigured
		return
	}
	worktree.ComposeScriptPath = scriptPath
	worktree.ComposeProjectName = ComposeProjectNameFor(worktree.Path)
	if _, running := runningProjects[worktree.ComposeProjectName]; running {
		worktree.ComposeStatus = ComposeRunning
	} else {
		worktree.ComposeStatus = ComposeStopped
	}
}
