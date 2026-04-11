package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type benchmarkRuntimeStatus struct {
	AgentStatus          string    `json:"agent_status,omitempty"`
	LastSessionEventAt   time.Time `json:"last_session_event_at,omitempty"`
	LastWorkChangeAt     time.Time `json:"last_edit_at,omitempty"`
	LastGeneratedNudgeAt time.Time `json:"last_generated_nudge_at,omitempty"`
	LastSentNudgeAt      time.Time `json:"last_sent_nudge_at,omitempty"`
	LastBrainActivityAt  time.Time `json:"last_brain_activity_at,omitempty"`
	BrainInFlight        bool      `json:"brain_in_flight"`
	SendInFlight         bool      `json:"send_in_flight"`
	OpenIssueCount       int       `json:"open_issue_count"`
	SendableIssueCount   int       `json:"sendable_issue_count"`
	ContinuousSteering   bool      `json:"continuous_steering"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func benchmarkRuntimeStatusPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".trupal.runtime.json")
}

func writeBenchmarkRuntimeStatus(repoRoot string, status benchmarkRuntimeStatus) error {
	if strings.TrimSpace(repoRoot) == "" {
		return nil
	}
	path := benchmarkRuntimeStatusPath(repoRoot)
	payload, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m model) countSendableIssues() int {
	count := 0
	for _, issue := range m.issueItems {
		if m.canSendNudge(issue, false) {
			count++
		}
	}
	return count
}

func (m model) benchmarkRuntimeStatus(now time.Time) benchmarkRuntimeStatus {
	return benchmarkRuntimeStatus{
		AgentStatus:          strings.TrimSpace(m.watchStatus()),
		LastSessionEventAt:   m.lastSessionEventAt,
		LastWorkChangeAt:     m.lastWorkChangeAt,
		LastGeneratedNudgeAt: m.lastGeneratedNudgeAt,
		LastSentNudgeAt:      m.lastSentNudgeAt,
		LastBrainActivityAt:  m.brain.lastTime,
		BrainInFlight:        m.brain.thinking,
		SendInFlight:         m.steerInFlight,
		OpenIssueCount:       len(m.issueItems),
		SendableIssueCount:   m.countSendableIssues(),
		ContinuousSteering:   m.continuousSteering,
		UpdatedAt:            now,
	}
}

func (m model) persistBenchmarkRuntimeStatus(now time.Time) {
	if !m.benchmarkMode || strings.TrimSpace(m.repoRoot) == "" {
		return
	}
	if err := writeBenchmarkRuntimeStatus(m.repoRoot, m.benchmarkRuntimeStatus(now)); err != nil {
		Debugf("[bench-status] failed to write runtime status: %v", err)
	}
}
