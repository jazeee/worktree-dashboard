package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// commandrunner.go wraps os/exec: a timeout-bounded synchronous runner for
// git/gh/docker, plus fire-and-forget launchers for terminals, editors, and the
// Claude session script. Ported from v1's run_command / clipboard / terminal
// helpers.

// CommandResult captures a finished subprocess. ExitCode uses 124 for a timeout
// and 127 for a missing executable, matching v1's conventions.
type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Succeeded reports whether the command exited zero.
func (result CommandResult) Succeeded() bool {
	return result.ExitCode == 0
}

// RunCommand executes name+arguments in workingDirectory (empty = inherit),
// captures output, and enforces timeout. It blocks until the process finishes.
func RunCommand(timeout time.Duration, workingDirectory string, name string, arguments ...string) CommandResult {
	commandContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command := exec.CommandContext(commandContext, name, arguments...)
	if workingDirectory != "" {
		command.Dir = workingDirectory
	}
	var standardOutput, standardError strings.Builder
	command.Stdout = &standardOutput
	command.Stderr = &standardError

	runError := command.Run()
	result := CommandResult{
		Stdout: standardOutput.String(),
		Stderr: standardError.String(),
	}
	if commandContext.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		result.Stderr = "timeout after " + timeout.String()
		return result
	}
	if runError != nil {
		var notFound *exec.Error
		if errors.As(runError, &notFound) {
			result.ExitCode = 127
			result.Stderr = runError.Error()
			return result
		}
		var exitError *exec.ExitError
		if errors.As(runError, &exitError) {
			result.ExitCode = exitError.ExitCode()
			return result
		}
		result.ExitCode = 1
		return result
	}
	result.ExitCode = command.ProcessState.ExitCode()
	return result
}

// clipboardCandidates are tried in order; the first present tool wins. Mirrors
// v1's CLIPBOARD_CANDIDATES.
var clipboardCandidates = [][]string{
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"xsel", "--clipboard", "--input"},
	{"pbcopy"},
}

// CopyToSystemClipboard pipes text to the first available clipboard tool and
// returns the tool's name. Output is discarded because tools like wl-copy fork a
// daemon that would otherwise keep the pipe open.
func CopyToSystemClipboard(text string) (toolName string, copyError error) {
	for _, candidate := range clipboardCandidates {
		if _, lookupError := exec.LookPath(candidate[0]); lookupError != nil {
			continue
		}
		clipboardContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(clipboardContext, candidate[0], candidate[1:]...)
		command.Stdin = strings.NewReader(text)
		command.Stdout = nil
		command.Stderr = nil
		runError := command.Run()
		cancel()
		if runError == nil {
			return candidate[0], nil
		}
		return candidate[0], runError
	}
	return "", errors.New("no clipboard tool found (install wl-clipboard / xclip / xsel)")
}

// terminalPreference is the ordered fallback list of terminal emulators, matching
// v1's TERMINAL_PREFERENCE. $TERMINAL is tried first (see SelectTerminalCommand).
var terminalPreference = []string{
	"wezterm", "ghostty", "kitty", "alacritty", "foot", "ptyxis",
	"gnome-terminal", "konsole", "tilix", "terminator", "xterm",
	"x-terminal-emulator",
}

// BuildTerminalCommand returns argv to open terminalName in workingDirectory,
// optionally running innerCommand inside it. Ported per-terminal from v1.
func BuildTerminalCommand(terminalName string, workingDirectory string, innerCommand []string) []string {
	hasInner := len(innerCommand) > 0
	switch terminalName {
	case "wezterm":
		argv := []string{"wezterm", "start", "--cwd", workingDirectory}
		if hasInner {
			argv = append(argv, "--")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "ghostty":
		argv := []string{"ghostty", "--working-directory=" + workingDirectory}
		if hasInner {
			argv = append(argv, "-e")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "kitty":
		argv := []string{"kitty", "--directory", workingDirectory}
		return append(argv, innerCommand...)
	case "alacritty":
		argv := []string{"alacritty", "--working-directory", workingDirectory}
		if hasInner {
			argv = append(argv, "-e")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "ptyxis":
		argv := []string{"ptyxis", "--new-window", "--working-directory=" + workingDirectory}
		if hasInner {
			argv = append(argv, "--")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "gnome-terminal":
		argv := []string{"gnome-terminal", "--working-directory=" + workingDirectory}
		if hasInner {
			argv = append(argv, "--")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "konsole":
		argv := []string{"konsole", "--workdir", workingDirectory}
		if hasInner {
			argv = append(argv, "-e")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "tilix":
		argv := []string{"tilix", "--working-directory", workingDirectory}
		if hasInner {
			argv = append(argv, "-e")
			argv = append(argv, innerCommand...)
		}
		return argv
	case "terminator":
		argv := []string{"terminator", "--working-directory", workingDirectory}
		if hasInner {
			// terminator's -e takes a single string.
			argv = append(argv, "-e", strings.Join(innerCommand, " "))
		}
		return argv
	default:
		argv := []string{terminalName}
		if hasInner {
			argv = append(argv, "-e")
			argv = append(argv, innerCommand...)
		}
		return argv
	}
}

// SelectTerminalCommand picks the first available terminal (preferring $TERMINAL)
// and returns argv plus the chosen name. The bool reports whether one was found.
func SelectTerminalCommand(workingDirectory string, innerCommand []string) (argv []string, terminalName string, found bool) {
	candidates := []string{}
	environmentTerminal := os.Getenv("TERMINAL")
	if environmentTerminal != "" {
		candidates = append(candidates, environmentTerminal)
	}
	for _, preferred := range terminalPreference {
		if preferred != environmentTerminal {
			candidates = append(candidates, preferred)
		}
	}
	for _, candidate := range candidates {
		if _, lookupError := exec.LookPath(candidate); lookupError != nil {
			continue
		}
		return BuildTerminalCommand(candidate, workingDirectory, innerCommand), candidate, true
	}
	return nil, "", false
}

// LaunchDetachedProcess starts argv in workingDirectory in its own session, with
// standard streams sent to /dev/null, and does not wait. Used for terminals, the
// editor, the PR opener, and the Claude launcher.
func LaunchDetachedProcess(argv []string, workingDirectory string) error {
	if len(argv) == 0 {
		return errors.New("no command to launch")
	}
	command := exec.Command(argv[0], argv[1:]...)
	if workingDirectory != "" {
		command.Dir = workingDirectory
	}
	devNull, openError := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if openError == nil {
		command.Stdin = devNull
		command.Stdout = devNull
		command.Stderr = devNull
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	startError := command.Start()
	if devNull != nil {
		devNull.Close()
	}
	if startError != nil {
		return startError
	}
	// Reap the child once it exits so it doesn't linger as a zombie.
	go command.Wait()
	return nil
}

// OpenUrl opens a URL in the default browser via xdg-open (detached).
func OpenUrl(url string) error {
	return LaunchDetachedProcess([]string{"xdg-open", url}, "")
}

// ClaudeSessionLauncherPath is ~/.local/bin/claude-worktree-session.sh, the
// resume-or-fresh Claude launcher opened by the `c` action. Ported from v1's
// CLAUDE_SESSION_LAUNCHER.
func ClaudeSessionLauncherPath() string {
	home, homeError := os.UserHomeDir()
	if homeError != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "bin", "claude-worktree-session.sh")
}
