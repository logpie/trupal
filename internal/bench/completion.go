package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BenchmarkCompletionState string

const (
	BenchmarkStateStarting    BenchmarkCompletionState = "starting"
	BenchmarkStateAttached    BenchmarkCompletionState = "attached"
	BenchmarkStateActive      BenchmarkCompletionState = "active"
	BenchmarkStateQuiescing   BenchmarkCompletionState = "quiescing"
	BenchmarkStateComplete    BenchmarkCompletionState = "complete"
	BenchmarkStateHardTimeout BenchmarkCompletionState = "hard_timeout"
	BenchmarkStateAttachFail  BenchmarkCompletionState = "attach_failed"
)

type BenchmarkStopReason string

const (
	BenchmarkStopReasonNone        BenchmarkStopReason = ""
	BenchmarkStopReasonConverged   BenchmarkStopReason = "converged"
	BenchmarkStopReasonHardTimeout BenchmarkStopReason = "hard_timeout"
	BenchmarkStopReasonAttachFail  BenchmarkStopReason = "attach_failed"
	BenchmarkStopReasonRunnerErr   BenchmarkStopReason = "runner_error"
)

type BenchmarkRuntimeStatus struct {
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
	UpdatedAt            time.Time `json:"updated_at,omitempty"`
}

type BenchmarkStatus struct {
	State                  BenchmarkCompletionState `json:"state"`
	Reason                 BenchmarkStopReason      `json:"reason,omitempty"`
	AgentStatus            string                   `json:"agent_status,omitempty"`
	AttachedAt             time.Time                `json:"attached_at,omitempty"`
	LastSessionEventAt     time.Time                `json:"last_session_event_at,omitempty"`
	LastWorkChangeAt       time.Time                `json:"last_edit_at,omitempty"`
	LastGeneratedNudgeAt   time.Time                `json:"last_generated_nudge_at,omitempty"`
	LastSentNudgeAt        time.Time                `json:"last_sent_nudge_at,omitempty"`
	LastBrainActivityAt    time.Time                `json:"last_brain_activity_at,omitempty"`
	LastMeaningfulActivity time.Time                `json:"last_meaningful_activity_at,omitempty"`
	BrainInFlight          bool                     `json:"brain_in_flight"`
	SendInFlight           bool                     `json:"send_in_flight"`
	OpenIssueCount         int                      `json:"open_issue_count"`
	SendableIssueCount     int                      `json:"sendable_issue_count"`
	IdleThreshold          string                   `json:"idle_threshold"`
	SettleWindow           string                   `json:"settle_window"`
	HardTimeout            string                   `json:"hard_timeout"`
	QuietFor               string                   `json:"quiet_for"`
	UpdatedAt              time.Time                `json:"updated_at,omitempty"`
}

func benchmarkRuntimeStatusPath(projectDir string) string {
	return filepath.Join(projectDir, ".trupal.runtime.json")
}

func benchmarkStatusPath(projectDir string) string {
	return filepath.Join(projectDir, ".trupal.bench.status.json")
}

func readBenchmarkRuntimeStatus(projectDir string) (BenchmarkRuntimeStatus, bool, error) {
	path := benchmarkRuntimeStatusPath(projectDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BenchmarkRuntimeStatus{}, false, nil
		}
		return BenchmarkRuntimeStatus{}, false, err
	}
	var status BenchmarkRuntimeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return BenchmarkRuntimeStatus{}, false, err
	}
	return status, true, nil
}

