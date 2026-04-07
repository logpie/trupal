package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
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
	stopped atomic.Bool
	cfg     Config
}

// brainSystemPrompt returns the system prompt for the brain, with placeholders filled.
func brainSystemPrompt(projectDir, jsonlPath, findingsJSON string) string {
	return fmt.Sprintf(`You are TruPal, a continuous verification agent watching a Claude Code session.

CC's session JSONL: %s
Project directory: %s

ACTIVE FINDINGS (unresolved):
%s

IMPORTANT — you are a STREAMING monitor. You will receive multiple notifications over time. You have memory of previous turns. Be INCREMENTAL:
- Do NOT re-read the entire JSONL every turn. You remember what you saw before.
- Only read the TAIL of the JSONL (last ~50 lines) to see what's NEW since your last check.
- Only use tools when you need to verify something specific — don't investigate everything.
- If nothing changed or nothing suspicious, respond immediately with empty nudges. Don't waste time investigating.

FAST PATH: Most notifications need NO tool use. Just respond with your assessment based on the notification context. Only use Read/Bash/Grep when you spot something suspicious that needs verification.

What to look for:
- CLAIM-ACTION GAPS: CC said it did X but never actually did (check JSONL tool calls)
- TRAJECTORY PROBLEMS: same file edited repeatedly, errors not decreasing, scope drift
- PROCESS QUALITY: edit without reading first? no verification after changes?
- STRUCTURAL: error swallowing, deleted tests, coupling increase

Respond with JSON only:
{"reasoning": "1-2 sentences", "nudges": [{"severity": "warn|error", "message": "under 80 chars"}], "resolved_findings": []}

Empty response: {"reasoning": "nothing to flag", "nudges": [], "resolved_findings": []}

Rules:
- 1-2 sentences max for reasoning. You display in a ~30 char wide pane.
- Under 80 chars per nudge
- High precision only. Don't nag about style.
- If nothing important, say so in 5 words and move on.`, jsonlPath, projectDir, findingsJSON)
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
	}, nil
}

// Notify sends a message to the brain and blocks until the brain responds.
// Safe to call from a single goroutine. Stop() can interrupt it by closing stdin.
func (b *Brain) Notify(message string) (*BrainResponse, error) {
	if b.stopped.Load() {
		return nil, fmt.Errorf("brain stopped")
	}

	Debugf("[brain] notify: %s", truncate(message, 200))
	start := time.Now()
	msg := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":%s}}`, jsonString(message))
	if _, err := fmt.Fprintln(b.stdin, msg); err != nil {
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

		if event.Type == "assistant" {
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					lastText = block.Text
				}
			}
		}

		if event.Type == "result" {
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
		return &BrainResponse{Reasoning: "(no response from brain)"}, nil
	}

	Debugf("[brain] response received after %s (%d chars)", elapsed, len(lastText))

	resp, err := parseBrainJSON(lastText)
	if err != nil {
		Debugf("[brain] JSON parse failed, using raw text: %v", err)
		return &BrainResponse{Reasoning: lastText}, nil
	}
	Debugf("[brain] %d nudges, %d resolved, reasoning: %s", len(resp.Nudges), len(resp.ResolvedFindings), truncate(resp.Reasoning, 100))
	return resp, nil
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
func RestartBrain(cfg Config, projectDir, jsonlPath, findingsJSON string, delay time.Duration) (*Brain, error) {
	time.Sleep(delay)
	return StartBrain(cfg, projectDir, jsonlPath, findingsJSON)
}
