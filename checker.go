package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type PatternFinding struct {
	Key      string
	Level    string
	Message  string
	File     string
	Line     int
	Category string
}

var (
	diffHunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	todoPattern           = regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK)\b`)
	suppressionPattern    = regexp.MustCompile(`(?i)(@ts-ignore|eslint-disable|nolint|type:\s*ignore|pyright:\s*ignore|noqa)\b`)
	swallowedErrorPattern = regexp.MustCompile(`\b_\s*=\s*err\b`)
)

var patternScanExtensions = map[string]bool{
	".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true,
	".swift": true, ".scala": true, ".sh": true, ".bash": true, ".zsh": true,
	".c": true, ".cc": true, ".cpp": true, ".h": true, ".hpp": true, ".cs": true,
	".php": true,
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

// ShouldRunBuild checks if any changed or untracked file matches the configured build extensions.
func ShouldRunBuild(changedFiles, untrackedFiles []string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	for _, files := range [][]string{changedFiles, untrackedFiles} {
		for _, f := range files {
			for _, ext := range extensions {
				if strings.HasSuffix(f, ext) {
					return true
				}
			}
		}
	}
	return false
}

func ParseBuildErrors(output string) []string {
	var errors []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), "error") {
			errors = append(errors, line)
		}
	}
	return errors
}

// ScanDiffPatterns checks added diff lines for high-precision trust regressions.
func ScanDiffPatterns(rawDiff string) []PatternFinding {
	if strings.TrimSpace(rawDiff) == "" {
		return nil
	}

	var findings []PatternFinding
	seen := make(map[string]bool)
	var currentFile string
	currentLine := 0
	lineKnown := false

	addFinding := func(level, category, message, file string, line int) {
		key := strings.TrimSpace(file) + ":" + strconv.Itoa(line) + ":" + category
		if seen[key] {
			return
		}
		seen[key] = true
		findings = append(findings, PatternFinding{
			Key:      key,
			Level:    level,
			Message:  message,
			File:     strings.TrimSpace(file),
			Line:     line,
			Category: category,
		})
	}

	for _, line := range strings.Split(rawDiff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			currentFile = ""
			currentLine = 0
			lineKnown = false
		case strings.HasPrefix(line, "+++ b/"):
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "@@"):
			matches := diffHunkHeaderPattern.FindStringSubmatch(line)
			if len(matches) == 2 {
				if start, err := strconv.Atoi(matches[1]); err == nil {
					currentLine = start
					lineKnown = true
				}
			}
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added := line[1:]
			if !shouldPatternScanFile(currentFile) {
				if lineKnown {
					currentLine++
				}
				continue
			}
			lineNo := currentLine
			if lineKnown {
				currentLine++
			}
			location := formatPatternLocation(currentFile, lineNo)
			codePart, commentPart := splitCodeAndComment(added)
			unquotedLine := stripQuotedContent(added)
			unquotedCode := stripQuotedContent(codePart)
			if match := todoPattern.FindString(unquotedLine); match != "" {
				addFinding("warn", "todo", strings.ToUpper(match)+" introduced ("+location+")", currentFile, lineNo)
			}
			if hasSuppressionDirective(unquotedCode, commentPart) {
				addFinding("warn", "suppression", "lint/type suppression introduced ("+location+")", currentFile, lineNo)
			}
			if swallowedErrorPattern.FindString(unquotedCode) != "" {
				addFinding("error", "swallowed-error", "error swallowed with `_ = err` ("+location+")", currentFile, lineNo)
			}
		case strings.HasPrefix(line, " ") && lineKnown:
			currentLine++
		}
	}

	return findings
}

func formatPatternLocation(file string, line int) string {
	file = strings.TrimSpace(file)
	if file == "" {
		file = "diff"
	}
	if line <= 0 {
		return file
	}
	return file + ":+" + strconv.Itoa(line)
}

func shouldPatternScanFile(file string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(file)))
	return patternScanExtensions[ext]
}

func splitCodeAndComment(line string) (code, comment string) {
	inSingle := false
	inDouble := false
	inBacktick := false
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case escaped:
			escaped = false
		case ch == '\\' && (inSingle || inDouble):
			escaped = true
		case ch == '\'' && !inDouble && !inBacktick:
			inSingle = !inSingle
		case ch == '"' && !inSingle && !inBacktick:
			inDouble = !inDouble
		case ch == '`' && !inSingle && !inDouble:
			inBacktick = !inBacktick
		case !inSingle && !inDouble && !inBacktick && i+1 < len(line) && line[i:i+2] == "//":
			return line[:i], line[i:]
		case !inSingle && !inDouble && !inBacktick && ch == '#':
			return line[:i], line[i:]
		}
	}
	return line, ""
}

func stripQuotedContent(line string) string {
	var b strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case escaped:
			escaped = false
			if !inSingle && !inDouble && !inBacktick {
				b.WriteByte(ch)
			}
		case ch == '\\' && (inSingle || inDouble):
			escaped = true
		case ch == '\'' && !inDouble && !inBacktick:
			inSingle = !inSingle
		case ch == '"' && !inSingle && !inBacktick:
			inDouble = !inDouble
		case ch == '`' && !inSingle && !inDouble:
			inBacktick = !inBacktick
		default:
			if !inSingle && !inDouble && !inBacktick {
				b.WriteByte(ch)
			}
		}
	}

	return b.String()
}

func hasSuppressionDirective(codePart, commentPart string) bool {
	if suppressionPattern.FindString(codePart) != "" {
		return true
	}
	comment := strings.TrimSpace(strings.TrimLeft(commentPart, "/# \t"))
	if comment == "" {
		return false
	}
	return suppressionPattern.FindString(comment) != ""
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

func FormatErrors(errors []string) string {
	result := ""
	for _, e := range errors {
		result += e + "\n"
	}
	return result
}
