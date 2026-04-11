package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBenchmarkAgentPromptForcesNonInteractiveExecution(t *testing.T) {
	prompt := benchmarkAgentPrompt("Implement the API")

	for _, want := range []string{
		"Avoid asking the user questions during the run.",
		"implement the task directly",
		"Task:",
		"Implement the API",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("benchmarkClaudePrompt() missing %q in:\n%s", want, prompt)
		}
	}
}

func TestScenarioUsesGenericAgentModelForCodex(t *testing.T) {
	scenario, err := parseScenarioYAML([]byte(`
id: sample
name: sample
category: api
timeout: 2m
agent_model: gpt-5.4-mini
trupal_config:
  session_provider: codex
  brain_provider: codex
`))
	if err != nil {
		t.Fatalf("parseScenarioYAML() error = %v", err)
	}
	if got := scenario.SessionProvider(); got != "codex" {
		t.Fatalf("SessionProvider() = %q, want codex", got)
	}
	if got := scenario.EffectiveAgentModel(); got != "gpt-5.4-mini" {
		t.Fatalf("EffectiveAgentModel() = %q, want gpt-5.4-mini", got)
	}
}

func TestEffectiveBenchmarkSteeringPolicyDefaults(t *testing.T) {
	policy := effectiveBenchmarkSteeringPolicy(Scenario{})

	if policy.Mode != SteeringModeSingle {
		t.Fatalf("mode = %q, want single", policy.Mode)
	}
	if policy.Rounds != 1 {
		t.Fatalf("rounds = %d, want 1", policy.Rounds)
	}
	if policy.Cooldown != 30*time.Second {
		t.Fatalf("cooldown = %s, want 30s", policy.Cooldown)
	}
}

func TestEffectiveBenchmarkSteeringPolicyPreservesScenarioValues(t *testing.T) {
	policy := effectiveBenchmarkSteeringPolicy(Scenario{
		SteeringMode:     SteeringModeContinuous,
		SteeringRounds:   3,
		SteeringCooldown: 45 * time.Second,
	})

	if policy.Mode != SteeringModeContinuous {
		t.Fatalf("mode = %q, want continuous", policy.Mode)
	}
	if policy.Rounds != 3 {
		t.Fatalf("rounds = %d, want 3", policy.Rounds)
	}
	if policy.Cooldown != 45*time.Second {
		t.Fatalf("cooldown = %s, want 45s", policy.Cooldown)
	}
}

func TestTrupalTUIReadyAcceptsFullControls(t *testing.T) {
	text := `
 ◉☰ TRUPAL  2m
 watch  codex · gpt-5.4-mini · repo · active
 brain  codex/gpt-5.4-mini · starting · steer manual
 ...
 move j/k  page pgup/pgdn  details o  issues p  send s  auto a
`
	if !trupalTUIReady(text) {
		t.Fatalf("trupalTUIReady() = false, want true")
	}
}

func TestTrupalTUIReadyAcceptsTruncatedFooterWhenHeaderIsLive(t *testing.T) {
	text := `
 ◉☰ TRUPAL  21s
 watch  codex · trupal-swebench-run-in… · starting
 brain  codex/gpt-5.4-mini · starting · steer manual
 ...
 move j/k  page pgup/pgdn  details o  issues …
`
	if !trupalTUIReady(text) {
		t.Fatalf("trupalTUIReady() = false, want true for live TUI header")
	}
}

func TestApplySteeringTelemetryCountsGeneratedAndSentNudges(t *testing.T) {
	start := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	result := &RunResult{
		StartedAt: start,
		SteeringEvents: []SteeringEvent{{
			Timestamp: start.Add(7 * time.Second),
		}},
	}
	debug := DebugSummary{
		Nudges: []ObservedFinding{{
			Message:   "first",
			FirstSeen: start.Add(5 * time.Second),
		}},
	}
	result.applySteeringTelemetry(debug)
	if result.GeneratedNudges != 1 || result.SentNudges != 1 || result.UnsentNudges != 0 {
		t.Fatalf("telemetry = generated:%d sent:%d unsent:%d", result.GeneratedNudges, result.SentNudges, result.UnsentNudges)
	}
	if result.FirstGeneratedNudge != 5*time.Second {
		t.Fatalf("FirstGeneratedNudge = %s, want 5s", result.FirstGeneratedNudge)
	}
	if result.FirstSentNudge != 7*time.Second {
		t.Fatalf("FirstSentNudge = %s, want 7s", result.FirstSentNudge)
	}
}

