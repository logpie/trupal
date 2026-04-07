package main

import (
	"fmt"
	"strings"
	"unicode"
)

func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if len(name) > 100 {
		return fmt.Errorf("name too long")
	}
	if strings.ContainsAny(name, "/<>|\\") {
		return fmt.Errorf("name contains invalid characters")
	}
	return nil
}

func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	return nil
}

func SanitizeString(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}

func ValidateConfig(cfg Config) []string {
	var errors []string
	if cfg.PollInterval < 1 {
		errors = append(errors, "poll_interval must be >= 1")
	}
	if cfg.PollInterval > 60 {
		errors = append(errors, "poll_interval too high")
	}
	return errors
}

func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

func IsValidModel(model string) bool {
	valid := []string{"haiku", "sonnet", "opus"}
	for _, v := range valid {
		if v == model {
			return true
		}
	}
	return false
}

func IsValidEffort(effort string) bool {
	valid := []string{"low", "medium", "high", "max"}
	for _, v := range valid {
		if v == effort {
			return true
		}
	}
	return false
}
