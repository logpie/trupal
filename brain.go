package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BrainResponse is the parsed JSON response from the brain.
type BrainResponse struct {
	Reasoning        string       `json:"reasoning"`
	Observations     []string     `json:"observations"`
	Nudges           []BrainNudge `json:"nudges"`
	ResolvedFindings []string     `json:"resolved_findings"`
	Usage            BrainUsage   `json:"usage"`
	TotalCostUSD     float64      `json:"total_cost_usd"`
}

// BrainNudge is a single nudge from the brain.
type BrainNudge struct {
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Reasoning string `json:"reasoning,omitempty"` // per-nudge context
}

// BrainUsage is per-turn token usage reported by the brain subprocess.
type BrainUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// BrainStats is the cumulative token and cost usage for the current trupal session.
type BrainStats struct {
	TotalInputTokens         int
	TotalOutputTokens        int
	TotalCacheCreationTokens int
	TotalCacheReadTokens     int
	TotalCostUSD             float64
}

func (s *BrainStats) addTurn(usage BrainUsage, costUSD float64) {
	s.TotalInputTokens += usage.InputTokens
	s.TotalOutputTokens += usage.OutputTokens
	s.TotalCacheCreationTokens += usage.CacheCreationInputTokens
	s.TotalCacheReadTokens += usage.CacheReadInputTokens
	s.TotalCostUSD += costUSD
}

// Brain manages the CC subprocess.
type Brain struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner
	stopped atomic.Bool
	cfg     Config
	statsMu sync.Mutex
	stats   BrainStats
}

// brainSystemPrompt returns the static system prompt for the brain.
func brainSystemPrompt(projectDir, jsonlPath string) string {
	return fmt.Sprintf(`You are TruPal, a continuous verification agent watching a Claude Code session.

CC's session JSONL: %s
Project directory: %s

IMPORTANT — you are a STREAMING monitor with memory across turns. Be INCREMENTAL:
- Each notification includes ACTIVE FINDINGS you already flagged. Do NOT re-flag them.
- Only generate nudges for NEW issues you haven't flagged before.
- If active findings are still unresolved, that's fine — don't repeat them.
- Every notification is a request to investigate. There is NO fast path.
- Start with the notification context: recent session activity, edited files, git diff summary, and build status.
- ALWAYS read every changed file named in the notification — the FULL file, not just the diff.
- When reading a file, check for SYSTEMIC issues across the entire file, not just the latest changes:
  race conditions (global maps/slices without mutex), error swallowing, panic misuse,
  cache invalidation gaps, resource leaks, middleware ordering bugs.
- Use the RECENT SESSION ACTIVITY included in the notification for claim/action checks.
- Do NOT reread the full JSONL file on every notification. Only open the JSONL directly if the notification excerpt is missing critical evidence.
- Silence means bugs escape. Flag anything you find — even pre-existing bugs that CC didn't introduce.
  CC is responsible for the code they're working in, including what was already there.

You are a nudge engine. You talk like a senior dev sitting next to CC.
Always start with "you" or "hey" — address CC directly. Never write like a linter.
Say what's wrong, why it matters, and what to do about it. Be specific — name files, lines, functions.

VOICE EXAMPLES (copy this tone):
- "Hey, you just added Expire() but it never removes the expired entries — they pile up in memory forever. Either delete them or drop the status field and just remove in place."
- "You've edited this file 4 times and the error count hasn't gone down. Step back — re-read the whole function before your next edit."
- "You said you verified all callers but I don't see a single grep or search in your tool calls. Can you actually check?"
- "You're swallowing the error from json.Marshal here — if serialization fails, the caller gets an empty array and no way to know something broke."
- "Hey, this global map has no mutex. Under concurrent requests you'll get a data race. Wrap it with sync.RWMutex."

NEVER write like this:
- "ActiveJSON swallows json.Marshal errors" (sounds like a linter)
- "missing error handling" (no context)
- "Race condition detected in sessions map" (passive, no action)

What to look for:
- CLAIM-ACTION GAPS: CC said it did X but JSONL shows no corresponding tool call
- ERROR HANDLING: swallowed errors, dropped return values, returns that hide failures
- RACE CONDITIONS: unsynchronized global state, shared maps/slices without locks, concurrent access patterns
- PANIC MISUSE: panic for expected errors, panics that skip cleanup, recover misuse
- CACHE INVALIDATION: stale cached values, missing eviction/update paths, cache/state divergence
- RESOURCE LEAKS: goroutines, timers, file handles, subprocesses, channels, memory growth
- PROCESS QUALITY: edit without reading first? no tests after changes?
- TRAJECTORY: same file edited repeatedly without progress

CRITICAL RULE: If you find a code issue (bug, race condition, swallowed error, missing validation),
ALWAYS generate a nudge for it. Do NOT suppress nudges because you think CC is "testing" or the bug
is "intentional." Your job is to flag code problems. CC decides what to do with them.
Another way to say it: silence means bugs escape, so investigate first and err on the side of speaking up.

Respond with JSON only:
{
  "observations": ["what you noticed — facts, patterns, context"],
  "nudges": [{"severity": "warn|error", "message": "what CC should do", "reasoning": "why"}],
  "resolved_findings": ["<finding_id>"]
}

Observations are for things worth the human's attention — patterns, risks, notable decisions.
NOT for: routine activity ("CC read a file"), internal state ("JSONL flushed"), timestamps.
Max 2 observations per response. If nothing notable, return empty.

Rules:
- Observations: 1 sentence each. Only notable patterns or risks.
- Nudges: conversational, addressed to CC. Include reasoning (1 sentence) for context.
- Focus on real correctness and verification risks, not style nits.
- After you investigate, if nothing important is wrong, return empty nudges.`, jsonlPath, projectDir)
}

