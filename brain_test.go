package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"reflect"
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

func TestParseBrainJSONExtractsFencedJSON(t *testing.T) {
	text := "Sure — here is the review.\n```json\n{\"info\":[\"watch the cache\"],\"nudges\":[{\"severity\":\"warn\",\"message\":\"you need a test\"}],\"resolved_findings\":[]}\n```"
	resp, err := parseBrainJSON(text)
	if err != nil {
		t.Fatalf("parseBrainJSON() error = %v", err)
	}
	if got := resp.InfoLines(); !reflect.DeepEqual(got, []string{"watch the cache"}) {
		t.Fatalf("InfoLines() = %#v, want fenced JSON info", got)
	}
	if len(resp.Nudges) != 1 || resp.Nudges[0].Message != "you need a test" {
		t.Fatalf("Nudges = %#v, want recovered fenced nudge", resp.Nudges)
	}
}

func TestParseBrainJSONSkipsUnrelatedObjectsAndFindsBrainPayload(t *testing.T) {
	text := `prefix {"foo":"bar"} middle {"info":[],"nudges":[{"severity":"error","message":"hey, this is the real issue"}],"resolved_findings":["f-1"]} suffix`
	resp, err := parseBrainJSON(text)
	if err != nil {
		t.Fatalf("parseBrainJSON() error = %v", err)
	}
	if len(resp.Nudges) != 1 || resp.Nudges[0].Message != "hey, this is the real issue" {
		t.Fatalf("Nudges = %#v, want recovered brain payload", resp.Nudges)
	}
	if !reflect.DeepEqual(resp.ResolvedFindings, []string{"f-1"}) {
		t.Fatalf("ResolvedFindings = %#v, want [f-1]", resp.ResolvedFindings)
	}
}

func TestBalancedJSONObjectCandidatesHandlesBracesInStrings(t *testing.T) {
	text := `noise {"info":["brace { inside } string"],"nudges":[{"severity":"warn","message":"you should keep {this} intact"}],"resolved_findings":[]} tail`
	candidates := balancedJSONObjectCandidates(text)
	if len(candidates) == 0 {
		t.Fatal("balancedJSONObjectCandidates() returned no candidates")
	}
	resp, err := parseBrainJSON(text)
	if err != nil {
		t.Fatalf("parseBrainJSON() error = %v", err)
	}
	if got := resp.InfoLines(); !reflect.DeepEqual(got, []string{"brace { inside } string"}) {
		t.Fatalf("InfoLines() = %#v, want string with braces preserved", got)
	}
	if len(resp.Nudges) != 1 || resp.Nudges[0].Message != "you should keep {this} intact" {
		t.Fatalf("Nudges = %#v, want brace-containing message", resp.Nudges)
	}
}

