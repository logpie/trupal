package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// BuildResult holds the output of a build check.
type BuildResult struct {
	OK         bool
	ErrorCount int
	Output     string
}

// RunBuildCheck runs the configured build command and counts errors.
func RunBuildCheck(projectDir string, buildCmd string) BuildResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run through shell for proper parsing of quotes, pipes, env vars.
	cmd := exec.CommandContext(ctx, "sh", "-c", buildCmd)
	cmd.Dir = projectDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := stdout.String() + stderr.String()

	if err == nil {
		return BuildResult{OK: true, Output: combined}
	}

	errorCount := 0
	for _, line := range strings.Split(combined, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") && !strings.Contains(lower, "0 error") {
			errorCount++
		}
	}
	if errorCount == 0 {
		errorCount = 1
	}

	return BuildResult{OK: false, ErrorCount: errorCount, Output: combined}
}

// ShouldRunBuild checks if any changed file matches the configured build extensions.
func ShouldRunBuild(changedFiles []string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	for _, f := range changedFiles {
		for _, ext := range extensions {
			if strings.HasSuffix(f, ext) {
				return true
			}
		}
	}
	return false
}

// ScanDeletedTests checks git diff --name-status output for deleted test files.
func ScanDeletedTests(nameStatus string) []string {
	var deleted []string
	for _, line := range strings.Split(nameStatus, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 || line[0] != 'D' {
			continue
		}
		filename := strings.TrimSpace(line[1:])
		lower := strings.ToLower(filename)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.HasSuffix(lower, "_test.go") {
			deleted = append(deleted, filename)
		}
	}
	return deleted
}
