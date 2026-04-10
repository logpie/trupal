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
	Observations      []ObservedFinding
	Nudges            []ObservedFinding
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
	MatchedTruths          int
	TotalTruths            int
	DetectionRate          float64
	FalsePositiveCount     int
	TrapHits               int
	ResponseCount          int
	MatchedFindings        []MatchedFinding
	UnmatchedTruths        []TruthBug
	ExtraFindings          []ObservedFinding
	Latency                LatencySummary
	InputTokens            int
	OutputTokens           int
	CacheReadTokens        int
	CacheCreateTokens      int
	TotalCostUSD           float64
	SteeringEventCount     int
	BugsFixedAfterNudge    int
	NudgesWithFollowupEdit int
	NudgeConversionRate    float64
	FirstNudgeToEdit       time.Duration
}

var (
	debugUsagePattern   = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3} \[brain\] usage: in=(\d+) out=(\d+) cache_read=(\d+) cache_create=(\d+) cost=\$([0-9.]+)$`)
	debugResultPattern  = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2}\.\d{3}) \[brain\] (\d+) nudges, (\d+) resolved, reasoning: (.*)$`)
	debugFindingPattern = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2}\.\d{3}) \[brain\] (observation|nudge): (.*)$`)
	logHeaderPattern    = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2})\s`)
	applyPatchFileRE    = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File: (.+)$`)
	stopWords           = map[string]struct{}{"the": {}, "and": {}, "for": {}, "with": {}, "without": {}, "into": {}, "from": {}, "this": {}, "that": {}, "should": {}, "not": {}, "flagged": {}, "global": {}, "missing": {}, "accessed": {}, "write": {}, "reads": {}, "uses": {}, "used": {}, "file": {}, "correct": {}, "flag": {}, "under": {}, "best": {}, "effort": {}, "are": {}, "all": {}, "one": {}, "two": {}, "api": {}, "http": {}, "user": {}, "users": {}}
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
	seenObservations := make(map[string]int)
	seenNudges := make(map[string]int)
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
			continue
		}
		if matches := debugFindingPattern.FindStringSubmatch(line); len(matches) == 4 {
			kind := strings.TrimSpace(matches[2])
			message := strings.TrimSpace(matches[3])
			if message == "" {
				continue
			}
			firstSeen := combineClock(baseDate, matches[1], true)
			switch kind {
			case "observation":
				if idx, ok := seenObservations[message]; ok {
					if summary.Observations[idx].FirstSeen.IsZero() || (!firstSeen.IsZero() && firstSeen.Before(summary.Observations[idx].FirstSeen)) {
						summary.Observations[idx].FirstSeen = firstSeen
					}
					continue
				}
				seenObservations[message] = len(summary.Observations)
				summary.Observations = append(summary.Observations, ObservedFinding{
					Message:   message,
					FirstSeen: firstSeen,
				})
			default:
				if idx, ok := seenNudges[message]; ok {
					if summary.Nudges[idx].FirstSeen.IsZero() || (!firstSeen.IsZero() && firstSeen.Before(summary.Nudges[idx].FirstSeen)) {
						summary.Nudges[idx].FirstSeen = firstSeen
					}
					continue
				}
				seenNudges[message] = len(summary.Nudges)
				summary.Nudges = append(summary.Nudges, ObservedFinding{
					Message:   message,
					FirstSeen: firstSeen,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return DebugSummary{}, err
	}
	return summary, nil
}

func MergeObservedFindings(groups ...[]ObservedFinding) []ObservedFinding {
	merged := make([]ObservedFinding, 0)
	seen := make(map[string]int)
	for _, group := range groups {
		for _, finding := range group {
			message := strings.TrimSpace(finding.Message)
			if message == "" {
				continue
			}
			finding.Message = message
			if idx, ok := seen[message]; ok {
				if merged[idx].FirstSeen.IsZero() || (!finding.FirstSeen.IsZero() && finding.FirstSeen.Before(merged[idx].FirstSeen)) {
					merged[idx].FirstSeen = finding.FirstSeen
				}
				continue
			}
			seen[message] = len(merged)
			merged = append(merged, finding)
		}
	}
	return merged
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
		Payload   json.RawMessage `json:"payload"`
	}

	var edits []EditEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var parsed entry
		if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
			continue
		}
		files, tool := parseEditEvent(parsed)
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

func parseEditEvent(parsed struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
	Payload   json.RawMessage `json:"payload"`
}) ([]string, string) {
	if parsed.Type == "assistant" {
		type block struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		type message struct {
			Content []block `json:"content"`
		}
		var msg message
		if err := json.Unmarshal(parsed.Message, &msg); err != nil {
			return nil, ""
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
				files = append(files, file)
			}
		}
		return files, tool
	}

	if parsed.Type == "response_item" {
		var payload struct {
			Type      string          `json:"type"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Input     json.RawMessage `json:"input"`
		}
		if json.Unmarshal(parsed.Payload, &payload) != nil || payload.Type != "function_call" {
			if json.Unmarshal(parsed.Payload, &payload) != nil || payload.Type != "custom_tool_call" {
				return nil, ""
			}
			switch strings.TrimSpace(payload.Name) {
			case "apply_patch":
				files := applyPatchFiles(string(normalizeRawJSON(payload.Input)))
				return files, "apply_patch"
			default:
				return nil, ""
			}
		}
		switch strings.TrimSpace(payload.Name) {
		case "apply_patch":
			files := applyPatchFiles(string(normalizeRawJSON(payload.Arguments)))
			return files, "apply_patch"
		case "function.exec_command", "exec_command":
			return nil, ""
		default:
			return nil, ""
		}
	}

	return nil, ""
}

