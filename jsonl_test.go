package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyEntryKeepsToolMetadataAligned(t *testing.T) {
	entry := JSONLEntry{
		Message: []byte(`{
			"role":"assistant",
			"content":[
				{"type":"tool_use","name":"Bash","input":{"description":"Verify","command":"go test ./..."}},
				{"type":"tool_use","name":"Edit","input":{"file_path":"/tmp/testproject/server.go"}}
			]
		}`),
	}

	classifyEntry(&entry)

	if len(entry.ToolNames) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(entry.ToolNames))
	}
	if len(entry.ToolFiles) != 2 {
		t.Fatalf("expected tool files aligned with tool names, got %d", len(entry.ToolFiles))
	}
	if got := entry.ToolFiles[0]; got != "" {
		t.Fatalf("expected bash tool to have empty file path, got %q", got)
	}
	if got := entry.ToolFiles[1]; got != "/tmp/testproject/server.go" {
		t.Fatalf("expected edit tool file path, got %q", got)
	}
	if len(entry.ToolDetails) != 2 {
		t.Fatalf("expected tool details aligned with tool names, got %d", len(entry.ToolDetails))
	}
	if got := entry.ToolDetails[0]; got != "Verify" {
		t.Fatalf("expected bash description, got %q", got)
	}
}

func TestReadRecentJSONLEntriesSeedsExistingSessionContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	data := "" +
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"Check /tmp/testproject/server.go\"}}\n" +
		"{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"name\":\"Read\",\"input\":{\"file_path\":\"/tmp/testproject/server.go\"}}]}}\n" +
		"{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entries := ReadRecentJSONLEntries(path, 2)
	if len(entries) != 2 {
		t.Fatalf("expected last 2 entries, got %d", len(entries))
	}
	if got := entries[0].ToolFiles[0]; got != "/tmp/testproject/server.go" {
		t.Fatalf("expected seeded tool file path, got %q", got)
	}
	if got := entries[1].TextSnip; got != "done" {
		t.Fatalf("expected final assistant text snippet, got %q", got)
	}
}

func TestFindSessionJSONLFallsBackToGitRootForSubdirStarts(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(homeDir, "work", "project")
	sessionDir := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("MkdirAll(src) error = %v", err)
	}

	encodedRepoRoot := strings.ReplaceAll(repoRoot, string(os.PathSeparator), "-")
	claudeDir := filepath.Join(homeDir, ".claude", "projects", encodedRepoRoot)
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("MkdirAll(claudeDir) error = %v", err)
	}

	jsonlPath := filepath.Join(claudeDir, "root-session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("WriteFile(root session) error = %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(jsonlPath, now, now); err != nil {
		t.Fatalf("Chtimes(root session) error = %v", err)
	}

	got := FindSessionJSONL(sessionDir)
	if got != jsonlPath {
		t.Fatalf("FindSessionJSONL(%q) = %q, want %q", sessionDir, got, jsonlPath)
	}
}
