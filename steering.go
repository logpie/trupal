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

func tmuxPaneInMode(paneID string) bool {
	out, err := runTmuxCommand("display-message", "-p", "-t", paneID, "#{pane_in_mode}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

func exitTmuxPaneMode(paneID string) error {
	out, err := runTmuxCommand("send-keys", "-X", "-t", paneID, "cancel")
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "not in a mode") {
			return nil
		}
		return fmt.Errorf("exit pane mode: %w: %s", err, text)
	}
	return nil
}

func sendAgentMessageToPane(paneID, message string) error {
	paneID = strings.TrimSpace(paneID)
	message = strings.TrimSpace(message)
	if paneID == "" {
		return fmt.Errorf("empty pane id")
	}
	if message == "" {
		return fmt.Errorf("empty message")
	}
	if tmuxPaneInMode(paneID) {
		if err := exitTmuxPaneMode(paneID); err != nil {
			return err
		}
	}
	if out, err := runTmuxCommand("send-keys", "-t", paneID, "-l", message); err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "not in a mode") {
			if exitErr := exitTmuxPaneMode(paneID); exitErr != nil {
				return exitErr
			}
			if retryOut, retryErr := runTmuxCommand("send-keys", "-t", paneID, "-l", message); retryErr == nil {
				out = retryOut
			} else {
				return fmt.Errorf("send message: %w: %s", retryErr, strings.TrimSpace(string(retryOut)))
			}
		} else {
			return fmt.Errorf("send message: %w: %s", err, text)
		}
	}
	time.Sleep(steeringSubmitDelay)
	if out, err := runTmuxCommand("send-keys", "-t", paneID, "Enter"); err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "not in a mode") {
			if exitErr := exitTmuxPaneMode(paneID); exitErr != nil {
				return exitErr
			}
			if retryOut, retryErr := runTmuxCommand("send-keys", "-t", paneID, "Enter"); retryErr == nil {
				return nil
			} else {
				return fmt.Errorf("submit message: %w: %s", retryErr, strings.TrimSpace(string(retryOut)))
			}
		}
		return fmt.Errorf("submit message: %w: %s", err, text)
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
