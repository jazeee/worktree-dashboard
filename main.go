package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// main.go is the entry point: resolve the repository root from --repo (default
// cwd), then run the Bubble Tea program on the alternate screen.

func main() {
	repositoryFlag := flag.String("repo", "", "Path inside the git repo whose worktrees to show (default: current directory).")
	flag.Parse()

	startDirectory := *repositoryFlag
	if startDirectory == "" {
		workingDirectory, workingDirectoryError := os.Getwd()
		if workingDirectoryError != nil {
			fmt.Fprintln(os.Stderr, "Could not determine current directory:", workingDirectoryError)
			os.Exit(1)
		}
		startDirectory = workingDirectory
	}

	repositoryRoot, repositoryError := FindRepositoryRoot(startDirectory)
	if repositoryError != nil {
		fmt.Fprintln(os.Stderr, repositoryError)
		os.Exit(1)
	}

	program := tea.NewProgram(
		NewDashboardModel(repositoryRoot),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, runError := program.Run(); runError != nil {
		fmt.Fprintln(os.Stderr, "worktree-dashboard exited with error:", runError)
		os.Exit(1)
	}
}
