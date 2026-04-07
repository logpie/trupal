package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error { return nil }

func TestBrainSystemPromptIsStatic(t *testing.T) {
	prompt := brainSystemPrompt("/tmp/project", "/tmp/session.jsonl")

	if strings.Contains(prompt, "ACTIVE FINDINGS (unresolved):") {
		t.Fatalf("system prompt should not embed active findings: %q", prompt)
	}
	if !strings.Contains(prompt, "Each notification includes ACTIVE FINDINGS") {
		t.Fatalf("system prompt should still describe notification-based findings flow: %q", prompt)
	}
}

func TestNotifyParsesUsageAndTracksStats(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"{\"observations\":[],\"nudges\":[],\"resolved_findings\":[]}"}]}}`,
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