func TestEffectiveInteractiveTimeoutDefaultsByMode(t *testing.T) {
	if got := effectiveInteractiveTimeout(Scenario{}); got != 2*time.Minute {
		t.Fatalf("single/default timeout = %s, want 2m", got)
	}
	if got := effectiveInteractiveTimeout(Scenario{SteeringMode: SteeringModeContinuous}); got != 5*time.Minute {
		t.Fatalf("continuous timeout = %s, want 5m", got)
	}
	if got := effectiveInteractiveTimeout(Scenario{Timeout: 90 * time.Second, SteeringMode: SteeringModeContinuous}); got != 90*time.Second {
		t.Fatalf("explicit timeout = %s, want 90s", got)
	}
}

func TestEvaluateBenchmarkStatusConvergesWhenQuietAndNoSendableIssues(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC)
	status := evaluateBenchmarkStatus(
		now,
		now.Add(-5*time.Minute),
		5*time.Minute,
		effectiveBenchmarkSteeringPolicy(Scenario{SteeringMode: SteeringModeContinuous}),
		BenchmarkRuntimeStatus{
			AgentStatus:          "idle",
			LastSessionEventAt:   now.Add(-70 * time.Second),
			LastWorkChangeAt:     now.Add(-70 * time.Second),
			LastGeneratedNudgeAt: now.Add(-70 * time.Second),
			LastSentNudgeAt:      now.Add(-70 * time.Second),
			LastBrainActivityAt:  now.Add(-70 * time.Second),
			SendableIssueCount:   0,
		},
		true,
	)
	if status.State != BenchmarkStateComplete || status.Reason != BenchmarkStopReasonConverged {
		t.Fatalf("status = %#v, want complete/converged", status)
	}
}

func TestEvaluateBenchmarkStatusQuiescesBeforeSettleWindow(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC)
	status := evaluateBenchmarkStatus(
		now,
		now.Add(-5*time.Minute),
		5*time.Minute,
		effectiveBenchmarkSteeringPolicy(Scenario{SteeringMode: SteeringModeContinuous}),
		BenchmarkRuntimeStatus{
			AgentStatus:        "idle",
			LastSessionEventAt: now.Add(-20 * time.Second),
		},
		true,
	)
	if status.State != BenchmarkStateQuiescing {
		t.Fatalf("state = %q, want quiescing", status.State)
	}
	if status.Reason != BenchmarkStopReasonNone {
		t.Fatalf("reason = %q, want empty", status.Reason)
	}
}

