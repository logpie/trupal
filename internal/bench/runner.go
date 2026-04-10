package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RunnerOptions struct {
	RepoRoot     string
	ScenariosDir string
	ResultsDir   string
	SWEBenchDir  string
	CodexCmd     string
	KeepTemp     bool
}

type Runner struct {
	opts      RunnerOptions
	goBin     string
	trupalBin string
}

type CodexAuditResult struct {
	Command  string
	ExitCode int
	Duration time.Duration
	Error    string
}

type RunResult struct {
	Scenario       Scenario
	Arm            BenchmarkArm
	SWEBenchTask   *SWEBenchTask
	StartedAt      time.Time
	FinishedAt     time.Time
	Duration       time.Duration
	ProjectDir     string
	TimedOut       bool
	AgentDuration  time.Duration
	AgentExitCode  int
	AgentError     string
	SessionJSONL   string
	Artifacts      ArtifactSet
	SteeringEvents []SteeringEvent
	Score          Scorecard
	CodexAudit     *CodexAuditResult
}

func NewRunner(opts RunnerOptions) (*Runner, error) {
	if opts.RepoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if opts.ScenariosDir == "" {
		opts.ScenariosDir = filepath.Join(opts.RepoRoot, "bench", "scenarios")
	}
	if opts.ResultsDir == "" {
		opts.ResultsDir = filepath.Join(opts.RepoRoot, "bench", "results")
	}
	if opts.SWEBenchDir == "" {
		opts.SWEBenchDir = filepath.Join(opts.RepoRoot, "bench", "swebench-sample")
	}
	if err := os.MkdirAll(opts.ResultsDir, 0755); err != nil {
		return nil, fmt.Errorf("create results dir: %w", err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("find go binary: %w", err)
	}
	required := []string{"git", "tmux"}
	for _, name := range required {
		if _, err := exec.LookPath(name); err != nil {
			return nil, fmt.Errorf("required binary %q not found: %w", name, err)
		}
	}

	trupalBin := filepath.Join(opts.ResultsDir, "bin", "trupal")
	runner := &Runner{
		opts:      opts,
		goBin:     goBin,
		trupalBin: trupalBin,
	}
	if err := runner.buildTrupalBinary(); err != nil {
		return nil, err
	}
	return runner, nil
}

func (r *Runner) RunScenario(name string) (*RunResult, error) {
	scenario, err := LoadScenario(r.opts.ScenariosDir, name)
	if err != nil {
		return nil, err
	}
	arms := scenario.EffectiveBenchmarkArms()
	return r.runScenario(scenario, arms[0])
}

func (r *Runner) RunScenarioArm(name string, arm BenchmarkArm) (*RunResult, error) {
	scenario, err := LoadScenario(r.opts.ScenariosDir, name)
	if err != nil {
		return nil, err
	}
	return r.runScenario(scenario, arm)
}

func (r *Runner) RunScenarioPair(name string) ([]*RunResult, error) {
	scenario, err := LoadScenario(r.opts.ScenariosDir, name)
	if err != nil {
		return nil, err
	}
	arms := scenario.EffectiveBenchmarkArms()
	results := make([]*RunResult, 0, len(arms))
	for _, arm := range arms {
		result, err := r.runScenario(scenario, arm)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Runner) PrepareSWEBenchTask(manifestPath, instanceID string) (SWEBenchTask, string, error) {
	if strings.TrimSpace(manifestPath) == "" {
		manifestPath = filepath.Join(r.opts.SWEBenchDir, "sample-task.json")
	}
	task, err := LoadSWEBenchTask(manifestPath, instanceID)
	if err != nil {
		return SWEBenchTask{}, "", err
	}
	workspace, err := os.MkdirTemp("", "trupal-swebench-"+task.Slug()+"-")
	if err != nil {
		return SWEBenchTask{}, "", fmt.Errorf("create swebench workspace: %w", err)
	}
	if err := writeSWEBenchTaskArtifact(workspace, task); err != nil {
		return SWEBenchTask{}, "", err
	}
	return task, workspace, nil
}

func (r *Runner) PrepareSWEBenchWorkspace(task SWEBenchTask, workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("workspace is required")
	}
	source := task.CloneSource()
	if strings.TrimSpace(source) == "" {
		return fmt.Errorf("task %s has no clone source", task.InstanceID)
	}
	taskPath := filepath.Join(workspace, "TASK.md")
	_ = os.Remove(taskPath)
	if _, err := os.Stat(filepath.Join(workspace, ".git")); err == nil {
		if err := writeSWEBenchTaskArtifact(workspace, task); err != nil {
			return err
		}
		return nil
	}
	cmd := exec.Command("git", "clone", source, workspace)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone %s: %w\n%s", source, err, string(out))
	}
	cmd = exec.Command("git", "checkout", task.BaseCommit)
	cmd.Dir = workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %w\n%s", task.BaseCommit, err, string(out))
	}
	return writeSWEBenchTaskArtifact(workspace, task)
}

