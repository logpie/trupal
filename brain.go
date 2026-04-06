package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// BrainResponse is the parsed JSON response from the brain.
type BrainResponse struct {
	Reasoning        string       `json:"reasoning"`
	Nudges           []BrainNudge `json:"nudges"`
	ResolvedFindings []string     `json:"resolved_findings"`
}

// BrainNudge is a single nudge from the brain.
type BrainNudge struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Brain manages the CC subprocess.
type Brain struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
	running bool
	cfg     Config
	prompt  string
}

// brainSystemPrompt returns the system prompt for the brain, with placeholders filled.
func brainSystemPrompt(projectDir, jsonlPath, findingsJSON string) string {
	return fmt.Sprintf(`You are TruPal, a verification agent watching another Claude Code session work.
You are a peer to CC — a pair programmer specializing in catching mistakes,
verifying claims, and keeping the other agent honest.

CC's session JSONL: %s
Project directory: %s

ACTIVE FINDINGS (unresolved):
%s

When notified of activity, investigate autonomously:

1. Read the recent JSONL entries to understand what CC just did
2. Check git diff to see what actually changed
3. Look for:
   - CLAIM-ACTION GAPS: CC said it did something but JSONL shows no
     corresponding tool call (e.g. "verified tests pass" but no test command)
   - TRAJECTORY PROBLEMS: same file edited repeatedly, errors not decreasing,
     scope drifting from original task
   - PROCESS QUALITY: did CC read before editing? did CC verify after changing?
     root cause vs symptom patching?
   - STRUCTURAL ISSUES: error swallowing, deleted tests, coupling increase
4. Check if any active findings have been resolved by CC's recent actions

Respond with JSON:
{
  "reasoning": "what you checked and what you found (be specific, quote evidence)",
  "nudges": [
    {"severity": "warn|error", "message": "short nudge under 120 chars"}
  ],
  "resolved_findings": ["<finding_id>", ...]
}

If nothing noteworthy: {"reasoning": "checked X, Y, Z — nothing to flag", "nudges": [], "resolved_findings": []}

Guidelines:
- Be specific: quote file names, line numbers, tool names, JSONL timestamps
- CONCISE reasoning: 2-3 SHORT sentences max. Your output displays in a narrow tmux pane (~30 chars wide). No paragraphs.
- CONCISE nudges: under 80 chars each
- Don't nag: minor style issues are not worth flagging
- High precision: only flag things you're confident about
- Check your active findings: if CC addressed one, mark it resolved
- If nothing important to report, say so briefly — don't fill space with summaries of what CC is doing`, jsonlPath, projectDir, findingsJSON)
}

// StartBrain spawns the CC subprocess.
func StartBrain(cfg Config, projectDir, jsonlPath, findingsJSON string) (*Brain, error) {
	prompt := brainSystemPrompt(projectDir, jsonlPath, findingsJSON)

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

	cmd := exec.Command("claude", args...)
	cmd.Dir = projectDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Discard stderr.
	cmd.Stderr = nil

	Debugf("[brain] starting: claude %s", strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		Debugf("[brain] start failed: %v", err)
		return nil, fmt.Errorf("start brain: %w", err)
	}
	Debugf("[brain] started (pid %d)", cmd.Process.Pid)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB lines

	return &Brain{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		scanner: scanner,
		running: true,
		cfg:     cfg,
		prompt:  prompt,
	}, nil
}

// Notify sends a message to the brain and blocks until the brain responds.
// Returns the parsed response or an error.
func (b *Brain) Notify(message string) (*BrainResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil, fmt.Errorf("brain not running")
	}

	// Send user message.
	Debugf("[brain] notify: %s", truncate(message, 200))
	start := time.Now()
	msg := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%s}}`, jsonString(message))
	if _, err := fmt.Fprintln(b.stdin, msg); err != nil {
		b.running = false
		Debugf("[brain] write failed: %v", err)
		return nil, fmt.Errorf("write to brain: %w", err)
	}

	// Read response lines until we see a "result" type (end of turn).
	var lastText string
	for b.scanner.Scan() {
		line := b.scanner.Text()
		if line == "" {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Collect text content from assistant messages.
		if event.Type == "assistant" {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					lastText = block.Text
				}
			}
		}

		// "result" marks end of turn.
		if event.Type == "result" {
			break
		}
	}

	elapsed := time.Since(start)
	if lastText == "" {
		Debugf("[brain] no response after %s", elapsed)
		return &BrainResponse{Reasoning: "(no response from brain)"}, nil
	}

	Debugf("[brain] response received after %s (%d chars)", elapsed, len(lastText))

	// Parse the JSON from the last text block.
	resp, err := parseBrainJSON(lastText)
	if err != nil {
		Debugf("[brain] JSON parse failed, using raw text: %v", err)
		return &BrainResponse{Reasoning: lastText}, nil
	}
	Debugf("[brain] %d nudges, %d resolved, reasoning: %s", len(resp.Nudges), len(resp.ResolvedFindings), truncate(resp.Reasoning, 100))
	return resp, nil
}

// parseBrainJSON extracts the BrainResponse JSON from the brain's text output.
// The brain might wrap it in markdown code fences or include preamble text.
func parseBrainJSON(text string) (*BrainResponse, error) {
	// Try direct parse first.
	var resp BrainResponse
	if err := json.Unmarshal([]byte(text), &resp); err == nil {
		return &resp, nil
	}

	// Try to find JSON object in the text.
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(text[start:end+1]), &resp); err == nil {
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("no valid JSON in brain response")
}

// truncate shortens s to maxLen with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Stop kills the brain subprocess.
func (b *Brain) Stop() {
	Debugf("[brain] stopping")
	b.mu.Lock()
	defer b.mu.Unlock()

	b.running = false
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
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// jsonString returns s as a JSON-encoded string (with escaping).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// RestartBrain stops the current brain and starts a new one after a delay.
func RestartBrain(cfg Config, projectDir, jsonlPath, findingsJSON string, delay time.Duration) (*Brain, error) {
	time.Sleep(delay)
	return StartBrain(cfg, projectDir, jsonlPath, findingsJSON)
}
