# TruPal MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a display-only tmux sidecar that watches a coding agent's git changes in real-time, detects trajectory anti-patterns (whack-a-mole, error stalls), and flags suspicious diff patterns.

**Architecture:** Single Go binary, zero external deps. Polls `git diff` every 3s, maintains in-memory session state for trajectory tracking, runs an optional configurable build command, and renders findings with ANSI colors to a tmux split pane.

**Tech Stack:** Go 1.24, stdlib only (os/exec, regexp, hash/fnv, fmt)

---

## File Structure

```
trupal/
  main.go        # CLI entry: parse args, dispatch start/stop commands, tmux pane management
  config.go      # Parse .trupal.toml (hand-rolled, 3 fields)
  session.go     # Session state struct, per-file edit tracking, trajectory signal evaluation
  checker.go     # Diff pattern regex matching, build command runner, error count extraction
  watcher.go     # Poll loop: git commands, orchestrate checks, drive display updates
  display.go     # ANSI color helpers, screen clear, render full display frame
  go.mod         # Module definition, no dependencies
```

---

### Task 1: Project scaffold and CLI entry

**Files:**
- Create: `go.mod`
- Create: `main.go`

- [ ] **Step 1: Initialize Go module**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go mod init github.com/yuxuan/trupal
```

Expected: `go.mod` created with module path.

- [ ] **Step 2: Write main.go with arg parsing and tmux detection**

Create `main.go`:

```go
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
		cmdWatch()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nusage: trupal <start|stop> [project-dir]\n", os.Args[1])
		os.Exit(1)
	}
}

func cmdStart() {
	// Must be inside tmux
	if os.Getenv("TMUX") == "" {
		fmt.Fprintln(os.Stderr, "trupal must be run inside a tmux session")
		os.Exit(1)
	}

	// Resolve project dir
	projectDir := "."
	if len(os.Args) > 2 {
		projectDir = os.Args[2]
	}
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Walk up to find .git/
	gitRoot := findGitRoot(absDir)
	if gitRoot == "" {
		fmt.Fprintln(os.Stderr, "no git repository found (walked up from "+absDir+")")
		os.Exit(1)
	}

	// Check if already running
	pidFile := filepath.Join(gitRoot, ".trupal.pid")
	if _, err := os.Stat(pidFile); err == nil {
		fmt.Fprintln(os.Stderr, "trupal is already running (found "+pidFile+")")
		os.Exit(1)
	}

	// Get path to our own binary
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find own executable: %v\n", err)
		os.Exit(1)
	}

	// Split tmux pane and run trupal watch inside it
	cmd := exec.Command("tmux", "split-window", "-h", "-l", "30%",
		"-d", // don't switch focus to new pane
		self, "watch", gitRoot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tmux split failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	fmt.Println("trupal started watching " + gitRoot)
}

func cmdStop() {
	// Find project dir to locate pid file
	projectDir := "."
	if len(os.Args) > 2 {
		projectDir = os.Args[2]
	}
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving path: %v\n", err)
		os.Exit(1)
	}
	gitRoot := findGitRoot(absDir)
	if gitRoot == "" {
		fmt.Fprintln(os.Stderr, "no git repository found")
		os.Exit(1)
	}

	pidFile := filepath.Join(gitRoot, ".trupal.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trupal is not running")
		os.Exit(1)
	}

	paneID := strings.TrimSpace(string(data))
	killCmd := exec.Command("tmux", "kill-pane", "-t", paneID)
	killCmd.Run() // best-effort

	os.Remove(pidFile)
	fmt.Println("trupal stopped")
}

func cmdWatch() {
	// Called internally inside the split pane.
	// Args: trupal watch <git-root>
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "watch requires project dir")
		os.Exit(1)
	}
	projectDir := os.Args[2]

	// Write pid file with our tmux pane ID
	paneID := getTmuxPaneID()
	pidFile := filepath.Join(projectDir, ".trupal.pid")
	os.WriteFile(pidFile, []byte(paneID), 0644)

	// Clean up pid file on exit
	defer os.Remove(pidFile)

	// Load config and start watching
	cfg := loadConfig(projectDir)
	runWatchLoop(projectDir, cfg)
}

