package bench

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ObservedFinding struct {
	Message   string
	FirstSeen time.Time
}

type EditEvent struct {
	Time  time.Time
	Files []string
	Tool  string
}

type BrainResponseEvent struct {
	Time      time.Time
	Nudges    int
	Resolved  int
	Reasoning string
}

type DebugSummary struct {
	ResponseCount     int
	ResponseEvents    []BrainResponseEvent
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	TotalCostUSD      float64
}

type MatchedFinding struct {
	Bug        TruthBug
	Finding    ObservedFinding
	MatchScore float64
	Latency    time.Duration
}

type LatencySummary struct {
	Count   int
	First   time.Duration
	Average time.Duration
	Max     time.Duration
}

type Scorecard struct {
	MatchedTruths      int
	TotalTruths        int
	DetectionRate      float64
	FalsePositiveCount int
	TrapHits           int
	ResponseCount      int
	MatchedFindings    []MatchedFinding
	UnmatchedTruths    []TruthBug
	ExtraFindings      []ObservedFinding
	Latency            LatencySummary
	InputTokens        int
	OutputTokens       int
	CacheReadTokens    int
	CacheCreateTokens  int
	TotalCostUSD       float64
}

var (
	debugUsagePattern  = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} \[brain\] usage: in=(\d+) out=(\d+) cache_read=(\d+) cache_create=(\d+) cost=\$([0-9.]+)$`)
	debugResultPattern = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2}\.\d{3}) \[brain\] (\d+) nudges, (\d+) resolved, reasoning: (.*)$`)
	logHeaderPattern   = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2})\s`)
	stopWords          = map[string]struct{}{"the": {}, "and": {}, "for": {}, "with": {}, "without": {}, "into": {}, "from": {}, "this": {}, "that": {}, "should": {}, "not": {}, "flagged": {}, "global": {}, "missing": {}, "accessed": {}, "write": {}, "reads": {}, "uses": {}, "used": {}, "file": {}, "correct": {}, "flag": {}, "under": {}, "best": {}, "effort": {}, "are": {}, "all": {}, "one": {}, "two": {}, "api": {}, "http": {}, "user": {}, "users": {}}
)

func ParseTrupalLog(path string, baseDate time.Time) ([]ObservedFinding, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var findings []ObservedFinding
	seen := make(map[string]bool)
	var currentTime time.Time

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := logHeaderPattern.FindStringSubmatch(line); len(matches) == 2 {
			currentTime = combineClock(baseDate, matches[1], false)
			continue
		}

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "⚠ ") {
			continue
		}
		message := strings.TrimSpace(strings.TrimPrefix(trimmed, "⚠ "))
		if message == "" || seen[message] {
			continue
		}
		seen[message] = true
		findings = append(findings, ObservedFinding{
			Message:   message,
			FirstSeen: currentTime,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return findings, nil
}

func ParseDebugLog(path string, baseDate time.Time) (DebugSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DebugSummary{}, nil
		}
		return DebugSummary{}, err
	}
	defer f.Close()

	var summary DebugSummary
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := debugUsagePattern.FindStringSubmatch(line); len(matches) == 6 {
			summary.InputTokens += atoi(matches[1])
			summary.OutputTokens += atoi(matches[2])
			summary.CacheReadTokens += atoi(matches[3])
			summary.CacheCreateTokens += atoi(matches[4])
			summary.TotalCostUSD += atof(matches[5])
			continue
		}
		if matches := debugResultPattern.FindStringSubmatch(line); len(matches) == 5 {
			summary.ResponseCount++
			summary.ResponseEvents = append(summary.ResponseEvents, BrainResponseEvent{
				Time:      combineClock(baseDate, matches[1], true),
				Nudges:    atoi(matches[2]),
				Resolved:  atoi(matches[3]),
				Reasoning: matches[4],
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return DebugSummary{}, err
	}
	return summary, nil
}

func ParseSessionEdits(path string) ([]EditEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	type block struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type message struct {
		Content []block `json:"content"`
	}
	type entry struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
	}

	var edits []EditEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var parsed entry
		if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
			continue
		}
		if parsed.Type != "assistant" {
			continue
		}

		var msg message
		if err := json.Unmarshal(parsed.Message, &msg); err != nil {
			continue
		}

		var files []string
		var tool string
		for _, content := range msg.Content {
			name := strings.ToLower(strings.TrimSpace(content.Name))
			if name != "edit" && name != "multiedit" && name != "write" {
				continue
			}
			tool = name
			var input struct {
				FilePath string `json:"file_path"`
				Path     string `json:"path"`
			}
			if err := json.Unmarshal(content.Input, &input); err != nil {
				continue
			}
			file := strings.TrimSpace(input.FilePath)
			if file == "" {
				file = strings.TrimSpace(input.Path)
			}
			if file != "" {
				files = append(files, filepath.Base(file))
			}
		}
		if len(files) == 0 {
			continue
		}
		timestamp, ok := parseFlexibleTime(parsed.Timestamp)
		if !ok {
			continue
		}
		edits = append(edits, EditEvent{
			Time:  timestamp,
			Files: files,
			Tool:  tool,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Slice(edits, func(i, j int) bool {
		return edits[i].Time.Before(edits[j].Time)
	})
	return edits, nil
}

func ScoreFindings(truth GroundTruth, findings []ObservedFinding, edits []EditEvent, debug DebugSummary) Scorecard {
	score := Scorecard{
		TotalTruths:       len(truth.Bugs),
		ResponseCount:     debug.ResponseCount,
		InputTokens:       debug.InputTokens,
		OutputTokens:      debug.OutputTokens,
		CacheReadTokens:   debug.CacheReadTokens,
		CacheCreateTokens: debug.CacheCreateTokens,
		TotalCostUSD:      debug.TotalCostUSD,
	}

	type candidate struct {
		bugIndex     int
		findingIndex int
		score        float64
	}

	var candidates []candidate
	for bugIdx, bug := range truth.Bugs {
		for findingIdx, finding := range findings {
			value := matchScore(bug, finding.Message)
			if value >= 0.45 {
				candidates = append(candidates, candidate{
					bugIndex:     bugIdx,
					findingIndex: findingIdx,
					score:        value,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	matchedBugs := make(map[int]bool)
	matchedFindings := make(map[int]bool)
	for _, candidate := range candidates {
		if matchedBugs[candidate.bugIndex] || matchedFindings[candidate.findingIndex] {
			continue
		}

		bug := truth.Bugs[candidate.bugIndex]
		finding := findings[candidate.findingIndex]
		latency, _ := latencyForFinding(finding, bug.File, edits)
		score.MatchedFindings = append(score.MatchedFindings, MatchedFinding{
			Bug:        bug,
			Finding:    finding,
			MatchScore: candidate.score,
			Latency:    latency,
		})
		matchedBugs[candidate.bugIndex] = true
		matchedFindings[candidate.findingIndex] = true
	}

	for idx, bug := range truth.Bugs {
		if !matchedBugs[idx] {
			score.UnmatchedTruths = append(score.UnmatchedTruths, bug)
		}
	}
	for idx, finding := range findings {
		if !matchedFindings[idx] {
			score.ExtraFindings = append(score.ExtraFindings, finding)
			if matchesTrap(truth.FalsePositiveTraps, finding.Message) {
				score.TrapHits++
			}
		}
	}

	score.MatchedTruths = len(score.MatchedFindings)
	if score.TotalTruths > 0 {
		score.DetectionRate = float64(score.MatchedTruths) / float64(score.TotalTruths)
	}
	score.FalsePositiveCount = len(score.ExtraFindings)
	score.Latency = summarizeLatency(score.MatchedFindings)
	return score
}

func CountBrainResponses(path string) (int, error) {
	summary, err := ParseDebugLog(path, time.Now())
	if err != nil {
		return 0, err
	}
	return summary.ResponseCount, nil
}

func summarizeLatency(matches []MatchedFinding) LatencySummary {
	var summary LatencySummary
	var total time.Duration
	for _, match := range matches {
		if match.Latency <= 0 {
			continue
		}
		if summary.Count == 0 {
			summary.First = match.Latency
		}
		summary.Count++
		total += match.Latency
		if match.Latency > summary.Max {
			summary.Max = match.Latency
		}
	}
	if summary.Count > 0 {
		summary.Average = total / time.Duration(summary.Count)
	}
	return summary
}

func latencyForFinding(finding ObservedFinding, preferredFile string, edits []EditEvent) (time.Duration, bool) {
	var best EditEvent
	var found bool

	for _, edit := range edits {
		if edit.Time.After(finding.FirstSeen) {
			break
		}
		if preferredFile != "" && !editTouchesFile(edit, preferredFile) {
			continue
		}
		best = edit
		found = true
	}

	if !found && preferredFile != "" {
		for _, edit := range edits {
			if edit.Time.After(finding.FirstSeen) {
				break
			}
			best = edit
			found = true
		}
	}

	if !found || best.Time.IsZero() || finding.FirstSeen.IsZero() {
		return 0, false
	}
	return finding.FirstSeen.Sub(best.Time), true
}

func editTouchesFile(edit EditEvent, file string) bool {
	file = filepath.Base(strings.TrimSpace(file))
	for _, candidate := range edit.Files {
		if filepath.Base(candidate) == file {
			return true
		}
	}
	return false
}

func matchesTrap(traps []FalsePositiveTrap, message string) bool {
	for _, trap := range traps {
		if scoreDescription(trap.Description, message) >= 0.45 {
			return true
		}
	}
	return false
}

func matchScore(bug TruthBug, message string) float64 {
	score := scoreDescription(bug.Description, message)
	file := filepath.Base(strings.ToLower(strings.TrimSpace(bug.File)))
	if file != "" && strings.Contains(normalizeText(message), file) {
		score += 0.35
	}
	if score > 1 {
		score = 1
	}
	return score
}

func scoreDescription(description, message string) float64 {
	keywords := keywordsFor(description)
	if len(keywords) == 0 {
		return 0
	}
	normalized := normalizeText(message)
	hits := 0
	for _, keyword := range keywords {
		if strings.Contains(normalized, keyword) {
			hits++
		}
	}
	return float64(hits) / float64(len(keywords))
}

func keywordsFor(text string) []string {
	normalized := normalizeText(text)
	parts := strings.Fields(normalized)
	seen := make(map[string]bool)
	var keywords []string
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		if _, skip := stopWords[part]; skip {
			continue
		}
		if seen[part] {
			continue
		}
		seen[part] = true
		keywords = append(keywords, part)
	}
	return keywords
}

func normalizeText(text string) string {
	text = strings.ToLower(text)
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func combineClock(baseDate time.Time, clock string, hasMillis bool) time.Time {
	layout := "15:04:05"
	if hasMillis {
		layout = "15:04:05.000"
	}
	t, err := time.ParseInLocation(layout, clock, baseDate.Location())
	if err != nil {
		return time.Time{}
	}
	return time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), baseDate.Location())
}

func parseFlexibleTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func atoi(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func atof(value string) float64 {
	n, _ := strconv.ParseFloat(value, 64)
	return n
}
