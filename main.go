package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: trupal <start|stop> [project-dir]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "watch":
		// Internal: called inside the split pane. Not user-facing.
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "internal error: watch requires git root")
			os.Exit(1)
		}
		cmdWatch(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "usage: trupal <start|stop> [project-dir]")
		os.Exit(1)
	}
}

func cmdStart() {
	// Require TMUX env to be set
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "error: trupal must be run inside a tmux session")
		os.Exit(1)
	}

	// Resolve project dir (cwd or first arg after "start")
	projectDir, err := resolveProjectDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving project dir: %v\n", err)
		os.Exit(1)
	}

	// Find git root
	gitRoot, err := findGitRoot(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Check if already running
	pidFile := filepath.Join(gitRoot, ".trupal.pid")
	if _, err := os.Stat(pidFile); err == nil {
		fmt.Fprintf(os.Stderr, "error: trupal is already running (pid file exists: %s)\n", pidFile)
		os.Exit(1)
	}

	// Get own executable path
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting executable path: %v\n", err)
		os.Exit(1)
	}

	// Launch watch command in a new tmux split pane.
	// Use "--" so tmux execs directly without shell (avoids word-splitting on paths with spaces).
	if err := exec.Command("tmux", "split-window", "-h", "-l", "30%", "-d",
		"--", self, "watch", gitRoot).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error creating tmux pane: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("trupal started for %s\n", gitRoot)
}

func cmdStop() {
	// Resolve project dir
	projectDir, err := resolveProjectDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving project dir: %v\n", err)
		os.Exit(1)
	}

	// Find git root
	gitRoot, err := findGitRoot(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Read pid file
	pidFile := filepath.Join(gitRoot, ".trupal.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trupal is not running")
		os.Exit(1)
	}

	paneID := strings.TrimSpace(string(data))
	if paneID == "" {
		fmt.Fprintln(os.Stderr, "error: pid file is empty")
		os.Exit(1)
	}

	// Kill the tmux pane
	if err := exec.Command("tmux", "kill-pane", "-t", paneID).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not kill pane %s: %v\n", paneID, err)
	}

	// Remove pid file
	if err := os.Remove(pidFile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove pid file: %v\n", err)
	}

	fmt.Printf("trupal stopped (pane %s killed)\n", paneID)
}

func cmdWatch(gitRoot string) {
	// Write pane ID to pid file
	paneID, err := getTmuxPaneID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting tmux pane ID: %v\n", err)
		os.Exit(1)
	}

	pidFile := filepath.Join(gitRoot, ".trupal.pid")
	if err := os.WriteFile(pidFile, []byte(paneID), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing pid file: %v\n", err)
		os.Exit(1)
	}

	// Defer cleanup of pid file
	defer func() {
		_ = os.Remove(pidFile)
	}()

	// Load config and run watch loop
	cfg := loadConfig(gitRoot)
	runWatchLoop(gitRoot, cfg)
}

// resolveProjectDir returns the project directory from args or cwd.
func resolveProjectDir() (string, error) {
	// Check for optional dir argument after the subcommand
	if len(os.Args) >= 3 {
		dir, err := filepath.Abs(os.Args[2])
		if err != nil {
			return "", err
		}
		return dir, nil
	}
	return os.Getwd()
}

// findGitRoot walks up from dir looking for a .git directory.
func findGitRoot(dir string) (string, error) {
	current := dir
	for {
		info, err := os.Stat(filepath.Join(current, ".git"))
		if err == nil && info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no git repository found from %s", dir)
		}
		current = parent
	}
}

// getTmuxPaneID returns the current tmux pane ID via display-message.
func getTmuxPaneID() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// --- Stubs (replaced in later tasks) ---

func runWatchLoop(projectDir string, cfg Config) {
	fmt.Println("watch loop not yet implemented")
}