func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func getTmuxPaneID() string {
	cmd := exec.Command("tmux", "display-message", "-p", "#{pane_id}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

Note: `loadConfig` and `runWatchLoop` are stubs — they'll be implemented in Tasks 2 and 5.

- [ ] **Step 3: Add temporary stubs so it compiles**

Add to the bottom of `main.go` (these will be removed as real implementations land):

```go
// Temporary stubs — removed in later tasks
func loadConfig(projectDir string) Config {
	return Config{PollInterval: 3}
}

func runWatchLoop(projectDir string, cfg Config) {
	fmt.Println("watch loop not yet implemented")
}

type Config struct {
	BuildCmd        string
	BuildExtensions []string
	PollInterval    int
}
```

- [ ] **Step 4: Verify it compiles and runs**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal . && ./trupal
```

Expected: prints `usage: trupal <start|stop> [project-dir]` to stderr, exits 1.

Run (outside tmux test):
```bash
TMUX= ./trupal start
```

Expected: prints `trupal must be run inside a tmux session`, exits 1.

- [ ] **Step 5: Commit**

```bash
git add go.mod main.go
git commit -m "feat: CLI scaffold with start/stop/watch commands and tmux pane management"
```

---

### Task 2: Config parser

**Files:**
- Create: `config.go`
- Modify: `main.go` (remove `Config` type and `loadConfig` stub)

- [ ] **Step 1: Write config.go**

Create `config.go`:

```go
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	BuildCmd        string
	BuildExtensions []string
	PollInterval    int // seconds
}

func DefaultConfig() Config {
	return Config{
		PollInterval: 3,
	}
}

func loadConfig(projectDir string) Config {
	cfg := DefaultConfig()

	f, err := os.Open(filepath.Join(projectDir, ".trupal.toml"))
	if err != nil {
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := parseTomlLine(line)
		if !ok {
			continue
		}

		switch key {
		case "build_cmd":
			cfg.BuildCmd = value
		case "build_extensions":
			cfg.BuildExtensions = parseTomlArray(value)
		case "poll_interval":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				cfg.PollInterval = n
			}
		}
	}

	return cfg
}

