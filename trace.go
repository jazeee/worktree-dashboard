package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// trace.go appends a lightweight resource line periodically so slow leaks or CPU
// regressions stay visible over long runs. Ported from v1's _trace.

const traceMaxBytes = 1_000_000

// traceDirectory is ~/.local/state/worktree-dashboard.
func traceDirectory() string {
	home, homeError := os.UserHomeDir()
	if homeError != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "state", "worktree-dashboard")
}

// TraceSnapshot is the set of counters recorded on each trace line.
type TraceSnapshot struct {
	Event                 string
	ItemCount             int
	WorktreePollMillis    int64
	PullRequestPollMillis int64
	SkipCount             int
}

// ReadResidentSetSizeMegabytes returns this process's RSS in MiB from /proc, or
// -1 where unavailable.
func ReadResidentSetSizeMegabytes() float64 {
	contents, readError := os.ReadFile("/proc/self/statm")
	if readError != nil {
		return -1
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 {
		return -1
	}
	residentPages, parseError := strconv.ParseInt(fields[1], 10, 64)
	if parseError != nil {
		return -1
	}
	return float64(residentPages) * float64(os.Getpagesize()) / (1024 * 1024)
}

// AppendTraceLine writes one best-effort resource line to the rotated trace log.
func AppendTraceLine(snapshot TraceSnapshot) {
	line := fmt.Sprintf(
		"%d %s rss=%.0fMB goroutines=%d items=%d wt_poll_ms=%d pr_poll_ms=%d skips=%d\n",
		time.Now().Unix(),
		snapshot.Event,
		ReadResidentSetSizeMegabytes(),
		runtime.NumGoroutine(),
		snapshot.ItemCount,
		snapshot.WorktreePollMillis,
		snapshot.PullRequestPollMillis,
		snapshot.SkipCount,
	)
	directory := traceDirectory()
	if os.MkdirAll(directory, 0o755) != nil {
		return
	}
	traceFilePath := filepath.Join(directory, "trace.log")
	if info, statError := os.Stat(traceFilePath); statError == nil && info.Size() > traceMaxBytes {
		os.Rename(traceFilePath, filepath.Join(directory, "trace.log.1"))
	}
	handle, openError := os.OpenFile(traceFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openError != nil {
		return
	}
	handle.WriteString(line)
	handle.Close()
}
