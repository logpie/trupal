package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// BrainResponse is the parsed JSON response from the brain.
type BrainResponse struct {
	Reasoning        string       `json:"reasoning"`
	Info             []string     `json:"info,omitempty"`
	Observations     []string     `json:"observations,omitempty"`
	Nudges           []BrainNudge `json:"nudges"`
	ResolvedFindings []string     `json:"resolved_findings"`
	Usage            BrainUsage   `json:"usage"`
	TotalCostUSD     float64      `json:"total_cost_usd"`
}

func (r BrainResponse) InfoLines() []string {
	if len(r.Info) > 0 {
		return r.Info
	}
	return r.Observations
}

// BrainNudge is a single nudge from the brain.
type BrainNudge struct {
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Why       string `json:"why,omitempty"`
	Reasoning string `json:"reasoning,omitempty"` // backward-compatible fallback
	Claim     string `json:"claim,omitempty"`
	Verified  string `json:"verified,omitempty"`
	Impact    string `json:"impact,omitempty"`
	Tell      string `json:"tell,omitempty"`
}

// BrainUsage is per-turn token usage reported by the brain subprocess.
type BrainUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u BrainUsage) add(other BrainUsage) BrainUsage {
	return BrainUsage{
		InputTokens:              u.InputTokens + other.InputTokens,
		OutputTokens:             u.OutputTokens + other.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + other.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + other.CacheReadInputTokens,
	}
}

// BrainStats is the cumulative token and cost usage for the current trupal session.
type BrainStats struct {
	Provider                 string
	CostKnown                bool
	TurnCount                int
	LastDuration             time.Duration
	LastEffort               string
	LastTriggerSummary       string
	TotalInputTokens         int
	TotalOutputTokens        int
	TotalCacheCreationTokens int
	TotalCacheReadTokens     int
	TotalCostUSD             float64
	LastUsage                BrainUsage
	LastCostUSD              float64
}

func (s *BrainStats) addTurn(usage BrainUsage, costUSD float64) {
	s.TurnCount++
	accounted := normalizeUsageForAccounting(s.Provider, usage, s.LastUsage)
	s.TotalInputTokens += accounted.InputTokens
	s.TotalOutputTokens += accounted.OutputTokens
	s.TotalCacheCreationTokens += accounted.CacheCreationInputTokens
	s.TotalCacheReadTokens += accounted.CacheReadInputTokens
	s.TotalCostUSD += costUSD
	s.LastUsage = usage
	s.LastCostUSD = costUSD
}

func (s *BrainStats) addCodexTurn(rawUsage, accountedUsage BrainUsage, costUSD float64) {
	s.TurnCount++
	s.TotalInputTokens += accountedUsage.InputTokens
	s.TotalOutputTokens += accountedUsage.OutputTokens
	s.TotalCacheCreationTokens += accountedUsage.CacheCreationInputTokens
	s.TotalCacheReadTokens += accountedUsage.CacheReadInputTokens
	s.TotalCostUSD += costUSD
	s.LastUsage = rawUsage
	s.LastCostUSD = costUSD
}

func normalizeUsageForAccounting(provider string, usage, previous BrainUsage) BrainUsage {
	if normalizeProvider(provider, ProviderClaude) != ProviderCodex {
		return usage
	}
	return BrainUsage{
		InputTokens:              codexUsageDelta(usage.InputTokens, previous.InputTokens),
		OutputTokens:             codexUsageDelta(usage.OutputTokens, previous.OutputTokens),
		CacheCreationInputTokens: codexUsageDelta(usage.CacheCreationInputTokens, previous.CacheCreationInputTokens),
		CacheReadInputTokens:     codexUsageDelta(usage.CacheReadInputTokens, previous.CacheReadInputTokens),
	}
}

func codexUsageDelta(current, previous int) int {
	if current <= 0 {
		return 0
	}
	if previous > 0 && current >= previous {
		return current - previous
	}
	return current
}

func (s *BrainStats) noteTurn(duration time.Duration, effort string) {
	s.LastDuration = duration
	s.LastEffort = strings.TrimSpace(effort)
}