// parseTomlLine extracts key and value from `key = "value"` or `key = value`.
// Returns unquoted value.
func parseTomlLine(line string) (key, value string, ok bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key = strings.TrimSpace(parts[0])
	value = strings.TrimSpace(parts[1])
	// Unquote string values
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

// parseTomlArray parses `[".ts", ".tsx"]` into a string slice.
func parseTomlArray(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil
	}
	s = s[1 : len(s)-1]
	var result []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if len(item) >= 2 && item[0] == '"' && item[len(item)-1] == '"' {
			item = item[1 : len(item)-1]
		}
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
```

- [ ] **Step 2: Remove stubs from main.go**

Remove the `Config` type, `loadConfig` stub, and the `runWatchLoop` stub from `main.go`. Replace `runWatchLoop` with a minimal stub that just prints and exits (will be implemented in Task 5):

```go
func runWatchLoop(projectDir string, cfg Config) {
	fmt.Println("watch loop not yet implemented")
}
```

Keep only this one stub. The `Config` type and `loadConfig` are now in `config.go`.

- [ ] **Step 3: Verify it compiles**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add config.go main.go
git commit -m "feat: config parser for .trupal.toml (build_cmd, build_extensions, poll_interval)"
```

---

### Task 3: Session state and trajectory evaluation

**Files:**
- Create: `session.go`

- [ ] **Step 1: Write session.go**

Create `session.go`:

```go
package main

import (
	"fmt"
	"hash/fnv"
	"time"
)

type Session struct {
	StartTime     time.Time
	ProjectDir    string
	FileEditCount map[string]int    // filename -> number of cycles where diff changed
	ErrorHistory  []int             // build error count per cycle
	LastDiffHash  map[string]uint64 // filename -> fnv hash of diff content for that file
}

func NewSession(projectDir string) *Session {
	return &Session{
		StartTime:     time.Now(),
		ProjectDir:    projectDir,
		FileEditCount: make(map[string]int),
		ErrorHistory:  nil,
		LastDiffHash:  make(map[string]uint64),
	}
}

// UpdateFileEdits takes a map of filename -> diff content for the current cycle.
// Hashes each file's diff, compares to last cycle, increments edit count if changed.
func (s *Session) UpdateFileEdits(fileDiffs map[string]string) {
	for filename, diffContent := range fileDiffs {
		h := fnvHash(diffContent)
		if prev, exists := s.LastDiffHash[filename]; !exists || prev != h {
			s.FileEditCount[filename]++
			s.LastDiffHash[filename] = h
		}
	}
	// Clean up files no longer in diff (they were committed or reset)
	for filename := range s.LastDiffHash {
		if _, exists := fileDiffs[filename]; !exists {
			delete(s.LastDiffHash, filename)
		}
	}
}

// AppendErrorCount records the build error count for this cycle.
func (s *Session) AppendErrorCount(count int) {
	s.ErrorHistory = append(s.ErrorHistory, count)
}

// Finding represents a single issue detected by TruPal.
type Finding struct {
	Level   string // "warn" or "error"
	Message string
}

// EvalTrajectory checks session state for trajectory anti-patterns.
// Returns a list of findings.
func (s *Session) EvalTrajectory() []Finding {
	var findings []Finding

	// Whack-a-mole: file edited 3+ cycles
	for filename, count := range s.FileEditCount {
		if count >= 3 {
			findings = append(findings, Finding{
				Level:   "warn",
				Message: fmt.Sprintf("%s edited %d times -- whack-a-mole?", filename, count),
			})
		}
	}

	// Error trend analysis (need at least 3 data points)
	if len(s.ErrorHistory) >= 3 {
		recent := s.ErrorHistory
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}

		last := recent[len(recent)-1]
		if last > 0 {
			// Check for stall: last 3+ entries equal
			stallCount := 1
			for i := len(recent) - 2; i >= 0; i-- {
				if recent[i] == last {
					stallCount++
				} else {
					break
				}
			}
			if stallCount >= 3 {
				findings = append(findings, Finding{
					Level:   "warn",
					Message: fmt.Sprintf("build errors stalled at %d (x%d cycles)", last, stallCount),
				})
			}

			// Check for fix-then-break: find min in recent, then increase
			minVal := recent[0]
			minIdx := 0
			for i, v := range recent {
				if v < minVal {
					minVal = v
					minIdx = i
				}
			}
			// If min is not at the end, and current is higher than min, it's fix-then-break
			if minIdx < len(recent)-1 && last > minVal && minVal < recent[0] {
				findings = append(findings, Finding{
					Level:   "warn",
					Message: fmt.Sprintf("build errors rose from %d back to %d -- fix-then-break?", minVal, last),
				})
			}
		}
	}

	return findings
}

// Elapsed returns the session duration as a human string.
func (s *Session) Elapsed() string {
	d := time.Since(s.StartTime)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add session.go
git commit -m "feat: session state with trajectory evaluation (whack-a-mole, error stall, fix-then-break)"
```

---

### Task 4: Checker — diff patterns and build runner

**Files:**
- Create: `checker.go`

- [ ] **Step 1: Write checker.go**

Create `checker.go`:

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pre-compiled patterns for diff scanning (matched against added lines only).
var (
	// Error swallowing
	pEmptyCatch   = regexp.MustCompile(`catch\s*\(\w*\)\s*\{\s*\}`)
	pExceptPass   = regexp.MustCompile(`except\s*(\s+\w+)?:\s*pass`)
	pGoIgnoreErr  = regexp.MustCompile(`_\s*,?\s*=\s*\w+`)

	// Lint/type suppression
	pSuppression = regexp.MustCompile(
		`@ts-ignore|@ts-expect-error|eslint-disable|#\s*type:\s*ignore|nolint|NOSONAR|@SuppressWarnings|#\s*nosec`)
)

// PatternFinding is a finding from diff pattern matching.
type PatternFinding struct {
	File    string
	Line    int    // line number in new file
	Pattern string // human description
}

// ScanDiffPatterns scans the raw unified diff for suspicious added lines.
// Returns pattern findings with file name and approximate line number.
func ScanDiffPatterns(rawDiff string) []PatternFinding {
	var findings []PatternFinding
	var currentFile string
	var newLineNum int

	for _, line := range strings.Split(rawDiff, "\n") {
		// Track current file from diff header
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = line[6:]
			continue
		}
		// Track line numbers from hunk header: @@ -a,b +c,d @@
		if strings.HasPrefix(line, "@@") {
			newLineNum = parseHunkNewStart(line)
			continue
		}
		// Count lines in new file
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedContent := line[1:]

			// Error swallowing
			if pEmptyCatch.MatchString(addedContent) {
				findings = append(findings, PatternFinding{currentFile, newLineNum, "empty catch block"})
			}
			if pExceptPass.MatchString(addedContent) {
				findings = append(findings, PatternFinding{currentFile, newLineNum, "except: pass"})
			}
			if pGoIgnoreErr.MatchString(addedContent) && isLikelyErrorIgnore(addedContent) {
				findings = append(findings, PatternFinding{currentFile, newLineNum, "ignored error"})
			}

			// Lint/type suppression
			if pSuppression.MatchString(addedContent) {
				findings = append(findings, PatternFinding{currentFile, newLineNum, "lint/type suppression"})
			}

			newLineNum++
		} else if !strings.HasPrefix(line, "-") {
			// Context line: exists in both old and new
			newLineNum++
		}
		// Removed lines ("-"): don't increment newLineNum
	}

	return findings
}

