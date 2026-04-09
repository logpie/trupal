package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

type AgentUsageStats struct {
	Provider          string
	UpdatedAt         time.Time
	TotalInputTokens  int
	TotalCachedTokens int
	TotalOutputTokens int
	LastInputTokens   int
	LastCachedTokens  int
	LastOutputTokens  int
}

func (s AgentUsageStats) Available() bool {
	return s.TotalInputTokens > 0 || s.TotalCachedTokens > 0 || s.TotalOutputTokens > 0 ||
		s.LastInputTokens > 0 || s.LastCachedTokens > 0 || s.LastOutputTokens > 0
}

func (s AgentUsageStats) PromptTokens() int {
	return s.TotalInputTokens + s.TotalCachedTokens
}

func (s AgentUsageStats) LastPromptTokens() int {
	return s.LastInputTokens + s.LastCachedTokens
}

func (s AgentUsageStats) CacheHitRate() int {
	total := s.PromptTokens()
	if total <= 0 {
		return 0
	}
	return (s.TotalCachedTokens*100 + total/2) / total
}

func (s AgentUsageStats) LastCacheHitRate() int {
	total := s.LastPromptTokens()
	if total <= 0 {
		return 0
	}
	return (s.LastCachedTokens*100 + total/2) / total
}

func ReadAgentUsageStats(path, provider string) AgentUsageStats {
	stats := AgentUsageStats{Provider: normalizeProvider(provider, ProviderClaude)}
	switch stats.Provider {
	case ProviderCodex:
		lines := readTailJSONLLines(path, 256*1024)
		parseCodexAgentUsage(lines, &stats)
	default:
		parseClaudeAgentUsage(path, &stats)
	}
	return stats
}

func ReadSessionModel(path, provider string) string {
	lines := readTailJSONLLines(path, 256*1024)
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return readCodexSessionModel(lines)
	default:
		return readClaudeSessionModel(lines)
	}
}

func readTailJSONLLines(path string, maxBytes int64) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	if _, err := f.Seek(start, 0); err != nil {
		return nil
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 && start > 0 {
		lines = lines[1:]
	}
	return lines
}

func parseCodexAgentUsage(lines []string, stats *AgentUsageStats) {
	type codexUsageCounts struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	}
	type codexTokenInfo struct {
		TotalTokenUsage codexUsageCounts `json:"total_token_usage"`
		LastTokenUsage  codexUsageCounts `json:"last_token_usage"`
	}
	type codexPayload struct {
		Type string         `json:"type"`
		Info codexTokenInfo `json:"info"`
	}
	type codexEntry struct {
		Timestamp string       `json:"timestamp"`
		Type      string       `json:"type"`
		Payload   codexPayload `json:"payload"`
	}

	for i := len(lines) - 1; i >= 0; i-- {
		var entry codexEntry
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			continue
		}
		if entry.Type != "event_msg" || entry.Payload.Type != "token_count" {
			continue
		}
		stats.TotalInputTokens = entry.Payload.Info.TotalTokenUsage.InputTokens
		stats.TotalCachedTokens = entry.Payload.Info.TotalTokenUsage.CachedInputTokens
		stats.TotalOutputTokens = entry.Payload.Info.TotalTokenUsage.OutputTokens
		stats.LastInputTokens = entry.Payload.Info.LastTokenUsage.InputTokens
		stats.LastCachedTokens = entry.Payload.Info.LastTokenUsage.CachedInputTokens
		stats.LastOutputTokens = entry.Payload.Info.LastTokenUsage.OutputTokens
		stats.UpdatedAt, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
		return
	}
}

func parseClaudeAgentUsage(path string, stats *AgentUsageStats) {
	type claudeUsage struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	}
	type claudeMessage struct {
		Role  string      `json:"role"`
		Usage claudeUsage `json:"usage"`
	}
	type claudeEntry struct {
		Timestamp string        `json:"timestamp"`
		Type      string        `json:"type"`
		Message   claudeMessage `json:"message"`
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry claudeEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Message.Role != "assistant" {
			continue
		}
		input := entry.Message.Usage.InputTokens + entry.Message.Usage.CacheCreationInputTokens
		cached := entry.Message.Usage.CacheReadInputTokens
		output := entry.Message.Usage.OutputTokens
		stats.LastInputTokens = input
		stats.LastCachedTokens = cached
		stats.LastOutputTokens = output
		stats.TotalInputTokens += input
		stats.TotalCachedTokens += cached
		stats.TotalOutputTokens += output
		stats.UpdatedAt, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
	}
}

func readCodexSessionModel(lines []string) string {
	type codexEntry struct {
		Type    string `json:"type"`
		Payload struct {
			Model string `json:"model"`
		} `json:"payload"`
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var entry codexEntry
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			continue
		}
		if (entry.Type == "turn_context" || entry.Type == "session_meta") && strings.TrimSpace(entry.Payload.Model) != "" {
			return strings.TrimSpace(entry.Payload.Model)
		}
	}
	return ""
}

func readClaudeSessionModel(lines []string) string {
	type claudeEntry struct {
		Message struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	for i := len(lines) - 1; i >= 0; i-- {
		var entry claudeEntry
		if err := json.Unmarshal([]byte(lines[i]), &entry); err != nil {
			continue
		}
		if strings.TrimSpace(entry.Message.Model) != "" {
			return strings.TrimSpace(entry.Message.Model)
		}
	}
	return ""
}
