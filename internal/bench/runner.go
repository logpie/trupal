package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type RunnerOptions struct {
	RepoRoot             string
	ScenariosDir         string
	ResultsDir           string
	SWEBenchDir          string
	CodexCmd             string
	SteeringModeOverride SteeringMode
	KeepTemp             bool
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
	Scenario            Scenario
	Arm                 BenchmarkArm
	SWEBenchTask        *SWEBenchTask
	StartedAt           time.Time
	FinishedAt          time.Time
	Duration            time.Duration
	ProjectDir          string
	TimedOut            bool
	StopReason          BenchmarkStopReason
	AgentDuration       time.Duration
	AgentExitCode       int
	AgentError          string
	SessionJSONL        string
	Artifacts           ArtifactSet
	SteeringEvents      []SteeringEvent
	GeneratedNudges     int
	SentNudges          int
	UnsentNudges        int
	FirstGeneratedNudge time.Duration
	FirstSentNudge      time.Duration
	Score               Scorecard
	CodexAudit          *CodexAuditResult
	SWEBenchSolved      bool
	SWEBenchEvalCommand string
}

type SteeringPolicy struct {
	Mode     SteeringMode
	Rounds   int
	Cooldown time.Duration
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
	scenario = r.applyScenarioOverrides(scenario)
	arms := scenario.EffectiveBenchmarkArms()
	return r.runScenario(scenario, arms[0])
}

func (r *Runner) RunScenarioArm(name string, arm BenchmarkArm) (*RunResult, error) {
	scenario, err := LoadScenario(r.opts.ScenariosDir, name)
	if err != nil {
		return nil, err
	}
	scenario = r.applyScenarioOverrides(scenario)
	return r.runScenario(scenario, arm)
}

