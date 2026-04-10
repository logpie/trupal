package bench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanBenchLastEntryType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "" +
		"{\"type\":\"event_msg\",\"payload\":{\"type\":\"user_message\"}}\n" +
		"{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got := scanBenchLastEntryType(path); got != "assistant" {
		t.Fatalf("scanBenchLastEntryType() = %q, want assistant", got)
	}
}

func TestDetectBenchAgentStatusIdleAfterAssistantTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	if got := detectBenchAgentStatus(path); got != "idle" {
		t.Fatalf("detectBenchAgentStatus() = %q, want idle", got)
	}
}