// PromptTokens returns the total prompt-side token volume, including cache hits
// and cache writes, for computing cache effectiveness.
func (s BrainStats) PromptTokens() int {
	if normalizeProvider(s.Provider, ProviderClaude) == ProviderCodex {
		return s.TotalInputTokens
	}
	return s.TotalInputTokens + s.TotalCacheReadTokens + s.TotalCacheCreationTokens
}

// CacheHitRate returns the percent of prompt-side tokens served from cache.
func (s BrainStats) CacheHitRate() int {
	total := s.PromptTokens()
	if total == 0 {
		return 0
	}
	return (s.TotalCacheReadTokens*100 + total/2) / total
}

func (s BrainStats) UncachedPromptTokens() int {
	if normalizeProvider(s.Provider, ProviderClaude) == ProviderCodex {
		uncached := s.TotalInputTokens - s.TotalCacheReadTokens
		if uncached < 0 {
			return 0
		}
		return uncached
	}
	return s.TotalInputTokens + s.TotalCacheCreationTokens
}

// Brain manages the CC subprocess.
type Brain struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	scanner        *bufio.Scanner
	stderr         *bytes.Buffer
	stopped        atomic.Bool
	cfg            Config
	projectDir     string
	jsonlPath      string
	codexWorkDir   string
	codexThreadID  string
	accessibleDirs []string
	statsMu        sync.Mutex
	stats          BrainStats
}

