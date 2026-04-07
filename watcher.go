package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

func runWatchLoop(projectDir string, cfg Config, p *tea.Program) {
	InitDebugLog(projectDir)
	defer CloseDebugLog()

	// Handle SIGINT for graceful shutdown.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt)

	session := NewSession(projectDir)
	findings := NewFindingStore()
	logPath := filepath.Join(projectDir, ".trupal.log")
	var lastLogHash uint64
	var brainLastTime time.Time

	os.WriteFile(logPath, nil, 0644)

	// Find CC's session JSONL.
	jsonlPath := FindSessionJSONL(projectDir)
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

	// Start JSONL watcher.
	var jsonlWatcher *JSONLWatcher
	if jsonlPath != "" {
		var err error
		jsonlWatcher, err = NewJSONLWatcher(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not watch JSONL: %v\n", err)
		}
	}

	// Start brain (no extra dirs yet — we haven't read JSONL).
	var brain *Brain
	if jsonlPath != "" {
		var err error
		brain, err = StartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON())
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start brain: %v\n", err)
		}
	}

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
	loggedTrajectory := make(map[string]bool)
	extraDirs := make(map[string]bool) // directories CC works in (from JSONL tool calls)

	extraDirSlice := func() []string {
		dirs := make([]string, 0, len(extraDirs))
		for d := range extraDirs {
			dirs = append(dirs, d)
		}
		return dirs
	}
	pendingTrigger := ""
	shuttingDown := false
	interval := time.Duration(cfg.PollInterval) * time.Second

	var lastStatusHash uint64

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
				significant := false
				var summary []string
				seen := make(map[string]bool)
				for _, e := range entries {
					if e.Type == "assistant" && e.HasText {
						significant = true
						// Include a snippet of what CC said.
						if e.TextSnip != "" {
							snip := e.TextSnip
							if len(snip) > 100 {
								snip = snip[:100]
							}
							key := "text:" + snip
							if !seen[key] {
								seen[key] = true
								summary = append(summary, fmt.Sprintf("CC said: %q", snip))
							}
						}
					}
					if e.Type == "assistant" && e.HasTool {
						for i, tool := range e.ToolNames {
							file := ""
							if i < len(e.ToolFiles) {
								file = filepath.Base(e.ToolFiles[i])
								// Track directories CC works in.
								dir := filepath.Dir(e.ToolFiles[i])
								if dir != "" && dir != "." {
									extraDirs[dir] = true
								}
							}
							key := tool + ":" + file
							if !seen[key] {
								seen[key] = true
								if file != "" {
									summary = append(summary, fmt.Sprintf("CC used %s on %s", tool, file))
								} else {
									summary = append(summary, fmt.Sprintf("CC used %s", tool))
								}
							}
						}
					}
					if e.Type == "user" && e.Role == "user" {
						significant = true
					}
				}

				if significant {
					reason := "CC session updated"
					if len(summary) > 0 {
						reason = strings.Join(summary, "; ")
					}
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
		changedFiles := gitDiffNameOnly(projectDir)
		rawDiff := gitDiff(projectDir)
		nameStatus := gitDiffNameStatus(projectDir)
		untrackedFiles := gitUntrackedFiles(projectDir)

		fileDiffs := splitDiffByFile(rawDiff)
		session.UpdateFileEdits(fileDiffs)

		// Build check.
		var buildDisplay *BuildDisplay
		if cfg.BuildCmd != "" &&
			(len(changedFiles) > 0 || len(untrackedFiles) > 0) &&
			ShouldRunBuild(changedFiles, untrackedFiles, cfg.BuildExtensions) {
			result := RunBuildCheck(projectDir, cfg.BuildCmd)
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
			if brainStale {
				break
			}
			if result.brain != brain {
				Debugf("[watcher] ignoring brain result from stale instance")
				break
			}
			if result.resp != nil {
				brainLastMsg = result.resp.Reasoning
				brainLastTime = time.Now()
				p.Send(brainStatusMsg{thinking: false, lastTime: brainLastTime})
				for _, nudge := range result.resp.Nudges {
					reasoning := nudge.Reasoning
					if reasoning == "" {
						reasoning = result.resp.Reasoning
					}
					id := findings.Add(nudge.Severity, nudge.Message, reasoning)
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
			// Restart brain unless shutting down.
			if !shuttingDown && brain != nil {
				brain.Stop()
				brain = nil
				newBrain, restartErr := RestartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON(), 5*time.Second, extraDirSlice()...)
				if restartErr != nil {
					Debugf("[watcher] brain restart failed: %v", restartErr)
				} else {
					brain = newBrain
				}
			}
		default:
		}

		// Check for new CC session — invalidate the current brain on switch.
		if jsonlPath != "" {
			if newPath := CheckForNewSession(projectDir, jsonlPath); newPath != "" {
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
					pendingTrigger = mergeTriggerReason(pendingTrigger, "CC session switched")
					brainStale = true
					if brain != nil {
						brain.Stop()
						brain = nil
					}
				}
			}
		} else {
			// No session yet — keep looking.
			newPath := FindSessionJSONL(projectDir)
			if newPath != "" {
				Debugf("[watcher] found CC session: %s", filepath.Base(newPath))
				newWatcher, err := NewJSONLWatcher(newPath)
				if err != nil {
					Debugf("[watcher] failed to watch session: %v", err)
				} else {
					jsonlPath = newPath
					jsonlWatcher = newWatcher
					if triggerBrain {
						pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
						triggerBrain = false
						triggerReason = ""
					}
					pendingTrigger = mergeTriggerReason(pendingTrigger, "CC session switched")
					brainStale = true
				}
			}
		}

		if brainStale && !brainBusy && !shuttingDown && jsonlPath != "" {
			newBrain, err := StartBrain(cfg, projectDir, jsonlPath, findings.ActiveJSON(), extraDirSlice()...)
			if err != nil {
				Debugf("[watcher] failed to start brain for current session: %v", err)
			} else {
				brain = newBrain
				brainStale = false
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
		brainCooldown := !brainLastTime.IsZero() && time.Since(brainLastTime) < 10*time.Second
		if triggerBrain && brainCooldown {
			// Queue for later instead of triggering now.
			pendingTrigger = mergeTriggerReason(pendingTrigger, triggerReason)
			triggerBrain = false
		}
		if triggerBrain && brain != nil && !brainBusy && !shuttingDown {
			Debugf("[watcher] triggering brain: %s", triggerReason)
			brainBusy = true
			brainThinking = true
			p.Send(brainStatusMsg{thinking: true})
			activeBrain := brain
			findingsJSON := findings.ActiveJSON()

			go func(brain *Brain, reason, findingsJSON string) {
				resp, err := brain.Notify(reason, findingsJSON)
				if err != nil {
					brainErrCh <- brainErr{brain: brain, err: err}
					return
				}
				brainResultCh <- brainResult{brain: brain, resp: resp}
			}(activeBrain, triggerReason, findingsJSON)
		}

		// Build display state.
		state := DisplayState{
			ProjectDir:         shortenPath(projectDir),
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
			CCStatus:           ccStatus,
		}

		// Log status only when files/build change.
		sh := statusOnlyHash(state)
		if sh != lastStatusHash {
			buildOK := (*bool)(nil)
			if state.Build != nil {
				v := state.Build.OK
				buildOK = &v
			}
			p.Send(statusMsg{
				ccStatus:   ccStatus,
				buildOK:    buildOK,
				buildErrs:  func() int { if state.Build != nil { return state.Build.ErrorCount }; return 0 }(),
				buildTrend: func() string { if state.Build != nil { return state.Build.Trend }; return "" }(),
				files:      changedFiles,
				newFiles:   untrackedFiles,
				elapsed:    session.Elapsed(),
				project:    filepath.Base(projectDir),
			})
			lastStatusHash = sh
		}
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

		// Send elapsed time update for the TUI footer.
		p.Send(statusMsg{elapsed: session.Elapsed(), ccStatus: ccStatus, project: filepath.Base(projectDir)})

		// Wait for next tick or signal.
		select {
		case <-ticker.C:
		case <-sigCh:
			// Graceful shutdown.
			shuttingDown = true
			Debugf("[watcher] received SIGINT, shutting down")
			if brain != nil {
				brain.Stop()
			}
			if jsonlWatcher != nil {
				jsonlWatcher.Close()
			}
			p.Send(tea.Quit())
			// Wait for second SIGINT to actually exit.
			<-sigCh
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
		return "was clean"
	}

	// Progress: error count decreased.
	if prev > curr {
		return fmt.Sprintf("was %d", prev)
	}

	// Regression: error count increased from a non-zero baseline.
	if prev > 0 && curr > prev {
		return fmt.Sprintf("was %d", prev)
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
			return fmt.Sprintf("stalled x%d", stallLen)
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
