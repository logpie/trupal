package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the parsed .trupal.toml configuration.
type Config struct {
	BuildCmd              string
	BuildExtensions       []string
	PollInterval          int
	SessionProvider       string // watched session provider: "claude" or "codex"
	BrainProvider         string // brain provider: "claude" or "codex"
	BrainModel            string // claude: haiku/sonnet/opus, codex: model id or empty for default
	BrainEffort           string // "low", "medium", "high", "max"
	BrainReplayPath       string // replay script for deterministic brain responses
	BenchmarkMode         bool
	BenchmarkScenario     string
	BenchmarkArm          string
	BenchmarkSteeringMode string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval:    3,
		SessionProvider: ProviderClaude,
		BrainProvider:   ProviderClaude,
	}
}

// loadConfig reads <projectDir>/.trupal.toml and returns a Config.
// If the file does not exist, DefaultConfig is returned with no error.
func loadConfig(projectDir string) (Config, error) {
	cfg := DefaultConfig()

	f, err := os.Open(filepath.Join(projectDir, ".trupal.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			applyConfigEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := parseTomlLine(line)
		if !ok {
			continue
		}

		switch key {
		case "build_cmd":
			cfg.BuildCmd = value
		case "build_extensions":
			cfg.BuildExtensions = parseTomlArray(value)
		case "poll_interval":
			n, err := strconv.Atoi(value)
			if err == nil {
				cfg.PollInterval = n
			}
		case "session_provider":
			cfg.SessionProvider = strings.ToLower(strings.TrimSpace(value))
		case "brain_provider":
			cfg.BrainProvider = strings.ToLower(strings.TrimSpace(value))
		case "brain_model":
			cfg.BrainModel = value
		case "brain_effort":
			cfg.BrainEffort = value
		case "brain_replay_path":
			cfg.BrainReplayPath = value
		case "benchmark_mode":
			cfg.BenchmarkMode = strings.EqualFold(value, "true")
		case "benchmark_scenario":
			cfg.BenchmarkScenario = value
		case "benchmark_arm":
			cfg.BenchmarkArm = value
		case "benchmark_steering_mode":
			cfg.BenchmarkSteeringMode = strings.ToLower(strings.TrimSpace(value))
		}
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}
	applyConfigEnvOverrides(&cfg)

	return cfg, nil
}

func applyConfigEnvOverrides(cfg *Config) {
	if replayPath := strings.TrimSpace(os.Getenv("TRUPAL_BRAIN_REPLAY_PATH")); replayPath != "" {
		cfg.BrainProvider = ProviderReplay
		cfg.BrainReplayPath = replayPath
	}
}

func (cfg Config) String() string {
	parts := []string{}
	if cfg.BuildCmd != "" {
		parts = append(parts, "build="+cfg.BuildCmd)
	}
	parts = append(parts, "session="+cfg.SessionProvider)
	brainModel := cfg.BrainModel
	if brainModel == "" {
		switch cfg.BrainProvider {
		case ProviderReplay:
			if cfg.BrainReplayPath != "" {
				brainModel = filepath.Base(cfg.BrainReplayPath)
			} else {
				brainModel = "script"
			}
		default:
			brainModel = "default"
		}
	}
	parts = append(parts, "brain="+cfg.BrainProvider+"/"+brainModel)
	parts = append(parts, "effort="+cfg.BrainEffort)
	parts = append(parts, fmt.Sprintf("poll=%ds", cfg.PollInterval))
	return strings.Join(parts, " ")
}

func (cfg Config) BrainIdentityDisplay() string {
	provider := normalizeProvider(cfg.BrainProvider, DefaultConfig().BrainProvider)
	model := strings.TrimSpace(cfg.BrainModel)
	switch provider {
	case ProviderClaude:
		if model == "" {
			model = "sonnet"
		}
	case ProviderCodex:
		if model == "" {
			model = "auto"
		}
	case ProviderReplay:
		if cfg.BrainReplayPath != "" {
			model = filepath.Base(cfg.BrainReplayPath)
		}
		if model == "" {
			model = "script"
		}
	default:
		if model == "" {
			model = "default"
		}
	}
	return provider + "/" + model
}

func ReloadConfig(projectDir string) Config {
	cfg, err := loadConfig(projectDir)
	if err != nil {
		Debugf("[config] failed to reload config: %v", err)
		return DefaultConfig()
	}
	return cfg
}

func SaveConfig(projectDir string, cfg Config) {
	path := filepath.Join(projectDir, ".trupal.toml")
	f, err := os.Create(path)
	if err != nil {
		Debugf("[config] failed to save config: %v", err)
		return
	}
	defer f.Close()
	if cfg.BuildCmd != "" {
		fmt.Fprintf(f, "build_cmd = %q\n", cfg.BuildCmd)
	}
	if len(cfg.BuildExtensions) > 0 {
		fmt.Fprintf(f, "build_extensions = [%s]\n", formatQuotedArray(cfg.BuildExtensions))
	}
	fmt.Fprintf(f, "poll_interval = %d\n", cfg.PollInterval)
	fmt.Fprintf(f, "session_provider = %q\n", cfg.SessionProvider)
	fmt.Fprintf(f, "brain_provider = %q\n", cfg.BrainProvider)
	if cfg.BrainModel != "" {
		fmt.Fprintf(f, "brain_model = %q\n", cfg.BrainModel)
	}
	fmt.Fprintf(f, "brain_effort = %q\n", cfg.BrainEffort)
	if cfg.BrainReplayPath != "" {
		fmt.Fprintf(f, "brain_replay_path = %q\n", cfg.BrainReplayPath)
	}
	if cfg.BenchmarkMode {
		fmt.Fprintf(f, "benchmark_mode = true\n")
	}
	if cfg.BenchmarkScenario != "" {
		fmt.Fprintf(f, "benchmark_scenario = %q\n", cfg.BenchmarkScenario)
	}
	if cfg.BenchmarkArm != "" {
		fmt.Fprintf(f, "benchmark_arm = %q\n", cfg.BenchmarkArm)
	}
	if cfg.BenchmarkSteeringMode != "" {
		fmt.Fprintf(f, "benchmark_steering_mode = %q\n", cfg.BenchmarkSteeringMode)
	}
}

// Validate normalizes and validates config values that must match runtime support.
func (cfg *Config) Validate() error {
	defaults := DefaultConfig()

	if cfg.PollInterval < 1 || cfg.PollInterval > 60 {
		return fmt.Errorf("poll_interval must be between 1 and 60 seconds")
	}

	cfg.SessionProvider = normalizeProvider(cfg.SessionProvider, defaults.SessionProvider)
	cfg.BrainProvider = normalizeProvider(cfg.BrainProvider, defaults.BrainProvider)
	switch cfg.SessionProvider {
	case ProviderClaude, ProviderCodex:
	default:
		return fmt.Errorf("unsupported session_provider %q (supported: claude, codex)", cfg.SessionProvider)
	}

	if cfg.BrainProvider == "" {
		cfg.BrainProvider = defaults.BrainProvider
	}
	cfg.BrainModel = strings.ToLower(strings.TrimSpace(cfg.BrainModel))
	cfg.BrainEffort = strings.ToLower(strings.TrimSpace(cfg.BrainEffort))
	cfg.BenchmarkSteeringMode = strings.ToLower(strings.TrimSpace(cfg.BenchmarkSteeringMode))
	if cfg.BrainEffort == "" {
		cfg.BrainEffort = "high"
	}
	if cfg.BenchmarkSteeringMode != "" {
		switch cfg.BenchmarkSteeringMode {
		case "single", "continuous":
		default:
			return fmt.Errorf("unsupported benchmark_steering_mode %q (supported: single, continuous)", cfg.BenchmarkSteeringMode)
		}
	}

	switch cfg.BrainProvider {
	case ProviderClaude:
		if cfg.BrainModel == "" {
			cfg.BrainModel = "sonnet"
		}
		if IsLikelyCodexModel(cfg.BrainModel) {
			return fmt.Errorf("brain_provider %q conflicts with brain_model %q (looks like a Codex/OpenAI model)", cfg.BrainProvider, cfg.BrainModel)
		}
		if !IsValidModel(cfg.BrainModel) {
			return fmt.Errorf("unsupported brain_model %q (supported: haiku, sonnet, opus)", cfg.BrainModel)
		}
		if !IsValidEffort(cfg.BrainEffort) {
			return fmt.Errorf("unsupported brain_effort %q (supported: low, medium, high, max)", cfg.BrainEffort)
		}
		return nil
	case ProviderCodex:
		if IsValidModel(cfg.BrainModel) {
			return fmt.Errorf("brain_provider %q conflicts with brain_model %q (looks like a Claude model)", cfg.BrainProvider, cfg.BrainModel)
		}
		if !IsValidEffort(cfg.BrainEffort) {
			return fmt.Errorf("unsupported brain_effort %q (supported: low, medium, high, max)", cfg.BrainEffort)
		}
		return nil
	case ProviderReplay:
		cfg.BrainReplayPath = strings.TrimSpace(cfg.BrainReplayPath)
		if cfg.BrainReplayPath == "" {
			return fmt.Errorf("brain_provider %q requires brain_replay_path", cfg.BrainProvider)
		}
		return nil
	default:
		return fmt.Errorf("unsupported brain_provider %q (supported: claude, codex, replay)", cfg.BrainProvider)
	}
}

// parseTomlLine splits a line on the first '=' and returns the trimmed key and
// raw value. For string values the surrounding quotes are stripped.
// Returns ok=false if the line cannot be parsed.
func parseTomlLine(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	raw := strings.TrimSpace(stripTomlComment(line[idx+1:]))
	if key == "" {
		return "", "", false
	}

	// Array value — return as-is for parseTomlArray.
	if strings.HasPrefix(raw, "[") {
		return key, raw, true
	}

	// Quoted string value — strip surrounding quotes.
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return key, raw[1 : len(raw)-1], true
	}

	// Bare value (e.g. integers).
	return key, raw, true
}

func stripTomlComment(raw string) string {
	inSingle := false
	inDouble := false
	escaped := false

	for i, r := range raw {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inDouble:
			escaped = true
		case r == '"' && !inSingle:
			inDouble = !inDouble
		case r == '\'' && !inDouble:
			inSingle = !inSingle
		case r == '#' && !inSingle && !inDouble:
			return strings.TrimSpace(raw[:i])
		}
	}

	return strings.TrimSpace(raw)
}

// parseTomlArray parses a TOML inline array of strings, e.g. [".ts", ".tsx"].
// Non-string elements and malformed input are silently skipped.
func parseTomlArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			result = append(result, part[1:len(part)-1])
		}
	}
	return result
}

func formatQuotedArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Quote(value))
	}
	return strings.Join(parts, ", ")
}
