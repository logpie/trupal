package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseScenarioYAMLSupportsBenchmarkArmsAndSteeringPolicy(t *testing.T) {
	scenario, err := parseScenarioYAML([]byte(`
id: sample
name: sample
category: api
timeout: 2m
agent_model: gpt-5.4-mini
benchmark_arms: control, steer
steering_mode: continuous
steering_rounds: 2
steering_cooldown: 45s
trupal_config:
  session_provider: codex
  brain_provider: codex
`))
	if err != nil {
		t.Fatalf("parseScenarioYAML() error = %v", err)
	}
	if got := scenario.BenchmarkArms; len(got) != 2 || got[0] != ArmControl || got[1] != ArmSteer {
		t.Fatalf("BenchmarkArms = %#v, want [control steer]", got)
	}
	if scenario.SteeringRounds != 2 {
		t.Fatalf("SteeringRounds = %d, want 2", scenario.SteeringRounds)
	}
	if scenario.SteeringMode != SteeringModeContinuous {
		t.Fatalf("SteeringMode = %q, want continuous", scenario.SteeringMode)
	}
	if scenario.SteeringCooldown.String() != "45s" {
		t.Fatalf("SteeringCooldown = %s, want 45s", scenario.SteeringCooldown)
	}
}

func TestLoadScenarioDirDefaultsBenchmarkPolicy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scenario.yaml"), []byte(`
id: sample
name: sample
category: api
timeout: 2m
agent_model: gpt-5.4-mini
trupal_config:
  session_provider: codex
  brain_provider: codex
`), 0644); err != nil {
		t.Fatalf("WriteFile(scenario.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte("fix it"), 0644); err != nil {
		t.Fatalf("WriteFile(task.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "truth.json"), []byte(`{"bugs":[],"false_positive_traps":[]}`), 0644); err != nil {
		t.Fatalf("WriteFile(truth.json) error = %v", err)
	}

	scenario, err := loadScenarioDir(dir)
	if err != nil {
		t.Fatalf("loadScenarioDir() error = %v", err)
	}
	if got := scenario.EffectiveBenchmarkArms(); len(got) != 1 || got[0] != ArmControl {
		t.Fatalf("EffectiveBenchmarkArms() = %#v, want [control]", got)
	}
	if scenario.SteeringRounds != 1 {
		t.Fatalf("SteeringRounds = %d, want 1", scenario.SteeringRounds)
	}
	if scenario.SteeringMode != SteeringModeSingle {
		t.Fatalf("SteeringMode = %q, want single", scenario.SteeringMode)
	}
	if scenario.SteeringCooldown.String() != "30s" {
		t.Fatalf("SteeringCooldown = %s, want 30s", scenario.SteeringCooldown)
	}
}