// isLikelyErrorIgnore reduces false positives for the `_ = x` pattern.
// Only flag if the line looks like a Go error ignore, not a general assignment.
func isLikelyErrorIgnore(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "_") && (strings.Contains(trimmed, "err") || strings.Contains(trimmed, "Err"))
}

// ScanDeletedTests checks git diff --name-status output for deleted test files.
func ScanDeletedTests(nameStatus string) []string {
	var deleted []string
	for _, line := range strings.Split(nameStatus, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "D\t") && !strings.HasPrefix(line, "D ") {
			continue
		}
		filename := strings.TrimSpace(line[1:])
		lower := strings.ToLower(filename)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.HasSuffix(lower, "_test.go") {
			deleted = append(deleted, filename)
		}
	}
	return deleted
}

// BuildResult holds the output of a build check.
type BuildResult struct {
	OK         bool
	ErrorCount int
	Output     string
}

// RunBuildCheck runs the configured build command and counts errors.
func RunBuildCheck(projectDir string, buildCmd string) BuildResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	parts := strings.Fields(buildCmd)
	if len(parts) == 0 {
		return BuildResult{OK: true}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = projectDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := stdout.String() + stderr.String()

	if err == nil {
		return BuildResult{OK: true, Output: combined}
	}

	// Count error lines. Heuristic: count lines containing "error" (case-insensitive).
	// This works across tsc, cargo, mypy, go vet, gcc, etc.
	errorCount := 0
	for _, line := range strings.Split(combined, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") && !strings.Contains(lower, "0 error") {
			errorCount++
		}
	}
	if errorCount == 0 {
		errorCount = 1 // at least 1 error if command failed
	}

	return BuildResult{OK: false, ErrorCount: errorCount, Output: combined}
}

// parseHunkNewStart extracts the new-file start line from a hunk header.
// Input: "@@ -10,5 +20,8 @@ optional context"
// Returns: 20
func parseHunkNewStart(hunkLine string) int {
	// Find the "+N" or "+N,M" part
	idx := strings.Index(hunkLine, "+")
	if idx < 0 {
		return 1
	}
	rest := hunkLine[idx+1:]
	// Take until comma or space
	end := strings.IndexAny(rest, ", @")
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 1
	}
	return n
}