func TestWriteBenchmarkStatusArtifact(t *testing.T) {
	dir := t.TempDir()
	status := BenchmarkStatus{
		State:         BenchmarkStateQuiescing,
		Reason:        BenchmarkStopReasonNone,
		AgentStatus:   "idle",
		IdleThreshold: "15s",
		SettleWindow:  "1m0s",
		HardTimeout:   "5m0s",
		UpdatedAt:     time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC),
	}
	if err := writeBenchmarkStatus(dir, status); err != nil {
		t.Fatalf("writeBenchmarkStatus() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".trupal.bench.status.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(raw)
	for _, want := range []string{`"state": "quiescing"`, `"agent_status": "idle"`, `"settle_window": "1m0s"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("status artifact missing %q in:\n%s", want, text)
		}
	}
}

func TestShouldEnterTimeoutGraceForLateGeneratedContinuousWork(t *testing.T) {
	now := time.Date(2026, 4, 10, 23, 20, 34, 0, time.UTC)
	policy := effectiveBenchmarkSteeringPolicy(Scenario{SteeringMode: SteeringModeContinuous})
	runtime := BenchmarkRuntimeStatus{
		OpenIssueCount:       4,
		SendableIssueCount:   0,
		LastSentNudgeAt:      now.Add(-23 * time.Second),
		LastGeneratedNudgeAt: now.Add(-6 * time.Second),
	}
	if !shouldEnterTimeoutGrace(policy, ArmSteer, runtime) {
		t.Fatal("shouldEnterTimeoutGrace() = false, want true for late generated unsent nudge")
	}
	if got := benchmarkTimeoutGrace(policy, now, runtime); got < 12*time.Second {
		t.Fatalf("benchmarkTimeoutGrace() = %s, want enough time for remaining cooldown", got)
	}
}

func TestShouldNotEnterTimeoutGraceWithoutPendingContinuousWork(t *testing.T) {
	now := time.Date(2026, 4, 10, 23, 20, 34, 0, time.UTC)
	policy := effectiveBenchmarkSteeringPolicy(Scenario{SteeringMode: SteeringModeContinuous})
	runtime := BenchmarkRuntimeStatus{
		AgentStatus:          "idle",
		OpenIssueCount:       0,
		SendableIssueCount:   0,
		LastSentNudgeAt:      now.Add(-2 * time.Minute),
		LastGeneratedNudgeAt: now.Add(-3 * time.Minute),
	}
	if shouldEnterTimeoutGrace(policy, ArmSteer, runtime) {
		t.Fatal("shouldEnterTimeoutGrace() = true, want false when no pending work remains")
	}
}

func TestShouldNotEnterTimeoutGraceForControlArm(t *testing.T) {
	now := time.Date(2026, 4, 10, 23, 20, 34, 0, time.UTC)
	policy := effectiveBenchmarkSteeringPolicy(Scenario{SteeringMode: SteeringModeContinuous})
	runtime := BenchmarkRuntimeStatus{
		OpenIssueCount:       4,
		SendableIssueCount:   4,
		LastGeneratedNudgeAt: now.Add(-6 * time.Second),
	}
	if shouldEnterTimeoutGrace(policy, ArmControl, runtime) {
		t.Fatal("shouldEnterTimeoutGrace() = true, want false for control arm")
	}
}

func TestFilterTelemetryByCutoffDropsLateGeneratedNudges(t *testing.T) {
	start := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	finished := start.Add(5 * time.Minute)
	cutoff := benchmarkTelemetryCutoff(finished)
	debug := DebugSummary{
		ResponseEvents: []BrainResponseEvent{
			{Time: start.Add(time.Minute)},
			{Time: finished.Add(7 * time.Second)},
		},
		Nudges: []ObservedFinding{
			{Message: "in-window", FirstSeen: finished.Add(4 * time.Second)},
			{Message: "late", FirstSeen: finished.Add(6 * time.Second)},
		},
		Observations: []ObservedFinding{
			{Message: "info", FirstSeen: finished.Add(4 * time.Second)},
			{Message: "late-info", FirstSeen: finished.Add(6 * time.Second)},
		},
		NudgeEventCount: 2,
	}
	events := []SteeringEvent{
		{Timestamp: finished.Add(4 * time.Second), Message: "sent"},
		{Timestamp: finished.Add(6 * time.Second), Message: "late-sent"},
	}

	filteredDebug := filterDebugSummaryByCutoff(debug, cutoff)
	filteredEvents := filterSteeringEventsByCutoff(events, cutoff)

	if len(filteredDebug.Nudges) != 1 || filteredDebug.Nudges[0].Message != "in-window" {
		t.Fatalf("filtered nudges = %#v, want only in-window entry", filteredDebug.Nudges)
	}
	if filteredDebug.NudgeEventCount != 1 {
		t.Fatalf("NudgeEventCount = %d, want 1", filteredDebug.NudgeEventCount)
	}
	if len(filteredEvents) != 1 || filteredEvents[0].Message != "sent" {
		t.Fatalf("filtered steering events = %#v, want only sent entry", filteredEvents)
	}
}
