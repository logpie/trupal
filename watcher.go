package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DisplayState holds state for the log file writer.
type DisplayState struct {
	ProjectDir         string
	AgentLabel         string
	Elapsed            string
	ChangedFiles       []string
	UntrackedFiles     []string
	Build              *BuildDisplay
	TrajectoryFindings []Finding
	PatternFindings    []PatternFinding
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
	Output     string
}

func WriteLog(logPath string, state DisplayState) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	t := time.Now().Format("15:04:05")
	label := state.AgentLabel
	if strings.TrimSpace(label) == "" {
		label = "agent"
	}
	fmt.Fprintf(f, "%s %s:%s", t, label, state.CCStatus)
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
	if state.BrainStats.CostKnown && state.BrainStats.TotalCostUSD > 0 {
		fmt.Fprintf(f, " brain:$%.4f", state.BrainStats.TotalCostUSD)
	}
	if state.BrainStats.PromptTokens() > 0 || state.BrainStats.TotalOutputTokens > 0 {
		fmt.Fprintf(f, " brain:uncached=%d cached=%d(%d%%) out=%d turns=%d",
			state.BrainStats.UncachedPromptTokens(),
			state.BrainStats.TotalCacheReadTokens,
			state.BrainStats.CacheHitRate(),
			state.BrainStats.TotalOutputTokens,
			state.BrainStats.TurnCount,
		)
	}
	if state.BrainStats.LastDuration > 0 || state.BrainStats.LastEffort != "" {
		fmt.Fprintf(f, " brain:last=%s/%s",
			roundDuration(state.BrainStats.LastDuration),
			defaultString(state.BrainStats.LastEffort, "?"),
		)
	}
	if strings.TrimSpace(state.BrainStats.LastTriggerSummary) != "" {
		fmt.Fprintf(f, "\n  i review: %s", state.BrainStats.LastTriggerSummary)
	}
	for _, bf := range state.BrainFindings {
		if bf.Status == "shown" {
			fmt.Fprintf(f, "\n  ⚠ %s", bf.Nudge)
		}
	}
	for _, tf := range state.TrajectoryFindings {
		fmt.Fprintf(f, "\n  ⚠ %s", tf.Message)
	}
	for _, pf := range state.PatternFindings {
		fmt.Fprintf(f, "\n  ⚠ %s", pf.Message)
	}
	fmt.Fprintf(f, "\n")
}

func roundDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return "<1s"
	}
	if d < 10*time.Second {
		return d.Round(100 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
	brainStats := BrainStats{Provider: cfg.BrainProvider}

	os.WriteFile(logPath, nil, 0644)
	agentName := sessionProviderDisplayName(cfg.SessionProvider)
	agentLabel := sessionProviderLabel(cfg.SessionProvider)

	// Find the watched agent's session JSONL.
	jsonlPath := FindSessionJSONLForProvider(sessionDir, cfg.SessionProvider)
	Debugf("[watcher] JSONL path: %q", jsonlPath)
	if jsonlPath == "" {
		p.Send(logLineMsg{line: fmt.Sprintf("no %s session found — waiting...", agentName)})
	} else {
		sessionName := filepath.Base(jsonlPath)
		if len(sessionName) > 12 {
			sessionName = sessionName[:8] + "…"
		}
		p.Send(logLineMsg{line: fmt.Sprintf("watching %s session %s", agentLabel, sessionName)})
	}

	recentEditedFiles := make([]string, 0, 8)
	recentSessionEntries := make([]JSONLEntry, 0, 16)
	extraDirs := make(map[string]bool) // directories the watched agent works in (from session tool calls)
	loggedTrajectory := make(map[string]bool)
	loggedPatterns := make(map[string]PatternFinding)
	loggedDeletedTests := make(map[string]bool)
	queuedTrajectory := make(map[string]bool)

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
		loggedTrajectory = make(map[string]bool)
		queuedTrajectory = make(map[string]bool)
		loggedPatterns = make(map[string]PatternFinding)
		loggedDeletedTests = make(map[string]bool)
		lastLogHash = 0
	}

	seedSessionContext := func(path string) string {
		entries := ReadRecentJSONLEntriesForProvider(path, 40, cfg.SessionProvider)
		return absorbJSONLEntries(repoRoot, entries, extraDirs, &recentEditedFiles, &recentSessionEntries)
	}

	initialSessionReason := ""

	// Start JSONL watcher.
	var jsonlWatcher *JSONLWatcher
	if jsonlPath != "" {
		var err error
		jsonlWatcher, err = NewJSONLWatcherForProvider(jsonlPath, cfg.SessionProvider)
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
	pendingTrigger := initialSessionReason
	shuttingDown := false
	interval := time.Duration(cfg.PollInterval) * time.Second
	lastSeenWorkHash := uint64(0)
	lastQueuedWorkHash := uint64(0)
	lastReviewedWorkHash := uint64(0)
	inFlightWorkHash := uint64(0)
	lastWorkChange := time.Time{}
	lastBuildSignature := ""

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
			queueTrigger(fmt.Sprintf("%s has been idle for 60s — good time for a session review", agentName))
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
			signature := fmt.Sprintf("%t:%d:%s", result.OK, result.ErrorCount, buildOutputExcerpt(result.Output, 6, 600))
			if signature != lastBuildSignature {
				session.AppendErrorCount(result.ErrorCount)
				lastBuildSignature = signature
			}

			trend := buildTrend(session.ErrorHistory, result.OK)
			buildDisplay = &BuildDisplay{
				OK:         result.OK,
				ErrorCount: result.ErrorCount,
				Trend:      trend,
				Output:     buildOutputExcerpt(result.Output, 6, 600),
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
		patternFindings := ScanDiffPatterns(rawDiff)
		trajectoryFindings := session.EvalTrajectory()
		currentWorkHash := reviewableWorkHash(rawDiff, untrackedFiles, buildDisplay)
		if currentWorkHash != lastSeenWorkHash {
			lastSeenWorkHash = currentWorkHash
			lastWorkChange = time.Now()
		}
		if shouldReviewWorkingTree(currentWorkHash, lastReviewedWorkHash, lastQueuedWorkHash, lastWorkChange, time.Now(), 5*time.Second) {
			queueTrigger("working tree changed — review the latest diff and changed files")
			lastQueuedWorkHash = currentWorkHash
		}

		// Trajectory signal → trigger brain.
		if len(trajectoryFindings) > 0 {
			key := trajectoryFindings[0].Message
			if !queuedTrajectory[key] {
				queueTrigger("trajectory signal: " + key)
				queuedTrajectory[key] = true
			}
		}

		// Watched agent status.
		ccStatus := ""
		if jsonlPath != "" {
			ccStatus = DetectAgentStatus(jsonlPath, cfg.SessionProvider)
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
				if result.brain != nil {
					result.brain.Stop()
				}
				inFlightWorkHash = 0
				break
			}
			if result.brain != brain && brain != nil {
				Debugf("[watcher] ignoring brain result from stale instance")
				if result.brain != nil {
					result.brain.Stop()
				}
				inFlightWorkHash = 0
				break
			}
			if brain == nil && result.resp == nil {
				brain = result.brain
				Debugf("[watcher] brain restarted successfully")
				inFlightWorkHash = 0
				break
			}
			if result.resp != nil {
				lastReviewedWorkHash = inFlightWorkHash
				inFlightWorkHash = 0
				brainLastMsg = result.resp.Reasoning
				brainLastTime = time.Now()
				p.Send(brainStatusMsg{thinking: false, lastTime: brainLastTime})
				Debugf("[brain] %d observations, %d nudges", len(result.resp.Observations), len(result.resp.Nudges))
				for _, obs := range result.resp.Observations {
					Debugf("[brain] observation: %s", obs)
				}
				for _, nudge := range result.resp.Nudges {
					why := nudge.Why
					if why == "" {
						why = nudge.Reasoning
					}
					if why == "" {
						why = result.resp.Reasoning
					}
					id := findings.Add(nudge.Severity, nudge.Message, why)
					Debugf("[brain] nudge: %s", nudge.Message)
					sourceDetail := brainStats.LastTriggerSummary
					detail := buildNudgeDetail(repoRoot, changedFiles, recentSessionEntries, nudge.Message)
					for _, f := range findings.Recent(1) {
						if f.ID == id {
							p.Send(nudgeMsg{finding: f, source: sourceDetail, detail: detail})
						}
					}
				}
				if len(result.resp.ResolvedFindings) > 0 {
					for _, rid := range result.resp.ResolvedFindings {
						if finding, ok := findings.Get(rid); ok {
							p.Send(resolvedMsg{finding: finding})
						}
					}
					findings.Resolve(result.resp.ResolvedFindings)
				}
			}
		case result := <-brainErrCh:
			brainBusy = false
			brainThinking = false
			inFlightWorkHash = 0
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
				restartNeeded = true
				restartAt = time.Now().Add(5 * time.Second)
			}
		default:
		}

		// Check for a new agent session — invalidate the current brain on switch.
		if jsonlPath != "" {
			if newPath := CheckForNewSessionForProvider(sessionDir, jsonlPath, cfg.SessionProvider); newPath != "" {
				Debugf("[watcher] session switch: %s -> %s", filepath.Base(jsonlPath), filepath.Base(newPath))
				newWatcher, err := NewJSONLWatcherForProvider(newPath, cfg.SessionProvider)
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
					pendingTrigger = mergeTriggerReason(pendingTrigger, agentName+" session switched")
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
			newPath := FindSessionJSONLForProvider(sessionDir, cfg.SessionProvider)
			if newPath != "" {
				Debugf("[watcher] found %s session: %s", agentLabel, filepath.Base(newPath))
				newWatcher, err := NewJSONLWatcherForProvider(newPath, cfg.SessionProvider)
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
					pendingTrigger = mergeTriggerReason(pendingTrigger, agentName+" session switched")
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
			brainStats.LastEffort = effectiveBrainEffort(cfg, triggerReason)
			brainStats.LastTriggerSummary = truncate(triggerReason, 120)
			beginWorkReview(currentWorkHash, &lastQueuedWorkHash, &inFlightWorkHash)
			p.Send(brainStatusMsg{thinking: true})
			p.Send(brainStatsMsg{stats: brainStats})
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
			AgentLabel:         agentLabel,
			Elapsed:            session.Elapsed(),
			ChangedFiles:       changedFiles,
			UntrackedFiles:     untrackedFiles,
			Build:              buildDisplay,
			TrajectoryFindings: trajectoryFindings,
			PatternFindings:    patternFindings,
			DeletedTests:       deletedTests,
			BrainFindings:      findings.Recent(10),
			BrainThinking:      brainThinking,
			BrainLastMsg:       brainLastMsg,
			BrainLastTime:      brainLastTime,
			BrainStats:         brainStats,
			CCStatus:           ccStatus,
		}

		activeBrainFindings, resolvedBrainFindings := findings.Count()
		buildOK := (*bool)(nil)
		if state.Build != nil {
			v := state.Build.OK
			buildOK = &v
		}
		p.Send(statusMsg{
			agentLabel: agentLabel,
			ccStatus:   ccStatus,
			buildOK:    buildOK,
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
			findings: activeBrainFindings + len(patternFindings),
			resolved: resolvedBrainFindings,
			issues:   collectCurrentIssues(findings.Active(), patternFindings, deletedTests, trajectoryFindings, 4),
		})
		// Log trajectory signals once.
		for _, f := range trajectoryFindings {
			key := f.Message
			if !loggedTrajectory[key] {
				p.Send(trajectoryMsg{message: f.Message})
				loggedTrajectory[key] = true
			}
		}
		activeTrajectory := make(map[string]bool, len(trajectoryFindings))
		for _, f := range trajectoryFindings {
			activeTrajectory[f.Message] = true
		}
		for key := range queuedTrajectory {
			if !activeTrajectory[key] {
				delete(queuedTrajectory, key)
			}
		}
		activePatternKeys := make(map[string]bool, len(patternFindings))
		for _, pf := range patternFindings {
			activePatternKeys[pf.Key] = true
			if _, ok := loggedPatterns[pf.Key]; ok {
				continue
			}
			p.Send(patternMsg{finding: pf})
			loggedPatterns[pf.Key] = pf
		}
		for key := range loggedPatterns {
			if !activePatternKeys[key] {
				delete(loggedPatterns, key)
			}
		}
		activeDeletedTests := make(map[string]bool, len(deletedTests))
		for _, file := range deletedTests {
			activeDeletedTests[file] = true
			if loggedDeletedTests[file] {
				continue
			}
			p.Send(patternMsg{finding: PatternFinding{
				Key:      "deleted-test:" + file,
				Level:    "warn",
				Message:  "deleted test file (" + file + ")",
				File:     file,
				Category: "deleted-test",
			}})
			loggedDeletedTests[file] = true
		}
		for file := range loggedDeletedTests {
			if !activeDeletedTests[file] {
				delete(loggedDeletedTests, file)
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
	var fallbackFile string
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
			fallbackFile = diffHeaderPath(line)
			buf.Reset()
		}

		if strings.HasPrefix(line, "+++ b/") {
			currentFile = strings.TrimPrefix(line, "+++ b/")
		} else if strings.HasPrefix(line, "--- a/") && currentFile == "" {
			currentFile = strings.TrimPrefix(line, "--- a/")
		} else if strings.HasPrefix(line, "+++ /dev/null") && currentFile == "" {
			currentFile = fallbackFile
		}

		if currentFile != "" || strings.HasPrefix(line, "diff --git ") {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()

	return result
}

func diffHeaderPath(line string) string {
	fields := strings.Fields(line)
	if len(fields) >= 3 {
		return strings.TrimPrefix(fields[2], "a/")
	}
	return ""
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
				summary = append(summary, fmt.Sprintf("agent asked: %q", snip))
			}
		}

		if e.Type == "assistant" && e.HasText && e.TextSnip != "" {
			significant = true
			snip := truncate(e.TextSnip, 100)
			key := "assistant:" + snip
			if !seen[key] {
				seen[key] = true
				summary = append(summary, fmt.Sprintf("agent said: %q", snip))
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
					summary = append(summary, fmt.Sprintf("agent used %s on %s", tool, filepath.Base(filePath)))
				case detail != "":
					summary = append(summary, fmt.Sprintf("agent used %s (%s)", tool, detail))
				default:
					summary = append(summary, fmt.Sprintf("agent used %s", tool))
				}
			}
		}
	}

	if !significant {
		return ""
	}
	if len(summary) == 0 {
		return "agent session updated"
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

func summarizeRecentSessionActivity(projectDir string, entries []JSONLEntry, maxLines int) string {
	if len(entries) == 0 {
		return "- none captured yet"
	}
	if maxLines <= 0 {
		maxLines = 10
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
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func buildBrainNotification(projectDir, reason string, recentEntries []JSONLEntry, editedFiles []string, nameStatus, rawDiff string, untrackedFiles []string, build *BuildDisplay) string {
	var sb strings.Builder
	sb.WriteString(reason)
	sb.WriteString("\n\nRECENT SESSION ACTIVITY:\n")
	maxActivityLines := 6
	includeEditedFiles := true
	includeBuildOutput := build != nil && strings.TrimSpace(build.Output) != ""

	switch {
	case strings.Contains(reason, "session switched"):
		maxActivityLines = 4
		includeEditedFiles = false
		includeBuildOutput = false
	case strings.Contains(reason, "trajectory signal"):
		maxActivityLines = 5
	case strings.Contains(reason, "working tree changed"):
		maxActivityLines = 4
	case strings.Contains(reason, "idle for 60s"):
		maxActivityLines = 5
	}

	sb.WriteString(summarizeRecentSessionActivity(projectDir, recentEntries, maxActivityLines))
	if includeEditedFiles {
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
	}

	sb.WriteString("\nGIT DIFF SUMMARY:\n")
	sb.WriteString(summarizeGitDiff(nameStatus, rawDiff, untrackedFiles))
	sb.WriteString("\n\nBUILD STATUS:\n")
	sb.WriteString("- ")
	sb.WriteString(formatBuildStatus(build))
	if includeBuildOutput {
		sb.WriteString("\n\nBUILD OUTPUT:\n")
		for _, line := range strings.Split(strings.TrimSpace(build.Output), "\n") {
			sb.WriteString("- ")
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
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

func buildOutputExcerpt(output string, maxLines, maxChars int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}

	lines := strings.Split(output, "\n")
	filtered := make([]string, 0, len(lines))
	chars := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if maxLines > 0 && len(filtered) >= maxLines {
			break
		}
		if maxChars > 0 && chars >= maxChars {
			break
		}
		if maxChars > 0 && chars+len(line) > maxChars {
			line = truncate(line, maxChars-chars)
		}
		filtered = append(filtered, line)
		chars += len(line)
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) < len(lines) {
		filtered = append(filtered, "...")
	}
	return strings.Join(filtered, "\n")
}

func reviewableWorkHash(rawDiff string, untrackedFiles []string, build *BuildDisplay) uint64 {
	if strings.TrimSpace(rawDiff) == "" && len(untrackedFiles) == 0 && build == nil {
		return 0
	}

	h := fnv.New64a()
	h.Write([]byte(rawDiff))

	files := append([]string(nil), untrackedFiles...)
	sort.Strings(files)
	for _, file := range files {
		h.Write([]byte(file))
	}
	if build != nil {
		if build.OK {
			h.Write([]byte("ok"))
		} else {
			fmt.Fprintf(h, "fail:%d:%s:%s", build.ErrorCount, build.Trend, buildOutputExcerpt(build.Output, 3, 240))
		}
	}
	return h.Sum64()
}

func collectCurrentIssues(activeFindings []BrainFinding, patterns []PatternFinding, deletedTests []string, trajectory []Finding, maxItems int) []CurrentIssue {
	if maxItems <= 0 {
		return nil
	}

	var items []CurrentIssue
	appendItem := func(issue CurrentIssue) {
		issue.Nudge = strings.TrimSpace(issue.Nudge)
		issue.Why = strings.TrimSpace(issue.Why)
		if issue.Nudge == "" {
			return
		}
		if len(items) < maxItems {
			items = append(items, issue)
		}
	}

	for _, finding := range activeFindings {
		appendItem(CurrentIssue{
			ID:       finding.ID,
			Severity: finding.Severity,
			Status:   finding.Status,
			Nudge:    shortIssueText(finding.Nudge),
			Why:      shortIssueText(finding.Why),
			Ref:      issueRef(finding.ID, finding.Timestamp),
		})
	}
	for _, finding := range patterns {
		appendItem(CurrentIssue{
			ID:       finding.Key,
			Severity: finding.Level,
			Status:   "shown",
			Nudge:    shortIssueText(finding.Message),
			Why:      shortIssueWhy(finding),
			Ref:      issueRef(finding.Key, time.Time{}),
		})
	}
	for _, file := range deletedTests {
		appendItem(CurrentIssue{
			ID:       "deleted-test:" + file,
			Severity: "warn",
			Status:   "shown",
			Nudge:    "deleted test: " + file,
			Why:      "Removing test coverage can hide regressions unless the behavior is re-verified elsewhere.",
			Ref:      "deleted-test",
		})
	}
	for _, finding := range trajectory {
		if finding.Level != "error" && finding.Message != "build errors increasing" {
			continue
		}
		appendItem(CurrentIssue{
			ID:       "trajectory:" + finding.Message,
			Severity: finding.Level,
			Status:   "shown",
			Nudge:    shortIssueText(finding.Message),
			Why:      "This is a process warning: the current session pattern suggests churn or non-progress, so the next step should be more deliberate.",
			Ref:      "trajectory",
		})
	}
	return items
}

func shortIssueText(text string) string {
	return normalizeIssueText(text)
}

func issueRef(id string, ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format("15:04")
}

func buildNudgeDetail(projectDir string, changedFiles []string, recentEntries []JSONLEntry, message string) []string {
	var detail []string
	if claim := latestAssistantClaim(recentEntries); claim != "" {
		detail = append(detail, "Claim\n"+claim)
	}
	if snippet := codeSnippetForNudge(projectDir, changedFiles, message); snippet != "" {
		detail = append(detail, "Code\n"+snippet)
	}
	return detail
}

func latestAssistantClaim(entries []JSONLEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type == "assistant" && e.HasText && strings.TrimSpace(e.TextSnip) != "" {
			claim := strings.TrimSpace(strings.ReplaceAll(e.TextSnip, "`", ""))
			if idx := strings.Index(claim, "Verification:"); idx >= 0 {
				claim = strings.TrimSpace(claim[:idx])
			}
			if idx := strings.Index(claim, "What changed:"); idx >= 0 {
				claim = strings.TrimSpace(claim[idx+len("What changed:"):])
			}
			claim = strings.TrimSpace(strings.Join(strings.Fields(claim), " "))
			if idx := strings.Index(claim, ". "); idx >= 0 {
				claim = claim[:idx+1]
			}
			return claim
		}
	}
	return ""
}

func codeSnippetForNudge(projectDir string, changedFiles []string, message string) string {
	for _, file := range changedFiles {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(projectDir, file)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		targets := snippetTargets(message)
		if len(targets) == 0 {
			continue
		}
		for _, target := range targets {
			for i, line := range lines {
				if !strings.Contains(line, target) {
					continue
				}
				start := i - 2
				if start < 0 {
					start = 0
				}
				end := i + 3
				if end > len(lines) {
					end = len(lines)
				}
				var snippet []string
				for j := start; j < end; j++ {
					text := strings.TrimRight(lines[j], " ")
					if strings.TrimSpace(text) == "" {
						continue
					}
					snippet = append(snippet, fmt.Sprintf("%d: %s", j+1, text))
				}
				if len(snippet) > 0 {
					return strings.Join(snippet, "\n")
				}
			}
		}
	}
	return ""
}

func snippetTargets(message string) []string {
	type targetGroup struct {
		match   string
		targets []string
	}
	groups := []targetGroup{
		{match: "writeJSONError", targets: []string{`w.Write([]byte(`, "json.NewEncoder", "json.Marshal", "func writeJSONError"}},
		{match: "ActiveJSON", targets: []string{"json.Marshal(sessions)", "func ActiveJSON"}},
		{match: "Expire", targets: []string{"delete(sessions, id)", "sessions[id] = s", "func Expire"}},
		{match: "/state", targets: []string{"StatusMethodNotAllowed", "http.MethodGet", `http.HandleFunc("/state"`}},
		{match: "/refresh", targets: []string{"StatusMethodNotAllowed", "http.MethodPost", `http.HandleFunc("/refresh"`}},
		{match: "ListenAndServe", targets: []string{"log.Fatal(err)", "ListenAndServe"}},
	}
	for _, group := range groups {
		if strings.Contains(message, group.match) {
			return group.targets
		}
	}
	return nil
}

func shortIssueWhy(finding PatternFinding) string {
	switch finding.Category {
	case "todo":
		return "This introduces deferred work into the diff instead of resolving the issue now."
	case "suppression":
		return "This adds a lint or type suppression, which is often a sign of patching around the real problem."
	case "swallowed-error":
		return "Ignoring the error hides failures from callers and makes runtime problems look like success."
	default:
		return ""
	}
}

func normalizeIssueText(text string) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	text = strings.ReplaceAll(text, "`", "")

	prefixes := []string{
		"hey, ",
		"hey ",
		"you still ",
		"you’re still ",
		"you're still ",
		"you’ve got ",
		"you've got ",
		"you need to ",
		"you should ",
	}
	for {
		trimmed := false
		lower := strings.ToLower(text)
		for _, prefix := range prefixes {
			if strings.HasPrefix(lower, prefix) {
				text = strings.TrimSpace(text[len(prefix):])
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	text = strings.Trim(text, " .")
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "findingstore.activejson") && strings.Contains(lower, "marshal"):
		return "Stop hiding marshal failures in ActiveJSON"
	case strings.Contains(lower, "findings.go::activejson") && strings.Contains(lower, "marshal"):
		return "Stop hiding marshal failures in ActiveJSON"
	case strings.Contains(lower, "recent finding") && strings.Contains(lower, "ordering guarantees"):
		return "Keep the focus drawer independent from finding order"
	case strings.Contains(lower, "wrong tree") || (strings.Contains(lower, "watch loop") && strings.Contains(lower, "edits are in")) || (strings.Contains(lower, "different directories") && strings.Contains(lower, "current patch")):
		return "Verify the same tree you are editing"
	case strings.Contains(lower, "post /refresh") && strings.Contains(lower, "nothing to verify"):
		return "Add POST /refresh"
	case strings.Contains(lower, "only handle /state") && strings.Contains(lower, "post /refresh"):
		return "Add POST /refresh"
	case strings.Contains(lower, "expire()") && strings.Contains(lower, "expired") && (strings.Contains(lower, "written back") || strings.Contains(lower, "reassign") || strings.Contains(lower, "stay resident") || strings.Contains(lower, "never deletes")):
		return "Fix Expire() so expired sessions are removed"
	case strings.Contains(lower, "expire() is backwards"):
		return "Fix Expire() so expired sessions are removed"
	case strings.Contains(lower, "activejson()") && strings.Contains(lower, "json.marshal"):
		return "Handle json.Marshal failures in ActiveJSON()"
	case strings.Contains(lower, "byte-count/error") && strings.Contains(lower, "write"):
		return "Check response write errors"
	case strings.Contains(lower, "wrap sessions with a mutex") || (strings.Contains(lower, "sessions map") && strings.Contains(lower, "no lock")) || (strings.Contains(lower, "same package-level sessions map") && strings.Contains(lower, "different handlers")):
		return "Protect sessions map with a mutex"
	case strings.Contains(lower, "package-global map") && strings.Contains(lower, "no lock"):
		return "Protect sessions map with a mutex"
	case strings.Contains(lower, "allow: post"):
		return "Add Allow: POST on /refresh 405"
	case strings.Contains(lower, "/state") && strings.Contains(lower, "accepts every method"):
		return "Restrict /state to GET"
	case strings.Contains(lower, "listenandserve") && strings.Contains(lower, "silently"):
		return "Handle ListenAndServe errors"
	}

	return strings.ToUpper(text[:1]) + text[1:]
}

func shouldReviewWorkingTree(currentHash, reviewedHash, queuedHash uint64, lastChange, now time.Time, quietPeriod time.Duration) bool {
	if currentHash == 0 || currentHash == reviewedHash || currentHash == queuedHash {
		return false
	}
	if lastChange.IsZero() {
		return false
	}
	return now.Sub(lastChange) >= quietPeriod
}

func beginWorkReview(currentHash uint64, queuedHash *uint64, inFlightHash *uint64) {
	if queuedHash != nil {
		*queuedHash = 0
	}
	if inFlightHash != nil {
		*inFlightHash = currentHash
	}
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
	for _, f := range state.PatternFindings {
		h.Write([]byte(f.Key))
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