// brainSystemPrompt returns the static system prompt for the brain.
func brainSystemPrompt(projectDir, jsonlPath, sessionProvider string) string {
	agentName := sessionProviderDisplayName(sessionProvider)
	agentLabel := sessionProviderLabel(sessionProvider)
	return fmt.Sprintf(`You are TruPal, a continuous verification agent watching a %s session.

%s session JSONL: %s
Project directory: %s

IMPORTANT — you are a CONTINUOUS monitor receiving repeated notifications. Be INCREMENTAL:
- Each notification includes ACTIVE FINDINGS you already flagged. Do NOT re-flag them.
- Only generate nudges for NEW issues you haven't flagged before.
- If active findings are still unresolved, that's fine — don't repeat them.
- Every notification is a request to investigate. There is NO fast path.
- Start with the notification context: recent session activity, edited files, git diff summary, and build status.
- If BUILD OUTPUT is present, use it directly. Do not ask the agent to paste the error again unless the notification truly omitted it.
- ALWAYS read every changed file named in the notification — the FULL file, not just the diff.
- When reading a file, check for SYSTEMIC issues across the entire file, not just the latest changes:
  race conditions (global maps/slices without mutex), error swallowing, panic misuse,
  cache invalidation gaps, resource leaks, middleware ordering bugs.
- For server, handler, or router files, explicitly audit:
  unsupported methods returning 405, auth coverage on every route, manual path parsing edge cases,
  shared mutable globals under concurrency, cache invalidation on update/delete,
  rate-limit state cleanup, and ignored json encode/decode errors.
- Use the RECENT SESSION ACTIVITY included in the notification for claim/action checks.
- Do NOT reread the full JSONL file on every notification. Only open the JSONL directly if the notification excerpt is missing critical evidence.
- Silence means bugs escape. Flag anything you find — even pre-existing bugs that the watched agent didn't introduce.
  %s is responsible for the code they're working in, including what was already there.

You are a nudge engine. You talk like a senior dev sitting next to %s.
Always start with "you" or "hey" — address the watched agent directly. Never write like a linter.
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
- CLAIM-ACTION GAPS: %s said it did X but the session log shows no corresponding tool call
- ERROR HANDLING: swallowed errors, dropped return values, returns that hide failures
- RACE CONDITIONS: unsynchronized global state, shared maps/slices without locks, concurrent access patterns
- PANIC MISUSE: panic for expected errors, panics that skip cleanup, recover misuse
- CACHE INVALIDATION: stale cached values, missing eviction/update paths, cache/state divergence
- RESOURCE LEAKS: goroutines, timers, file handles, subprocesses, channels, memory growth
- PROCESS QUALITY: edit without reading first? no tests after changes?
- TRAJECTORY: same file edited repeatedly without progress
- HTTP/API SAFETY: missing 405 handling, route parsing that accepts invalid paths, middleware/auth gaps, stale caches after update/delete

CRITICAL RULE: If you find a code issue (bug, race condition, swallowed error, missing validation),
ALWAYS generate a nudge for it. Do NOT suppress nudges because you think the agent is "testing" or the bug
is "intentional." Your job is to flag code problems. %s decides what to do with them.
Another way to say it: silence means bugs escape, so investigate first and err on the side of speaking up.

Respond with JSON only:
{
  "info": ["what the human operator should notice — facts, patterns, context"],
  "nudges": [{
    "severity": "warn|error",
    "message": "short operator-facing nudge",
    "claim": "what the coding agent implied, promised, or said (omit if none)",
    "verified": "what you verified in code or session evidence that conflicts with the claim",
    "impact": "short plain-language explanation for the human operator",
    "tell": "one short sentence the human could say back to the coding agent"
  }],
  "resolved_findings": ["<finding_id>"]
}

Info lines are for the human operator.
Only emit one when it meaningfully improves the operator's mental model of:
- verification scope or trust changed
- agent intent/drift/stall changed
- review confidence changed
- a state transition matters even if no new nudge is needed
NOT for: routine activity ("the agent read a file"), internal state ("JSONL flushed"), timestamps, ordinary token stats.
Max 2 info lines per response. If nothing notable, return empty.

Rules:
- Info lines: 1 sentence each. Only notable patterns or risks.
- Nudges: each one should be worth actually sending to the coding agent right now.
- "message" is the canonical steering instruction. Write it as a short sentence the human could send verbatim.
- If it is not worth steering the coding agent, do not emit a nudge.
- "tell": optional richer outbound variant only when it adds a small but useful clarification beyond "message". If it would just restate "message", omit it.
- "claim": only when the coding agent actually said or strongly implied something relevant.
- "verified": what you checked in reality that conflicts with the claim or expected outcome.
- "impact": 1 short plain-language sentence for the human operator. Not raw inner monologue.
- Focus on real correctness and verification risks, not style nits.
- Before returning empty, explicitly check for concurrency, cache invalidation, auth coverage, route parsing, method handling, and dropped JSON errors in the changed files.
- After you investigate, if nothing important is wrong, return empty nudges.`, agentName, agentName, jsonlPath, projectDir, agentLabel, agentLabel, agentLabel, agentLabel)
}

