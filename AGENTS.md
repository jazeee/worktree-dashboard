# Agent instructions

## Always rebuild the installed binary

`~/.local/bin/worktree-dashboard` is a symlink to `bin/worktree-dashboard` in
this repo, so the user runs whatever binary is committed there. After **any**
source change, finish the task with:

```sh
go vet ./... && go test ./... && CGO_ENABLED=0 go build -o bin/worktree-dashboard .
```

Never report a change as done without rebuilding: the user will otherwise test
a stale binary and see the old behaviour. Tell them to restart the dashboard to
pick up the new build.
