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

func TestFindSessionJSONLForProviderFindsMatchingCodexSession(t *testing.T) {
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

	codexDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "04", "07")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("MkdirAll(codexDir) error = %v", err)
	}

	nonMatch := filepath.Join(codexDir, "other.jsonl")
	nonMatchData := `{"timestamp":"2026-04-08T00:00:00Z","type":"session_meta","payload":{"cwd":"/tmp/other"}}` + "\n"
	if err := os.WriteFile(nonMatch, []byte(nonMatchData), 0644); err != nil {
		t.Fatalf("WriteFile(nonMatch) error = %v", err)
	}

	match := filepath.Join(codexDir, "match.jsonl")
	matchData := `{"timestamp":"2026-04-08T00:00:00Z","type":"session_meta","payload":{"cwd":"` + sessionDir + `"}}` + "\n"
	if err := os.WriteFile(match, []byte(matchData), 0644); err != nil {
		t.Fatalf("WriteFile(match) error = %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(match, now, now); err != nil {
		t.Fatalf("Chtimes(match) error = %v", err)
	}

	got := FindSessionJSONLForProvider(repoRoot, ProviderCodex)
	if got != match {
		t.Fatalf("FindSessionJSONLForProvider(%q, codex) = %q, want %q", repoRoot, got, match)
	}
}

func TestFindSessionJSONLForProviderIgnoresParentCodexSession(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	parent := filepath.Join(homeDir, "work")
	repoRoot := filepath.Join(parent, "project")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}

	codexDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "04", "07")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("MkdirAll(codexDir) error = %v", err)
	}

	parentMatch := filepath.Join(codexDir, "parent.jsonl")
	parentData := `{"timestamp":"2026-04-08T00:00:00Z","type":"session_meta","payload":{"cwd":"` + parent + `"}}` + "\n"
	if err := os.WriteFile(parentMatch, []byte(parentData), 0644); err != nil {
		t.Fatalf("WriteFile(parentMatch) error = %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(parentMatch, now, now); err != nil {
		t.Fatalf("Chtimes(parentMatch) error = %v", err)
	}

	got := FindSessionJSONLForProvider(repoRoot, ProviderCodex)
	if got != "" {
		t.Fatalf("FindSessionJSONLForProvider(%q, codex) = %q, want empty", repoRoot, got)
	}
}

func TestFindSessionJSONLForProviderRespectsCODEXHOME(t *testing.T) {
	homeDir := t.TempDir()
	codexHome := filepath.Join(homeDir, "isolated-codex")
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_HOME", codexHome)

	repoRoot := filepath.Join(homeDir, "work", "project")
	sessionDir := filepath.Join(repoRoot, "src")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatalf("MkdirAll(src) error = %v", err)
	}

	codexDir := filepath.Join(codexHome, "sessions", "2026", "04", "11")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatalf("MkdirAll(codexDir) error = %v", err)
	}
	match := filepath.Join(codexDir, "match.jsonl")
	matchData := `{"timestamp":"2026-04-11T00:00:00Z","type":"session_meta","payload":{"cwd":"` + sessionDir + `"}}` + "\n"
	if err := os.WriteFile(match, []byte(matchData), 0644); err != nil {
		t.Fatalf("WriteFile(match) error = %v", err)
	}

	if got := FindSessionJSONLForProvider(repoRoot, ProviderCodex); got != match {
		t.Fatalf("FindSessionJSONLForProvider() with CODEX_HOME = %q, want %q", got, match)
	}
}

