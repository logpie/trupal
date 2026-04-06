package main

import (
	"bufio"
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
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{PollInterval: 3}
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
			if err == nil && n > 0 {
				cfg.PollInterval = n
			}
		}
	}

	return cfg
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