// ShouldRunBuild checks if any changed file matches the configured build extensions.
// If no extensions configured, returns true (run on any change).
func ShouldRunBuild(changedFiles []string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	for _, f := range changedFiles {
		for _, ext := range extensions {
			if strings.HasSuffix(f, ext) {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add checker.go
git commit -m "feat: diff pattern scanner and build command runner"
```

---

### Task 5: Display renderer

**Files:**
- Create: `display.go`

- [ ] **Step 1: Write display.go**

Create `display.go`:

```go
package main

import (
	"fmt"
	"strings"
)

// ANSI color codes
const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	dim    = "\033[2m"
	bold   = "\033[1m"
)

// DisplayState holds everything needed to render one frame.
type DisplayState struct {
	ProjectDir   string
	Elapsed      string
	ChangedFiles []string

	// Build status (nil = no build configured or not run this cycle)
	Build *BuildDisplay

	// Findings
	TrajectoryFindings []Finding
	PatternFindings    []PatternFinding
	DeletedTests       []string
}

// BuildDisplay holds build info for rendering.
type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string // "was N", "stalled xN", "" (first run)
}

// Render clears the screen and draws the full display frame.
func Render(state DisplayState) {
	// Clear screen and move cursor to top-left
	fmt.Print("\033[2J\033[H")

	width := 40
	header := "TruPal"
	line := strings.Repeat("-", width)

	fmt.Printf("%s--- %s%s %s%s\n", dim, reset, bold, header, reset)
	fmt.Printf("%s%s%s\n", dim, line, reset)
	fmt.Printf("%swatching %s%s\n", dim, state.ProjectDir, reset)
	fmt.Printf("%ssession: %s%s\n", dim, state.Elapsed, reset)
	fmt.Println()

	if len(state.ChangedFiles) == 0 && state.Build == nil && len(state.TrajectoryFindings) == 0 && len(state.PatternFindings) == 0 && len(state.DeletedTests) == 0 {
		fmt.Printf("  %swatching...%s\n", dim, reset)
	} else {
		// Changed files
		if len(state.ChangedFiles) > 0 {
			truncated := state.ChangedFiles
			if len(truncated) > 8 {
				truncated = truncated[:8]
			}
			fmt.Printf("  %schanged: %s%s\n", dim, strings.Join(truncated, ", "), reset)
			if len(state.ChangedFiles) > 8 {
				fmt.Printf("  %s  ...and %d more%s\n", dim, len(state.ChangedFiles)-8, reset)
			}
			fmt.Println()
		}

		// Build status
		if state.Build != nil {
			renderBuild(state.Build)
			fmt.Println()
		}

		// Trajectory findings
		for _, f := range state.TrajectoryFindings {
			fmt.Printf("  %s! %s%s\n", yellow, f.Message, reset)
		}

		// Pattern findings
		for _, f := range state.PatternFindings {
			fmt.Printf("  %s! %s (%s:+%d)%s\n", yellow, f.Pattern, f.File, f.Line, reset)
		}

		// Deleted tests
		for _, f := range state.DeletedTests {
			fmt.Printf("  %s! deleted %s%s\n", yellow, f, reset)
		}
	}

	fmt.Println()
	fmt.Printf("  %slast check: just now%s\n", dim, reset)
	fmt.Printf("%s%s%s\n", dim, line, reset)
}

func renderBuild(b *BuildDisplay) {
	if b.OK {
		fmt.Printf("  build: %s%s clean%s\n", green, bold, reset) // ✓ replaced with bold green
		return
	}

	color := red
	trend := ""
	if b.Trend != "" {
		trend = " (" + b.Trend + ")"
		if strings.HasPrefix(b.Trend, "was") {
			// "was N" where N > current means fix-then-break
			// Already handled by trajectory findings, just show the trend
		}
	}
	fmt.Printf("  build: %s%d errors%s%s\n", color, b.ErrorCount, trend, reset)
}
```

- [ ] **Step 2: Verify it compiles**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add display.go
git commit -m "feat: ANSI display renderer for tmux pane"
```

---

### Task 6: Watch loop — tie everything together

**Files:**
- Create: `watcher.go`
- Modify: `main.go` (remove `runWatchLoop` stub)

- [ ] **Step 1: Write watcher.go**

Create `watcher.go`:

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runWatchLoop is the main poll loop. Runs until the process is killed.
func runWatchLoop(projectDir string, cfg Config) {
	session := NewSession(projectDir)
	interval := time.Duration(cfg.PollInterval) * time.Second

	// Print startup info
	fmt.Printf("trupal watching %s\n", projectDir)
	if cfg.BuildCmd != "" {
		fmt.Printf("build: %s\n", cfg.BuildCmd)
		if len(cfg.BuildExtensions) > 0 {
			fmt.Printf("extensions: %s\n", strings.Join(cfg.BuildExtensions, ", "))
		}
	} else {
		fmt.Println("no build command configured")
	}
	fmt.Println("starting in 3s...")
	time.Sleep(3 * time.Second)

	for {
		state := pollCycle(session, projectDir, cfg)
		Render(state)
		time.Sleep(interval)
	}
}

// pollCycle runs one complete check cycle and returns display state.
func pollCycle(session *Session, projectDir string, cfg Config) DisplayState {
	state := DisplayState{
		ProjectDir: shortenPath(projectDir),
		Elapsed:    session.Elapsed(),
	}

	// 1. Get changed files
	changedFiles := gitDiffNameOnly(projectDir)
	state.ChangedFiles = changedFiles

	// 2. Get full diff and split per file
	rawDiff := gitDiff(projectDir)
	fileDiffs := splitDiffByFile(rawDiff)

	// 3. Update session state (edit counting)
	session.UpdateFileEdits(fileDiffs)

	// 4. Run build check if configured and relevant files changed
	if cfg.BuildCmd != "" && len(changedFiles) > 0 && ShouldRunBuild(changedFiles, cfg.BuildExtensions) {
		result := RunBuildCheck(projectDir, cfg.BuildCmd)
		session.AppendErrorCount(result.ErrorCount)

		bd := &BuildDisplay{
			OK:         result.OK,
			ErrorCount: result.ErrorCount,
		}

		// Compute trend string from error history
		if len(session.ErrorHistory) >= 2 {
			prev := session.ErrorHistory[len(session.ErrorHistory)-2]
			curr := result.ErrorCount
			if !result.OK {
				if prev > curr {
					bd.Trend = fmt.Sprintf("was %d", prev)
				} else if prev == curr {
					// Count stall length
					stallCount := 1
					for i := len(session.ErrorHistory) - 2; i >= 0; i-- {
						if session.ErrorHistory[i] == curr {
							stallCount++
						} else {
							break
						}
					}
					if stallCount >= 3 {
						bd.Trend = fmt.Sprintf("stalled x%d", stallCount)
					}
				} else if prev < curr && prev > 0 {
					bd.Trend = fmt.Sprintf("was %d", prev)
				}
			}
		}

		state.Build = bd
	}

	// 5. Scan diff patterns
	if rawDiff != "" {
		state.PatternFindings = ScanDiffPatterns(rawDiff)
	}

	// 6. Scan for deleted tests
	nameStatus := gitDiffNameStatus(projectDir)
	state.DeletedTests = ScanDeletedTests(nameStatus)

	// 7. Evaluate trajectory
	state.TrajectoryFindings = session.EvalTrajectory()

	return state
}

// splitDiffByFile splits a unified diff into per-file diff content.
// Returns map of filename -> diff content for that file.
func splitDiffByFile(rawDiff string) map[string]string {
	result := make(map[string]string)
	var currentFile string
	var currentDiff strings.Builder

	for _, line := range strings.Split(rawDiff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Save previous file's diff
			if currentFile != "" {
				result[currentFile] = currentDiff.String()
			}
			currentFile = ""
			currentDiff.Reset()
		}
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = line[6:]
		}
		currentDiff.WriteString(line)
		currentDiff.WriteByte('\n')
	}
	// Save last file
	if currentFile != "" {
		result[currentFile] = currentDiff.String()
	}

	return result
}

