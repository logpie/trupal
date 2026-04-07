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
	BuildCmd        string
	BuildExtensions []string
	PollInterval    int
	BrainProvider   string // currently only "claude"
	BrainModel      string // e.g. "sonnet", "opus", "haiku"
	BrainEffort     string // "low", "medium", "high", "max"
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval:  3,
		BrainProvider: "claude",
		BrainModel:    "sonnet",
		BrainEffort:   "high",
	}
}

// loadConfig reads <projectDir>/.trupal.toml and returns a Config.
// If the file does not exist, DefaultConfig is returned with no error.
func loadConfig(projectDir string) Config {
	cfg := DefaultConfig()

	f, err := os.Open(filepath.Join(projectDir, ".trupal.toml"))
	if err != nil {
		// File absent or unreadable — use defaults.
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
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
		case "brain_provider":
			cfg.BrainProvider = strings.ToLower(strings.TrimSpace(value))
		case "brain_model":
			cfg.BrainModel = value
		case "brain_effort":
			cfg.BrainEffort = value
		}
	}

	return cfg
}

func (cfg Config) String() string {
	parts := []string{}
	if cfg.BuildCmd != "" {
		parts = append(parts, "build="+cfg.BuildCmd)
	}
	parts = append(parts, "brain="+cfg.BrainProvider+"/"+cfg.BrainModel)
	parts = append(parts, "effort="+cfg.BrainEffort)
	parts = append(parts, fmt.Sprintf("poll=%ds", cfg.PollInterval))
	return strings.Join(parts, " ")
}

func ReloadConfig(projectDir string) Config {
	cfg := loadConfig(projectDir)
	return cfg
}

func SaveConfig(projectDir string, cfg Config) {
	path := filepath.Join(projectDir, ".trupal.toml")
	f, _ := os.Create(path)
	defer f.Close()
	fmt.Fprintf(f, "build_cmd = \"%s\"\n", cfg.BuildCmd)
	fmt.Fprintf(f, "brain_model = \"%s\"\n", cfg.BrainModel)
	fmt.Fprintf(f, "brain_effort = \"%s\"\n", cfg.BrainEffort)
}

// Validate normalizes and validates config values that must match runtime support.
func (cfg *Config) Validate() error {
	defaults := DefaultConfig()

	if cfg.PollInterval < 1 || cfg.PollInterval > 60 {
		return fmt.Errorf("poll_interval must be between 1 and 60 seconds")
	}

	cfg.BrainProvider = strings.ToLower(strings.TrimSpace(cfg.BrainProvider))
	if cfg.BrainProvider == "" {
		cfg.BrainProvider = defaults.BrainProvider
	}
	cfg.BrainModel = strings.ToLower(strings.TrimSpace(cfg.BrainModel))
	if cfg.BrainModel == "" {
		cfg.BrainModel = defaults.BrainModel
	}
	cfg.BrainEffort = strings.ToLower(strings.TrimSpace(cfg.BrainEffort))
	if cfg.BrainEffort == "" {
		cfg.BrainEffort = defaults.BrainEffort
	}

	switch cfg.BrainProvider {
	case "claude":
		if !IsValidModel(cfg.BrainModel) {
			return fmt.Errorf("unsupported brain_model %q (supported: haiku, sonnet, opus)", cfg.BrainModel)
		}
		if !IsValidEffort(cfg.BrainEffort) {
			return fmt.Errorf("unsupported brain_effort %q (supported: low, medium, high, max)", cfg.BrainEffort)
		}
		return nil
	default:
		return fmt.Errorf("unsupported brain_provider %q (supported: claude)", cfg.BrainProvider)
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
	raw := strings.TrimSpace(line[idx+1:])
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
