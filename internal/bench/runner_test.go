package bench

import (
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
		NudgeEventCount: 3,
		Nudges: []ObservedFinding{{
			Message:   "first",
			FirstSeen: start.Add(5 * time.Second),
		}},
	}
	result.applySteeringTelemetry(debug)
	if result.GeneratedNudges != 3 || result.SentNudges != 1 || result.UnsentNudges != 2 {
		t.Fatalf("telemetry = generated:%d sent:%d unsent:%d", result.GeneratedNudges, result.SentNudges, result.UnsentNudges)
	}
	if result.FirstGeneratedNudge != 5*time.Second {
		t.Fatalf("FirstGeneratedNudge = %s, want 5s", result.FirstGeneratedNudge)
	}
	if result.FirstSentNudge != 7*time.Second {
		t.Fatalf("FirstSentNudge = %s, want 7s", result.FirstSentNudge)
	}
}
