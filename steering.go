package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SteeringEvent struct {
	Timestamp string `json:"timestamp"`
	FindingID string `json:"finding_id"`
	Message   string `json:"message"`
	Source    string `json:"source"`
	PaneID    string `json:"pane_id"`
}

type SteeringSendState struct {
	Message string
	Source  string
	At      time.Time
}

var sendSteeringMessage = sendAgentMessageToPane
var recordSteeringEvent = appendSteeringEvent
var runTmuxCommand = func(args ...string) ([]byte, error) {
	return exec.Command("tmux", args...).CombinedOutput()
}
var steeringSubmitDelay = 150 * time.Millisecond

func sendAgentMessageToPane(paneID, message string) error {
	paneID = strings.TrimSpace(paneID)
	message = strings.TrimSpace(message)
	if paneID == "" {
		return fmt.Errorf("empty pane id")
	}
	if message == "" {
		return fmt.Errorf("empty message")
	}
	if out, err := runTmuxCommand("send-keys", "-t", paneID, "-l", message); err != nil {
		return fmt.Errorf("send message: %w: %s", err, strings.TrimSpace(string(out)))
	}
	time.Sleep(steeringSubmitDelay)
	if out, err := runTmuxCommand("send-keys", "-t", paneID, "Enter"); err != nil {
		return fmt.Errorf("submit message: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func appendSteeringEvent(repoRoot string, event SteeringEvent) error {
	if strings.TrimSpace(repoRoot) == "" {
		return nil
	}
	path := filepath.Join(repoRoot, ".trupal.steer.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(event)
}
