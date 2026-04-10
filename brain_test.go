package main

import (
	"bufio"
	"io"
	"slices"
	"strings"
	"testing"
)

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error { return nil }

func TestBrainSystemPromptIsStatic(t *testing.T) {
	prompt := brainSystemPrompt("/tmp/project", "/tmp/session.jsonl", ProviderClaude)

	if strings.Contains(prompt, "ACTIVE FINDINGS (unresolved):") {
		t.Fatalf("system prompt should not embed active findings: %q", prompt)
	}
	if !strings.Contains(prompt, "Each notification includes ACTIVE FINDINGS") {
		t.Fatalf("system prompt should still describe notification-based findings flow: %q", prompt)
	}
}

func TestNotifyParsesUsageAndTracksStats(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"{\"info\":[],\"nudges\":[],\"resolved_findings\":[]}"}]}}`,
		`{"type":"result","usage":{"input_tokens":11,"output_tokens":7,"cache_creation_input_tokens":5,"cache_read_input_tokens":13},"total_cost_usd":0.0123}`,
	}, "\n")

	brain := &Brain{
		stdin:   nopWriteCloser{Writer: io.Discard},
		scanner: bufio.NewScanner(strings.NewReader(stream)),
	}

	resp, err := brain.Notify("check this", "[]")
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	if got := resp.Usage.InputTokens; got != 11 {
		t.Fatalf("input tokens = %d, want 11", got)
	}
	if got := resp.Usage.OutputTokens; got != 7 {
		t.Fatalf("output tokens = %d, want 7", got)
	}
	if got := resp.Usage.CacheCreationInputTokens; got != 5 {
		t.Fatalf("cache creation tokens = %d, want 5", got)
	}
	if got := resp.Usage.CacheReadInputTokens; got != 13 {
		t.Fatalf("cache read tokens = %d, want 13", got)
	}
	if got := resp.TotalCostUSD; got != 0.0123 {
		t.Fatalf("total cost = %f, want 0.0123", got)
	}

	stats := brain.Stats()
	if got := stats.TotalInputTokens; got != 11 {
		t.Fatalf("stats input tokens = %d, want 11", got)
	}
	if got := stats.TotalOutputTokens; got != 7 {
		t.Fatalf("stats output tokens = %d, want 7", got)
	}
	if got := stats.TotalCacheCreationTokens; got != 5 {
		t.Fatalf("stats cache creation tokens = %d, want 5", got)
	}
	if got := stats.TotalCacheReadTokens; got != 13 {
		t.Fatalf("stats cache read tokens = %d, want 13", got)
	}
	if got := stats.TotalCostUSD; got != 0.0123 {
		t.Fatalf("stats total cost = %f, want 0.0123", got)
	}
}

func TestNotifyConcatenatesAssistantTextBlocks(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"{\"info\":["}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"],\"nudges\":[],\"resolved_findings\":[]}"}]}}`,
		`{"type":"result","usage":{"input_tokens":1,"output_tokens":2},"total_cost_usd":0.001}`,
	}, "\n")

	brain := &Brain{
		stdin:   nopWriteCloser{Writer: io.Discard},
		scanner: bufio.NewScanner(strings.NewReader(stream)),
	}

	resp, err := brain.Notify("check this", "[]")
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if len(resp.Nudges) != 0 {
		t.Fatalf("nudges = %d, want 0", len(resp.Nudges))
	}
	if len(resp.InfoLines()) != 0 {
		t.Fatalf("info lines = %d, want 0", len(resp.InfoLines()))
	}
}

