package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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

	// Find CC's pane to split alongside it (match by project dir).
	ccPane := findCCPane(gitRoot)

	// Launch watch command in a new tmux split pane.
	// Use -P -F to get the new pane ID, and "--" to avoid shell word-splitting.
	args := []string{"split-window", "-h", "-l", "30%", "-d", "-P", "-F", "#{pane_id}"}
	if ccPane != "" {
		args = append(args, "-t", ccPane)
	}
	args = append(args, "--", self, "watch", gitRoot)
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating tmux pane: %v\n", err)
		os.Exit(1)
	}

	// Set remain-on-exit so the pane stays visible after trupal stops.
	newPane := strings.TrimSpace(string(out))
	if newPane != "" {
		exec.Command("tmux", "set-option", "-t", newPane, "remain-on-exit", "on").Run()
	}

	fmt.Printf("trupal started for %s\n", gitRoot)
}

func cmdStop() {
	// Check for --close flag.
	closePane := false
	for _, arg := range os.Args[2:] {
		if arg == "--close" {
			closePane = true
		}
	}

	// Resolve project dir (skip --close in args).
	projectDir := ""
	for _, arg := range os.Args[2:] {
		if arg != "--close" {
			projectDir = arg
			break
		}
	}
	if projectDir == "" {
		var err error
		projectDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		var err error
		projectDir, err = filepath.Abs(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	gitRoot, err := findGitRoot(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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

	// Send C-c to stop the watcher gracefully. Process blocks at summary screen.
	exec.Command("tmux", "send-keys", "-t", paneID, "C-c", "").Run()
	time.Sleep(500 * time.Millisecond)
	os.Remove(pidFile)

	if closePane {
		// Also kill the pane. Safe: we verified this is trupal's pane via pid file.
		exec.Command("tmux", "kill-pane", "-t", paneID).Run()
		fmt.Printf("trupal stopped and pane closed\n")
	} else {
		fmt.Printf("trupal stopped (pane stays open for review)\n")
	}
}

func cmdWatch(gitRoot string) {
	// Recover from panics — show error in pane instead of crashing.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n\ntrupal crashed: %v\n", r)
			fmt.Fprintf(os.Stderr, "check .trupal.debug for details\n")
			// Block forever so the pane stays visible with the error.
			select {}
		}
	}()

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

// findCCPane searches all tmux panes for one running `claude` in the given project directory.
// Returns "" if not found (falls back to splitting the current pane).
func findCCPane(projectDir string) string {
	out, err := exec.Command("tmux", "list-panes", "-s", "-F", "#{pane_id}\t#{pane_current_command}\t#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 && parts[1] == "claude" && parts[2] == projectDir {
			return parts[0]
		}
	}
	// Fallback: any claude pane (better than nothing).
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 2 && parts[1] == "claude" {
			return parts[0]
		}
	}
	return ""
}

// getTmuxPaneID returns the pane ID of the pane this process is running in.
// Uses $TMUX_PANE which tmux sets per-pane (unlike display-message which
// reports the active pane, not necessarily the caller's pane).
func getTmuxPaneID() (string, error) {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return "", fmt.Errorf("TMUX_PANE not set")
	}
	return paneID, nil
}