// Git helpers

func gitDiffNameOnly(projectDir string) []string {
	out := runGit(projectDir, "diff", "--name-only", "HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func gitDiffNameStatus(projectDir string) string {
	return runGit(projectDir, "diff", "--name-status", "HEAD")
}

func gitDiff(projectDir string) string {
	return runGit(projectDir, "diff", "HEAD")
}

func runGit(projectDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func shortenPath(path string) string {
	home, err := homeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func homeDir() (string, error) {
	home := envOrDefault("HOME", "")
	if home == "" {
		return "", fmt.Errorf("HOME not set")
	}
	return home, nil
}

func envOrDefault(key, def string) string {
	v := strings.TrimSpace(mustGetenv(key))
	if v == "" {
		return def
	}
	return v
}

func mustGetenv(key string) string {
	return strings.TrimSpace(getenv(key))
}

func getenv(key string) string {
	for _, e := range envPairs() {
		if strings.HasPrefix(e, key+"=") {
			return e[len(key)+1:]
		}
	}
	return ""
}

func envPairs() []string {
	return nil // will use os.Getenv instead
}
```

Wait — that env helper chain is overengineered. Let me simplify.

Replace the `shortenPath`, `homeDir`, and all env helpers with:

```go
func shortenPath(path string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
```

And add `"os"` to the imports. Remove `homeDir`, `envOrDefault`, `mustGetenv`, `getenv`, `envPairs`.

Here is the complete, final `watcher.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func runWatchLoop(projectDir string, cfg Config) {
	session := NewSession(projectDir)
	interval := time.Duration(cfg.PollInterval) * time.Second

	fmt.Printf("trupal watching %s\n", projectDir)
	if cfg.BuildCmd != "" {
		fmt.Printf("build: %s\n", cfg.BuildCmd)
		if len(cfg.BuildExtensions) > 0 {
			fmt.Printf("extensions: %s\n", strings.Join(cfg.BuildExtensions, ", "))
		}
	} else {
		fmt.Println("no build command configured")
	}
	fmt.Println("starting in 3s...")
	time.Sleep(3 * time.Second)

	for {
		state := pollCycle(session, projectDir, cfg)
		Render(state)
		time.Sleep(interval)
	}
}

func pollCycle(session *Session, projectDir string, cfg Config) DisplayState {
	state := DisplayState{
		ProjectDir: shortenPath(projectDir),
		Elapsed:    session.Elapsed(),
	}

	changedFiles := gitDiffNameOnly(projectDir)
	state.ChangedFiles = changedFiles

	rawDiff := gitDiff(projectDir)
	fileDiffs := splitDiffByFile(rawDiff)

	session.UpdateFileEdits(fileDiffs)

	if cfg.BuildCmd != "" && len(changedFiles) > 0 && ShouldRunBuild(changedFiles, cfg.BuildExtensions) {
		result := RunBuildCheck(projectDir, cfg.BuildCmd)
		session.AppendErrorCount(result.ErrorCount)

		bd := &BuildDisplay{
			OK:         result.OK,
			ErrorCount: result.ErrorCount,
		}

		if len(session.ErrorHistory) >= 2 {
			prev := session.ErrorHistory[len(session.ErrorHistory)-2]
			curr := result.ErrorCount
			if !result.OK {
				if prev > curr {
					bd.Trend = fmt.Sprintf("was %d", prev)
				} else if prev == curr {
					stallCount := 1
					for i := len(session.ErrorHistory) - 2; i >= 0; i-- {
						if session.ErrorHistory[i] == curr {
							stallCount++
						} else {
							break
						}
					}
					if stallCount >= 3 {
						bd.Trend = fmt.Sprintf("stalled x%d", stallCount)
					}
				} else if prev < curr && prev > 0 {
					bd.Trend = fmt.Sprintf("was %d", prev)
				}
			}
		}

		state.Build = bd
	}

	if rawDiff != "" {
		state.PatternFindings = ScanDiffPatterns(rawDiff)
	}

	nameStatus := gitDiffNameStatus(projectDir)
	state.DeletedTests = ScanDeletedTests(nameStatus)

	state.TrajectoryFindings = session.EvalTrajectory()

	return state
}

func splitDiffByFile(rawDiff string) map[string]string {
	result := make(map[string]string)
	var currentFile string
	var currentDiff strings.Builder

	for _, line := range strings.Split(rawDiff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			if currentFile != "" {
				result[currentFile] = currentDiff.String()
			}
			currentFile = ""
			currentDiff.Reset()
		}
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = line[6:]
		}
		currentDiff.WriteString(line)
		currentDiff.WriteByte('\n')
	}
	if currentFile != "" {
		result[currentFile] = currentDiff.String()
	}

	return result
}

func gitDiffNameOnly(projectDir string) []string {
	out := runGit(projectDir, "diff", "--name-only", "HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func gitDiffNameStatus(projectDir string) string {
	return runGit(projectDir, "diff", "--name-status", "HEAD")
}

func gitDiff(projectDir string) string {
	return runGit(projectDir, "diff", "HEAD")
}

func runGit(projectDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func shortenPath(path string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
```

- [ ] **Step 2: Remove runWatchLoop stub from main.go**

Delete the `runWatchLoop` stub function from `main.go`. The real implementation is now in `watcher.go`.

- [ ] **Step 3: Verify it compiles**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 4: Commit**

```bash
git add watcher.go main.go
git commit -m "feat: watch loop tying together git polling, session tracking, checks, and display"
```

---

### Task 7: End-to-end smoke test

**Files:**
- No new files. Tests against the built binary in a temp git repo.

- [ ] **Step 1: Build the binary**

Run:
```bash
cd /home/yuxuan/work/trupal && /usr/local/go/bin/go build -o trupal .
```

Expected: builds with no errors.

- [ ] **Step 2: Initialize trupal's own repo for testing**

Run:
```bash
cd /home/yuxuan/work/trupal && git init && git add -A && git commit -m "initial"
```

- [ ] **Step 3: Test trupal start inside tmux**

Run inside a tmux session:
```bash
cd /home/yuxuan/work/trupal && ./trupal start
```

Expected: a new tmux pane appears on the right (30% width) showing:
```
trupal watching /home/yuxuan/work/trupal
no build command configured
starting in 3s...
```

Then after 3s, the display renders with "watching..." (since there are no uncommitted changes).

Verify `.trupal.pid` was created:
```bash
cat /home/yuxuan/work/trupal/.trupal.pid
```

Expected: a tmux pane ID like `%5`.

- [ ] **Step 4: Test change detection**

In the main pane, make a dirty change:
```bash
echo "// test change" >> /home/yuxuan/work/trupal/main.go
```

Wait 3 seconds. The trupal pane should update to show `changed: main.go`.

- [ ] **Step 5: Test pattern detection**

Create a file with a suspicious pattern:
```bash
cat >> /tmp/test_pattern.py << 'EOF'
try:
    something()
except:
    pass
EOF
cp /tmp/test_pattern.py /home/yuxuan/work/trupal/test_pattern.py
```

Wait 3 seconds. Trupal pane should show a finding for `except: pass`.

- [ ] **Step 6: Test trupal stop**

Run:
```bash
cd /home/yuxuan/work/trupal && ./trupal stop
```

Expected: trupal pane closes, prints "trupal stopped", `.trupal.pid` removed.

- [ ] **Step 7: Clean up test artifacts**

```bash
cd /home/yuxuan/work/trupal && git checkout main.go && rm -f test_pattern.py
```

- [ ] **Step 8: Add .gitignore**

Create `.gitignore`:
```
trupal
.trupal.pid
```

- [ ] **Step 9: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore for binary and pid file"
```

---

## Self-Review

**Spec coverage check:**
- Commands (start/stop): Task 1 ✓
- Watch loop (3s polling, 8-step cycle): Task 6 ✓
- Trajectory signals (whack-a-mole, error stall, fix-then-break): Task 3 ✓
- Diff patterns (error swallowing, lint suppression): Task 4 ✓
- Deleted tests: Task 4 ✓
- Session state (in-memory, per-file edit count, error history, diff hashing): Task 3 ✓
- Config (.trupal.toml, build_cmd, build_extensions, poll_interval): Task 2 ✓
- Display (ANSI, clear+redraw, active/idle states, color scheme): Task 5 ✓
- Line number references from hunk headers: Task 4 (parseHunkNewStart) ✓
- tmux detection / error if not in tmux: Task 1 ✓
- .trupal.pid lifecycle: Task 1 ✓
- .gitignore: Task 7 ✓
- Platform support (macOS + Linux): no OS-specific code throughout ✓
- Zero external deps: verified, stdlib only ✓

**Placeholder scan:** No TBDs, TODOs, or "implement later" found.

**Type consistency:**
- `Config` defined in Task 2, used in Tasks 1, 6 ✓
- `Session` / `NewSession` / `UpdateFileEdits` / `AppendErrorCount` / `EvalTrajectory` / `Finding` defined in Task 3, used in Task 6 ✓
- `ScanDiffPatterns` / `PatternFinding` / `ScanDeletedTests` / `RunBuildCheck` / `BuildResult` / `ShouldRunBuild` defined in Task 4, used in Task 6 ✓
- `Render` / `DisplayState` / `BuildDisplay` defined in Task 5, used in Task 6 ✓
- `loadConfig` defined in Task 2, called in Task 1 ✓
- `runWatchLoop` stub created in Task 1, real impl in Task 6 replaces it ✓