func writeSWEBenchTaskArtifact(workspace string, task SWEBenchTask) error {
	return os.WriteFile(filepath.Join(workspace, "TASK.md"), []byte(task.ProblemStatement+"\n"), 0644)
}

func (r *Runner) EvaluateSWEBenchTask(task SWEBenchTask, workspace, evalCommand string) (string, error) {
	if strings.TrimSpace(evalCommand) == "" {
		evalCommand = task.EvalCommand
	}
	if strings.TrimSpace(evalCommand) == "" {
		return "", fmt.Errorf("no evaluation command provided")
	}
	if strings.TrimSpace(task.TestPatch) != "" {
		patchPath := filepath.Join(workspace, ".swebench-test.patch")
		patch := task.TestPatch
		if !strings.HasSuffix(patch, "\n") {
			patch += "\n"
		}
		if err := os.WriteFile(patchPath, []byte(patch), 0644); err != nil {
			return "", err
		}
		cmd := exec.Command("git", "apply", patchPath)
		cmd.Dir = workspace
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git apply test patch: %w\n%s", err, string(out))
		}
	}
	cmd := exec.Command("sh", "-lc", evalCommand)
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("evaluate swebench task: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *Runner) RunAll() ([]*RunResult, error) {
	scenarios, err := LoadAllScenarios(r.opts.ScenariosDir)
	if err != nil {
		return nil, err
	}

	results := make([]*RunResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		for _, arm := range scenario.EffectiveBenchmarkArms() {
			result, err := r.runScenario(scenario, arm)
			if err != nil {
				return results, err
			}
			results = append(results, result)
		}
	}
	return results, nil
}

func (r *Runner) runScenario(scenario Scenario, arm BenchmarkArm) (*RunResult, error) {
	setupStartedAt := time.Now()
	runSlug := setupStartedAt.Format("20060102-150405") + "-" + scenario.ID + "-" + string(arm)
	artifactsDir := filepath.Join(r.opts.ResultsDir, runSlug)
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, fmt.Errorf("create artifacts dir: %w", err)
	}

	result := &RunResult{
		Scenario:  scenario,
		Arm:       arm,
		Artifacts: NewArtifactSet(artifactsDir),
	}

	projectDir, err := os.MkdirTemp("", "trupal-bench-"+scenario.ID+"-")
	if err != nil {
		return nil, fmt.Errorf("create temp project dir: %w", err)
	}
	result.ProjectDir = projectDir
	if !r.opts.KeepTemp {
		defer os.RemoveAll(projectDir)
	}

	if err := CopyTree(scenario.TemplateDir, projectDir); err != nil {
		return nil, fmt.Errorf("copy template: %w", err)
	}
	if err := r.writeScenarioConfig(projectDir, scenario, arm); err != nil {
		return nil, err
	}
	if err := r.initGitRepo(projectDir); err != nil {
		return nil, err
	}
	if err := r.copyScenarioInputs(result); err != nil {
		return nil, err
	}

	if scenario.SessionProvider() == "codex" {
		return r.runScenarioInteractiveCodex(result)
	}
	return r.runScenarioLegacy(result)
}

