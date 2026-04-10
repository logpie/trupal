package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAgentUsageStatsAggregatesClaudeAssistantTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	data := "" +
		`{"timestamp":"2026-04-09T00:00:00Z","type":"assistant","message":{"role":"assistant","usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":20,"output_tokens":3}}}` + "\n" +
		`{"timestamp":"2026-04-09T00:00:01Z","type":"assistant","message":{"role":"assistant","usage":{"input_tokens":4,"cache_creation_input_tokens":1,"cache_read_input_tokens":8,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	stats := ReadAgentUsageStats(path, ProviderClaude)
	if stats.TotalInputTokens != 20 {
		t.Fatalf("TotalInputTokens = %d, want 20", stats.TotalInputTokens)
	}
	if stats.TotalCachedTokens != 28 {
		t.Fatalf("TotalCachedTokens = %d, want 28", stats.TotalCachedTokens)
	}
	if stats.TotalOutputTokens != 5 {
		t.Fatalf("TotalOutputTokens = %d, want 5", stats.TotalOutputTokens)
	}
	if stats.LastInputTokens != 5 {
		t.Fatalf("LastInputTokens = %d, want 5", stats.LastInputTokens)
	}
	if stats.LastCachedTokens != 8 {
		t.Fatalf("LastCachedTokens = %d, want 8", stats.LastCachedTokens)
	}
}

func TestAgentUsageStatsCodexSemanticsTreatCachedAsSubsetOfInput(t *testing.T) {
	stats := AgentUsageStats{
		Provider:          ProviderCodex,
		TotalInputTokens:  178922,
		TotalCachedTokens: 171904,
		TotalOutputTokens: 4431,
		LastInputTokens:   32340,
		LastCachedTokens:  26624,
		LastOutputTokens:  348,
	}
	if got := stats.PromptTokens(); got != 178922 {
		t.Fatalf("PromptTokens = %d, want 178922", got)
	}
	if got := stats.UncachedPromptTokens(); got != 7018 {
		t.Fatalf("UncachedPromptTokens = %d, want 7018", got)
	}
	if got := stats.CacheHitRate(); got != 96 {
		t.Fatalf("CacheHitRate = %d, want 96", got)
	}
	if got := stats.LastUncachedPromptTokens(); got != 5716 {
		t.Fatalf("LastUncachedPromptTokens = %d, want 5716", got)
	}
	if got := stats.LastCacheHitRate(); got != 82 {
		t.Fatalf("LastCacheHitRate = %d, want 82", got)
	}
}
