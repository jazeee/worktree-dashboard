#!/usr/bin/env bash
# Open pi for a worktree inside a split tmux session, in a new terminal.
# Usage: pi-worktree-session.sh <worktree-dir>
#
# Layout: one tmux session per worktree with pi in the left pane and a bash
# shell (same cwd) in the right pane, split 50/50. Re-running reattaches to the
# same session instead of spawning a duplicate.
#
# pi is run through an interactive shell so the user's ~/.bash_aliases and env
# exports apply.
#
# pi keys session storage by the directory it was launched from, so a resume
# must run from that exact directory — which may be a subdirectory of the
# worktree. The state file records it as `cwd=`; we resume from there.
set -euo pipefail

dir="${1:?usage: pi-worktree-session.sh <worktree-dir>}"
state_file="$dir/.claude-session-state"

id=""
session_cwd=""
if [ -f "$state_file" ]; then
  id="$(sed -n 's/^id=//p' "$state_file")"
  session_cwd="$(sed -n 's/^cwd=//p' "$state_file")"
fi

# Resume from the recorded launch dir when it still exists, else the worktree root.
target="$dir"
if [ -n "$id" ] && [ -n "$session_cwd" ] && [ -d "$session_cwd" ]; then
  target="$session_cwd"
fi

# Command for the pi pane. A resume falls back to a fresh session if the
# recorded id can no longer be resumed. After pi exits, exec an interactive
# shell in the same pane so it drops to a shell instead of the pane closing.
shell_after='exec "${SHELL:-bash}" -i'
if [ -n "$id" ]; then
  pi_pane_cmd="bash -ic 'pi --session $id || pi; $shell_after'"
else
  pi_pane_cmd="bash -ic 'pi; $shell_after'"
fi

# Without tmux, run pi directly in this terminal (still via an interactive
# shell for the aliases/env).
if ! command -v tmux >/dev/null 2>&1; then
  cd "$target"
  if [ -n "$id" ]; then
    exec bash -ic 'pi --session "$1" || pi; exec "${SHELL:-bash}" -i' bash "$id"
  fi
  exec bash -ic 'pi; exec "${SHELL:-bash}" -i'
fi

# One stable, reattachable tmux session per worktree.
session="piwt-$(printf '%s\n' "$dir" | md5sum | cut -c1-8)"

if ! tmux has-session -t "=$session" 2>/dev/null; then
  pi_pane="$(tmux new-session -d -P -F '#{pane_id}' \
    -s "$session" -c "$target" "$pi_pane_cmd")"
  # Right pane: a plain shell in the same cwd. Split side-by-side, 50/50.
  tmux split-window -h -l 50% -t "$pi_pane" -c "$target"
fi

# Open the session in this (new) terminal window.
if [ -n "${TMUX:-}" ]; then
  exec tmux switch-client -t "=$session"
fi
exec tmux attach-session -t "=$session"
