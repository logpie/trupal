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
	StartedAt      time.Time
	FinishedAt     time.Time
	Duration       time.Duration
	ProjectDir     string
	TimedOut       bool
	ClaudeDuration time.Duration
	ClaudeExitCode int
	ClaudeError    string
	SessionJSONL   string
	Artifacts      ArtifactSet
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
	if err := os.MkdirAll(opts.ResultsDir, 0755); err != nil {
		return nil, fmt.Errorf("create results dir: %w", err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("find go binary: %w", err)
	}
	required := []string{"git", "tmux", "claude"}
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
	return r.runScenario(scenario)
}

func (r *Runner) RunAll() ([]*RunResult, error) {
	scenarios, err := LoadAllScenarios(r.opts.ScenariosDir)
	if err != nil {
		return nil, err
	}

	results := make([]*RunResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		result, err := r.runScenario(scenario)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Runner) runScenario(scenario Scenario) (*RunResult, error) {
	setupStartedAt := time.Now()
	runSlug := setupStartedAt.Format("20060102-150405") + "-" + scenario.ID
	artifactsDir := filepath.Join(r.opts.ResultsDir, runSlug)
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, fmt.Errorf("create artifacts dir: %w", err)
	}

	result := &RunResult{
		Scenario:  scenario,
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
	if err := r.writeScenarioConfig(projectDir, scenario.TrupalConfig); err != nil {
		return nil, err
	}
	if err := r.initGitRepo(projectDir); err != nil {
		return nil, err
	}
	if err := r.copyScenarioInputs(result); err != nil {
		return nil, err
	}

	ccStartedAt, ccFinishedAt, err := r.runClaude(result)
	if err != nil {
		return nil, err
	}
	result.StartedAt = ccStartedAt
	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestClaudeSessionJSONL(projectDir)
	}

	sessionName, err := r.startTrupal(projectDir, scenario.ID)
	if err != nil {
		return nil, err
	}
	defer r.stopTmuxSession(sessionName)

	brainFinishedAt, err := r.waitForBrain(result, ccFinishedAt)
	if err != nil {
		return nil, err
	}
	if brainFinishedAt.IsZero() {
		brainFinishedAt = ccFinishedAt
	}
	result.FinishedAt = brainFinishedAt
	result.Duration = result.FinishedAt.Sub(result.StartedAt)

	pidFile := filepath.Join(projectDir, ".trupal.pid")
	paneID, _ := ReadPaneID(pidFile)
	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestClaudeSessionJSONL(projectDir)
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
	result.Score = ScoreFindings(scenario.Truth, findings, edits, debugSummary)

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

func (r *Runner) writeScenarioConfig(projectDir string, cfg TrupalConfig) error {
	var lines []string
	if cfg.BuildCmd != "" {
		lines = append(lines, fmt.Sprintf("build_cmd = %q", cfg.BuildCmd))
	}
	if cfg.BrainModel != "" {
		lines = append(lines, fmt.Sprintf("brain_model = %q", cfg.BrainModel))
	}
	if cfg.BrainEffort != "" {
		lines = append(lines, fmt.Sprintf("brain_effort = %q", cfg.BrainEffort))
	}
	lines = append(lines, `brain_provider = "claude"`)
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

func (r *Runner) runClaude(result *RunResult) (time.Time, time.Time, error) {
	if err := os.MkdirAll(result.Artifacts.RootDir, 0755); err != nil {
		return time.Time{}, time.Time{}, err
	}
	stdoutFile, err := os.Create(result.Artifacts.ClaudeStdoutPath)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("create claude stdout log: %w", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(result.Artifacts.ClaudeStderrPath)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("create claude stderr log: %w", err)
	}
	defer stderrFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), result.Scenario.Timeout)
	defer cancel()

	args := []string{
		"-p",
		"--model", result.Scenario.ClaudeModel,
		"--dangerously-skip-permissions",
		result.Scenario.TaskPrompt,
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = result.ProjectDir
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start claude: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	for {
		if result.SessionJSONL == "" {
			result.SessionJSONL, _ = FindLatestClaudeSessionJSONL(result.ProjectDir)
		}

		select {
		case waitErr = <-waitCh:
			finished := time.Now()
			result.ClaudeDuration = finished.Sub(started)
			result.ClaudeExitCode = exitCode(waitErr)
			if ctx.Err() == context.DeadlineExceeded {
				result.TimedOut = true
			}
			if waitErr != nil {
				result.ClaudeError = waitErr.Error()
			}
			return started, finished, nil
		case <-time.After(1 * time.Second):
			if ctx.Err() == context.DeadlineExceeded {
				continue
			}
		}
	}
}

func (r *Runner) waitForBrain(result *RunResult, ccFinishedAt time.Time) (time.Time, error) {
	debugPath := filepath.Join(result.ProjectDir, ".trupal.debug")
	deadline := result.StartedAt.Add(result.Scenario.Timeout)
	minAnalysisUntil := ccFinishedAt.Add(30 * time.Second)
	if minAnalysisUntil.After(deadline) {
		minAnalysisUntil = deadline
	}

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
