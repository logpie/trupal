package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DisplayState holds state for the log file writer.
type DisplayState struct {
	ProjectDir         string
	Elapsed            string
	ChangedFiles       []string
	UntrackedFiles     []string
	Build              *BuildDisplay
	TrajectoryFindings []Finding
	DeletedTests       []string
	BrainFindings      []BrainFinding
	BrainThinking      bool
	BrainLastMsg       string
	BrainLastTime      time.Time
	BrainStats         BrainStats
	CCStatus           string
}

type BuildDisplay struct {
	OK         bool
	ErrorCount int
	Trend      string
}

func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	t := time.Now().Format("15:04:05")
	fmt.Fprintf(f, "%s cc:%s", t, state.CCStatus)
	if state.Build != nil {
		if state.Build.OK {
			fmt.Fprintf(f, " build:ok")
		} else {
			fmt.Fprintf(f, " build:%d-err", state.Build.ErrorCount)
		}
	}
	if len(state.ChangedFiles) > 0 {
		fmt.Fprintf(f, " mod:%s", strings.Join(state.ChangedFiles, ","))
	}
	if state.BrainStats.TotalCostUSD > 0 {
		fmt.Fprintf(f, " brain:$%.4f", state.BrainStats.TotalCostUSD)
	}
	for _, bf := range state.BrainFindings {
		if bf.Status == "shown" {
			fmt.Fprintf(f, "\n  ⚠ %s", bf.Nudge)
		}
	}
	fmt.Fprintf(f, "\n")
}