func (r *Runner) runScenarioLegacy(result *RunResult) (*RunResult, error) {
	scenario := result.Scenario
	projectDir := result.ProjectDir

	sessionName, err := r.startTrupal(projectDir, scenario.ID)
	if err != nil {
		return nil, err
	}
	defer r.stopTmuxSession(sessionName)

	ccStartedAt, ccFinishedAt, err := r.runAgent(result)
	if err != nil {
		return nil, err
	}
	result.StartedAt = ccStartedAt
	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestSessionJSONL(projectDir, scenario.SessionProvider())
	}

	brainFinishedAt, err := r.waitForBrain(result, ccFinishedAt)
	if err != nil {
		return nil, err
	}
	if brainFinishedAt.IsZero() {
		brainFinishedAt = ccFinishedAt
	}
	result.FinishedAt = brainFinishedAt
	result.Duration = result.FinishedAt.Sub(result.StartedAt)

	time.Sleep(3 * time.Second)

	pidFile := filepath.Join(projectDir, ".trupal.pid")
	paneID, _ := ReadPaneID(pidFile)
	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestSessionJSONL(projectDir, scenario.SessionProvider())
	}
	if err := CollectArtifacts(projectDir, result.Artifacts, result.SessionJSONL, paneID); err != nil {
		return nil, err
	}

	if err := r.stopTrupal(projectDir); err != nil {
		return nil, err
	}

	result.CodexAudit = r.runCodexAudit(result)

	findings, err := ParseTrupalLog(result.Artifacts.TrupalLogPath, result.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse trupal log: %w", err)
	}
	debugSummary, err := ParseDebugLog(result.Artifacts.DebugLogPath, result.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse debug log: %w", err)
	}
	edits, err := ParseSessionEdits(result.Artifacts.SessionJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("parse session edits: %w", err)
	}
	result.SteeringEvents, err = ParseSteeringEvents(result.Artifacts.SteerLogPath)
	if err != nil {
		return nil, fmt.Errorf("parse steering log: %w", err)
	}
	result.Score = ScoreFindings(scenario.Truth, MergeObservedFindings(findings, debugSummary.Nudges), edits, debugSummary, result.SteeringEvents)

	if err := WriteReport(result.Artifacts.ReportPath, result); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	return result, nil
}