func brainCommand(provider string) (string, error) {
	switch provider {
	case "", ProviderClaude:
		return providerExecutable(ProviderClaude)
	case ProviderCodex:
		return providerExecutable(ProviderCodex)
	default:
		return "", fmt.Errorf("unsupported brain_provider %q (supported: claude, codex)", provider)
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
	accessibleDirs := brainAccessibleDirs(projectDir, extraDirs...)
	if normalizeProvider(cfg.BrainProvider, ProviderClaude) == ProviderCodex {
		if _, err := brainCommand(cfg.BrainProvider); err != nil {
			return nil, err
		}
		codexWorkDir, err := ensureCodexBrainWorkDir(projectDir)
		if err != nil {
			return nil, err
		}
		return &Brain{
			cfg:            cfg,
			projectDir:     projectDir,
			jsonlPath:      jsonlPath,
			codexWorkDir:   codexWorkDir,
			accessibleDirs: accessibleDirs,
			stats:          initialStats,
		}, nil
	}

	prompt := brainSystemPrompt(projectDir, jsonlPath, cfg.SessionProvider)
	command, err := brainCommand(cfg.BrainProvider)
	if err != nil {
		return nil, err
	}

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
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

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
		cmd:            cmd,
		stdin:          stdin,
		stdout:         stdout,
		scanner:        scanner,
		stderr:         stderr,
		cfg:            cfg,
		projectDir:     projectDir,
		jsonlPath:      jsonlPath,
		accessibleDirs: accessibleDirs,
		stats:          initialStats,
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
	if normalizeProvider(b.cfg.BrainProvider, ProviderClaude) == ProviderCodex {
		return b.notifyCodex(reason, findingsJSON)
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
	var textBuilder strings.Builder
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
					textBuilder.WriteString(block.Text)
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
		return nil, fmt.Errorf("brain read error: %w%s", err, b.stderrSuffix())
	}

	text := textBuilder.String()
	elapsed := time.Since(start)
	if text == "" {
		Debugf("[brain] no response after %s", elapsed)
		// If scanner hit EOF without a "result" event, the subprocess likely died.
		return nil, fmt.Errorf("brain exited unexpectedly (no response after %s)%s", elapsed, b.stderrSuffix())
	}
	if !sawResult {
		Debugf("[brain] missing result event after %s", elapsed)
		return nil, fmt.Errorf("brain exited unexpectedly (missing result event after %s)%s", elapsed, b.stderrSuffix())
	}

	Debugf("[brain] response received after %s (%d chars)", elapsed, len(text))

	resp, err := parseBrainJSON(text)
	if err != nil {
		Debugf("[brain] JSON parse failed, using raw text: %v", err)
		resp = &BrainResponse{Reasoning: text}
	}
	resp.Usage = usage
	resp.TotalCostUSD = totalCostUSD
	b.statsMu.Lock()
	accountedUsage := normalizeUsageForAccounting(b.cfg.BrainProvider, usage, b.stats.LastUsage)
	b.stats.Provider = b.cfg.BrainProvider
	b.stats.CostKnown = true
	b.stats.noteTurn(elapsed, effectiveBrainEffort(b.cfg, reason))
	b.stats.addTurn(usage, totalCostUSD)
	b.statsMu.Unlock()
	Debugf("[brain] usage: in=%d out=%d cache_read=%d cache_create=%d cost=$%.4f",
		accountedUsage.InputTokens,
		accountedUsage.OutputTokens,
		accountedUsage.CacheReadInputTokens,
		accountedUsage.CacheCreationInputTokens,
		totalCostUSD,
	)
	Debugf("[brain] %d nudges, %d resolved, reasoning: %s", len(resp.Nudges), len(resp.ResolvedFindings), truncate(resp.Reasoning, 100))
	return resp, nil
}

func (b *Brain) stderrSuffix() string {
	if b == nil || b.stderr == nil {
		return ""
	}
	text := strings.TrimSpace(b.stderr.String())
	if text == "" {
		return ""
	}
	return ": " + truncate(text, 400)
}

func (b *Brain) notifyCodex(reason, findingsJSON string) (*BrainResponse, error) {
	message := brainNotifyMessage(reason, findingsJSON)
	prompt := codexPromptForTurn(b, message)
	effort := effectiveBrainEffort(b.cfg, reason)
	Debugf("[brain] codex exec notify: %s", truncate(message, 200))
	baseUsage := b.Stats().LastUsage
	text, rawUsage, threadID, elapsed, err := b.runCodexBrainExec(prompt, effort)
	if err != nil {
		return nil, err
	}
	if threadID != "" {
		b.codexThreadID = threadID
	}
	Debugf("[brain] codex response received after %s (%d chars)", elapsed, len(text))
	resp, parseErr := parseBrainJSON(text)
	accountedUsage := normalizeUsageForAccounting(b.cfg.BrainProvider, rawUsage, baseUsage)
	if parseErr != nil {
		Debugf("[brain] codex JSON parse failed, using raw text: %v", parseErr)
		Debugf("[brain] codex raw response preview: %s", truncate(strings.ReplaceAll(text, "\n", " "), 500))
		repairedResp, repairedRawUsage, repairedElapsed, repairErr := b.retryCodexBrainJSONRepair()
		if repairErr != nil {
			Debugf("[brain] codex JSON repair failed: %v", repairErr)
			resp = recoverLooseBrainResponse(text)
			if len(resp.Nudges) == 0 && len(resp.InfoLines()) == 0 && len(resp.ResolvedFindings) == 0 {
				resp = &BrainResponse{Reasoning: text}
			}
		} else {
			resp = repairedResp
			accountedUsage = accountedUsage.add(normalizeUsageForAccounting(b.cfg.BrainProvider, repairedRawUsage, rawUsage))
			rawUsage = repairedRawUsage
			elapsed += repairedElapsed
			Debugf("[brain] codex JSON repair succeeded after %s", repairedElapsed)
		}
	}
	resp.Usage = accountedUsage

	b.statsMu.Lock()
	b.stats.Provider = b.cfg.BrainProvider
	b.stats.CostKnown = false
	b.stats.noteTurn(elapsed, effort)
	b.stats.addCodexTurn(rawUsage, accountedUsage, 0)
	b.statsMu.Unlock()
	Debugf("[brain] usage: in=%d out=%d cache_read=%d cache_create=%d cost=$0.0000",
		accountedUsage.InputTokens,
		accountedUsage.OutputTokens,
		accountedUsage.CacheReadInputTokens,
		accountedUsage.CacheCreationInputTokens,
	)
	resp.Usage = accountedUsage
	return resp, nil
}

func (b *Brain) runCodexBrainExec(prompt, effort string) (text string, usage BrainUsage, threadID string, elapsed time.Duration, err error) {
	args := buildCodexBrainArgs(b, prompt, effort)
	start := time.Now()
	command, err := brainCommand(ProviderCodex)
	if err != nil {
		return "", BrainUsage{}, "", 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), brainNotifyTimeout(b.cfg.BrainEffort))
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.CombinedOutput()
	elapsed = time.Since(start)
	if ctx.Err() == context.DeadlineExceeded {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return "", BrainUsage{}, "", elapsed, fmt.Errorf("codex exec brain timed out after %s", elapsed)
	}
	if err != nil {
		Debugf("[brain] codex exec failed after %s: %v", elapsed, err)
		return "", BrainUsage{}, "", elapsed, fmt.Errorf("codex exec brain: %w: %s", err, strings.TrimSpace(string(out)))
	}
	text, usage, threadID, err = parseCodexExecOutput(out)
	if err != nil {
		return "", BrainUsage{}, "", elapsed, err
	}
	if text == "" {
		return "", BrainUsage{}, "", elapsed, fmt.Errorf("codex brain returned empty output after %s", elapsed)
	}
	return text, usage, threadID, elapsed, nil
}

