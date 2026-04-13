package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderReplay = "replay"
)

func normalizeProvider(provider, fallback string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return fallback
	}
	return provider
}

func sessionProviderDisplayName(provider string) string {
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return "Codex"
	default:
		return "Claude Code"
	}
}

func sessionProviderLabel(provider string) string {
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return "codex"
	default:
		return "claude"
	}
}

func providerPaneCommand(provider string) string {
	switch normalizeProvider(provider, ProviderClaude) {
	case ProviderCodex:
		return "codex"
	default:
		return "claude"
	}
}

func providerExecutable(provider string) (string, error) {
	return exec.LookPath(providerPaneCommand(provider))
}

func pathsOverlap(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	relAB, errAB := filepath.Rel(a, b)
	if errAB == nil && relAB != ".." && !strings.HasPrefix(relAB, ".."+string(filepath.Separator)) {
		return true
	}
	relBA, errBA := filepath.Rel(b, a)
	if errBA == nil && relBA != ".." && !strings.HasPrefix(relBA, ".."+string(filepath.Separator)) {
		return true
	}
	return false
}