func (r *Runner) RunScenarioPair(name string) ([]*RunResult, error) {
	scenario, err := LoadScenario(r.opts.ScenariosDir, name)
	if err != nil {
		return nil, err
	}
	scenario = r.applyScenarioOverrides(scenario)
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

func (r *Runner) applyScenarioOverrides(s Scenario) Scenario {
	if r.opts.SteeringModeOverride != "" {
		s.SteeringMode = r.opts.SteeringModeOverride
	}
	return s
}

func (r *Runner) RunSWEBenchTask(manifestPath, instanceID string, arm BenchmarkArm, evalCommand string) (*RunResult, error) {
	task, err := LoadSWEBenchTask(manifestPath, instanceID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(evalCommand) != "" {
		task.EvalCommand = strings.TrimSpace(evalCommand)
	}
	task.DockerImage = task.EffectiveDockerImage()
	if r.opts.SteeringModeOverride != "" {
		task.SteeringMode = string(r.opts.SteeringModeOverride)
	}
	scenarioCfg := scenarioConfigForSWEBenchTask(task)
	started := time.Now()
	runSlug := started.Format("20060102-150405") + "-" + task.Slug() + "-" + string(arm)
	artifactsDir := filepath.Join(r.opts.ResultsDir, runSlug)
	if err := os.MkdirAll(artifactsDir, 0755); err != nil {
		return nil, fmt.Errorf("create artifacts dir: %w", err)
	}
	result := &RunResult{
		Arm:          arm,
		Scenario:     scenarioCfg,
		SWEBenchTask: &task,
		Artifacts:    NewArtifactSet(artifactsDir),
	}
	workspace, err := os.MkdirTemp("", "trupal-swebench-run-"+task.Slug()+"-")
	if err != nil {
		return nil, err
	}
	result.ProjectDir = workspace
	if !r.opts.KeepTemp {
		defer os.RemoveAll(workspace)
	}
	if err := r.PrepareSWEBenchWorkspace(task, workspace); err != nil {
		return nil, err
	}
	if err := r.SetupSWEBenchWorkspace(task, workspace); err != nil {
		return nil, err
	}
	if strings.TrimSpace(task.TestPatch) != "" {
		if err := r.applySWEBenchTestPatch(task.TestPatch, workspace); err != nil {
			return nil, fmt.Errorf("apply SWE-bench test patch before agent run: %w", err)
		}
	}
	if err := r.writeScenarioConfig(workspace, scenarioCfg, arm); err != nil {
		return nil, err
	}

	sessionName, codexPaneID, trupalPaneID, err := r.startInteractiveCodexSession(workspace, task.Slug(), "gpt-5.4-mini")
	if err != nil {
		return nil, err
	}
	defer r.stopTmuxSession(sessionName)

	if err := r.waitForCodexReady(codexPaneID); err != nil {
		return nil, err
	}
	if err := r.waitForTrupalWatch(workspace, trupalPaneID); err != nil {
		return nil, err
	}
	if arm == ArmSteer {
		if err := r.waitForTrupalReady(trupalPaneID); err != nil {
			return nil, err
		}
		if err := r.sendKeys(trupalPaneID, "a"); err != nil {
			return nil, err
		}
		if err := r.waitForTrupalAuto(trupalPaneID); err != nil {
			return nil, err
		}
	}
	result.StartedAt = time.Now()
	if err := r.submitLiteral(codexPaneID, singleLinePrompt(benchmarkAgentPrompt(task.ProblemStatement))); err != nil {
		return nil, err
	}
	sessionJSONL, err := r.waitForBenchmarkSessionJSONL(workspace, "codex", 20*time.Second, codexPaneID)
	if err != nil {
		return nil, err
	}
	result.SessionJSONL = sessionJSONL
	finishedAt, exitCode, timedOut, err := r.waitForInteractiveCodex(result, codexPaneID, trupalPaneID)
	if err != nil {
		return nil, err
	}
	result.FinishedAt = finishedAt
	result.Duration = finishedAt.Sub(result.StartedAt)
	result.AgentDuration = result.Duration
	result.AgentExitCode = exitCode
	result.TimedOut = timedOut

	if result.SessionJSONL == "" {
		result.SessionJSONL, _ = FindLatestSessionJSONL(workspace, "codex")
	}
	var evalOutput string
	var evalErr error
	if strings.TrimSpace(task.DockerImage) != "" && strings.TrimSpace(task.DockerEvalCommand) != "" {
		evalOutput, evalErr = r.EvaluateSWEBenchTaskDocker(task, workspace)
		result.SWEBenchEvalCommand = task.DockerEvalCommand
	} else if strings.TrimSpace(task.DockerImage) != "" && strings.TrimSpace(task.RunScriptURL) != "" {
		evalOutput, evalErr = r.EvaluateSWEBenchTaskDocker(task, workspace)
		result.SWEBenchEvalCommand = task.RunScriptURL
	} else {
		evalOutput, evalErr = r.runSWEBenchEvalCommand(task.EvalCommand, workspace)
		result.SWEBenchEvalCommand = task.EvalCommand
	}
	if err := os.WriteFile(result.Artifacts.EvalOutputPath, []byte(evalOutput), 0644); err != nil {
		return nil, err
	}
	if evalErr == nil {
		result.SWEBenchSolved = true
	}
	paneID, _ := ReadPaneID(filepath.Join(workspace, ".trupal.pid"))
	if strings.TrimSpace(paneID) == "" {
		paneID = trupalPaneID
	}
	if err := CollectArtifacts(workspace, result.Artifacts, result.SessionJSONL, paneID); err != nil {
		return nil, err
	}
	result.SteeringEvents, _ = ParseSteeringEvents(result.Artifacts.SteerLogPath)
	debugSummary, _ := ParseDebugLog(result.Artifacts.DebugLogPath, result.StartedAt)
	cutoff := benchmarkTelemetryCutoff(result.FinishedAt)
	debugSummary = filterDebugSummaryByCutoff(debugSummary, cutoff)
	result.SteeringEvents = filterSteeringEventsByCutoff(result.SteeringEvents, cutoff)
	result.Score.SteeringEventCount = len(result.SteeringEvents)
	result.applySteeringTelemetry(debugSummary)
	if err := WriteReport(result.Artifacts.ReportPath, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runner) waitForBenchmarkSessionJSONL(projectDir, provider string, timeout time.Duration, codexPaneID string) (string, error) {
	deadline := time.Now().Add(timeout)
	retryCount := 0
	for time.Now().Before(deadline) {
		if path, _ := FindLatestSessionJSONL(projectDir, provider); strings.TrimSpace(path) != "" {
			return path, nil
		}
		if retryCount < 3 && time.Until(deadline) < timeout/2 {
			capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
			text := string(capture)
			if strings.Contains(text, "[Pasted Content") || strings.Contains(text, "› ") {
				_ = r.sendKeys(codexPaneID, "C-m")
				if strings.Contains(text, "Update available!") {
					time.Sleep(500 * time.Millisecond)
					_ = r.sendKeys(codexPaneID, "C-m")
				}
				retryCount++
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
	return "", fmt.Errorf("benchmark session JSONL not found for %s under %s within %s; codex pane:\n%s", provider, projectDir, timeout, string(capture))
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

func (r *Runner) SetupSWEBenchWorkspace(task SWEBenchTask, workspace string) error {
	if strings.TrimSpace(task.SetupCommand) == "" {
		return nil
	}
	cmd := exec.Command("sh", "-lc", task.SetupCommand)
	cmd.Dir = workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setup swebench workspace: %w\n%s", err, string(out))
	}
	return nil
}

func (r *Runner) SetupSWEBenchPostPatch(task SWEBenchTask, workspace string) error {
	if strings.TrimSpace(task.PostPatchSetupCommand) == "" {
		return nil
	}
	cmd := exec.Command("sh", "-lc", task.PostPatchSetupCommand)
	cmd.Dir = workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("post-patch swebench setup: %w\n%s", err, string(out))
	}
	return nil
}

func writeSWEBenchTaskArtifact(workspace string, task SWEBenchTask) error {
	return os.WriteFile(filepath.Join(workspace, "TASK.md"), []byte(task.ProblemStatement+"\n"), 0644)
}

func (r *Runner) EvaluateSWEBenchTask(task SWEBenchTask, workspace, evalCommand string) (string, error) {
	if strings.TrimSpace(evalCommand) == "" {
		evalCommand = task.EvalCommand
	}
	if strings.TrimSpace(task.TestPatch) != "" {
		if err := r.applySWEBenchTestPatch(task.TestPatch, workspace); err != nil {
			return "", err
		}
	}
	if err := r.SetupSWEBenchPostPatch(task, workspace); err != nil {
		return "", err
	}
	return r.runSWEBenchEvalCommand(evalCommand, workspace)
}

func (r *Runner) ApplySWEBenchGoldPatch(task SWEBenchTask, workspace string) error {
	if strings.TrimSpace(task.Patch) == "" {
		return nil
	}
	return r.applyPatchString(task.Patch, workspace, ".swebench-gold.patch")
}

func (r *Runner) EvaluateSWEBenchTaskDocker(task SWEBenchTask, workspace string) (string, error) {
	imageRef, err := r.resolveSWEBenchDockerImage(task)
	if err != nil {
		return "", err
	}
	evalCommand, err := r.resolveSWEBenchDockerEvalCommand(task, workspace)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(task.TestPatch) != "" {
		if err := r.applySWEBenchTestPatch(task.TestPatch, workspace); err != nil {
			return "", err
		}
	}
	if err := r.SetupSWEBenchPostPatch(task, workspace); err != nil {
		return "", err
	}
	cmd := exec.Command(
		"docker", "run", "--rm",
		"--entrypoint", "bash",
		"-v", workspace+":/workspace",
		"-w", "/workspace",
		imageRef,
		"-lc", evalCommand,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker eval: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *Runner) resolveSWEBenchDockerImage(task SWEBenchTask) (string, error) {
	imageRef := strings.TrimSpace(task.EffectiveDockerImage())
	if imageRef == "" {
		return "", fmt.Errorf("no docker image available for %s", task.InstanceID)
	}
	if err := dockerPullImage(imageRef); err == nil {
		return imageRef, nil
	}
	if err := dockerInspectImage(imageRef); err == nil {
		return imageRef, nil
	}
	localImageRef, err := r.buildLocalSWEBenchProImage(task)
	if err != nil {
		return "", fmt.Errorf("resolve docker image %q: %w", imageRef, err)
	}
	return localImageRef, nil
}

func dockerPullImage(imageRef string) error {
	cmd := exec.Command("docker", "pull", imageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull %s: %w\n%s", imageRef, err, string(out))
	}
	return nil
}

func dockerInspectImage(imageRef string) error {
	cmd := exec.Command("docker", "image", "inspect", imageRef)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker inspect %s: %w\n%s", imageRef, err, string(out))
	}
	return nil
}

func (r *Runner) buildLocalSWEBenchProImage(task SWEBenchTask) (string, error) {
	supportRoot, err := r.ensureSWEBenchProSupportRepo()
	if err != nil {
		return "", err
	}
	baseDockerfile := filepath.Join(supportRoot, "dockerfiles", "base_dockerfile", task.InstanceID, "Dockerfile")
	instanceDockerfile := filepath.Join(supportRoot, "dockerfiles", "instance_dockerfile", task.InstanceID, "Dockerfile")
	baseContent, err := os.ReadFile(baseDockerfile)
	if err != nil {
		return "", fmt.Errorf("read base dockerfile: %w", err)
	}
	instanceContent, err := os.ReadFile(instanceDockerfile)
	if err != nil {
		return "", fmt.Errorf("read instance dockerfile: %w", err)
	}
	baseImageRef := localSWEBenchProBaseImageRef(task)
	finalImageRef := localSWEBenchProInstanceImageRef(task)
	if err := dockerInspectImage(finalImageRef); err == nil {
		return finalImageRef, nil
	}
	if err := dockerBuildImage(baseImageRef, baseContent, supportRoot); err != nil {
		return "", err
	}
	patchedInstance, err := rewriteDockerfileForLocalBuild(string(instanceContent), baseImageRef)
	if err != nil {
		return "", err
	}
	if err := dockerBuildImage(finalImageRef, []byte(patchedInstance), supportRoot); err != nil {
		return "", err
	}
	return finalImageRef, nil
}

func (r *Runner) ensureSWEBenchProSupportRepo() (string, error) {
	dir := filepath.Join(r.opts.RepoRoot, ".omx", "cache", "swebench-pro-os")
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return "", fmt.Errorf("create swebench-pro-os cache dir: %w", err)
	}
	cmd := exec.Command("git", "clone", "--depth", "1", "https://github.com/scaleapi/SWE-bench_Pro-os.git", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("clone SWE-bench Pro support repo: %w\n%s", err, string(out))
	}
	return dir, nil
}

func localSWEBenchProBaseImageRef(task SWEBenchTask) string {
	return "trupal-swebench-base:" + sanitizeDockerTag(task.InstanceID)
}

func localSWEBenchProInstanceImageRef(task SWEBenchTask) string {
	return "trupal-swebench-task:" + sanitizeDockerTag(task.InstanceID)
}

func sanitizeDockerTag(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "-", "_", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-.")
	if value == "" {
		return "task"
	}
	return value
}

func rewriteDockerfileForLocalBuild(dockerfile, baseImageRef string) (string, error) {
	lines := strings.Split(dockerfile, "\n")
	insertedEnv := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "FROM ") {
			lines[i] = "FROM " + baseImageRef
			lines = append(lines[:i+1], append([]string{"ENV PIP_BREAK_SYSTEM_PACKAGES=1"}, lines[i+1:]...)...)
			insertedEnv = true
			break
		}
	}
	if !insertedEnv {
		return "", fmt.Errorf("dockerfile missing FROM line")
	}
	return strings.Join(lines, "\n"), nil
}

func dockerBuildImage(imageRef string, dockerfileContent []byte, contextDir string) error {
	tmp, err := os.CreateTemp("", "trupal-swebench-dockerfile-*.Dockerfile")
	if err != nil {
		return fmt.Errorf("create temp dockerfile: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(dockerfileContent); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp dockerfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp dockerfile: %w", err)
	}
	cmd := exec.Command("docker", "build", "-t", imageRef, "-f", tmp.Name(), contextDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build %s: %w\n%s", imageRef, err, string(out))
	}
	return nil
}

func (r *Runner) resolveSWEBenchDockerEvalCommand(task SWEBenchTask, workspace string) (string, error) {
	if strings.TrimSpace(task.DockerEvalCommand) != "" {
		return task.DockerEvalCommand, nil
	}
	if strings.TrimSpace(task.RunScriptURL) == "" {
		return "", fmt.Errorf("no docker_evaluation_command or run_script provided")
	}
	scriptPath := filepath.Join(workspace, ".swebench-run_script.sh")
	if err := downloadFile(task.RunScriptURL, scriptPath); err != nil {
		return "", fmt.Errorf("download run_script: %w", err)
	}
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return "", fmt.Errorf("chmod run_script: %w", err)
	}
	var selected string
	if len(task.SelectedTests) > 0 {
		selected = shellQuote(strings.Join(task.SelectedTests, ","))
	}
	cmd := "cp -a /workspace/. /app/ && cd /app && bash /workspace/.swebench-run_script.sh"
	if selected != "" {
		cmd += " " + selected
	}
	return cmd, nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url) //nolint:gosec // benchmark harness downloads public scripts intentionally
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func (r *Runner) runSWEBenchEvalCommand(evalCommand, workspace string) (string, error) {
	if strings.TrimSpace(evalCommand) == "" {
		return "", fmt.Errorf("no evaluation command provided")
	}
	cmd := exec.Command("sh", "-lc", evalCommand)
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("evaluate swebench task: %w\n%s", err, string(out))
	}
	return string(out), nil
}