func (b *Brain) retryCodexBrainJSONRepair() (*BrainResponse, BrainUsage, time.Duration, error) {
	repairPrompt := "Your previous answer was not valid JSON. Re-emit the same analysis as exactly one JSON object matching the existing TruPal schema. Return JSON only: no prose, no markdown, no code fences."
	text, usage, threadID, elapsed, err := b.runCodexBrainExec(repairPrompt, "low")
	if err != nil {
		return nil, BrainUsage{}, elapsed, err
	}
	if threadID != "" {
		b.codexThreadID = threadID
	}
	resp, parseErr := parseBrainJSON(text)
	if parseErr != nil {
		Debugf("[brain] codex JSON repair preview: %s", truncate(strings.ReplaceAll(text, "\n", " "), 500))
		return nil, BrainUsage{}, elapsed, parseErr
	}
	return resp, usage, elapsed, nil
}

func codexPromptForTurn(b *Brain, message string) string {
	notification := "NOTIFICATION:\n" + message
	if strings.TrimSpace(b.codexThreadID) != "" {
		return notification
	}
	return brainSystemPrompt(b.projectDir, b.jsonlPath, b.cfg.SessionProvider) + "\n\n" + notification
}

func buildCodexBrainArgs(b *Brain, prompt, effort string) []string {
	if strings.TrimSpace(b.codexThreadID) != "" {
		args := []string{
			"exec", "resume",
			"--json",
			"-c", fmt.Sprintf("model_reasoning_effort=%q", effort),
			"--skip-git-repo-check",
		}
		if strings.TrimSpace(b.cfg.BrainModel) != "" {
			args = append(args, "--model", b.cfg.BrainModel)
		}
		args = append(args, b.codexThreadID, prompt)
		return args
	}

	args := []string{
		"exec",
		"--json",
		"-c", fmt.Sprintf("model_reasoning_effort=%q", effort),
		"--skip-git-repo-check",
		"-C", b.codexWorkDir,
		"-s", "read-only",
	}
	if strings.TrimSpace(b.cfg.BrainModel) != "" {
		args = append(args, "--model", b.cfg.BrainModel)
	}
	for _, dir := range b.accessibleDirs {
		if filepath.Clean(dir) == filepath.Clean(b.projectDir) {
			continue
		}
		args = append(args, "--add-dir", dir)
	}
	args = append(args, prompt)
	return args
}