func brainCommand(provider string) (string, error) {
	switch provider {
	case "", "claude":
		return "claude", nil
	default:
		return "", fmt.Errorf("unsupported brain_provider %q (supported: claude)", provider)
	}
}

func brainNotifyMessage(reason, findingsJSON string) string {
	if findingsJSON == "" {
		findingsJSON = "[]"
	}
	// reason already contains files changed + diff summary from buildBrainNotification
	return fmt.Sprintf("%s\n\nACTIVE FINDINGS:\n%s", reason, findingsJSON)
}

// StartBrain spawns the CC subprocess. extraDirs are additional directories
// the brain can access (from CC's tool calls to files outside projectDir).
func StartBrain(cfg Config, projectDir, jsonlPath string, initialStats BrainStats, extraDirs ...string) (*Brain, error) {
	prompt := brainSystemPrompt(projectDir, jsonlPath)
	command, err := brainCommand(cfg.BrainProvider)
	if err != nil {
		return nil, err
	}

	accessibleDirs := brainAccessibleDirs(projectDir, extraDirs...)

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--system-prompt", prompt,
		"--model", cfg.BrainModel,
		"--effort", cfg.BrainEffort,
		"--dangerously-skip-permissions",
		"--no-session-persistence",
		"--allowed-tools", "Read,Bash,Grep,Glob",
	}
	// Allow brain to read common cross-project locations plus any dirs learned
	// from CC's session activity.
	for _, d := range accessibleDirs {
		args = append(args, "--add-dir", d)
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = projectDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	Debugf("[brain] starting subprocess")
	if err := cmd.Start(); err != nil {
		stdin.Close()
		Debugf("[brain] start failed: %v", err)
		return nil, fmt.Errorf("start brain: %w", err)
	}
	Debugf("[brain] started (pid %d)", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	return &Brain{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		scanner: scanner,
		cfg:     cfg,
		stats:   initialStats,
	}, nil
}

func brainAccessibleDirs(projectDir string, extraDirs ...string) []string {
	seen := make(map[string]bool)
	var dirs []string

	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		add(homeDir)
	}
	add(projectDir)
	add(os.TempDir())

	for _, dir := range extraDirs {
		add(dir)
	}

	sort.Strings(dirs)
	return dirs
}

