package main

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// messages.go defines every tea.Msg the model reacts to and the tea.Cmd
// constructors that produce them. Background work runs in goroutines here and
// returns a single message; the model's Update stays pure and single-threaded.

// NotificationSeverity classifies a transient status-line message.
type NotificationSeverity string

const (
	SeverityInfo    NotificationSeverity = "Info"
	SeverityWarning NotificationSeverity = "Warning"
	SeverityError   NotificationSeverity = "Error"
)

// ---- Message types ----

// worktreesDiscoveredMsg carries a fast structural discovery (statuses still
// unknown), used for startup and the `r` refresh so rows paint immediately.
type worktreesDiscoveredMsg struct {
	worktrees []WorktreeInfo
}

// discoveryFailedMsg reports a failed `git worktree list`.
type discoveryFailedMsg struct {
	message string
}

// statusesCollectedMsg carries git+compose+pi fields for a previously
// discovered set (the refresh path's second phase).
type statusesCollectedMsg struct {
	worktrees []WorktreeInfo
}

// worktreesResolvedMsg carries a combined discover+collect from the 30s poll.
type worktreesResolvedMsg struct {
	worktrees      []WorktreeInfo
	durationMillis int64
}

// pullRequestsFetchedMsg carries successful PR payloads keyed by ItemKey; keys
// present in requested but absent from payloads had no PR.
type pullRequestsFetchedMsg struct {
	requested      []string
	payloads       map[string]pullRequestPayload
	durationMillis int64
}

// piRefreshedMsg carries re-read pi records keyed by ItemKey.
type piRefreshedMsg struct {
	records map[string]PiSessionRecord
}

// Tick messages for the self-rescheduling live tiers.
type spinnerTickMsg time.Time
type piPollTickMsg time.Time
type worktreePollTickMsg time.Time
type pullRequestPollTickMsg time.Time
type traceTickMsg time.Time

// clearNotificationMsg dismisses the transient status line.
type clearNotificationMsg struct{}

// actionResultMsg reports the outcome of a fire-and-forget external action.
type actionResultMsg struct {
	text     string
	severity NotificationSeverity
}

// worktreeCreatedMsg reports the result of a create-worktree action, already
// performed (including opening pi) off the UI goroutine. On success failure
// is empty and note describes what happened; the model refreshes either way.
type worktreeCreatedMsg struct {
	failure string
	note    string
}

// rowDeletedMsg reports the result of a delete action (already performed off the
// UI goroutine). key identifies the row to tombstone and drop; notification is
// the message to surface at the given severity.
type rowDeletedMsg struct {
	key          string
	notification string
	severity     NotificationSeverity
}

// ---- Command constructors ----

// discoverWorktreesCommand runs a fast structural discovery.
func discoverWorktreesCommand(repositoryRoot string) tea.Cmd {
	return func() tea.Msg {
		worktrees, discoverError := DiscoverAll(repositoryRoot)
		if discoverError != nil {
			return discoveryFailedMsg{message: discoverError.Error()}
		}
		return worktreesDiscoveredMsg{worktrees: worktrees}
	}
}

// collectStatusesInPlace fans out git status, compose, and pi collection over
// worktrees with bounded concurrency, mutating the slice in place.
func collectStatusesInPlace(worktrees []WorktreeInfo) {
	runningProjects := ListRunningComposeProjects()
	semaphore := make(chan struct{}, FanOutWidth)
	var waitGroup sync.WaitGroup
	for index := range worktrees {
		waitGroup.Add(1)
		go func(position int) {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			CollectGitStatus(&worktrees[position])
			AttachComposeStatus(&worktrees[position], runningProjects)
			AttachPiState(&worktrees[position])
		}(index)
	}
	waitGroup.Wait()
}

// collectStatusesCommand collects statuses for an already-discovered set.
func collectStatusesCommand(worktrees []WorktreeInfo) tea.Cmd {
	return func() tea.Msg {
		copied := append([]WorktreeInfo(nil), worktrees...)
		collectStatusesInPlace(copied)
		return statusesCollectedMsg{worktrees: copied}
	}
}

// resolveWorktreesCommand rediscovers and collects statuses in one shot (30s poll).
func resolveWorktreesCommand(repositoryRoot string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		worktrees, discoverError := DiscoverAll(repositoryRoot)
		if discoverError != nil {
			// A failed poll simply resolves to nothing; the guard resets and the
			// next tick retries.
			return worktreesResolvedMsg{worktrees: nil, durationMillis: time.Since(start).Milliseconds()}
		}
		collectStatusesInPlace(worktrees)
		return worktreesResolvedMsg{
			worktrees:      worktrees,
			durationMillis: time.Since(start).Milliseconds(),
		}
	}
}

// fetchPullRequestsCommand fetches PR state for the given rows with bounded
// concurrency, keyed by ItemKey.
func fetchPullRequestsCommand(worktrees []WorktreeInfo) tea.Cmd {
	requested := make([]string, len(worktrees))
	snapshot := append([]WorktreeInfo(nil), worktrees...)
	for index, worktree := range snapshot {
		requested[index] = ItemKey(worktree)
	}
	return func() tea.Msg {
		start := time.Now()
		payloads := map[string]pullRequestPayload{}
		var mutex sync.Mutex
		semaphore := make(chan struct{}, FanOutWidth)
		var waitGroup sync.WaitGroup
		for _, worktree := range snapshot {
			waitGroup.Add(1)
			go func(target WorktreeInfo) {
				defer waitGroup.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				payload, found := FetchPullRequest(target)
				if found {
					mutex.Lock()
					payloads[ItemKey(target)] = payload
					mutex.Unlock()
				}
			}(worktree)
		}
		waitGroup.Wait()
		return pullRequestsFetchedMsg{
			requested:      requested,
			payloads:       payloads,
			durationMillis: time.Since(start).Milliseconds(),
		}
	}
}

// refreshPiStatesCommand re-reads .pi-session-state for worktree rows.
func refreshPiStatesCommand(worktrees []WorktreeInfo) tea.Cmd {
	snapshot := append([]WorktreeInfo(nil), worktrees...)
	return func() tea.Msg {
		records := map[string]PiSessionRecord{}
		for _, worktree := range snapshot {
			if worktree.Path == "" {
				continue
			}
			records[ItemKey(worktree)] = ReadPiSessionState(worktree.Path)
		}
		return piRefreshedMsg{records: records}
	}
}

// ---- Tick schedulers (each reschedules itself from the handler) ----

func scheduleSpinnerTick() tea.Cmd {
	return tea.Tick(SpinnerTickInterval, func(now time.Time) tea.Msg { return spinnerTickMsg(now) })
}

func schedulePiPollTick() tea.Cmd {
	return tea.Tick(PiPollInterval, func(now time.Time) tea.Msg { return piPollTickMsg(now) })
}

func scheduleWorktreePollTick() tea.Cmd {
	return tea.Tick(WorktreePollInterval, func(now time.Time) tea.Msg { return worktreePollTickMsg(now) })
}

func schedulePullRequestPollTick() tea.Cmd {
	return tea.Tick(PullRequestPollInterval, func(now time.Time) tea.Msg { return pullRequestPollTickMsg(now) })
}

func scheduleTraceTick() tea.Cmd {
	return tea.Tick(TraceInterval, func(now time.Time) tea.Msg { return traceTickMsg(now) })
}

// scheduleClearNotification dismisses the status line after a delay.
func scheduleClearNotification(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return clearNotificationMsg{} })
}