func runWatchLoop(sessionDir, repoRoot string, cfg Config, p *tea.Program, cancelCh <-chan struct{}) {
	InitDebugLog(repoRoot)
	defer CloseDebugLog()

	// Handle SIGINT for graceful shutdown.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	session := NewSession(repoRoot)
	findings := NewFindingStore()
	logPath := filepath.Join(repoRoot, ".trupal.log")
	var lastLogHash uint64
	var brainLastTime time.Time
	brainStats := BrainStats{}

	os.WriteFile(logPath, nil, 0644)

	// Find CC's session JSONL.
	jsonlPath := FindSessionJSONL(sessionDir)
	Debugf("[watcher] JSONL path: %q", jsonlPath)
	if jsonlPath == "" {
		p.Send(logLineMsg{line: "no CC session found — waiting..."})
	} else {
		sessionName := filepath.Base(jsonlPath)
		if len(sessionName) > 12 {
			sessionName = sessionName[:8] + "…"
		}
		p.Send(logLineMsg{line: fmt.Sprintf("watching session %s", sessionName)})
	}

	recentEditedFiles := make([]string, 0, 8)
	recentSessionEntries := make([]JSONLEntry, 0, 16)
	extraDirs := make(map[string]bool) // directories CC works in (from JSONL tool calls)

	extraDirSlice := func() []string {
		dirs := make([]string, 0, len(extraDirs))
		for d := range extraDirs {
			dirs = append(dirs, d)
		}
		return dirs
	}

	resetSessionContext := func() {
		recentEditedFiles = recentEditedFiles[:0]
		recentSessionEntries = recentSessionEntries[:0]
		extraDirs = make(map[string]bool)
	}

	seedSessionContext := func(path string) string {
		entries := ReadRecentJSONLEntries(path, 40)
		return absorbJSONLEntries(repoRoot, entries, extraDirs, &recentEditedFiles, &recentSessionEntries)
	}

	initialSessionReason := ""

	// Start JSONL watcher.
	var jsonlWatcher *JSONLWatcher
	if jsonlPath != "" {
		var err error
		jsonlWatcher, err = NewJSONLWatcher(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not watch JSONL: %v\n", err)
		}
		initialSessionReason = seedSessionContext(jsonlPath)
	}

	// Start brain with any dirs/context recovered from the current session tail.
	var brain *Brain
	if jsonlPath != "" {
		var err error
		brain, err = StartBrain(cfg, repoRoot, jsonlPath, brainStats, extraDirSlice()...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start brain: %v\n", err)
		}
	}
	p.Send(brainStatsMsg{stats: brainStats})

	// Debounce channel: AfterFunc sends the reason here; loop reads it next iteration.
	debounceCh := make(chan string, 1)
	var debounceTimer *time.Timer

	// Brain state — brainResultCh receives results from the brain goroutine.
	// brainErrCh receives errors. Main loop is sole owner of brain lifecycle.
	type brainResult struct {
		brain *Brain
		resp  *BrainResponse
	}
	type brainErr struct {
		brain *Brain
		err   error
	}
	brainResultCh := make(chan brainResult, 1)
	brainErrCh := make(chan brainErr, 1)
	brainBusy := false
	brainStale := false
	brainThinking := false
	brainLastMsg := ""
	restartNeeded := false
	restartAt := time.Time{}
	loggedTrajectory := make(map[string]bool)
	pendingTrigger := initialSessionReason
	shuttingDown := false
	interval := time.Duration(cfg.PollInterval) * time.Second

	// Main loop ticker.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Idle detection.
	lastJSONLActivity := time.Now()
	idleNotified := false

	mergeTriggerReason := func(current, next string) string {
		switch {
		case next == "":
			return current
		case current == "":
			return next
		case strings.Contains(current, next):
			return current
		default:
			return current + " | " + next
		}
	}

	clearBrainState := func() {
		brainBusy = false
		brainThinking = false
		p.Send(brainStatusMsg{thinking: false})
	}

	for {
		triggerBrain := false
		triggerReason := ""
		queueTrigger := func(reason string) {
			if reason == "" {
				return
			}
			if brainBusy {
				pendingTrigger = mergeTriggerReason(pendingTrigger, reason)
				return
			}
			if !triggerBrain {
				triggerBrain = true
				triggerReason = reason
				return
			}
			triggerReason = mergeTriggerReason(triggerReason, reason)
		}

		// Drain any pending debounce signal.
		select {
		case reason := <-debounceCh:
			queueTrigger(reason)
		default:
		}

		// Check for JSONL events (non-blocking).
		if jsonlWatcher != nil {
			select {
			case <-jsonlWatcher.Events:
				lastJSONLActivity = time.Now()
				idleNotified = false

				entries := jsonlWatcher.ReadNew()
				Debugf("[watcher] JSONL event: %d new entries", len(entries))
				reason := absorbJSONLEntries(repoRoot, entries, extraDirs, &recentEditedFiles, &recentSessionEntries)
				if reason != "" {
					// Debounce: wait 2s for burst to settle.
					if debounceTimer != nil {
						debounceTimer.Stop()
					}
					debounceTimer = time.AfterFunc(2*time.Second, func() {
						select {
						case debounceCh <- reason:
						default:
						}
					})
				}
			default:
				// No JSONL events.
			}
		}

		// Check for idle (60s since last JSONL activity).
		if jsonlWatcher != nil && !idleNotified && time.Since(lastJSONLActivity) > 60*time.Second {
			queueTrigger("CC has been idle for 60s — good time for a session review")
			idleNotified = true
		}

		// Git poll cycle.
		changedFiles := gitDiffNameOnly(repoRoot)
		rawDiff := gitDiff(repoRoot)
		nameStatus := gitDiffNameStatus(repoRoot)
		untrackedFiles := gitUntrackedFiles(repoRoot)

		fileDiffs := splitDiffByFile(rawDiff)
		session.UpdateFileEdits(fileDiffs)

		// Build check.
		var buildDisplay *BuildDisplay
		if cfg.BuildCmd != "" &&
			(len(changedFiles) > 0 || len(untrackedFiles) > 0) &&
			ShouldRunBuild(changedFiles, untrackedFiles, cfg.BuildExtensions) {
			result := RunBuildCheck(repoRoot, cfg.BuildCmd)
			session.AppendErrorCount(result.ErrorCount)

			trend := buildTrend(session.ErrorHistory, result.OK)
			buildDisplay = &BuildDisplay{
				OK:         result.OK,
				ErrorCount: result.ErrorCount,
				Trend:      trend,
			}

			// Build status changed → trigger brain.
			if len(session.ErrorHistory) >= 2 {
				prev := session.ErrorHistory[len(session.ErrorHistory)-2]
				curr := result.ErrorCount
				if (prev == 0 && curr > 0) || (prev > 0 && curr == 0) {
					queueTrigger(fmt.Sprintf("build status changed: %d → %d errors", prev, curr))
				}
			}
		}

		deletedTests := ScanDeletedTests(nameStatus)
		trajectoryFindings := session.EvalTrajectory()

		// Trajectory signal → trigger brain.
		if len(trajectoryFindings) > 0 {
			queueTrigger("trajectory signal: " + trajectoryFindings[0].Message)
		}

		// CC status.
		ccStatus := ""
		if jsonlPath != "" {
			ccStatus = DetectCCStatus(jsonlPath)
		}

		// Drain brain results (non-blocking).
		select {
		case result := <-brainResultCh:
			brainBusy = false
			brainThinking = false
			if result.brain != nil {
				brainStats = result.brain.Stats()
				p.Send(brainStatsMsg{stats: brainStats})
			}
			if brainStale {
				break
			}
			if result.brain != brain && brain != nil {
				Debugf("[watcher] ignoring brain result from stale instance")
				break
			}
			if brain == nil && result.resp == nil {
				brain = result.brain
				Debugf("[watcher] brain restarted successfully")
				break
			}
			if result.resp != nil {
				brainLastMsg = result.resp.Reasoning
				brainLastTime = time.Now()
				p.Send(brainStatusMsg{thinking: false, lastTime: brainLastTime})
				// Send observations (no finding lifecycle, just display)
				Debugf("[brain] %d observations, %d nudges", len(result.resp.Observations), len(result.resp.Nudges))
				for _, obs := range result.resp.Observations {
					Debugf("[brain] observation: %s", obs)
					p.Send(observationMsg{text: obs})
				}
				for _, nudge := range result.resp.Nudges {
					reasoning := nudge.Reasoning
					if reasoning == "" {
						reasoning = result.resp.Reasoning
					}
					id := findings.Add(nudge.Severity, nudge.Message, reasoning)
					Debugf("[brain] nudge: %s", nudge.Message)
					for _, f := range findings.Recent(1) {
						if f.ID == id {
							p.Send(nudgeMsg{finding: f})
						}
					}
				}
				if len(result.resp.ResolvedFindings) > 0 {
					for _, rid := range result.resp.ResolvedFindings {
						for _, f := range findings.Recent(100) {
							if f.ID == rid {
								p.Send(resolvedMsg{finding: f})
							}
						}
					}
					findings.Resolve(result.resp.ResolvedFindings)
				}
			}
		case result := <-brainErrCh:
			brainBusy = false
			brainThinking = false
			if brainStale {
				break
			}
			if result.brain != brain {
				Debugf("[watcher] ignoring brain error from stale instance: %v", result.err)
				break
			}
			Debugf("[watcher] brain error: %v", result.err)
			clearBrainState()
			// Restart brain unless shutting down.
			if !shuttingDown && brain != nil {
				brainStats = brain.Stats()
				p.Send(brainStatsMsg{stats: brainStats})
				brain.Stop()
				brain = nil
				restartDirs := extraDirSlice()
				restartStats := brainStats
				go func() {
					newBrain, restartErr := RestartBrain(cfg, repoRoot, jsonlPath, restartStats, 5*time.Second, restartDirs...)
					if restartErr != nil {
						Debugf("[watcher] brain restart failed: %v", restartErr)
						return
					}
					brainResultCh <- brainResult{brain: newBrain}
				}()
			}
		default:
		}

		// Check for new CC session — invalidate the current brain on switch.
		if jsonlPath != "" {
			if newPath := CheckForNewSession(sessionDir, jsonlPath); newPath != "" {
				Debugf("[watcher] session switch: %s -> %s", filepath.Base(jsonlPath), filepath.Base(newPath))
				newWatcher, err := NewJSONLWatcher(newPath)
				if err != nil {
					Debugf("[watcher] failed to watch new session: %v", err)
				} else {
					if jsonlWatcher != nil {
						jsonlWatcher.Close()
					}
					if triggerBrain {
						pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
						triggerBrain = false
						triggerReason = ""
					}
					jsonlPath = newPath
					jsonlWatcher = newWatcher
					lastJSONLActivity = time.Now()
					idleNotified = false
					resetSessionContext()
					seedReason := seedSessionContext(newPath)
					pendingTrigger = mergeTriggerReason(pendingTrigger, "CC session switched")
					pendingTrigger = mergeTriggerReason(pendingTrigger, seedReason)
					restartNeeded = false
					restartAt = time.Time{}
					brainStale = true
					clearBrainState()
					if brain != nil {
						brainStats = brain.Stats()
						p.Send(brainStatsMsg{stats: brainStats})
						brain.Stop()
						brain = nil
					}
				}
			}
		} else {
			// No session yet — keep looking.
			newPath := FindSessionJSONL(sessionDir)
			if newPath != "" {
				Debugf("[watcher] found CC session: %s", filepath.Base(newPath))
				newWatcher, err := NewJSONLWatcher(newPath)
				if err != nil {
					Debugf("[watcher] failed to watch session: %v", err)
				} else {
					jsonlPath = newPath
					jsonlWatcher = newWatcher
					resetSessionContext()
					seedReason := seedSessionContext(newPath)
					if triggerBrain {
						pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
						triggerBrain = false
						triggerReason = ""
					}
					pendingTrigger = mergeTriggerReason(pendingTrigger, "CC session switched")
					pendingTrigger = mergeTriggerReason(pendingTrigger, seedReason)
					restartNeeded = false
					restartAt = time.Time{}
					brainStale = true
					clearBrainState()
				}
			}
		}

		if brainStale && !brainBusy && !shuttingDown && jsonlPath != "" {
			clearBrainState()
			newBrain, err := StartBrain(cfg, repoRoot, jsonlPath, brainStats, extraDirSlice()...)
			if err != nil {
				Debugf("[watcher] failed to start brain for current session: %v", err)
			} else {
				brain = newBrain
				brainStale = false
			}
		}

		if restartNeeded && !brainBusy && !brainStale && !shuttingDown && jsonlPath != "" && brain == nil && !time.Now().Before(restartAt) {
			clearBrainState()
			newBrain, err := StartBrain(cfg, repoRoot, jsonlPath, brainStats, extraDirSlice()...)
			if err != nil {
				Debugf("[watcher] brain restart failed: %v", err)
				restartAt = time.Now().Add(5 * time.Second)
			} else {
				brain = newBrain
				restartNeeded = false
				restartAt = time.Time{}
			}
		}

		if !brainBusy && pendingTrigger != "" && !triggerBrain {
			triggerBrain = true
			triggerReason = pendingTrigger
			pendingTrigger = ""
		}

		if triggerBrain && brain == nil && !shuttingDown {
			pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
			triggerBrain = false
			triggerReason = ""
		}

		// Trigger brain analysis if needed. Cooldown: wait 10s after last response.
		brainCooldown := !brainLastTime.IsZero() && time.Since(brainLastTime) < 3*time.Second
		if triggerBrain && brainCooldown {
			// Queue for later instead of triggering now.
			pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
			triggerBrain = false
		}
		if triggerBrain && brain != nil && !brainBusy && !shuttingDown {
			notification := buildBrainNotification(repoRoot, triggerReason, recentSessionEntries, recentEditedFiles, nameStatus, rawDiff, untrackedFiles, buildDisplay)
			Debugf("[watcher] triggering brain: %s", truncate(notification, 200))
			brainBusy = true
			brainThinking = true
			p.Send(brainStatusMsg{thinking: true})
			activeBrain := brain
			findingsJSON := findings.ActiveJSON()

			go func(brain *Brain, notification, findingsJSON string) {
				resp, err := brain.Notify(notification, findingsJSON)
				if err != nil {
					brainErrCh <- brainErr{brain: brain, err: err}
					return
				}
				brainResultCh <- brainResult{brain: brain, resp: resp}
			}(activeBrain, notification, findingsJSON)
		}

		// Build display state.
		state := DisplayState{
			ProjectDir:         shortenPath(repoRoot),
			Elapsed:            session.Elapsed(),
			ChangedFiles:       changedFiles,
			UntrackedFiles:     untrackedFiles,
			Build:              buildDisplay,
			TrajectoryFindings: trajectoryFindings,
			DeletedTests:       deletedTests,
			BrainFindings:      findings.Recent(10),
			BrainThinking:      brainThinking,
			BrainLastMsg:       brainLastMsg,
			BrainLastTime:      brainLastTime,
			BrainStats:         brainStats,
			CCStatus:           ccStatus,
		}

		buildOK := (*bool)(nil)
		if state.Build != nil {
			v := state.Build.OK
			buildOK = &v
		}
		p.Send(statusMsg{
			ccStatus: ccStatus,
			buildOK:  buildOK,
			buildErrs: func() int {
				if state.Build != nil {
					return state.Build.ErrorCount
				}
				return 0
			}(),
			buildTrend: func() string {
				if state.Build != nil {
					return state.Build.Trend
				}
				return ""
			}(),
			files:    changedFiles,
			newFiles: untrackedFiles,
			elapsed:  session.Elapsed(),
			project:  filepath.Base(repoRoot),
		})
		// Log trajectory signals once.
		for _, f := range trajectoryFindings {
			key := f.Message
			if !loggedTrajectory[key] {
				p.Send(trajectoryMsg{message: f.Message})
				loggedTrajectory[key] = true
			}
		}
		// Write full state to log file on any change.
		h := stateHash(state)
		if h != lastLogHash {
			WriteLog(logPath, state)
			lastLogHash = h
		}

		// Wait for next tick or signal.
		select {
		case <-ticker.C:
		case <-cancelCh:
			shuttingDown = true
			Debugf("[watcher] watcher canceled after TUI exit")
			clearBrainState()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			if brain != nil {
				brain.Stop()
			}
			if jsonlWatcher != nil {
				jsonlWatcher.Close()
			}
			return
		case <-sigCh:
			// Graceful shutdown.
			shuttingDown = true
			Debugf("[watcher] received SIGINT, shutting down")
			clearBrainState()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			if brain != nil {
				brain.Stop()
			}
			if jsonlWatcher != nil {
				jsonlWatcher.Close()
			}
			return
		}
	}
}