func writeBenchmarkStatus(projectDir string, status BenchmarkStatus) error {
	path := benchmarkStatusPath(projectDir)
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

func benchmarkIdleThreshold() time.Duration {
	return 15 * time.Second
}

func benchmarkSettleWindow(policy SteeringPolicy) time.Duration {
	window := 45 * time.Second
	if policy.Mode == SteeringModeContinuous {
		window = 60 * time.Second
	}
	if cooldownWindow := policy.Cooldown * 2; cooldownWindow > window {
		window = cooldownWindow
	}
	return window
}

func shouldEnterTimeoutGrace(policy SteeringPolicy, arm BenchmarkArm, runtime BenchmarkRuntimeStatus) bool {
	if arm != ArmSteer {
		return false
	}
	if policy.Mode != SteeringModeContinuous {
		return false
	}
	if runtime.BrainInFlight || runtime.SendInFlight {
		return true
	}
	if runtime.SendableIssueCount > 0 {
		return true
	}
	if runtime.OpenIssueCount <= 0 {
		return false
	}
	if runtime.LastGeneratedNudgeAt.IsZero() {
		return false
	}
	return runtime.LastSentNudgeAt.IsZero() || runtime.LastGeneratedNudgeAt.After(runtime.LastSentNudgeAt)
}

func benchmarkTimeoutGrace(policy SteeringPolicy, now time.Time, runtime BenchmarkRuntimeStatus) time.Duration {
	grace := benchmarkSettleWindow(policy)
	if policy.Mode != SteeringModeContinuous {
		return 0
	}
	if !runtime.LastSentNudgeAt.IsZero() {
		if remainingCooldown := policy.Cooldown - now.Sub(runtime.LastSentNudgeAt); remainingCooldown > 0 {
			if remainingCooldown+5*time.Second > grace {
				grace = remainingCooldown + 5*time.Second
			}
		}
	}
	return grace
}

func benchmarkRecentActivityGrace(policy SteeringPolicy, arm BenchmarkArm, now time.Time, runtime BenchmarkRuntimeStatus) time.Duration {
	if arm != ArmSteer || policy.Mode != SteeringModeContinuous {
		return 0
	}
	lastMeaningful := maxTime(
		runtime.LastSessionEventAt,
		runtime.LastWorkChangeAt,
		runtime.LastGeneratedNudgeAt,
		runtime.LastSentNudgeAt,
		runtime.LastBrainActivityAt,
	)
	if lastMeaningful.IsZero() || now.Before(lastMeaningful) {
		return 0
	}
	quietFor := now.Sub(lastMeaningful)
	settleWindow := benchmarkSettleWindow(policy)
	if quietFor >= settleWindow {
		return 0
	}
	grace := settleWindow - quietFor + policy.Cooldown
	if grace < 15*time.Second {
		grace = 15 * time.Second
	}
	return grace
}

func evaluateBenchmarkStatus(now, attachedAt time.Time, hardTimeout time.Duration, policy SteeringPolicy, runtime BenchmarkRuntimeStatus, runtimeSeen bool) BenchmarkStatus {
	idleThreshold := benchmarkIdleThreshold()
	settleWindow := benchmarkSettleWindow(policy)
	agentStatus := strings.TrimSpace(runtime.AgentStatus)
	lastMeaningful := maxTime(
		attachedAt,
		runtime.LastSessionEventAt,
		runtime.LastWorkChangeAt,
		runtime.LastGeneratedNudgeAt,
		runtime.LastSentNudgeAt,
		runtime.LastBrainActivityAt,
	)
	quietFor := time.Duration(0)
	if !lastMeaningful.IsZero() && !now.Before(lastMeaningful) {
		quietFor = now.Sub(lastMeaningful)
	}

	status := BenchmarkStatus{
		State:                  BenchmarkStateStarting,
		AgentStatus:            agentStatus,
		AttachedAt:             attachedAt,
		LastSessionEventAt:     runtime.LastSessionEventAt,
		LastWorkChangeAt:       runtime.LastWorkChangeAt,
		LastGeneratedNudgeAt:   runtime.LastGeneratedNudgeAt,
		LastSentNudgeAt:        runtime.LastSentNudgeAt,
		LastBrainActivityAt:    runtime.LastBrainActivityAt,
		LastMeaningfulActivity: lastMeaningful,
		BrainInFlight:          runtime.BrainInFlight,
		SendInFlight:           runtime.SendInFlight,
		OpenIssueCount:         runtime.OpenIssueCount,
		SendableIssueCount:     runtime.SendableIssueCount,
		IdleThreshold:          idleThreshold.String(),
		SettleWindow:           settleWindow.String(),
		HardTimeout:            hardTimeout.String(),
		QuietFor:               quietFor.String(),
		UpdatedAt:              now,
	}

	switch {
	case !runtimeSeen:
		if attachedAt.IsZero() {
			status.State = BenchmarkStateStarting
		} else {
			status.State = BenchmarkStateAttached
		}
	case agentStatus != "idle" || runtime.BrainInFlight || runtime.SendInFlight || runtime.SendableIssueCount > 0:
		status.State = BenchmarkStateActive
	case quietFor < idleThreshold:
		status.State = BenchmarkStateActive
	case quietFor < settleWindow:
		status.State = BenchmarkStateQuiescing
	default:
		status.State = BenchmarkStateComplete
		status.Reason = BenchmarkStopReasonConverged
	}

	return status
}

func maxTime(times ...time.Time) time.Time {
	var best time.Time
	for _, t := range times {
		if t.IsZero() {
			continue
		}
		if best.IsZero() || t.After(best) {
			best = t
		}
	}
	return best
}
