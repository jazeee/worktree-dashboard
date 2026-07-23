# worktree-dashboard

A terminal dashboard for git worktrees, written in Go on
[Bubble Tea](https://github.com/charmbracelet/bubbletea). This is a full-parity
successor to an earlier Python/Textual implementation.

It discovers every worktree of a repository, enriches each with git status, pull
request state, docker-compose status, and live Claude session state, and lets you
open, create, and delete worktrees from one screen.

## Why the rewrite

The Python/Textual predecessor worked but the framework fought us: Textual
re-composites on a timer, so it burned ~27% of a core at idle (~10% even after we
hand-throttled the spinner), grew RSS ~3 MB/min inside the render loop, and could
take up to 20 s to shut down because Python threads can't be cancelled.

Bubble Tea's Elm architecture repaints **only when state changes**, so this app
idles at ~0–1% of a core with flat RSS (~11 MB) and quits instantly — background
work runs in `tea.Cmd` goroutines under a cancellable `context`, so on quit the
loop ends immediately and stray results are simply dropped.

Go — not Rust — because this is I/O-bound subprocess orchestration
(`git`/`gh`/`docker`) wrapped in a TUI, exactly the niche of lazygit / gh-dash /
k9s. Rust + ratatui would cost immediate-mode redraw complexity and borrow-checker
friction around the async fan-out for no benefit on this workload.

## Build & run

Requires Go on `PATH` (`sudo apt install golang-go`).

```sh
worktree-dashboard            # run against the current repo
worktree-dashboard --repo /path/to/repo
```

Build / test:

```sh
go vet ./...
go test ./...
CGO_ENABLED=0 go build -o bin/worktree-dashboard .
```

## Keybindings

| Key           | Action                                            |
|---------------|---------------------------------------------------|
| `↑`/`k` `↓`/`j` | move cursor                                     |
| `pgup` `pgdn` `home`/`g` `end`/`G` | scroll                       |
| `/`           | open search (regex, case-insensitive; literal fallback) |
| `esc`         | clear search / close dialog                        |
| `r`           | refresh (rediscover + recollect)                  |
| `o`           | open the pull request in the browser              |
| `v`           | open the worktree in VS Code                       |
| `t`           | open a terminal in the worktree                    |
| `c`           | open Claude (resume or fresh) in a terminal        |
| `n`           | new worktree (input dialog, branched off `origin/main`) |
| `y` / `ctrl+c`| copy the worktree path (`ctrl+c` is **copy**, not quit — matches v1) |
| `d`           | delete the worktree (confirm dialog)               |
| `q`           | quit                                              |

The mouse works too: **left-click a row** to select it, and the **scroll wheel**
moves the cursor. Because the app enables mouse tracking, your terminal's own
click-drag text selection is suppressed while it runs — hold **Shift** to select
text as usual.

## Live refresh tiers

Each tier is a self-rescheduling `tea.Tick`; a worktree/PR tick that fires while
a poll is still running is skipped and counted:

- Claude session state — 1 s
- spinner frame — 0.25 s (pure frame increment; only repaints while a row animates)
- worktree rediscovery + status — 30 s
- pull requests — 15 min
- trace sample — 60 s

## Design notes

- **Hand-rendered ANSI-aware table.** `bubbles/table` styles cells as plain
  strings and truncates with a non-ANSI-aware routine that corrupts pre-colored
  cells. The table is rendered by hand with lipgloss
  (`Width(w).MaxWidth(w).Inline(true)`), preserving per-cell color.
- **Booleans replaced by string-union enum types** (mirroring TypeScript
  `value: 'A' | 'B'`) throughout — e.g. `PullRequestState`, `ClaudeLiveness`,
  `WorktreeKind`, `LayoutOrientation`, `PollActivity`.
- **Responsive layout.** `tea.WindowSizeMsg` switches between a side-by-side
  table+detail split and a stacked layout at a width threshold.
- **Delete-race tombstones.** Deleted keys are held for 60 s and filtered out of
  incoming discovery results, and rows are removed by key (never by index).
- **Delete guards.** The repo root, primary/root worktrees, protected branches,
  and the worktree the dashboard was launched from cannot be deleted.

## Tracing

Resource samples (RSS via `/proc/self/statm`, goroutine count, poll durations,
skipped-poll counts) are appended to:

```
~/.local/state/worktree-dashboard/trace.log
```

## Known deviations from v1

- **No OSC 52 clipboard fallback.** Copy uses `wl-copy`/`xclip`/`xsel`/`pbcopy`;
  if none is present, the copy reports an error rather than falling back to an
  OSC 52 terminal escape.
- **The detail pane is clipped, not scrollable.** Content taller than the pane is
  truncated to fit rather than scrolled.