func (r *Runner) runScenarioInteractiveCodex(result *RunResult) (*RunResult, error) {
	scenario := result.Scenario
	projectDir := result.ProjectDir

	sessionName, codexPaneID, trupalPaneID, err := r.startInteractiveCodexSession(projectDir, scenario.ID, scenario.EffectiveAgentModel())
	if err != nil {
		return nil, err
	}
	defer r.stopTmuxSession(sessionName)

	if err := r.waitForCodexReady(codexPaneID); err != nil {
		return nil, err
	}
	if err := r.waitForTrupalWatch(projectDir, trupalPaneID); err != nil {
		return nil, err
	}
	if result.Arm == ArmSteer {
		if err := r.sendKeys(trupalPaneID, "a"); err != nil {
			return nil, fmt.Errorf("enable auto steer: %w", err)
		}
	}

	startedAt := time.Now()
	if err := r.submitLiteral(codexPaneID, singleLinePrompt(benchmarkAgentPrompt(scenario.TaskPrompt))); err != nil {
		return nil, fmt.Errorf("submit benchmark prompt: %w", err)
	}
	result.StartedAt = startedAt

	finishedAt, agentExitCode, timedOut, err := r.waitForInteractiveCodex(result, codexPaneID, trupalPaneID)
	if err != nil {
		return nil, err
	}
	result.FinishedAt = finishedAt
	result.Duration = finishedAt.Sub(startedAt)
	result.AgentDuration = result.Duration
	result.AgentExitCode = agentExitCode
	result.TimedOut = timedOut

	time.Sleep(3 * time.Second)
	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestSessionJSONL(projectDir, scenario.SessionProvider())
	}
	paneID, _ := ReadPaneID(filepath.Join(projectDir, ".trupal.pid"))
	if strings.TrimSpace(paneID) == "" {
		paneID = trupalPaneID
	}
	if err := CollectArtifacts(projectDir, result.Artifacts, result.SessionJSONL, paneID); err != nil {
		return nil, err
	}

	result.CodexAudit = r.runCodexAudit(result)

	findings, err := ParseTrupalLog(result.Artifacts.TrupalLogPath, result.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse trupal log: %w", err)
	}
	debugSummary, err := ParseDebugLog(result.Artifacts.DebugLogPath, result.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse debug log: %w", err)
	}
	edits, err := ParseSessionEdits(result.Artifacts.SessionJSONLPath)
	if err != nil {
		return nil, fmt.Errorf("parse session edits: %w", err)
	}
	result.SteeringEvents, err = ParseSteeringEvents(result.Artifacts.SteerLogPath)
	if err != nil {
		return nil, fmt.Errorf("parse steering log: %w", err)
	}
	result.Score = ScoreFindings(scenario.Truth, MergeObservedFindings(findings, debugSummary.Nudges), edits, debugSummary, result.SteeringEvents)

	if err := WriteReport(result.Artifacts.ReportPath, result); err != nil {
		return nil, fmt.Errorf("write report: %w", err)
	}

	return result, nil
}

func (r *Runner) buildTrupalBinary() error {
	if err := os.MkdirAll(filepath.Dir(r.trupalBin), 0755); err != nil {
		return fmt.Errorf("create trupal bin dir: %w", err)
	}
	cmd := exec.Command(r.goBin, "build", "-o", r.trupalBin, ".")
	cmd.Dir = r.opts.RepoRoot
	goCache := os.Getenv("GOCACHE")
	if strings.TrimSpace(goCache) == "" {
		goCache = filepath.Join(os.TempDir(), "trupal-go-cache")
	}
	if err := os.MkdirAll(goCache, 0755); err != nil {
		return fmt.Errorf("create GOCACHE dir: %w", err)
	}
	cmd.Env = withEnv(os.Environ(), "GOCACHE", goCache)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build trupal binary: %w\n%s", err, string(out))
	}
	return nil
}

func (r *Runner) startInteractiveCodexSession(projectDir, scenarioID, model string) (string, string, string, error) {
	sessionName := tmuxSessionName(scenarioID + "-interactive")
	r.stopTmuxSession(sessionName)

	codexCmd := fmt.Sprintf("codex -C %s --dangerously-bypass-approvals-and-sandbox", shellQuote(projectDir))
	if strings.TrimSpace(model) != "" {
		codexCmd += " --model " + shellQuote(strings.TrimSpace(model))
	}
	out, err := exec.Command("tmux", "new-session", "-d", "-P", "-F", "#{pane_id}", "-s", sessionName, "-c", projectDir, codexCmd).CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("start codex tmux session: %w\n%s", err, string(out))
	}
	codexPaneID := strings.TrimSpace(string(out))
	if codexPaneID == "" {
		return "", "", "", fmt.Errorf("start codex tmux session: missing pane id")
	}

	out, err = exec.Command("tmux", "split-window", "-P", "-F", "#{pane_id}", "-v", "-t", codexPaneID, "-c", projectDir, fmt.Sprintf("%s watch %s %s", shellQuote(r.trupalBin), shellQuote(projectDir), shellQuote(projectDir))).CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("start trupal watch pane: %w\n%s", err, string(out))
	}
	trupalPaneID := strings.TrimSpace(string(out))
	if trupalPaneID == "" {
		return "", "", "", fmt.Errorf("start trupal watch pane: missing pane id")
	}
	return sessionName, codexPaneID, trupalPaneID, nil
}