func (r *Runner) applySWEBenchTestPatch(testPatch, workspace string) error {
	return r.applyPatchString(testPatch, workspace, ".swebench-test.patch")
}

func (r *Runner) applyPatchString(testPatch, workspace, filename string) error {
	patchPath := filepath.Join(workspace, ".swebench-test.patch")
	patch := testPatch
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	patchPath = filepath.Join(workspace, filename)
	if err := os.WriteFile(patchPath, []byte(patch), 0644); err != nil {
		return err
	}
	cmd := exec.Command("git", "apply", patchPath)
	cmd.Dir = workspace
	if out, err := cmd.CombinedOutput(); err != nil {
		reverseCheck := exec.Command("git", "apply", "--reverse", "--check", patchPath)
		reverseCheck.Dir = workspace
		if reverseOut, reverseErr := reverseCheck.CombinedOutput(); reverseErr == nil {
			return nil
		} else if strings.Contains(string(reverseOut), "applies cleanly") {
			return nil
		}
		fallback := exec.Command("sh", "-lc", "patch -p1 < "+filename)
		fallback.Dir = workspace
		if patchOut, patchErr := fallback.CombinedOutput(); patchErr != nil {
			combined := string(patchOut)
			if strings.Contains(combined, "Reversed (or previously applied) patch detected") {
				return nil
			}
			return fmt.Errorf("git apply test patch: %w\n%s\npatch fallback: %v\n%s", err, string(out), patchErr, combined)
		}
	}
	return nil
}