// --- Helpers (unchanged from MVP) ---

// splitDiffByFile splits a unified diff into per-file chunks keyed by filename.
// File boundaries are detected by "diff --git" headers; the filename is
// extracted from "+++ b/<path>" lines.
func splitDiffByFile(rawDiff string) map[string]string {
	result := make(map[string]string)
	if rawDiff == "" {
		return result
	}

	lines := strings.Split(rawDiff, "\n")
	var currentFile string
	var buf strings.Builder

	flush := func() {
		if currentFile != "" && buf.Len() > 0 {
			result[currentFile] = buf.String()
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			currentFile = ""
			buf.Reset()
		}

		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		}

		if currentFile != "" || strings.HasPrefix(line, "diff --git ") {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()

	return result
}

func isEditTool(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "edit", "multiedit", "write":
		return true
	default:
		return false
	}
}

func appendUniqueLimited(items []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	var filtered []string
	for _, item := range items {
		if item != value {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, value)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}

func displayPath(projectDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(projectDir, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

func absorbJSONLEntries(projectDir string, entries []JSONLEntry, extraDirs map[string]bool, recentEditedFiles *[]string, recentEntries *[]JSONLEntry) string {
	if len(entries) == 0 {
		return ""
	}

	significant := false
	var summary []string
	seen := make(map[string]bool)

	for _, e := range entries {
		*recentEntries = appendRecentJSONLEntries(*recentEntries, e, 16)

		if e.Role == "user" && e.TextSnip != "" {
			significant = true
			snip := truncate(e.TextSnip, 100)
			key := "user:" + snip
			if !seen[key] {
				seen[key] = true
				summary = append(summary, fmt.Sprintf("CC asked: %q", snip))
			}
		}

		if e.Type == "assistant" && e.HasText && e.TextSnip != "" {
			significant = true
			snip := truncate(e.TextSnip, 100)
			key := "assistant:" + snip
			if !seen[key] {
				seen[key] = true
				summary = append(summary, fmt.Sprintf("CC said: %q", snip))
			}
		}

		if e.Type == "assistant" && e.HasTool {
			significant = true
			for i, tool := range e.ToolNames {
				filePath := indexedValue(e.ToolFiles, i)
				if filePath != "" {
					dir := filepath.Dir(filePath)
					if dir != "" && dir != "." {
						extraDirs[dir] = true
					}
					if isEditTool(tool) {
						*recentEditedFiles = appendUniqueLimited(*recentEditedFiles, displayPath(projectDir, filePath), 8)
					}
				}

				detail := indexedValue(e.ToolDetails, i)
				key := tool + ":" + filePath + ":" + detail
				if seen[key] {
					continue
				}
				seen[key] = true

				switch {
				case filePath != "":
					summary = append(summary, fmt.Sprintf("CC used %s on %s", tool, filepath.Base(filePath)))
				case detail != "":
					summary = append(summary, fmt.Sprintf("CC used %s (%s)", tool, detail))
				default:
					summary = append(summary, fmt.Sprintf("CC used %s", tool))
				}
			}
		}
	}

	if !significant {
		return ""
	}
	if len(summary) == 0 {
		return "CC session updated"
	}
	return strings.Join(summary, "; ")
}

func appendRecentJSONLEntries(entries []JSONLEntry, entry JSONLEntry, limit int) []JSONLEntry {
	entries = append(entries, entry)
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries
}

func indexedValue(values []string, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[idx])
}

func summarizeRecentSessionActivity(projectDir string, entries []JSONLEntry) string {
	if len(entries) == 0 {
		return "- none captured yet"
	}

	var lines []string
	for _, e := range entries {
		switch {
		case e.Role == "user" && e.TextSnip != "":
			lines = append(lines, fmt.Sprintf("- user: %q", truncate(e.TextSnip, 120)))
		case e.Type == "assistant" && e.HasTool:
			for i, tool := range e.ToolNames {
				filePath := indexedValue(e.ToolFiles, i)
				detail := indexedValue(e.ToolDetails, i)
				switch {
				case filePath != "":
					lines = append(lines, fmt.Sprintf("- tool: %s %s", tool, displayPath(projectDir, filePath)))
				case detail != "":
					lines = append(lines, fmt.Sprintf("- tool: %s (%s)", tool, detail))
				default:
					lines = append(lines, fmt.Sprintf("- tool: %s", tool))
				}
			}
		case e.Type == "assistant" && e.HasText && e.TextSnip != "":
			lines = append(lines, fmt.Sprintf("- assistant: %q", truncate(e.TextSnip, 120)))
		}
	}

	if len(lines) == 0 {
		return "- none captured yet"
	}
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return strings.Join(lines, "\n")
}

func buildBrainNotification(projectDir, reason string, recentEntries []JSONLEntry, editedFiles []string, nameStatus, rawDiff string, untrackedFiles []string, build *BuildDisplay) string {
	var sb strings.Builder
	sb.WriteString(reason)
	sb.WriteString("\n\nRECENT SESSION ACTIVITY:\n")
	sb.WriteString(summarizeRecentSessionActivity(projectDir, recentEntries))
	sb.WriteString("\n\nRECENT JSONL EDITS:\n")
	if len(editedFiles) == 0 {
		sb.WriteString("- none detected from recent Edit/Write tool calls\n")
	} else {
		for _, file := range editedFiles {
			sb.WriteString("- ")
			sb.WriteString(file)
			sb.WriteByte('\n')
		}
	}

	sb.WriteString("\nGIT DIFF SUMMARY:\n")
	sb.WriteString(summarizeGitDiff(nameStatus, rawDiff, untrackedFiles))
	sb.WriteString("\n\nBUILD STATUS:\n")
	sb.WriteString("- ")
	sb.WriteString(formatBuildStatus(build))
	return sb.String()
}

func summarizeGitDiff(nameStatus, rawDiff string, untrackedFiles []string) string {
	type diffStat struct {
		added   int
		deleted int
	}

	stats := make(map[string]diffStat)
	for file, diff := range splitDiffByFile(rawDiff) {
		var stat diffStat
		for _, line := range strings.Split(diff, "\n") {
			switch {
			case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
				continue
			case strings.HasPrefix(line, "+"):
				stat.added++
			case strings.HasPrefix(line, "-"):
				stat.deleted++
			}
		}
		stats[file] = stat
	}

	var lines []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(nameStatus), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		path := fields[len(fields)-1]
		seen[path] = true
		stat := stats[path]
		switch {
		case strings.HasPrefix(status, "R") && len(fields) >= 3:
			lines = append(lines, fmt.Sprintf("- R %s -> %s%s", fields[1], fields[2], formatDiffCounts(stat.added, stat.deleted)))
		default:
			lines = append(lines, fmt.Sprintf("- %s %s%s", status, path, formatDiffCounts(stat.added, stat.deleted)))
		}
	}
	for _, path := range untrackedFiles {
		if seen[path] {
			continue
		}
		lines = append(lines, fmt.Sprintf("- ?? %s (untracked)", path))
	}
	if len(lines) == 0 {
		return "- clean working tree"
	}
	const maxLines = 8
	if len(lines) > maxLines {
		extra := len(lines) - maxLines
		lines = append(lines[:maxLines], fmt.Sprintf("- ... %d more changed files", extra))
	}
	return strings.Join(lines, "\n")
}

func formatDiffCounts(added, deleted int) string {
	if added == 0 && deleted == 0 {
		return ""
	}
	return fmt.Sprintf(" (+%d -%d)", added, deleted)
}

func formatBuildStatus(build *BuildDisplay) string {
	if build == nil {
		return "not run this cycle"
	}
	if build.OK {
		return "passing"
	}
	if build.Trend != "" {
		return fmt.Sprintf("failing: %d errors (%s)", build.ErrorCount, build.Trend)
	}
	return fmt.Sprintf("failing: %d errors", build.ErrorCount)
}

// buildTrend computes the Trend string for BuildDisplay from the error history.
// It is called after the current cycle's error count has been appended, so the
// last entry is the current count.
func buildTrend(history []int, buildOK bool) string {
	if buildOK || len(history) < 2 {
		return ""
	}

	curr := history[len(history)-1]
	prev := history[len(history)-2]

	// fix-then-break: previously clean, now broken.
	if prev == 0 && curr > 0 {
		return "new breakage"
	}

	// Progress: error count decreased.
	if prev > curr {
		return "fewer errors than last cycle"
	}

	// Regression: error count increased from a non-zero baseline.
	if prev > 0 && curr > prev {
		return "more errors than last cycle"
	}

	// Stall: same non-zero count for several cycles.
	if prev == curr && curr > 0 {
		stallLen := 0
		for i := len(history) - 1; i >= 0; i-- {
			if history[i] == curr {
				stallLen++
			} else {
				break
			}
		}
		if stallLen >= 3 {
			return fmt.Sprintf("%d cycles", stallLen)
		}
	}

	return ""
}

// statusOnlyHash hashes files/build/CC status — excludes brain state.
// Used to dedup status line logging.
func statusOnlyHash(state DisplayState) uint64 {
	h := fnv.New64a()
	for _, f := range state.ChangedFiles {
		h.Write([]byte(f))
	}
	for _, f := range state.UntrackedFiles {
		h.Write([]byte(f))
	}
	if state.Build != nil {
		if state.Build.OK {
			h.Write([]byte("ok"))
		} else {
			fmt.Fprintf(h, "%d%s", state.Build.ErrorCount, state.Build.Trend)
		}
	}
	for _, f := range state.DeletedTests {
		h.Write([]byte(f))
	}
	return h.Sum64()
}

// stateHash returns a hash of the full display state (excluding elapsed time).
// Used to deduplicate log file entries.
func stateHash(state DisplayState) uint64 {
	h := fnv.New64a()
	for _, f := range state.ChangedFiles {
		h.Write([]byte(f))
	}
	for _, f := range state.UntrackedFiles {
		h.Write([]byte(f))
	}
	if state.Build != nil {
		if state.Build.OK {
			h.Write([]byte("ok"))
		} else {
			fmt.Fprintf(h, "%d%s", state.Build.ErrorCount, state.Build.Trend)
		}
	}
	for _, f := range state.TrajectoryFindings {
		h.Write([]byte(f.Message))
	}
	for _, f := range state.DeletedTests {
		h.Write([]byte(f))
	}
	for _, f := range state.BrainFindings {
		h.Write([]byte(f.ID))
		h.Write([]byte(f.Status))
	}
	fmt.Fprintf(h, "%.4f", state.BrainStats.TotalCostUSD)
	h.Write([]byte(fmt.Sprintf("%v", state.BrainThinking)))
	h.Write([]byte(state.BrainLastMsg))
	h.Write([]byte(state.CCStatus))
	return h.Sum64()
}

// gitDiffNameOnly returns the list of changed files relative to HEAD using
// "git diff --name-only HEAD".
func gitDiffNameOnly(projectDir string) []string {
	out, err := runGit(projectDir, "diff", "--name-only", "HEAD")
	if isUnbornHeadError(err) {
		Debugf("[watcher] git diff --name-only HEAD: unborn HEAD, retrying with --cached")
		out, err = runGit(projectDir, "diff", "--name-only", "--cached")
	}
	if err != nil {
		Debugf("[watcher] git diff --name-only failed: %v", err)
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// gitUntrackedFiles returns untracked files (not in .gitignore), excluding trupal's own files.
func gitUntrackedFiles(projectDir string) []string {
	out, err := runGit(projectDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		Debugf("[watcher] git ls-files --others failed: %v", err)
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(filepath.Base(line), ".trupal.") {
			continue
		}
		files = append(files, line)
	}
	return files
}

// gitDiffNameStatus returns the output of "git diff --name-status HEAD".
func gitDiffNameStatus(projectDir string) string {
	out, err := runGit(projectDir, "diff", "--name-status", "HEAD")
	if isUnbornHeadError(err) {
		Debugf("[watcher] git diff --name-status HEAD: unborn HEAD, retrying with --cached")
		out, err = runGit(projectDir, "diff", "--name-status", "--cached")
	}
	if err != nil {
		Debugf("[watcher] git diff --name-status failed: %v", err)
		return ""
	}
	return out
}

// gitDiff returns the full unified diff output of "git diff HEAD".
func gitDiff(projectDir string) string {
	out, err := runGit(projectDir, "diff", "HEAD")
	if isUnbornHeadError(err) {
		Debugf("[watcher] git diff HEAD: unborn HEAD, retrying with --cached")
		out, err = runGit(projectDir, "diff", "--cached")
	}
	if err != nil {
		Debugf("[watcher] git diff failed: %v", err)
		return ""
	}
	return out
}

func isUnbornHeadError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "bad revision 'HEAD'") ||
		strings.Contains(msg, "ambiguous argument 'HEAD'") ||
		strings.Contains(msg, "unknown revision or path not in the working tree")
}

// runGit executes a git command in projectDir and returns its combined output.
func runGit(projectDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return string(out), nil
}

// shortenPath replaces the user's home directory prefix with "~".
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
