package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Scenario struct {
	ID               string
	Name             string
	Category         string
	Timeout          time.Duration
	ClaudeModel      string
	AgentModel       string
	BenchmarkArms    []BenchmarkArm
	SteeringRounds   int
	SteeringCooldown time.Duration
	TrupalConfig     TrupalConfig

	RootDir     string
	ScenarioYML string
	TaskPath    string
	TemplateDir string
	TruthPath   string

	TaskPrompt string
	Truth      GroundTruth
}

type TrupalConfig struct {
	SessionProvider string
	BrainProvider   string
	BrainModel      string
	BrainEffort     string
	BuildCmd        string
}

type BenchmarkArm string

const (
	ArmControl BenchmarkArm = "control"
	ArmSteer   BenchmarkArm = "steer"
)

type GroundTruth struct {
	Bugs               []TruthBug          `json:"bugs"`
	FalsePositiveTraps []FalsePositiveTrap `json:"false_positive_traps"`
}

type TruthBug struct {
	ID          string `json:"id"`
	File        string `json:"file"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
}

type FalsePositiveTrap struct {
	Description string `json:"description"`
}

func normalizeBenchProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "claude"
	}
	return provider
}

func (s Scenario) SessionProvider() string {
	return normalizeBenchProvider(s.TrupalConfig.SessionProvider)
}

func (s Scenario) EffectiveAgentModel() string {
	if strings.TrimSpace(s.AgentModel) != "" {
		return strings.TrimSpace(s.AgentModel)
	}
	return strings.TrimSpace(s.ClaudeModel)
}

func (s Scenario) EffectiveBenchmarkArms() []BenchmarkArm {
	if len(s.BenchmarkArms) == 0 {
		return []BenchmarkArm{ArmControl}
	}
	return append([]BenchmarkArm(nil), s.BenchmarkArms...)
}

func LoadScenario(rootDir, name string) (Scenario, error) {
	if strings.TrimSpace(name) == "" {
		return Scenario{}, fmt.Errorf("scenario name is required")
	}

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenarios dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(rootDir, entry.Name())
		scenario, err := loadScenarioDir(dir)
		if err != nil {
			return Scenario{}, err
		}
		if scenario.ID == name || entry.Name() == name {
			return scenario, nil
		}
	}

	return Scenario{}, fmt.Errorf("scenario %q not found under %s", name, rootDir)
}

func LoadAllScenarios(rootDir string) ([]Scenario, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, fmt.Errorf("read scenarios dir: %w", err)
	}

	var scenarios []Scenario
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scenario, err := loadScenarioDir(filepath.Join(rootDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, scenario)
	}

	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].ID < scenarios[j].ID
	})
	return scenarios, nil
}

func loadScenarioDir(dir string) (Scenario, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "scenario.yaml"))
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario yaml in %s: %w", dir, err)
	}

	scenario, err := parseScenarioYAML(raw)
	if err != nil {
		return Scenario{}, fmt.Errorf("parse scenario yaml in %s: %w", dir, err)
	}

	scenario.RootDir = dir
	scenario.ScenarioYML = filepath.Join(dir, "scenario.yaml")
	scenario.TaskPath = filepath.Join(dir, "task.md")
	scenario.TemplateDir = filepath.Join(dir, "template")
	scenario.TruthPath = filepath.Join(dir, "truth.json")

	task, err := os.ReadFile(scenario.TaskPath)
	if err != nil {
		return Scenario{}, fmt.Errorf("read task.md for %s: %w", scenario.ID, err)
	}
	scenario.TaskPrompt = string(task)

	truthRaw, err := os.ReadFile(scenario.TruthPath)
	if err != nil {
		return Scenario{}, fmt.Errorf("read truth.json for %s: %w", scenario.ID, err)
	}
	if err := json.Unmarshal(truthRaw, &scenario.Truth); err != nil {
		return Scenario{}, fmt.Errorf("parse truth.json for %s: %w", scenario.ID, err)
	}

	if scenario.Timeout <= 0 {
		return Scenario{}, fmt.Errorf("scenario %s has invalid timeout", scenario.ID)
	}
	if scenario.ID == "" {
		return Scenario{}, fmt.Errorf("scenario in %s is missing id", dir)
	}
	if scenario.EffectiveAgentModel() == "" {
		return Scenario{}, fmt.Errorf("scenario %s is missing agent model", scenario.ID)
	}
	if len(scenario.BenchmarkArms) == 0 {
		scenario.BenchmarkArms = []BenchmarkArm{ArmControl}
	}
	for _, arm := range scenario.BenchmarkArms {
		switch arm {
		case ArmControl, ArmSteer:
		default:
			return Scenario{}, fmt.Errorf("scenario %s has unsupported benchmark arm %q", scenario.ID, arm)
		}
	}
	if scenario.SteeringRounds <= 0 {
		scenario.SteeringRounds = 1
	}
	if scenario.SteeringCooldown <= 0 {
		scenario.SteeringCooldown = 30 * time.Second
	}

	return scenario, nil
}

func parseScenarioYAML(raw []byte) (Scenario, error) {
	var scenario Scenario
	section := ""

	for lineNo, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(trimmed, ":")
			if indent == 0 && key == "trupal_config" {
				section = key
				continue
			}
			return Scenario{}, fmt.Errorf("unsupported YAML section %q on line %d", key, lineNo+1)
		}

		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return Scenario{}, fmt.Errorf("invalid YAML line %d: %q", lineNo+1, trimmed)
		}
		key := strings.TrimSpace(parts[0])
		value := parseYAMLScalar(parts[1])

		if indent == 0 {
			section = ""
			switch key {
			case "id":
				scenario.ID = value
			case "name":
				scenario.Name = value
			case "category":
				scenario.Category = value
			case "timeout":
				d, err := time.ParseDuration(value)
				if err != nil {
					return Scenario{}, fmt.Errorf("parse timeout %q: %w", value, err)
				}
				scenario.Timeout = d
			case "claude_model":
				scenario.ClaudeModel = value
			case "codex_model", "agent_model":
				scenario.AgentModel = value
			case "benchmark_arms":
				scenario.BenchmarkArms = parseBenchmarkArms(value)
			case "steering_rounds":
				var rounds int
				if _, err := fmt.Sscanf(value, "%d", &rounds); err != nil {
					return Scenario{}, fmt.Errorf("parse steering_rounds %q: %w", value, err)
				}
				scenario.SteeringRounds = rounds
			case "steering_cooldown":
				d, err := time.ParseDuration(value)
				if err != nil {
					return Scenario{}, fmt.Errorf("parse steering_cooldown %q: %w", value, err)
				}
				scenario.SteeringCooldown = d
			default:
				return Scenario{}, fmt.Errorf("unsupported scenario key %q on line %d", key, lineNo+1)
			}
			continue
		}

		if section != "trupal_config" {
			return Scenario{}, fmt.Errorf("unexpected nested key %q on line %d", key, lineNo+1)
		}
		switch key {
		case "session_provider":
			scenario.TrupalConfig.SessionProvider = value
		case "brain_provider":
			scenario.TrupalConfig.BrainProvider = value
		case "brain_model":
			scenario.TrupalConfig.BrainModel = value
		case "brain_effort":
			scenario.TrupalConfig.BrainEffort = value
		case "build_cmd":
			scenario.TrupalConfig.BuildCmd = value
		default:
			return Scenario{}, fmt.Errorf("unsupported trupal_config key %q on line %d", key, lineNo+1)
		}
	}

	return scenario, nil
}

func parseYAMLScalar(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, "\r")
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			return strings.ReplaceAll(value[1:len(value)-1], `\"`, `"`)
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func parseBenchmarkArms(raw string) []BenchmarkArm {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	arms := make([]BenchmarkArm, 0, len(parts))
	for _, part := range parts {
		arm := BenchmarkArm(strings.ToLower(strings.TrimSpace(part)))
		if arm == "" {
			continue
		}
		arms = append(arms, arm)
	}
	return arms
}