func normalizeRawJSON(raw json.RawMessage) json.RawMessage {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return raw
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		return json.RawMessage(encoded)
	}
	return raw
}

func applyPatchFiles(raw string) []string {
	matches := applyPatchFileRE.FindAllStringSubmatch(raw, -1)
	var files []string
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		file := strings.TrimSpace(match[1])
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
	}
	return files
}

func ScoreFindings(truth GroundTruth, findings []ObservedFinding, edits []EditEvent, debug DebugSummary, steeringEvents []SteeringEvent) Scorecard {
	score := Scorecard{
		TotalTruths:        len(truth.Bugs),
		ResponseCount:      debug.ResponseCount,
		InputTokens:        debug.InputTokens,
		OutputTokens:       debug.OutputTokens,
		CacheReadTokens:    debug.CacheReadTokens,
		CacheCreateTokens:  debug.CacheCreateTokens,
		TotalCostUSD:       debug.TotalCostUSD,
		SteeringEventCount: len(steeringEvents),
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
	score.BugsFixedAfterNudge, score.NudgesWithFollowupEdit, score.FirstNudgeToEdit = steeringEffectMetrics(truth, steeringEvents, edits)
	if score.SteeringEventCount > 0 {
		score.NudgeConversionRate = float64(score.NudgesWithFollowupEdit) / float64(score.SteeringEventCount)
	}
	return score
}

func steeringEffectMetrics(truth GroundTruth, steeringEvents []SteeringEvent, edits []EditEvent) (int, int, time.Duration) {
	if len(steeringEvents) == 0 || len(edits) == 0 {
		return 0, 0, 0
	}
	convertedNudges := 0
	bugsFixed := make(map[string]bool)
	var firstLatency time.Duration

	for _, event := range steeringEvents {
		if event.Timestamp.IsZero() {
			continue
		}
		bug, ok := bestMatchingTruthBug(truth.Bugs, event.Message)
		if !ok {
			continue
		}
		if latency, found := firstEditLatencyAfter(event.Timestamp, bug.File, edits); found {
			convertedNudges++
			if firstLatency == 0 || latency < firstLatency {
				firstLatency = latency
			}
			bugsFixed[bug.ID] = true
		}
	}
	return len(bugsFixed), convertedNudges, firstLatency
}

func bestMatchingTruthBug(bugs []TruthBug, message string) (TruthBug, bool) {
	var best TruthBug
	bestScore := 0.0
	for _, bug := range bugs {
		score := matchScore(bug, message)
		if score >= 0.45 && score > bestScore {
			best = bug
			bestScore = score
		}
	}
	return best, bestScore > 0
}

func firstEditLatencyAfter(start time.Time, preferredFile string, edits []EditEvent) (time.Duration, bool) {
	for _, edit := range edits {
		if edit.Time.Before(start) {
			continue
		}
		if preferredFile != "" && !editTouchesFile(edit, preferredFile) {
			continue
		}
		return edit.Time.Sub(start), true
	}
	return 0, false
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
	file = normalizeText(strings.TrimSpace(file))
	for _, candidate := range edit.Files {
		if strings.Contains(normalizeText(candidate), file) {
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
	file := normalizeText(filepath.Base(strings.TrimSpace(bug.File)))
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
