package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type BrainNotificationRecord struct {
	Timestamp          time.Time      `json:"timestamp"`
	BrainProvider      string         `json:"brain_provider"`
	TriggerSummary     string         `json:"trigger_summary"`
	Notification       string         `json:"notification"`
	ActiveFindingsJSON string         `json:"active_findings_json,omitempty"`
	CurrentIssues      []CurrentIssue `json:"current_issues,omitempty"`
	WorkHash           uint64         `json:"work_hash,omitempty"`
}

func brainNotificationLogPath(projectDir string) string {
	return filepath.Join(projectDir, ".trupal.notifications.jsonl")
}

func appendBrainNotificationRecord(projectDir string, record BrainNotificationRecord) error {
	path := brainNotificationLogPath(projectDir)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = f.Write(append(payload, '\n'))
	return err
}

func ReadBrainNotificationRecords(path string) ([]BrainNotificationRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []BrainNotificationRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record BrainNotificationRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("parse notification record: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

type BrainReplayResponseRecord struct {
	Index          int           `json:"index"`
	TriggerSummary string        `json:"trigger_summary,omitempty"`
	Response       BrainResponse `json:"response"`
}

type NotificationReplayResult struct {
	Notifications   int
	GeneratedNudges int
	OutputPath      string
}

func ReplayBrainNotifications(cfg Config, projectDir, jsonlPath, notificationsPath, outputPath string) (NotificationReplayResult, error) {
	records, err := ReadBrainNotificationRecords(notificationsPath)
	if err != nil {
		return NotificationReplayResult{}, err
	}
	if outputPath == "" {
		outputPath = notificationsPath + ".replayed.jsonl"
	}
	brain, err := StartBrain(cfg, projectDir, jsonlPath, BrainStats{Provider: cfg.BrainProvider})
	if err != nil {
		return NotificationReplayResult{}, err
	}
	defer brain.Stop()

	f, err := os.Create(outputPath)
	if err != nil {
		return NotificationReplayResult{}, err
	}
	defer f.Close()

	result := NotificationReplayResult{
		Notifications: len(records),
		OutputPath:    outputPath,
	}
	enc := json.NewEncoder(f)
	for i, record := range records {
		resp, err := brain.Notify(record.Notification, record.ActiveFindingsJSON)
		if err != nil {
			return result, fmt.Errorf("replay notification %d: %w", i+1, err)
		}
		result.GeneratedNudges += len(resp.Nudges)
		out := BrainReplayResponseRecord{
			Index:          i + 1,
			TriggerSummary: record.TriggerSummary,
			Response:       *resp,
		}
		if err := enc.Encode(out); err != nil {
			return result, err
		}
	}
	return result, nil
}