func (r *Runner) waitForTrupalWatch(projectDir, trupalPaneID string) error {
	deadline := time.Now().Add(20 * time.Second)
	pidFile := filepath.Join(projectDir, ".trupal.pid")
	debugPath := filepath.Join(projectDir, ".trupal.debug")
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			if _, err := os.Stat(debugPath); err == nil {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	capture, captureErr := exec.Command("tmux", "capture-pane", "-p", "-t", trupalPaneID).CombinedOutput()
	if captureErr != nil {
		return fmt.Errorf("trupal watch did not start (also failed to capture pane %s: %v)", trupalPaneID, captureErr)
	}
	return fmt.Errorf("trupal watch did not start; pane %s output:\n%s", trupalPaneID, string(capture))
}

func (r *Runner) waitForCodexReady(codexPaneID string) error {
	deadline := time.Now().Add(20 * time.Second)
	sawTrustPrompt := false
	for time.Now().Before(deadline) {
		capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
		text := string(capture)
		switch {
		case strings.Contains(text, "Do you trust the contents of this directory?"):
			sawTrustPrompt = true
			if err := r.sendKeys(codexPaneID, "C-m"); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		case strings.Contains(text, "Use /skills") || strings.Contains(text, "Tip:"):
			if !sawTrustPrompt {
				time.Sleep(1500 * time.Millisecond)
				verify, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
				if strings.Contains(string(verify), "Do you trust the contents of this directory?") {
					continue
				}
			}
			return nil
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}
	capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
	return fmt.Errorf("codex did not become ready:\n%s", string(capture))
}

func (r *Runner) waitForInteractiveCodex(result *RunResult, codexPaneID, trupalPaneID string) (time.Time, int, bool, error) {
	deadline := result.StartedAt.Add(result.Scenario.Timeout)
	quietWindow := 8 * time.Second
	if result.Arm == ArmSteer {
		quietWindow = result.Scenario.SteeringCooldown + 5*time.Second
	}
	autoDisabled := result.Arm != ArmSteer

	for {
		if result.SessionJSONL == "" {
			result.SessionJSONL, _ = FindLatestSessionJSONL(result.ProjectDir, result.Scenario.SessionProvider())
		}

		var lastActivity time.Time
		if result.SessionJSONL != "" {
			if info, err := os.Stat(result.SessionJSONL); err == nil {
				lastActivity = info.ModTime()
			}
		}

		events, err := ParseSteeringEvents(filepath.Join(result.ProjectDir, ".trupal.steer.jsonl"))
		if err != nil {
			return time.Time{}, -1, false, err
		}
		if len(events) > 0 && events[len(events)-1].Timestamp.After(lastActivity) {
			lastActivity = events[len(events)-1].Timestamp
		}
		if result.Arm == ArmSteer && !autoDisabled && len(events) >= result.Scenario.SteeringRounds {
			if err := r.sendKeys(trupalPaneID, "a"); err != nil {
				return time.Time{}, -1, false, fmt.Errorf("disable auto steer after %d rounds: %w", result.Scenario.SteeringRounds, err)
			}
			autoDisabled = true
			lastActivity = time.Now()
		}

		now := time.Now()
		if result.SessionJSONL != "" && lastActivity.After(result.StartedAt) && now.Sub(lastActivity) >= quietWindow {
			capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
			if !strings.Contains(string(capture), "Working (") {
				return now, 0, false, nil
			}
		}
		if !now.Before(deadline) {
			_ = r.sendKeys(codexPaneID, "C-c")
			return now, 124, true, nil
		}
		time.Sleep(2 * time.Second)
	}
}

func (r *Runner) sendLiteral(paneID, text string) error {
	out, err := exec.Command("tmux", "send-keys", "-t", paneID, "-l", text).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys -l: %w\n%s", err, string(out))
	}
	return nil
}

func (r *Runner) submitLiteral(paneID, text string) error {
	if err := r.sendLiteral(paneID, text); err != nil {
		return err
	}
	delay := 200 * time.Millisecond
	if extra := time.Duration(len(text)) * time.Millisecond; extra > delay {
		delay = extra
	}
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	time.Sleep(delay)
	return r.sendKeys(paneID, "Enter")
}

func (r *Runner) sendKeys(paneID string, keys ...string) error {
	args := append([]string{"send-keys", "-t", paneID}, keys...)
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys %v: %w\n%s", keys, err, string(out))
	}
	return nil
}

