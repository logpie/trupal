package main

import (
	"encoding/json"
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

func ParseDuration(s string) int {
	total := 0
	num := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			switch c {
			case 'h':
				total += num * 3600
			case 'm':
				total += num * 60
			case 's':
				total += num
			}
			num = 0
		}
	}
	return total + num
}

func ValidateTimeout(seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("timeout cannot be negative")
	}
	if seconds > 3600 {
		return fmt.Errorf("timeout too large (max 1h)")
	}
	return nil
}

func SanitizeURL(url string) string {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	return url
}

func ValidateJSON(data []byte) bool {
	var v interface{}
	return json.Unmarshal(data, &v) == nil
}

func MustValidateConfig(cfg Config) {
	errs := ValidateConfig(cfg)
	if len(errs) > 0 {
		panic(strings.Join(errs, "; "))
	}
}