func (r *Runner) RunAll() ([]*RunResult, error) {
	scenarios, err := LoadAllScenarios(r.opts.ScenariosDir)
	if err != nil {
		return nil, err
	}

	results := make([]*RunResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		scenario = r.applyScenarioOverrides(scenario)
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
	cutoff := benchmarkTelemetryCutoff(result.FinishedAt)
	debugSummary = filterDebugSummaryByCutoff(debugSummary, cutoff)
	result.SteeringEvents = filterSteeringEventsByCutoff(result.SteeringEvents, cutoff)
	result.applySteeringTelemetry(debugSummary)
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
	cutoff := benchmarkTelemetryCutoff(result.FinishedAt)
	debugSummary = filterDebugSummaryByCutoff(debugSummary, cutoff)
	result.SteeringEvents = filterSteeringEventsByCutoff(result.SteeringEvents, cutoff)
	result.applySteeringTelemetry(debugSummary)
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
	out, err := exec.Command(
		"tmux", "new-session", "-d", "-P", "-F", "#{pane_id}",
		"-s", sessionName, "-c", projectDir,
		"bash", "-lc", wrapInteractiveCommandForTmux(codexCmd),
	).CombinedOutput()
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

func wrapInteractiveCommandForTmux(command string) string {
	return command + `; status=$?; printf '\n__CODEX_EXIT__:%s\n' "$status"; exec bash`
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

func (r *Runner) waitForTrupalReady(trupalPaneID string) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", trupalPaneID).CombinedOutput()
		text := string(capture)
		if trupalTUIReady(text) {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", trupalPaneID).CombinedOutput()
	return fmt.Errorf("trupal TUI not ready for steering toggle:\n%s", string(capture))
}

func trupalTUIReady(text string) bool {
	if strings.Contains(text, "send s") && strings.Contains(text, "auto a") {
		return true
	}
	return strings.Contains(text, "TRUPAL") &&
		(strings.Contains(text, "steer manual") || strings.Contains(text, "steer auto"))
}

func (r *Runner) waitForTrupalAuto(trupalPaneID string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", trupalPaneID).CombinedOutput()
		if strings.Contains(string(capture), "steer auto") {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", trupalPaneID).CombinedOutput()
	return fmt.Errorf("trupal did not switch to auto steer:\n%s", string(capture))
}

func (r *Runner) waitForCodexReady(codexPaneID string) error {
	deadline := time.Now().Add(20 * time.Second)
	sawTrustPrompt := false
	for time.Now().Before(deadline) {
		capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
		text := string(capture)
		switch codexReadyPromptAction(text) {
		case "trust":
			sawTrustPrompt = true
			if err := r.sendKeys(codexPaneID, "C-m"); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		case "skip_update":
			if err := r.sendKeys(codexPaneID, "Down"); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			if err := r.sendKeys(codexPaneID, "C-m"); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		case "dismiss_update":
			if err := r.sendKeys(codexPaneID, "C-m"); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
		case "ready":
			if !sawTrustPrompt {
				time.Sleep(1500 * time.Millisecond)
				verify, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
				if codexReadyPromptAction(string(verify)) == "trust" {
					continue
				}
			}
			return nil
		case "exited":
			return fmt.Errorf("codex exited before becoming ready:\n%s", text)
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}
	capture, _ := exec.Command("tmux", "capture-pane", "-p", "-t", codexPaneID).CombinedOutput()
	return fmt.Errorf("codex did not become ready:\n%s", string(capture))
}

func codexReadyPromptAction(text string) string {
	switch {
	case strings.Contains(text, "Do you trust the contents of this directory?"):
		return "trust"
	case strings.Contains(text, "Update available!") && strings.Contains(text, "Skip until next version"):
		return "skip_update"
	case strings.Contains(text, "Press enter to continue") && strings.Contains(text, "Update available!"):
		return "dismiss_update"
	case strings.Contains(text, "__CODEX_EXIT__:"):
		return "exited"
	case strings.Contains(text, "Use /skills") || strings.Contains(text, "Tip:"):
		return "ready"
	default:
		return ""
	}
}

func (r *Runner) waitForInteractiveCodex(result *RunResult, codexPaneID, trupalPaneID string) (time.Time, int, bool, error) {
	timeout := effectiveInteractiveTimeout(result.Scenario)
	deadline := result.StartedAt.Add(timeout)
	policy := effectiveBenchmarkSteeringPolicy(result.Scenario)
	autoDisabled := result.Arm != ArmSteer
	var graceDeadline time.Time

	for {
		if result.SessionJSONL == "" {
			result.SessionJSONL, _ = FindLatestSessionJSONL(result.ProjectDir, result.Scenario.SessionProvider())
		}

		events, err := ParseSteeringEvents(filepath.Join(result.ProjectDir, ".trupal.steer.jsonl"))
		if err != nil {
			return time.Time{}, -1, false, err
		}
		if result.Arm == ArmSteer && !autoDisabled && policy.Mode == SteeringModeSingle && len(events) >= policy.Rounds {
			if err := r.sendKeys(trupalPaneID, "a"); err != nil {
				return time.Time{}, -1, false, fmt.Errorf("disable auto steer after %d rounds: %w", policy.Rounds, err)
			}
			autoDisabled = true
		}

		now := time.Now()
		runtime, runtimeSeen, err := readBenchmarkRuntimeStatus(result.ProjectDir)
		if err != nil {
			return time.Time{}, -1, false, err
		}
		if result.SessionJSONL != "" {
			if info, statErr := os.Stat(result.SessionJSONL); statErr == nil && info.ModTime().After(runtime.LastSessionEventAt) {
				runtime.LastSessionEventAt = info.ModTime()
			}
			if runtime.AgentStatus == "" {
				runtime.AgentStatus = detectBenchAgentStatus(result.SessionJSONL)
			}
		}
		status := evaluateBenchmarkStatus(now, result.StartedAt, timeout, policy, runtime, runtimeSeen)
		if err := writeBenchmarkStatus(result.ProjectDir, status); err != nil {
			return time.Time{}, -1, false, err
		}
		if status.Reason == BenchmarkStopReasonConverged {
			result.StopReason = BenchmarkStopReasonConverged
			return now, 0, false, nil
		}
		if !now.Before(deadline) {
			if graceDeadline.IsZero() && shouldEnterTimeoutGrace(policy, result.Arm, runtime) {
				graceDeadline = now.Add(benchmarkTimeoutGrace(policy, now, runtime))
			}
			if !graceDeadline.IsZero() && now.Before(graceDeadline) {
				time.Sleep(2 * time.Second)
				continue
			}
			status.State = BenchmarkStateHardTimeout
			status.Reason = BenchmarkStopReasonHardTimeout
			status.UpdatedAt = now
			if err := writeBenchmarkStatus(result.ProjectDir, status); err != nil {
				return time.Time{}, -1, false, err
			}
			result.StopReason = BenchmarkStopReasonHardTimeout
			_ = r.sendKeys(codexPaneID, "C-c")
			return now, 124, true, nil
		}
		time.Sleep(2 * time.Second)
	}
}

func effectiveInteractiveTimeout(s Scenario) time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	if s.SteeringMode == SteeringModeContinuous {
		return 5 * time.Minute
	}
	return 2 * time.Minute
}

func effectiveBenchmarkSteeringPolicy(s Scenario) SteeringPolicy {
	mode := s.SteeringMode
	if mode == "" {
		mode = SteeringModeSingle
	}
	rounds := s.SteeringRounds
	if rounds <= 0 {
		rounds = 1
	}
	cooldown := s.SteeringCooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return SteeringPolicy{
		Mode:     mode,
		Rounds:   rounds,
		Cooldown: cooldown,
	}
}

func scenarioConfigForSWEBenchTask(task SWEBenchTask) Scenario {
	scenarioCfg := Scenario{
		ID:             task.Slug(),
		AgentModel:     "gpt-5.4-mini",
		SteeringMode:   SteeringMode(task.SteeringMode),
		SteeringRounds: task.SteeringRounds,
		TrupalConfig:   TrupalConfig{SessionProvider: "codex", BrainProvider: "codex", BrainModel: "gpt-5.4-mini", BrainEffort: "medium"},
	}
	if strings.TrimSpace(task.Timeout) != "" {
		if d, parseErr := time.ParseDuration(task.Timeout); parseErr == nil {
			scenarioCfg.Timeout = d
		}
	}
	if strings.TrimSpace(task.SteeringCooldown) != "" {
		if d, parseErr := time.ParseDuration(task.SteeringCooldown); parseErr == nil {
			scenarioCfg.SteeringCooldown = d
		}
	}
	return scenarioCfg
}

func (r *RunResult) applySteeringTelemetry(debugSummary DebugSummary) {
	r.GeneratedNudges = len(debugSummary.Nudges)
	r.SentNudges = len(r.SteeringEvents)
	if r.GeneratedNudges > r.SentNudges {
		r.UnsentNudges = r.GeneratedNudges - r.SentNudges
	}
	if len(debugSummary.Nudges) > 0 && !debugSummary.Nudges[0].FirstSeen.IsZero() && !r.StartedAt.IsZero() {
		r.FirstGeneratedNudge = debugSummary.Nudges[0].FirstSeen.Sub(r.StartedAt)
	}
	if len(r.SteeringEvents) > 0 && !r.SteeringEvents[0].Timestamp.IsZero() && !r.StartedAt.IsZero() {
		r.FirstSentNudge = r.SteeringEvents[0].Timestamp.Sub(r.StartedAt)
	}
}

func benchmarkTelemetryCutoff(finishedAt time.Time) time.Time {
	if finishedAt.IsZero() {
		return time.Time{}
	}
	return finishedAt.Add(5 * time.Second)
}

func filterDebugSummaryByCutoff(summary DebugSummary, cutoff time.Time) DebugSummary {
	if cutoff.IsZero() {
		return summary
	}
	summary.Nudges = filterObservedFindingsByCutoff(summary.Nudges, cutoff)
	summary.Observations = filterObservedFindingsByCutoff(summary.Observations, cutoff)
	summary.ResponseEvents = filterResponseEventsByCutoff(summary.ResponseEvents, cutoff)
	summary.ResponseCount = len(summary.ResponseEvents)
	summary.NudgeEventCount = len(summary.Nudges)
	return summary
}

func filterObservedFindingsByCutoff(findings []ObservedFinding, cutoff time.Time) []ObservedFinding {
	filtered := make([]ObservedFinding, 0, len(findings))
	for _, finding := range findings {
		if finding.FirstSeen.IsZero() || !finding.FirstSeen.After(cutoff) {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func filterResponseEventsByCutoff(events []BrainResponseEvent, cutoff time.Time) []BrainResponseEvent {
	filtered := make([]BrainResponseEvent, 0, len(events))
	for _, event := range events {
		if event.Time.IsZero() || !event.Time.After(cutoff) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func filterSteeringEventsByCutoff(events []SteeringEvent, cutoff time.Time) []SteeringEvent {
	if cutoff.IsZero() {
		return events
	}
	filtered := make([]SteeringEvent, 0, len(events))
	for _, event := range events {
		if event.Timestamp.IsZero() || !event.Timestamp.After(cutoff) {
			filtered = append(filtered, event)
		}
	}
	return filtered
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
	if strings.TrimSpace(string(scenario.SteeringMode)) != "" {
		lines = append(lines, fmt.Sprintf("benchmark_steering_mode = %q", scenario.SteeringMode))
	}
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