// Notify sends a message to the brain and blocks until the brain responds.
// Safe to call from a single goroutine. Stop() can interrupt it by closing stdin.
func (b *Brain) Notify(reason, findingsJSON string) (*BrainResponse, error) {
	if b.stopped.Load() {
		return nil, fmt.Errorf("brain stopped")
	}

	message := brainNotifyMessage(reason, findingsJSON)
	Debugf("[brain] notify: %s", truncate(message, 200))
	start := time.Now()
	msg := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%s}}`, jsonString(message))
	if _, err := fmt.Fprintln(b.stdin, msg); err != nil {
		Debugf("[brain] write failed: %v", err)
		return nil, fmt.Errorf("write to brain: %w", err)
	}

	// Read response lines until we see a "result" type (end of turn).
	var lastText string
	var usage BrainUsage
	var totalCostUSD float64
	sawResult := false
	for b.scanner.Scan() {
		line := b.scanner.Text()
		if line == "" {
			continue
		}

		var event struct {
			Type         string     `json:"type"`
			Usage        BrainUsage `json:"usage"`
			TotalCostUSD float64    `json:"total_cost_usd"`
			Message      struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.Type == "assistant" {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					lastText = block.Text
				}
			}
		}

		if event.Type == "result" {
			usage = event.Usage
			totalCostUSD = event.TotalCostUSD
			sawResult = true
			break
		}
	}

	// Check scanner error (EOF, broken pipe, etc.)
	if err := b.scanner.Err(); err != nil {
		Debugf("[brain] scanner error: %v", err)
		return nil, fmt.Errorf("brain read error: %w", err)
	}

	elapsed := time.Since(start)
	if lastText == "" {
		Debugf("[brain] no response after %s", elapsed)
		// If scanner hit EOF without a "result" event, the subprocess likely died.
		return nil, fmt.Errorf("brain exited unexpectedly (no response after %s)", elapsed)
	}

	Debugf("[brain] response received after %s (%d chars)", elapsed, len(lastText))

	resp, err := parseBrainJSON(lastText)
	if err != nil {
		Debugf("[brain] JSON parse failed, using raw text: %v", err)
		resp = &BrainResponse{Reasoning: lastText}
	}
	resp.Usage = usage
	resp.TotalCostUSD = totalCostUSD
	if sawResult {
		b.statsMu.Lock()
		b.stats.addTurn(usage, totalCostUSD)
		b.statsMu.Unlock()
		Debugf("[brain] usage: in=%d out=%d cache_read=%d cache_create=%d cost=$%.4f",
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheReadInputTokens,
			usage.CacheCreationInputTokens,
			totalCostUSD,
		)
	}
	Debugf("[brain] %d nudges, %d resolved, reasoning: %s", len(resp.Nudges), len(resp.ResolvedFindings), truncate(resp.Reasoning, 100))
	return resp, nil
}

// Stats returns a snapshot of cumulative brain usage for the current trupal session.
func (b *Brain) Stats() BrainStats {
	b.statsMu.Lock()
	defer b.statsMu.Unlock()
	return b.stats
}

// parseBrainJSON extracts the BrainResponse JSON from the brain's text output.
func parseBrainJSON(text string) (*BrainResponse, error) {
	var resp BrainResponse
	if err := json.Unmarshal([]byte(text), &resp); err == nil {
		return &resp, nil
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(text[start:end+1]), &resp); err == nil {
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("no valid JSON in brain response")
}

// Stop kills the brain subprocess. Non-blocking — closes stdin to unblock Notify.
func (b *Brain) Stop() {
	if b.stopped.Swap(true) {
		return // already stopped
	}
	Debugf("[brain] stopping")
	// Close stdin to unblock scanner.Scan() in Notify.
	if b.stdin != nil {
		b.stdin.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		b.cmd.Process.Kill()
		b.cmd.Wait()
	}
}

// IsRunning returns whether the brain process is alive.
func (b *Brain) IsRunning() bool {
	return !b.stopped.Load()
}

// truncate shortens s to maxLen with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// jsonString returns s as a JSON-encoded string (with escaping).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// RestartBrain stops the current brain and starts a new one after a delay.
func RestartBrain(cfg Config, projectDir, jsonlPath string, stats BrainStats, delay time.Duration, extraDirs ...string) (*Brain, error) {
	time.Sleep(delay)
	return StartBrain(cfg, projectDir, jsonlPath, stats, extraDirs...)
}