func TestParseCodexFunctionCallsAndMessages(t *testing.T) {
	toolLine := []byte(`{"timestamp":"2026-04-08T00:00:01Z","type":"response_item","payload":{"type":"function_call","name":"multi_tool_use.parallel","arguments":"{\"tool_uses\":[{\"recipient_name\":\"functions.exec_command\",\"parameters\":{\"cmd\":\"rg hello\"}},{\"recipient_name\":\"functions.apply_patch\",\"parameters\":\"*** Begin Patch\\n*** Update File: foo.go\\n*** End Patch\\n\"}]}"}}`)
	toolEntry, ok := parseJSONLEntryForProvider(ProviderCodex, toolLine)
	if !ok {
		t.Fatal("expected codex tool entry to parse")
	}
	if len(toolEntry.ToolNames) != 2 {
		t.Fatalf("expected 2 tool entries, got %d", len(toolEntry.ToolNames))
	}
	if toolEntry.ToolNames[0] != "Bash" {
		t.Fatalf("expected exec_command to map to Bash, got %q", toolEntry.ToolNames[0])
	}
	if toolEntry.ToolNames[1] != "Edit" {
		t.Fatalf("expected apply_patch to map to Edit, got %q", toolEntry.ToolNames[1])
	}
	if toolEntry.ToolFiles[1] != "foo.go" {
		t.Fatalf("expected apply_patch file path foo.go, got %q", toolEntry.ToolFiles[1])
	}

	msgLine := []byte(`{"timestamp":"2026-04-08T00:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"checking the diff now"}}`)
	msgEntry, ok := parseJSONLEntryForProvider(ProviderCodex, msgLine)
	if !ok {
		t.Fatal("expected codex agent message to parse")
	}
	if msgEntry.Role != "assistant" || !msgEntry.HasText {
		t.Fatalf("expected assistant text entry, got role=%q hasText=%v", msgEntry.Role, msgEntry.HasText)
	}
	if msgEntry.TextSnip != "checking the diff now" {
		t.Fatalf("unexpected text snippet %q", msgEntry.TextSnip)
	}

	responseItemLine := []byte(`{"timestamp":"2026-04-08T00:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer here"}]}}`)
	responseItemEntry, ok := parseJSONLEntryForProvider(ProviderCodex, responseItemLine)
	if !ok {
		t.Fatal("expected response_item.message to parse")
	}
	if responseItemEntry.Role != "assistant" || responseItemEntry.TextSnip != "final answer here" {
		t.Fatalf("unexpected response_item message parse: %#v", responseItemEntry)
	}
}

func TestParseCodexCustomToolCallsAndLegacyShell(t *testing.T) {
	patchLine := []byte(`{"timestamp":"2026-04-08T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","name":"apply_patch","input":"*** Begin Patch\n*** Update File: foo.go\n@@\n-old\n+new\n*** End Patch"}}`)
	patchEntry, ok := parseJSONLEntryForProvider(ProviderCodex, patchLine)
	if !ok {
		t.Fatal("expected codex custom_tool_call to parse")
	}
	if len(patchEntry.ToolNames) != 1 || patchEntry.ToolNames[0] != "Edit" {
		t.Fatalf("expected apply_patch to map to Edit, got %#v", patchEntry.ToolNames)
	}
	if len(patchEntry.ToolFiles) != 1 || patchEntry.ToolFiles[0] != "foo.go" {
		t.Fatalf("expected apply_patch file path foo.go, got %#v", patchEntry.ToolFiles)
	}

	shellLine := []byte(`{"timestamp":"2026-04-08T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"sed -n '1,20p' foo.go\"]}"}}`)
	shellEntry, ok := parseJSONLEntryForProvider(ProviderCodex, shellLine)
	if !ok {
		t.Fatal("expected legacy codex shell call to parse")
	}
	if len(shellEntry.ToolNames) != 1 || shellEntry.ToolNames[0] != "Bash" {
		t.Fatalf("expected shell to map to Bash, got %#v", shellEntry.ToolNames)
	}
}

func TestDetectAgentStatusForCodexUsesSemanticTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-04-08T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-04-08T00:00:01Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\",\"message\":\"check this\"}}\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	if got := DetectAgentStatus(path, ProviderCodex); got != "thinking" {
		t.Fatalf("DetectAgentStatus(codex) = %q, want thinking", got)
	}
}

func TestDetectAgentStatusForCodexIdleAfterAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	data := "" +
		"{\"timestamp\":\"2026-04-08T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/tmp/project\"}}\n" +
		"{\"timestamp\":\"2026-04-08T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	if got := DetectAgentStatus(path, ProviderCodex); got != "idle" {
		t.Fatalf("DetectAgentStatus(codex) = %q, want idle", got)
	}
}