func TestRecoverLooseBrainResponseSalvagesInvalidJSONNudge(t *testing.T) {
	text := `prefix {"info":["cache drift"],"nudges":[{"severity":"warn","message":"you need a test","verified":"route missing",}],"resolved_findings":["f-1"]} suffix`
	resp := recoverLooseBrainResponse(text)
	if got := resp.InfoLines(); !reflect.DeepEqual(got, []string{"cache drift"}) {
		t.Fatalf("InfoLines() = %#v, want recovered info", got)
	}
	if !reflect.DeepEqual(resp.ResolvedFindings, []string{"f-1"}) {
		t.Fatalf("ResolvedFindings = %#v, want [f-1]", resp.ResolvedFindings)
	}
	if len(resp.Nudges) != 1 {
		t.Fatalf("Nudges = %#v, want exactly one recovered nudge", resp.Nudges)
	}
	if resp.Nudges[0].Message != "you need a test" || resp.Nudges[0].Verified != "route missing" {
		t.Fatalf("Recovered nudge = %#v, want message+verified preserved", resp.Nudges[0])
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

func TestReplayBrainReturnsScriptedResponsesDeterministically(t *testing.T) {
	projectDir := t.TempDir()
	scriptPath := filepath.Join(projectDir, "brain-replay.json")
	if err := os.WriteFile(scriptPath, []byte(`[
		{"match":"working tree changed","response":{"info":["first turn"],"nudges":[{"severity":"warn","message":"first nudge"}],"resolved_findings":[]}},
		{"match":"idle for 60s","response":{"info":["second turn"],"nudges":[],"resolved_findings":["f-1"]}}
	]`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	brain, err := StartBrain(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: scriptPath,
		BrainEffort:     "high",
	}, projectDir, filepath.Join(projectDir, "session.jsonl"), BrainStats{})
	if err != nil {
		t.Fatalf("StartBrain() error = %v", err)
	}

	first, err := brain.Notify("working tree changed", "[]")
	if err != nil {
		t.Fatalf("first Notify() error = %v", err)
	}
	if got := first.InfoLines(); !reflect.DeepEqual(got, []string{"first turn"}) {
		t.Fatalf("first InfoLines() = %#v, want scripted first turn", got)
	}
	if len(first.Nudges) != 1 || first.Nudges[0].Message != "first nudge" {
		t.Fatalf("first Nudges = %#v, want scripted first nudge", first.Nudges)
	}

	second, err := brain.Notify("Codex has been idle for 60s — good time for a session review", "[]")
	if err != nil {
		t.Fatalf("second Notify() error = %v", err)
	}
	if got := second.InfoLines(); !reflect.DeepEqual(got, []string{"second turn"}) {
		t.Fatalf("second InfoLines() = %#v, want scripted second turn", got)
	}
	if !reflect.DeepEqual(second.ResolvedFindings, []string{"f-1"}) {
		t.Fatalf("second ResolvedFindings = %#v, want [f-1]", second.ResolvedFindings)
	}

	stats := brain.Stats()
	if stats.Provider != ProviderReplay || stats.TurnCount != 2 {
		t.Fatalf("replay stats = %#v, want provider replay with 2 turns", stats)
	}
}

func TestReplayBrainFailsLoudlyOnNotificationMismatch(t *testing.T) {
	projectDir := t.TempDir()
	scriptPath := filepath.Join(projectDir, "brain-replay.json")
	if err := os.WriteFile(scriptPath, []byte(`[
		{"match":"idle for 60s","response":{"nudges":[],"resolved_findings":[]}}
	]`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	brain, err := StartBrain(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: scriptPath,
		BrainEffort:     "high",
	}, projectDir, filepath.Join(projectDir, "session.jsonl"), BrainStats{})
	if err != nil {
		t.Fatalf("StartBrain() error = %v", err)
	}

	if _, err := brain.Notify("working tree changed", "[]"); err == nil {
		t.Fatal("expected replay Notify() to fail on unmatched notification")
	}
}

func TestReplayBrainUsesFallbackOnlyAfterTryingLaterSpecificMatches(t *testing.T) {
	projectDir := t.TempDir()
	scriptPath := filepath.Join(projectDir, "brain-replay.json")
	if err := os.WriteFile(scriptPath, []byte(`[
		{"match":"first","response":{"nudges":[{"severity":"warn","message":"n1"}],"resolved_findings":[]}},
		{"match":"later","response":{"nudges":[{"severity":"warn","message":"n2"}],"resolved_findings":[]}},
		{"response":{"info":["fallback"],"nudges":[],"resolved_findings":[]}}
	]`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	brain, err := StartBrain(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: scriptPath,
		BrainEffort:     "high",
	}, projectDir, filepath.Join(projectDir, "session.jsonl"), BrainStats{})
	if err != nil {
		t.Fatalf("StartBrain() error = %v", err)
	}

	first, err := brain.Notify("first notification", "[]")
	if err != nil {
		t.Fatalf("first Notify() error = %v", err)
	}
	if len(first.Nudges) != 1 || first.Nudges[0].Message != "n1" {
		t.Fatalf("unexpected first replay response: %#v", first)
	}

	second, err := brain.Notify("later notification", "[]")
	if err != nil {
		t.Fatalf("second Notify() error = %v", err)
	}
	if len(second.Nudges) != 1 || second.Nudges[0].Message != "n2" {
		t.Fatalf("unexpected second replay response: %#v", second)
	}

	third, err := brain.Notify("unmatched notification", "[]")
	if err != nil {
		t.Fatalf("third Notify() error = %v", err)
	}
	if got := third.InfoLines(); !reflect.DeepEqual(got, []string{"fallback"}) {
		t.Fatalf("fallback InfoLines() = %#v, want [fallback]", got)
	}
}
