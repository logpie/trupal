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
	rounds, cooldown := effectiveBenchmarkSteeringPolicy(Scenario{})

	if rounds != 1 {
		t.Fatalf("rounds = %d, want 1", rounds)
	}
	if cooldown != 30*time.Second {
		t.Fatalf("cooldown = %s, want 30s", cooldown)
	}
}

func TestEffectiveBenchmarkSteeringPolicyPreservesScenarioValues(t *testing.T) {
	rounds, cooldown := effectiveBenchmarkSteeringPolicy(Scenario{
		SteeringRounds:   3,
		SteeringCooldown: 45 * time.Second,
	})

	if rounds != 3 {
		t.Fatalf("rounds = %d, want 3", rounds)
	}
	if cooldown != 45*time.Second {
		t.Fatalf("cooldown = %s, want 45s", cooldown)
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