func TestNotifyErrorsWithoutResultEvent(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"text","text":"{\"info\":[],\"nudges\":[],\"resolved_findings\":[]}"}]}}`

	brain := &Brain{
		stdin:   nopWriteCloser{Writer: io.Discard},
		scanner: bufio.NewScanner(strings.NewReader(stream)),
	}

	if _, err := brain.Notify("check this", "[]"); err == nil {
		t.Fatal("expected Notify() to fail without a result event")
	}
}

func TestBrainResponseInfoLinesFallsBackToObservations(t *testing.T) {
	resp := BrainResponse{
		Observations: []string{"legacy"},
	}
	lines := resp.InfoLines()
	if len(lines) != 1 || lines[0] != "legacy" {
		t.Fatalf("InfoLines() fallback = %#v, want legacy observation", lines)
	}
}

func TestBrainStatsCacheSemanticsDifferByProvider(t *testing.T) {
	claude := BrainStats{
		Provider:                 ProviderClaude,
		TotalInputTokens:         3,
		TotalCacheCreationTokens: 120,
		TotalCacheReadTokens:     14833,
	}
	if got := claude.PromptTokens(); got != 14956 {
		t.Fatalf("claude PromptTokens = %d, want 14956", got)
	}
	if got := claude.UncachedPromptTokens(); got != 123 {
		t.Fatalf("claude UncachedPromptTokens = %d, want 123", got)
	}
	if got := claude.CacheHitRate(); got != 99 {
		t.Fatalf("claude CacheHitRate = %d, want 99", got)
	}

	codex := BrainStats{
		Provider:             ProviderCodex,
		TotalInputTokens:     36574,
		TotalCacheReadTokens: 33792,
	}
	if got := codex.PromptTokens(); got != 36574 {
		t.Fatalf("codex PromptTokens = %d, want 36574", got)
	}
	if got := codex.UncachedPromptTokens(); got != 2782 {
		t.Fatalf("codex UncachedPromptTokens = %d, want 2782", got)
	}
	if got := codex.CacheHitRate(); got != 92 {
		t.Fatalf("codex CacheHitRate = %d, want 92", got)
	}
}

func TestBrainStatsCodexAddTurnUsesDeltasForResumedThreadTotals(t *testing.T) {
	stats := BrainStats{Provider: ProviderCodex}
	stats.addTurn(BrainUsage{
		InputTokens:          1000,
		CacheReadInputTokens: 800,
		OutputTokens:         40,
	}, 0)
	stats.addTurn(BrainUsage{
		InputTokens:          1600,
		CacheReadInputTokens: 1200,
		OutputTokens:         70,
	}, 0)

	if stats.TotalInputTokens != 1600 {
		t.Fatalf("TotalInputTokens = %d, want 1600", stats.TotalInputTokens)
	}
	if stats.TotalCacheReadTokens != 1200 {
		t.Fatalf("TotalCacheReadTokens = %d, want 1200", stats.TotalCacheReadTokens)
	}
	if stats.TotalOutputTokens != 70 {
		t.Fatalf("TotalOutputTokens = %d, want 70", stats.TotalOutputTokens)
	}
	if got := stats.UncachedPromptTokens(); got != 400 {
		t.Fatalf("UncachedPromptTokens = %d, want 400", got)
	}
	if got := stats.CacheHitRate(); got != 75 {
		t.Fatalf("CacheHitRate = %d, want 75", got)
	}
}

func TestBuildCodexBrainArgsShapesExecAndResume(t *testing.T) {
	brain := &Brain{
		cfg: Config{
			BrainModel: "gpt-5.4",
		},
		projectDir:     "/tmp/project",
		codexWorkDir:   "/tmp/trupal-brain",
		accessibleDirs: []string{"/tmp/project", "/tmp/extra"},
	}

	execArgs := buildCodexBrainArgs(brain, "prompt here", "low")
	if execArgs[0] != "exec" || execArgs[1] != "--json" {
		t.Fatalf("unexpected exec args prefix: %v", execArgs[:2])
	}
	if slices.Contains(execArgs, "--ephemeral") || !slices.Contains(execArgs, "--add-dir") {
		t.Fatalf("expected exec args to include add-dir but not ephemeral flags: %v", execArgs)
	}
	if execArgs[len(execArgs)-1] != "prompt here" {
		t.Fatalf("expected prompt at end of exec args, got %q", execArgs[len(execArgs)-1])
	}
	if !slices.Contains(execArgs, "/tmp/trupal-brain") {
		t.Fatalf("expected exec args to use codex workdir, got %v", execArgs)
	}

	brain.codexThreadID = "thread-123"
	resumeArgs := buildCodexBrainArgs(brain, "prompt two", "medium")
	if len(resumeArgs) < 5 || resumeArgs[0] != "exec" || resumeArgs[1] != "resume" {
		t.Fatalf("unexpected resume args prefix: %v", resumeArgs)
	}
	if slices.Contains(resumeArgs, "--ephemeral") || slices.Contains(resumeArgs, "--add-dir") || slices.Contains(resumeArgs, "-C") {
		t.Fatalf("unexpected non-resume flags in resume args: %v", resumeArgs)
	}
	if resumeArgs[len(resumeArgs)-2] != "thread-123" || resumeArgs[len(resumeArgs)-1] != "prompt two" {
		t.Fatalf("expected resume args to end with thread id and prompt, got %v", resumeArgs[len(resumeArgs)-2:])
	}
}

func TestCodexPromptForTurnUsesSystemPromptOnlyOnFirstTurn(t *testing.T) {
	brain := &Brain{
		cfg:        Config{SessionProvider: ProviderCodex},
		projectDir: "/tmp/project",
		jsonlPath:  "/tmp/session.jsonl",
	}

	first := codexPromptForTurn(brain, "hello")
	if !strings.Contains(first, "You are TruPal") || !strings.Contains(first, "NOTIFICATION:\nhello") {
		t.Fatalf("unexpected first-turn prompt: %q", first)
	}

	brain.codexThreadID = "thread-123"
	resumed := codexPromptForTurn(brain, "hello")
	if strings.Contains(resumed, "You are TruPal") {
		t.Fatalf("resume prompt should not repeat system prompt: %q", resumed)
	}
	if resumed != "NOTIFICATION:\nhello" {
		t.Fatalf("unexpected resumed prompt: %q", resumed)
	}
}
