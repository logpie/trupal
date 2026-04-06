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

// Pre-compiled single-line patterns matched against added lines.
var (
	pSuppression = regexp.MustCompile(
		`@ts-ignore|@ts-expect-error|eslint-disable|#\s*type:\s*ignore|nolint|NOSONAR|@SuppressWarnings|#\s*nosec`)
)

// PatternFinding records a single pattern match inside a diff.
type PatternFinding struct {
	File    string
	Line    int    // line number in the new file
	Pattern string // human-readable description
}

// ScanDiffPatterns scans a unified diff for suspicious patterns.
// Uses both single-line matching and multi-line context window.
func ScanDiffPatterns(rawDiff string) []PatternFinding {
	var findings []PatternFinding
	var currentFile string
	var newLineNum int

	// Collect added lines with context for multi-line detection.
	var addedLines []addedLine

	for _, line := range strings.Split(rawDiff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
			newLineNum = 0
			continue
		}

		if strings.HasPrefix(line, "@@") {
			newLineNum = parseHunkNewStart(line)
			continue
		}

		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		if strings.HasPrefix(line, "+") {
			content := line[1:]

			// Single-line: lint/type suppression.
			if pSuppression.MatchString(content) {
				findings = append(findings, PatternFinding{
					File:    currentFile,
					Line:    newLineNum,
					Pattern: "lint/type suppression",
				})
			}

			addedLines = append(addedLines, addedLine{currentFile, newLineNum, content})
			newLineNum++
		} else if !strings.HasPrefix(line, "-") {
			// Context line.
			newLineNum++
		}
	}

	// Multi-line: detect error swallowing patterns across consecutive added lines.
	findings = append(findings, detectErrorSwallowing(addedLines)...)

	return findings
}

// detectErrorSwallowing finds multi-line error swallowing patterns:
//   - Python: except ... : \n pass (or return None)
//   - JS/TS/Java: catch(...) { } with empty body
//   - Go: _, err := ...; _ = err  or just _ = err
func detectErrorSwallowing(lines []addedLine) []PatternFinding {
	var findings []PatternFinding

	for i, line := range lines {
		trimmed := strings.TrimSpace(line.content)

		// Python: "except" line followed by "pass" or "return None" with nothing else.
		if strings.HasPrefix(trimmed, "except") && strings.HasSuffix(trimmed, ":") {
			// Look at next non-empty added line in same file.
			if next := nextNonEmptyInFile(lines, i); next != nil {
				nextTrimmed := strings.TrimSpace(next.content)
				if nextTrimmed == "pass" || nextTrimmed == "return None" || nextTrimmed == "return" {
					findings = append(findings, PatternFinding{
						File:    line.file,
						Line:    line.lineNum,
						Pattern: "except → " + nextTrimmed,
					})
				}
			}
		}

		// JS/TS/Java: catch block. Look for "catch" line, then "}" with nothing between.
		if (strings.Contains(trimmed, "catch") && strings.HasSuffix(trimmed, "{")) ||
			(strings.Contains(trimmed, "catch") && strings.Contains(trimmed, "{")) {
			if next := nextNonEmptyInFile(lines, i); next != nil {
				nextTrimmed := strings.TrimSpace(next.content)
				if nextTrimmed == "}" || nextTrimmed == "} catch" {
					findings = append(findings, PatternFinding{
						File:    line.file,
						Line:    line.lineNum,
						Pattern: "empty catch block",
					})
				}
			}
		}

		// Go: _ = err or _, _ = someFunc()
		if strings.HasPrefix(trimmed, "_") {
			if strings.Contains(trimmed, "err") || strings.Contains(trimmed, "Err") {
				findings = append(findings, PatternFinding{
					File:    line.file,
					Line:    line.lineNum,
					Pattern: "error ignored (Go)",
				})
			}
		}
	}

	return findings
}

type addedLine struct {
	file    string
	lineNum int
	content string
}

// nextNonEmptyInFile returns the next non-empty added line in the same file, or nil.
func nextNonEmptyInFile(lines []addedLine, i int) *addedLine {
	file := lines[i].file
	for j := i + 1; j < len(lines); j++ {
		if lines[j].file != file {
			return nil
		}
		if strings.TrimSpace(lines[j].content) != "" {
			return &lines[j]
		}
	}
	return nil
}

// ScanDeletedTests checks git diff --name-status output for deleted test files.
func ScanDeletedTests(nameStatus string) []string {
	var deleted []string
	for _, line := range strings.Split(nameStatus, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 {
			continue
		}
		if line[0] != 'D' {
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

	parts := strings.Fields(buildCmd)
	if len(parts) == 0 {
		return BuildResult{OK: true}
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
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

// parseHunkNewStart extracts the new-file start line from a hunk header.
func parseHunkNewStart(hunkLine string) int {
	re := regexp.MustCompile(`\+(\d+)`)
	m := re.FindStringSubmatch(hunkLine)
	if len(m) < 2 {
		return 1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 1
	}
	return n
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
