package bench

import (
	"strings"
	"testing"
)

func TestBenchmarkAgentPromptForcesNonInteractiveExecution(t *testing.T) {
	prompt := benchmarkAgentPrompt("Implement the API")

	for _, want := range []string{
		"Do not ask clarifying questions.",
		"Do not stop at planning, brainstorming, or design.",
		"implement the task end-to-end",
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
