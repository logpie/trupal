package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
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
	case "log":
		cmdLog()
	case "watch":
		// Internal: called inside the split pane. Not user-facing.
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "internal error: watch requires session dir and git root")
			os.Exit(1)
		}
		cmdWatch(os.Args[2], os.Args[3])
	case "replay-notifications":
		if err := cmdReplayNotifications(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "usage: trupal <start|stop|log|replay-notifications> [project-dir]")
		os.Exit(1)
	}
}

func cmdStart() {
	// Require TMUX env to be set
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "error: trupal must be run inside a tmux session")
		os.Exit(1)
	}

	sessionDir, gitRoot := resolveStartTarget()
	cfg, err := loadConfig(gitRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
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

	// Find the watched agent's pane to split alongside it (match by project dir).
	agentPane := findAgentPane(gitRoot, cfg.SessionProvider)

	// Launch watch command in a new tmux split pane.
	// Use "--" so tmux execs directly without shell (avoids word-splitting on paths with spaces).
	args := []string{"split-window", "-h", "-l", "30%", "-d"}
	if agentPane != "" {
		args = append(args, "-t", agentPane)
	}
	args = append(args, "--", self, "watch", sessionDir, gitRoot)
	if err := exec.Command("tmux", args...).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error creating tmux pane: %v\n", err)
		os.Exit(1)
	}
	// No remain-on-exit — the watch process blocks after stop, keeping the pane alive.

	fmt.Printf("trupal started for %s\n", gitRoot)
}

func cmdStop() {
	// Parse flags and project dir.
	closePane := false
	var projectDir string
	for _, arg := range os.Args[2:] {
		switch {
		case arg == "--close":
			closePane = true
		case projectDir == "":
			projectDir = arg
		}
	}
	if projectDir != "" {
		projectDir, _ = filepath.Abs(projectDir)
	} else {
		projectDir, _ = os.Getwd()
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

	// Send SIGINT to the trupal process in that pane.
	// Get the pane's PID and signal it.
	pidOut, pidErr := exec.Command("tmux", "display", "-t", paneID, "-p", "#{pane_pid}").Output()
	if pidErr == nil {
		panePid := strings.TrimSpace(string(pidOut))
		if panePid != "" {
			exec.Command("kill", "-INT", panePid).Run()
		}
	}
	time.Sleep(500 * time.Millisecond)
	os.Remove(pidFile)

	if closePane {
		exec.Command("tmux", "kill-pane", "-t", paneID).Run()
		fmt.Println("trupal stopped and pane closed")
	} else {
		fmt.Println("trupal stopped (pane stays open for review)")
	}
}

func cmdWatch(sessionDir, gitRoot string) {
	// Recover from panics — show error in pane, wait for keypress.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n\ntrupal crashed: %v\n", r)
			fmt.Fprintf(os.Stderr, "check .trupal.debug for details\n")
			fmt.Fprintf(os.Stderr, "press ctrl+c to close pane\n")
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt)
			<-sig
		}
	}()

	// Load config before creating state that requires deferred cleanup.
	cfg, err := loadConfig(gitRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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

	// Start TUI + watcher.
	p := tea.NewProgram(initialModel(filepath.Base(gitRoot)), ProgramOptions()...)
	watchCancel := make(chan struct{})
	go runWatchLoop(sessionDir, gitRoot, cfg, p, watchCancel)
	p.Run() // ignore exit error — expected on SIGINT
	close(watchCancel)

	// After TUI exits, show stop summary in normal terminal and block.
	fmt.Printf("\n trupal stopped (%s)\n", cfg.String())
	fmt.Printf(" log: .trupal.log  debug: .trupal.debug\n")
	fmt.Printf(" press ctrl+c to close\n")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}

func cmdLog() {
	_, gitRoot := resolveStartTarget()
	logPath := filepath.Join(gitRoot, ".trupal.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no log file found")
		os.Exit(1)
	}
	fmt.Print(string(data))
}

func cmdReplayNotifications(args []string) error {
	fs := flag.NewFlagSet("replay-notifications", flag.ExitOnError)
	projectDir := fs.String("project-dir", "", "project directory whose config should be used")
	jsonlPath := fs.String("jsonl", "", "optional session JSONL path for prompt context")
	outputPath := fs.String("output", "", "output JSONL path for replayed brain responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: trupal replay-notifications [--project-dir DIR] [--jsonl PATH] [--output PATH] <notifications.jsonl>")
	}

	notificationsPath, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	targetDir := *projectDir
	if strings.TrimSpace(targetDir) == "" {
		targetDir = filepath.Dir(notificationsPath)
	}
	targetDir, err = filepath.Abs(targetDir)
	if err != nil {
		return err
	}

	cfg, err := loadConfig(targetDir)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	result, err := ReplayBrainNotifications(cfg, targetDir, *jsonlPath, notificationsPath, *outputPath)
	if err != nil {
		return err
	}
	fmt.Printf("notifications=%d\n", result.Notifications)
	fmt.Printf("generated_nudges=%d\n", result.GeneratedNudges)
	fmt.Printf("output=%s\n", result.OutputPath)
	return nil
}

// resolveStartTarget returns the user's requested start dir plus the enclosing git root.
func resolveStartTarget() (sessionDir, repoRoot string) {
	var err error
	sessionDir, err = resolveProjectDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving project dir: %v\n", err)
		os.Exit(1)
	}
	repoRoot, err = findGitRoot(sessionDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return sessionDir, repoRoot
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
		// Accept .git as either directory (normal repo) or file (worktree/submodule).
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no git repository found from %s", dir)
		}
		current = parent
	}
}

// findAgentPane searches all tmux panes for one running the watched agent in the
// given project directory. Returns "" if not found (falls back to splitting the current pane).
func findAgentPane(projectDir, provider string) string {
	command := providerPaneCommand(provider)
	out, err := exec.Command("tmux", "list-panes", "-s", "-F", "#{pane_id}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_start_command}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) >= 3 && paneMatchesProvider(command, parts[1], indexedField(parts, 3)) && pathsOverlap(parts[2], projectDir) {
			return parts[0]
		}
	}
	// Fallback: any pane for the configured provider (better than nothing).
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) >= 2 && paneMatchesProvider(command, parts[1], indexedField(parts, 3)) {
			return parts[0]
		}
	}
	return ""
}

func findAgentPaneStrict(projectDir, provider string) string {
	command := providerPaneCommand(provider)
	out, err := exec.Command("tmux", "list-panes", "-s", "-F", "#{pane_id}\t#{pane_current_command}\t#{pane_current_path}\t#{pane_start_command}").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) >= 3 && paneMatchesProvider(command, parts[1], indexedField(parts, 3)) && pathsOverlap(parts[2], projectDir) {
			return parts[0]
		}
	}
	return ""
}

func paneMatchesProvider(command, currentCommand, startCommand string) bool {
	currentCommand = strings.TrimSpace(currentCommand)
	startCommand = strings.TrimSpace(startCommand)
	if currentCommand == command {
		return true
	}
	if command == ProviderCodex {
		return strings.Contains(startCommand, "codex ")
	}
	return strings.Contains(startCommand, command)
}

func indexedField(parts []string, idx int) string {
	if idx < 0 || idx >= len(parts) {
		return ""
	}
	return parts[idx]
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