func (r *Runner) writeScenarioConfig(projectDir string, scenario Scenario, arm BenchmarkArm) error {
	cfg := scenario.TrupalConfig
	var lines []string
	if cfg.BuildCmd != "" {
		lines = append(lines, fmt.Sprintf("build_cmd = %q", cfg.BuildCmd))
	}
	if cfg.SessionProvider != "" {
		lines = append(lines, fmt.Sprintf("session_provider = %q", cfg.SessionProvider))
	}
	if cfg.BrainProvider != "" {
		lines = append(lines, fmt.Sprintf("brain_provider = %q", cfg.BrainProvider))
	}
	if cfg.BrainModel != "" {
		lines = append(lines, fmt.Sprintf("brain_model = %q", cfg.BrainModel))
	}
	if cfg.BrainEffort != "" {
		lines = append(lines, fmt.Sprintf("brain_effort = %q", cfg.BrainEffort))
	}
	lines = append(lines, "benchmark_mode = true")
	lines = append(lines, fmt.Sprintf("benchmark_scenario = %q", scenario.ID))
	lines = append(lines, fmt.Sprintf("benchmark_arm = %q", arm))
	lines = append(lines, "")
	return os.WriteFile(filepath.Join(projectDir, ".trupal.toml"), []byte(strings.Join(lines, "\n")), 0644)
}

func (r *Runner) initGitRepo(projectDir string) error {
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "TruPal Bench"},
		{"git", "config", "user.email", "bench@trupal.local"},
		{"git", "add", "."},
		{"git", "commit", "-m", "benchmark template"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = projectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("run %q in temp repo: %w\n%s", strings.Join(args, " "), err, string(out))
		}
	}
	return nil
}

