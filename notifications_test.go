package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadBrainNotificationRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notifications.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"trigger_summary":"one","notification":"n1","active_findings_json":"[]"}`+"\n"+
			`{"trigger_summary":"two","notification":"n2","active_findings_json":"[]"}`+"\n",
	), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	records, err := ReadBrainNotificationRecords(path)
	if err != nil {
		t.Fatalf("ReadBrainNotificationRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[1].TriggerSummary != "two" || records[1].Notification != "n2" {
		t.Fatalf("unexpected second record: %#v", records[1])
	}
}

func TestReplayBrainNotificationsUsesReplayProviderDeterministically(t *testing.T) {
	dir := t.TempDir()
	replayPath := filepath.Join(dir, "brain-replay.json")
	notificationsPath := filepath.Join(dir, "notifications.jsonl")
	outputPath := filepath.Join(dir, "replayed.jsonl")

	if err := os.WriteFile(replayPath, []byte(`[
		{"match":"first","response":{"info":["turn one"],"nudges":[{"severity":"warn","message":"n1"}],"resolved_findings":[]}},
		{"response":{"info":["fallback"],"nudges":[],"resolved_findings":["f-1"]}}
	]`), 0644); err != nil {
		t.Fatalf("WriteFile(replay) error = %v", err)
	}
	if err := os.WriteFile(notificationsPath, []byte(
		`{"trigger_summary":"a","notification":"first notification","active_findings_json":"[]"}`+"\n"+
			`{"trigger_summary":"b","notification":"second notification","active_findings_json":"[]"}`+"\n",
	), 0644); err != nil {
		t.Fatalf("WriteFile(notifications) error = %v", err)
	}

	result, err := ReplayBrainNotifications(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: replayPath,
		BrainEffort:     "high",
	}, dir, filepath.Join(dir, "session.jsonl"), notificationsPath, outputPath)
	if err != nil {
		t.Fatalf("ReplayBrainNotifications() error = %v", err)
	}
	if result.Notifications != 2 || result.GeneratedNudges != 1 {
		t.Fatalf("result = %#v, want 2 notifications and 1 nudge", result)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	lines := bytesToLines(raw)
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	var first BrainReplayResponseRecord
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal(first) error = %v", err)
	}
	if first.Index != 1 || len(first.Response.Nudges) != 1 || first.Response.Nudges[0].Message != "n1" {
		t.Fatalf("unexpected first replay record: %#v", first)
	}
	var second BrainReplayResponseRecord
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal(second) error = %v", err)
	}
	if len(second.Response.ResolvedFindings) != 1 || second.Response.ResolvedFindings[0] != "f-1" {
		t.Fatalf("unexpected second replay record: %#v", second)
	}
}

func TestReplayBrainNotificationsMatchesQutebrowserGolden(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	fixtureDir := filepath.Join(wd, "testdata", "qutebrowser_parity")
	projectDir := t.TempDir()
	notificationsPath := filepath.Join(fixtureDir, "notifications.jsonl")
	replayPath := filepath.Join(fixtureDir, "brain_replay.json")
	outputPath := filepath.Join(projectDir, "replayed.jsonl")
	goldenPath := filepath.Join(fixtureDir, "replayed.golden.jsonl")

	result, err := ReplayBrainNotifications(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: replayPath,
		BrainEffort:     "high",
	}, projectDir, filepath.Join(projectDir, "session.jsonl"), notificationsPath, outputPath)
	if err != nil {
		t.Fatalf("ReplayBrainNotifications() error = %v", err)
	}
	if result.Notifications != 24 || result.GeneratedNudges != 1 {
		t.Fatalf("result = %#v, want 24 notifications and 1 generated nudge", result)
	}

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(golden) error = %v", err)
	}
	if !reflect.DeepEqual(bytesToLines(got), bytesToLines(want)) {
		t.Fatalf("replayed output did not match golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestReplayBrainNotificationsMatchesQutebrowserMultiNudgeGolden(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	fixtureDir := filepath.Join(wd, "testdata", "qutebrowser_multi_nudge")
	projectDir := t.TempDir()
	notificationsPath := filepath.Join(fixtureDir, "notifications.jsonl")
	replayPath := filepath.Join(fixtureDir, "brain_replay.json")
	outputPath := filepath.Join(projectDir, "replayed.jsonl")
	goldenPath := filepath.Join(fixtureDir, "replayed.golden.jsonl")

	result, err := ReplayBrainNotifications(Config{
		SessionProvider: ProviderCodex,
		BrainProvider:   ProviderReplay,
		BrainReplayPath: replayPath,
		BrainEffort:     "high",
	}, projectDir, filepath.Join(projectDir, "session.jsonl"), notificationsPath, outputPath)
	if err != nil {
		t.Fatalf("ReplayBrainNotifications() error = %v", err)
	}
	if result.Notifications != 24 || result.GeneratedNudges != 3 {
		t.Fatalf("result = %#v, want 24 notifications and 3 generated nudges", result)
	}

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(golden) error = %v", err)
	}
	if !reflect.DeepEqual(bytesToLines(got), bytesToLines(want)) {
		t.Fatalf("replayed multi-nudge output did not match golden\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func bytesToLines(raw []byte) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
