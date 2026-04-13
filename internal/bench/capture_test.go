package bench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindLatestSessionJSONLRespectsCODEXHOME(t *testing.T) {
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

	sessionsDir := filepath.Join(codexHome, "sessions", "2026", "04", "11")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("MkdirAll(sessionsDir) error = %v", err)
	}
	match := filepath.Join(sessionsDir, "match.jsonl")
	data := `{"timestamp":"2026-04-11T00:00:00Z","type":"session_meta","payload":{"cwd":"` + sessionDir + `"}}` + "\n"
	if err := os.WriteFile(match, []byte(data), 0644); err != nil {
		t.Fatalf("WriteFile(match) error = %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(match, now, now); err != nil {
		t.Fatalf("Chtimes(match) error = %v", err)
	}

	got, err := FindLatestSessionJSONL(repoRoot, "codex")
	if err != nil {
		t.Fatalf("FindLatestSessionJSONL() error = %v", err)
	}
	if got != match {
		t.Fatalf("FindLatestSessionJSONL() = %q, want %q", got, match)
	}
}