func (r *Runner) copyScenarioInputs(result *RunResult) error {
	info, err := os.Stat(result.Scenario.ScenarioYML)
	if err == nil {
		if err := copyFile(result.Scenario.ScenarioYML, result.Artifacts.ScenarioYAMLPath, info.Mode()); err != nil {
			return err
		}
	}
	info, err = os.Stat(result.Scenario.TaskPath)
	if err == nil {
		if err := copyFile(result.Scenario.TaskPath, result.Artifacts.TaskPath, info.Mode()); err != nil {
			return err
		}
	}
	info, err = os.Stat(result.Scenario.TruthPath)
	if err == nil {
		if err := copyFile(result.Scenario.TruthPath, result.Artifacts.TruthPath, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) startTrupal(projectDir, scenarioID string) (string, error) {
	sessionName := tmuxSessionName(scenarioID)
	r.stopTmuxSession(sessionName)
	cmd := exec.Command("tmux", "new-session", "-d", "-P", "-F", "#{pane_id}", "-s", sessionName, "-c", projectDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("start tmux session: %w\n%s", err, string(out))
	}
	startPaneID := strings.TrimSpace(string(out))
	if startPaneID == "" {
		return "", fmt.Errorf("start tmux session: tmux did not report a pane id")
	}

	startCmd := fmt.Sprintf("%s start %s", shellQuote(r.trupalBin), shellQuote(projectDir))
	send := exec.Command("tmux", "send-keys", "-t", startPaneID, startCmd, "C-m")
	if out, err := send.CombinedOutput(); err != nil {
		return "", fmt.Errorf("send trupal start command: %w\n%s", err, string(out))
	}

	deadline := time.Now().Add(20 * time.Second)
	pidFile := filepath.Join(projectDir, ".trupal.pid")
	debugPath := filepath.Join(projectDir, ".trupal.debug")
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidFile); err == nil {
			if _, err := os.Stat(debugPath); err == nil {
				return sessionName, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	capture, captureErr := exec.Command("tmux", "capture-pane", "-p", "-t", startPaneID).CombinedOutput()
	if captureErr != nil {
		return "", fmt.Errorf("trupal did not start within deadline (also failed to capture pane %s: %v)", startPaneID, captureErr)
	}
	return "", fmt.Errorf("trupal did not start within deadline; pane %s output:\n%s", startPaneID, string(capture))
}

func (r *Runner) stopTrupal(projectDir string) error {
	cmd := exec.Command(r.trupalBin, "stop", "--close", projectDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			combined := strings.TrimSpace(string(out))
			if strings.Contains(combined, "trupal is not running") {
				return nil
			}
		}
		return fmt.Errorf("stop trupal: %w\n%s", err, string(out))
	}
	return nil
}

func benchAgentCommand(provider string) string {
	switch normalizeBenchProvider(provider) {
	case "codex":
		return "codex"
	default:
		return "claude"
	}
}

func (r *Runner) runAgent(result *RunResult) (time.Time, time.Time, error) {
	if err := os.MkdirAll(result.Artifacts.RootDir, 0755); err != nil {
		return time.Time{}, time.Time{}, err
	}
	stdoutFile, err := os.Create(result.Artifacts.AgentStdoutPath)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("create agent stdout log: %w", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(result.Artifacts.AgentStderrPath)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("create agent stderr log: %w", err)
	}
	defer stderrFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), result.Scenario.Timeout)
	defer cancel()

	provider := result.Scenario.SessionProvider()
	command := benchAgentCommand(provider)
	if _, err := exec.LookPath(command); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("required binary %q not found: %w", command, err)
	}
	args := benchmarkAgentArgs(provider, result.Scenario.EffectiveAgentModel(), result.Scenario.TaskPrompt, result.ProjectDir)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = result.ProjectDir
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start agent: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	for {
		if result.SessionJSONL == "" {
			result.SessionJSONL, _ = FindLatestSessionJSONL(result.ProjectDir, provider)
		}

		select {
		case waitErr = <-waitCh:
			finished := time.Now()
			result.AgentDuration = finished.Sub(started)
			result.AgentExitCode = exitCode(waitErr)
			if ctx.Err() == context.DeadlineExceeded {
				result.TimedOut = true
			}
			if waitErr != nil {
				result.AgentError = waitErr.Error()
			}
			return started, finished, nil
		case <-time.After(1 * time.Second):
			if ctx.Err() == context.DeadlineExceeded {
				continue
			}
		}
	}
}

func benchmarkAgentArgs(provider, model, task, projectDir string) []string {
	prompt := benchmarkAgentPrompt(task)
	switch normalizeBenchProvider(provider) {
	case "codex":
		args := []string{
			"exec",
			"--skip-git-repo-check",
			"-C", projectDir,
		}
		if strings.TrimSpace(model) != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return args
	default:
		return []string{
			"-p",
			"--model", model,
			"--dangerously-skip-permissions",
			prompt,
		}
	}
}

func benchmarkAgentPrompt(task string) string {
	task = strings.TrimSpace(task)
	return strings.TrimSpace(`
You are running inside a non-interactive benchmark harness.

Requirements for this run:
- Work autonomously in the current working directory.
- Make reasonable assumptions yourself and implement the task directly.
- Avoid asking the user questions during the run.
- Verify the resulting code with focused commands before you finish.
- End with a brief summary of what you changed and what you verified.

Task:
` + "\n" + task)
}

func singleLinePrompt(prompt string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(prompt, "\n", " ")), " ")
}