func ensureCodexBrainWorkDir(projectDir string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(filepath.Clean(projectDir), string(os.PathSeparator), "_")
	dir := filepath.Join(homeDir, ".trupal", "codex-brain", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func effectiveBrainEffort(cfg Config, reason string) string {
	if normalizeProvider(cfg.BrainProvider, ProviderClaude) != ProviderCodex {
		return strings.ToLower(strings.TrimSpace(cfg.BrainEffort))
	}
	return codexEffortForReason(cfg.BrainEffort, reason)
}

func codexEffortForReason(configEffort, reason string) string {
	maxEffort := normalizeCodexEffort(configEffort)
	base := "low"
	switch {
	case strings.Contains(reason, "idle for 60s"), strings.Contains(reason, "build status changed"):
		base = "high"
	case strings.Contains(reason, "trajectory signal"), strings.Contains(reason, "working tree changed"):
		base = "medium"
	case strings.Contains(reason, "session switched"):
		base = "low"
	}
	return minEffort(base, maxEffort)
}

func normalizeCodexEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high", "max":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return "high"
	}
}

func minEffort(base, max string) string {
	order := map[string]int{"low": 0, "medium": 1, "high": 2, "max": 3}
	if order[base] > order[max] {
		return max
	}
	return base
}

func brainNotifyTimeout(effort string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low":
		return 45 * time.Second
	case "medium":
		return 75 * time.Second
	case "max":
		return 150 * time.Second
	default:
		return 105 * time.Second
	}
}

