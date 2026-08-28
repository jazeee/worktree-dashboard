# worktree-dashboard

A terminal dashboard for git worktrees, written in Go on
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

It discovers every worktree of a repository and presents them in one screen,
enriching each row with git status, pull-request state, docker-compose status,
and live pi session state. From the same screen you can open a worktree
(VS Code, a terminal, or pi), open its pull request, create a new worktree,
and delete one. The renderer is event-driven, so it repaints only when state
changes and idles at ~0–1% of a core with a flat, small memory footprint.

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

`~/.local/bin/worktree-dashboard` symlinks to `bin/worktree-dashboard`, so
rebuild after every change or you will keep running the old binary.

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
| `p`           | open pi (resume or fresh) in a terminal            |
| `n`           | new worktree (input dialog, branched off `origin/main`) |
| `y` / `ctrl+c`| copy the worktree path (`ctrl+c` is **copy**, not quit) |
| `d`           | delete the worktree (confirm dialog)               |
| `m`           | toggle mouse capture (release the mouse to select text) |
| `q`           | quit                                              |

The mouse works too: **left-click a row** to select it, and the **scroll wheel**
moves the cursor. Because the app enables mouse tracking, your terminal's own
click-drag text selection is suppressed while it runs. To copy text from the
detail pane, press **`m`** to release the mouse — the terminal's native
click-drag selection comes back — then press **`m`** again to restore
click-to-select and wheel scrolling. (Holding **Shift** also selects text
without toggling, if your terminal supports it.)

## Live refresh tiers

Each tier is a self-rescheduling `tea.Tick`; a worktree/PR tick that fires while
a poll is still running is skipped and counted:

- pi session state — 1 s
- spinner frame — 0.125 s (pure frame increment; only repaints while a row animates)
- worktree rediscovery + status — 30 s
- pull requests — 15 min
- trace sample — 60 s

## Design notes

- **Hand-rendered ANSI-aware table.** `bubbles/table` styles cells as plain
  strings and truncates with a non-ANSI-aware routine that corrupts pre-colored
  cells. The table is rendered by hand with lipgloss
  (`Width(w).MaxWidth(w).Inline(true)`), preserving per-cell color.
- **Booleans replaced by string-union enum types** (mirroring TypeScript
  `value: 'A' | 'B'`) throughout — e.g. `PullRequestState`, `PiLiveness`,
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

## Limitations

- **No OSC 52 clipboard fallback.** Copy uses `wl-copy`/`xclip`/`xsel`/`pbcopy`;
  if none is present, the copy reports an error rather than falling back to an
  OSC 52 terminal escape.
- **The detail pane is clipped, not scrollable.** Content taller than the pane is
  truncated to fit rather than scrolled.