func (r *Runner) waitForBrain(result *RunResult, ccFinishedAt time.Time) (time.Time, error) {
	debugPath := filepath.Join(result.ProjectDir, ".trupal.debug")
	deadline := ccFinishedAt.Add(benchBrainWaitTimeout(result.Scenario.TrupalConfig.BrainEffort))
	minAnalysisUntil := ccFinishedAt.Add(30 * time.Second)

	var lastResponseAt time.Time
	for {
		summary, err := ParseDebugLog(debugPath, result.StartedAt)
		if err != nil {
			return time.Time{}, err
		}

		respondedWithNudges := false
		for _, event := range summary.ResponseEvents {
			if event.Time.Before(ccFinishedAt) {
				continue
			}
			if event.Time.After(lastResponseAt) {
				lastResponseAt = event.Time
			}
			if event.Nudges > 0 {
				respondedWithNudges = true
			}
		}

		now := time.Now()
		if respondedWithNudges {
			return lastResponseAt, nil
		}
		if !lastResponseAt.IsZero() && !now.Before(minAnalysisUntil) {
			return lastResponseAt, nil
		}
		if !now.Before(deadline) {
			return lastResponseAt, nil
		}

		sleepFor := 2 * time.Second
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor <= 0 {
			return lastResponseAt, nil
		}
		time.Sleep(sleepFor)
	}
}

func benchBrainWaitTimeout(effort string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low":
		return 45 * time.Second
	case "medium":
		return 75 * time.Second
	case "max":
		return 150 * time.Second
	default:
		return 105 * time.Second
	}
}

func (r *Runner) runCodexAudit(result *RunResult) *CodexAuditResult {
	if strings.TrimSpace(r.opts.CodexCmd) == "" {
		return nil
	}

	prompt := buildCodexAuditPrompt(result.Scenario)
	if err := os.WriteFile(result.Artifacts.CodexPromptPath, []byte(prompt), 0644); err != nil {
		return &CodexAuditResult{
			Command: r.opts.CodexCmd,
			Error:   err.Error(),
		}
	}

	stdoutFile, err := os.Create(result.Artifacts.CodexStdoutPath)
	if err != nil {
		return &CodexAuditResult{
			Command: r.opts.CodexCmd,
			Error:   err.Error(),
		}
	}
	defer stdoutFile.Close()

	stderrFile, err := os.Create(result.Artifacts.CodexStderrPath)
	if err != nil {
		return &CodexAuditResult{
			Command: r.opts.CodexCmd,
			Error:   err.Error(),
		}
	}
	defer stderrFile.Close()

	cmd := exec.Command("sh", "-lc", r.opts.CodexCmd)
	cmd.Dir = result.ProjectDir
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = append(os.Environ(),
		"TRUPAL_BENCH_PROMPT_FILE="+result.Artifacts.CodexPromptPath,
		"TRUPAL_BENCH_PROJECT_DIR="+result.ProjectDir,
		"TRUPAL_BENCH_SCENARIO_ID="+result.Scenario.ID,
	)

	started := time.Now()
	err = cmd.Run()
	return &CodexAuditResult{
		Command:  r.opts.CodexCmd,
		ExitCode: exitCode(err),
		Duration: time.Since(started),
		Error:    errorString(err),
	}
}

func (r *Runner) stopTmuxSession(sessionName string) {
	if strings.TrimSpace(sessionName) == "" {
		return
	}
	_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
}

func buildCodexAuditPrompt(scenario Scenario) string {
	return strings.TrimSpace(fmt.Sprintf(`
Audit the project in the current working directory for correctness bugs and verification gaps.

Scenario: %s
Category: %s

Original task:
%s

Focus on concrete correctness issues in the final code. Do not modify files.
`, scenario.Name, scenario.Category, scenario.TaskPrompt))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func tmuxSessionName(scenarioID string) string {
	name := strings.TrimSpace(scenarioID)
	if name == "" {
		return "trupal-bench"
	}
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, prefix+value)
}