func parseCodexExecUsage(out []byte, usage *BrainUsage) {
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Type  string `json:"type"`
			Usage struct {
				InputTokens       int `json:"input_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
				OutputTokens      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type != "turn.completed" {
			continue
		}
		usage.InputTokens = event.Usage.InputTokens
		usage.CacheReadInputTokens = event.Usage.CachedInputTokens
		usage.OutputTokens = event.Usage.OutputTokens
	}
}

func parseCodexExecOutput(out []byte) (text string, usage BrainUsage, threadID string, err error) {
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	var textBuilder strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				InputTokens       int `json:"input_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
				OutputTokens      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		switch event.Type {
		case "thread.started":
			if event.ThreadID != "" {
				threadID = event.ThreadID
			}
		case "item.completed":
			if event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) != "" {
				textBuilder.WriteString(event.Item.Text)
			}
		case "turn.completed":
			usage.InputTokens = event.Usage.InputTokens
			usage.CacheReadInputTokens = event.Usage.CachedInputTokens
			usage.OutputTokens = event.Usage.OutputTokens
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", BrainUsage{}, "", scanErr
	}
	text = strings.TrimSpace(textBuilder.String())
	return text, usage, threadID, nil
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
	for _, candidate := range brainJSONCandidates(text) {
		if !looksLikeBrainJSON(candidate) {
			continue
		}
		if err := json.Unmarshal([]byte(candidate), &resp); err == nil {
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("no valid JSON in brain response")
}

func looksLikeBrainJSON(candidate string) bool {
	return strings.Contains(candidate, `"nudges"`) ||
		strings.Contains(candidate, `"info"`) ||
		strings.Contains(candidate, `"observations"`) ||
		strings.Contains(candidate, `"resolved_findings"`) ||
		strings.Contains(candidate, `"reasoning"`)
}

func brainJSONCandidates(text string) []string {
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}
	for _, block := range fencedJSONBlocks(text) {
		add(block)
	}
	for _, block := range balancedJSONObjectCandidates(text) {
		add(block)
	}
	return candidates
}

var (
	jsonStringFieldRE = regexp.MustCompile(`"(?P<field>severity|message|claim|verified|impact|tell|reasoning)"\s*:\s*"((?:\\.|[^"\\])*)"`)
	jsonInfoFieldRE   = regexp.MustCompile(`"(?:info|observations)"\s*:\s*\[((?:\s*"(?:\\.|[^"\\])*"\s*,?)*)\]`)
	jsonResolvedRE    = regexp.MustCompile(`"resolved_findings"\s*:\s*\[((?:\s*"(?:\\.|[^"\\])*"\s*,?)*)\]`)
	jsonQuotedRE      = regexp.MustCompile(`"((?:\\.|[^"\\])*)"`)
)

func recoverLooseBrainResponse(text string) *BrainResponse {
	resp := &BrainResponse{}
	for _, candidate := range brainJSONCandidates(text) {
		if len(resp.InfoLines()) == 0 {
			if info := recoverJSONArrayStrings(jsonInfoFieldRE, candidate); len(info) > 0 {
				resp.Info = info
			}
		}
		if len(resp.ResolvedFindings) == 0 {
			if resolved := recoverJSONArrayStrings(jsonResolvedRE, candidate); len(resolved) > 0 {
				resp.ResolvedFindings = resolved
			}
		}
		fields := jsonStringFieldRE.FindAllStringSubmatch(candidate, -1)
		if len(fields) == 0 {
			continue
		}
		var current BrainNudge
		var haveMessage bool
		for _, match := range fields {
			fieldName := match[1]
			value, err := strconv.Unquote(`"` + match[2] + `"`)
			if err != nil {
				value = match[2]
			}
			switch fieldName {
			case "severity":
				if haveMessage {
					resp.Nudges = append(resp.Nudges, current)
					current = BrainNudge{}
					haveMessage = false
				}
				current.Severity = value
			case "message":
				if haveMessage {
					resp.Nudges = append(resp.Nudges, current)
					current = BrainNudge{}
				}
				current.Message = value
				haveMessage = true
			case "claim":
				current.Claim = value
			case "verified":
				current.Verified = value
			case "impact":
				current.Impact = value
			case "tell":
				current.Tell = value
			case "reasoning":
				current.Reasoning = value
			}
		}
		if haveMessage {
			resp.Nudges = append(resp.Nudges, current)
		}
		if len(resp.Nudges) > 0 {
			break
		}
	}
	return resp
}

func recoverJSONArrayStrings(re *regexp.Regexp, text string) []string {
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return nil
	}
	items := jsonQuotedRE.FindAllStringSubmatch(match[1], -1)
	var out []string
	for _, item := range items {
		value, err := strconv.Unquote(`"` + item[1] + `"`)
		if err != nil {
			value = item[1]
		}
		out = append(out, value)
	}
	return out
}

func fencedJSONBlocks(text string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	var inFence bool
	var buf []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				out = append(out, strings.Join(buf, "\n"))
				inFence = false
				buf = nil
			} else {
				inFence = true
				buf = nil
			}
			continue
		}
		if inFence {
			buf = append(buf, line)
		}
	}
	return out
}

func balancedJSONObjectCandidates(text string) []string {
	var out []string
	runes := []rune(text)
	for start := 0; start < len(runes); start++ {
		if runes[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for end := start; end < len(runes); end++ {
			r := runes[end]
			if escaped {
				escaped = false
				continue
			}
			if inString {
				if r == '\\' {
					escaped = true
				} else if r == '"' {
					inString = false
				}
				continue
			}
			switch r {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, string(runes[start:end+1]))
					start = end
					end = len(runes)
				}
			}
		}
	}
	return out
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
