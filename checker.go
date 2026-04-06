package main

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pre-compiled regex patterns matched against added lines in unified diffs.
var (
	pEmptyCatch  = regexp.MustCompile(`catch\s*\(\w*\)\s*\{\s*\}`)
	pExceptPass  = regexp.MustCompile(`except\s*(\s+\w+)?:\s*pass`)
	pGoIgnoreErr = regexp.MustCompile(`_\s*,?\s*=\s*\w+`)
	pSuppression = regexp.MustCompile(`@ts-ignore|@ts-expect-error|eslint-disable|#\s*type:\s*ignore|nolint|NOSONAR|@SuppressWarnings|#\s*nosec`)
	pHunkHeader  = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
)

// PatternFinding records a single pattern match inside a diff.
type PatternFinding struct {
	File    string
	Line    int    // line number in the new file
	Pattern string // human-readable description
}

// ScanDiffPatterns scans a unified diff and returns all pattern findings on added lines.
func ScanDiffPatterns(rawDiff string) []PatternFinding {
	var findings []PatternFinding
	var currentFile string
	var newLineNum int

	for _, line := range strings.Split(rawDiff, "\n") {
		// Track current file from "+++ b/<path>" lines.
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
			newLineNum = 0
			continue
		}

		// Track hunk start line numbers.
		if strings.HasPrefix(line, "@@") {
			newLineNum = parseHunkNewStart(line)
			continue
		}

		// Skip file header lines.
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		// Added line.
		if strings.HasPrefix(line, "+") {
			content := line[1:] // strip leading "+"

			if pEmptyCatch.MatchString(content) {
				findings = append(findings, PatternFinding{
					File:    currentFile,
					Line:    newLineNum,
					Pattern: "empty catch block",
				})
			}
			if pExceptPass.MatchString(content) {
				findings = append(findings, PatternFinding{
					File:    currentFile,
					Line:    newLineNum,
					Pattern: "bare except: pass",
				})
			}
			if pGoIgnoreErr.MatchString(content) && isLikelyErrorIgnore(content) {
				findings = append(findings, PatternFinding{
					File:    currentFile,
					Line:    newLineNum,
					Pattern: "ignored error (Go)",
				})
			}
			if pSuppression.MatchString(content) {
				findings = append(findings, PatternFinding{
					File:    currentFile,
					Line:    newLineNum,
					Pattern: "lint/type suppression",
				})
			}

			newLineNum++
			continue
		}

		// Removed line — do NOT increment newLineNum.
		if strings.HasPrefix(line, "-") {
			continue
		}

		// Context line — increment newLineNum.
		if newLineNum > 0 {
			newLineNum++
		}
	}

	return findings
}

// isLikelyErrorIgnore returns true when a Go line looks like an ignored error
// assignment (trimmed line starts with "_" and contains "err" or "Err").
func isLikelyErrorIgnore(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "_") && (strings.Contains(trimmed, "err") || strings.Contains(trimmed, "Err"))
}

// ScanDeletedTests parses `git diff --name-status` output and returns filenames
// of deleted test files.
func ScanDeletedTests(nameStatus string) []string {
	var deleted []string
	for _, line := range strings.Split(nameStatus, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Match "D\t<filename>" or "D <filename>".
		var filename string
		if strings.HasPrefix(line, "D\t") {
			filename = strings.TrimPrefix(line, "D\t")
		} else if strings.HasPrefix(line, "D ") {
			filename = strings.TrimSpace(strings.TrimPrefix(line, "D "))
		} else {
			continue
		}

		lower := strings.ToLower(filename)
		if strings.Contains(lower, "test") || strings.Contains(lower, "spec") || strings.HasSuffix(lower, "_test.go") {
			deleted = append(deleted, filename)
		}
	}
	return deleted
}

// BuildResult holds the outcome of a build command run.
type BuildResult struct {
	OK         bool
	ErrorCount int
	Output     string
}

// RunBuildCheck runs buildCmd in projectDir with a 30-second timeout.
// It captures combined stdout+stderr, reports success/failure, and counts error lines.
func RunBuildCheck(projectDir string, buildCmd string) BuildResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	parts := strings.Fields(buildCmd)
	var cmd *exec.Cmd
	if len(parts) == 0 {
		return BuildResult{OK: false, ErrorCount: 1, Output: "empty build command"}
	}
	cmd = exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = projectDir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := buf.String()

	if err == nil {
		return BuildResult{OK: true, ErrorCount: 0, Output: output}
	}

	// Count lines containing "error" (case-insensitive) but exclude "0 error".
	errorCount := 0
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") && !strings.Contains(lower, "0 error") {
			errorCount++
		}
	}
	if errorCount == 0 {
		errorCount = 1
	}

	return BuildResult{OK: false, ErrorCount: errorCount, Output: output}
}

// parseHunkNewStart extracts the new-file start line number from a hunk header
// like "@@ -10,5 +20,8 @@". Returns 0 if parsing fails.
func parseHunkNewStart(hunkLine string) int {
	matches := pHunkHeader.FindStringSubmatch(hunkLine)
	if len(matches) < 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}

// ShouldRunBuild returns true if any changed file ends with a configured extension.
// If no extensions are configured, it always returns true.
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
