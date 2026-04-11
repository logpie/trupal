package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ArtifactSet struct {
	RootDir             string
	PaneCapturePath     string
	DebugLogPath        string
	TrupalLogPath       string
	SteerLogPath        string
	SessionJSONLPath    string
	ProjectCopyDir      string
	AgentStdoutPath     string
	AgentStderrPath     string
	ScenarioYAMLPath    string
	TaskPath            string
	TruthPath           string
	ReportPath          string
	CodexPromptPath     string
	CodexStdoutPath     string
	CodexStderrPath     string
	EvalOutputPath      string
	BenchmarkStatusPath string
}

func NewArtifactSet(rootDir string) ArtifactSet {
	return ArtifactSet{
		RootDir:             rootDir,
		PaneCapturePath:     filepath.Join(rootDir, "pane.txt"),
		DebugLogPath:        filepath.Join(rootDir, "trupal.debug"),
		TrupalLogPath:       filepath.Join(rootDir, "trupal.log"),
		SteerLogPath:        filepath.Join(rootDir, "trupal.steer.jsonl"),
		SessionJSONLPath:    filepath.Join(rootDir, "session.jsonl"),
		ProjectCopyDir:      filepath.Join(rootDir, "_project"),
		AgentStdoutPath:     filepath.Join(rootDir, "agent.stdout.log"),
		AgentStderrPath:     filepath.Join(rootDir, "agent.stderr.log"),
		ScenarioYAMLPath:    filepath.Join(rootDir, "scenario.yaml"),
		TaskPath:            filepath.Join(rootDir, "task.md"),
		TruthPath:           filepath.Join(rootDir, "truth.json"),
		ReportPath:          filepath.Join(rootDir, "report.md"),
		CodexPromptPath:     filepath.Join(rootDir, "codex-audit-prompt.txt"),
		CodexStdoutPath:     filepath.Join(rootDir, "codex.stdout.log"),
		CodexStderrPath:     filepath.Join(rootDir, "codex.stderr.log"),
		EvalOutputPath:      filepath.Join(rootDir, "eval.log"),
		BenchmarkStatusPath: filepath.Join(rootDir, "bench.status.json"),
	}
}

func CopyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dstDir, 0755)
		}

		if strings.HasPrefix(rel, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, ".venv") || strings.HasPrefix(rel, ".pytest_cache") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func CollectArtifacts(projectDir string, artifacts ArtifactSet, sessionJSONL, paneID string) error {
	if err := os.MkdirAll(artifacts.RootDir, 0755); err != nil {
		return fmt.Errorf("create artifacts dir: %w", err)
	}

	if paneID != "" {
		if err := CaptureTmuxPane(paneID, artifacts.PaneCapturePath); err != nil {
			return err
		}
	}

	if err := copyFileIfExists(filepath.Join(projectDir, ".trupal.debug"), artifacts.DebugLogPath); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(projectDir, ".trupal.log"), artifacts.TrupalLogPath); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(projectDir, ".trupal.steer.jsonl"), artifacts.SteerLogPath); err != nil {
		return err
	}
	if err := copyFileIfExists(filepath.Join(projectDir, ".trupal.bench.status.json"), artifacts.BenchmarkStatusPath); err != nil {
		return err
	}
	if sessionJSONL != "" {
		if err := copyFileIfExists(sessionJSONL, artifacts.SessionJSONLPath); err != nil {
			return err
		}
	}

	return CopyTree(projectDir, artifacts.ProjectCopyDir)
}

func CaptureTmuxPane(paneID, dstPath string) error {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	cmd := exec.Command("tmux", "capture-pane", "-p", "-t", paneID)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("capture tmux pane %s: %w", paneID, err)
	}
	return os.WriteFile(dstPath, out, 0644)
}

func FindLatestClaudeSessionJSONL(projectDir string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	encoded := strings.ReplaceAll(filepath.Clean(projectDir), string(os.PathSeparator), "-")
	candidates := []string{
		filepath.Join(homeDir, ".claude", "projects", encoded),
		filepath.Join(homeDir, ".config", "claude", "projects", encoded),
	}

	var bestPath string
	var bestTime time.Time
	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				bestPath = filepath.Join(dir, entry.Name())
			}
		}
	}

	return bestPath, nil
}

func FindLatestSessionJSONL(projectDir, provider string) (string, error) {
	switch normalizeBenchProvider(provider) {
	case "codex":
		return findLatestCodexSessionJSONL(projectDir)
	default:
		return FindLatestClaudeSessionJSONL(projectDir)
	}
}

func findLatestCodexSessionJSONL(projectDir string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	sessionsRoot := filepath.Join(homeDir, ".codex", "sessions")
	targets := sessionSearchDirs(projectDir)
	var bestPath string
	var bestTime time.Time
	_ = filepath.WalkDir(sessionsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		cwd, ok := codexSessionCWD(path)
		if !ok || !codexSessionMatchesTargets(cwd, targets) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			bestPath = path
		}
		return nil
	})
	return bestPath, nil
}

func sessionSearchDirs(projectDir string) []string {
	projectDir = filepath.Clean(projectDir)
	dirs := []string{projectDir}
	if gitRoot, err := findGitRoot(projectDir); err == nil && gitRoot != projectDir {
		dirs = append(dirs, gitRoot)
	}
	return dirs
}

func findGitRoot(dir string) (string, error) {
	current := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no git repository found from %s", dir)
		}
		current = parent
	}
}

func codexSessionMatchesTargets(cwd string, targets []string) bool {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" {
		return false
	}
	cwdRoot, err := findGitRoot(cwd)
	if err != nil {
		cwdRoot = cwd
	}
	for _, target := range targets {
		target = filepath.Clean(strings.TrimSpace(target))
		if target == "" {
			continue
		}
		targetRoot, err := findGitRoot(target)
		if err != nil {
			targetRoot = target
		}
		if cwd == target || cwd == targetRoot || cwdRoot == target || cwdRoot == targetRoot {
			return true
		}
	}
	return false
}

func codexSessionCWD(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Type    string `json:"type"`
			Payload struct {
				Cwd string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		if (raw.Type == "session_meta" || raw.Type == "turn_context") && strings.TrimSpace(raw.Payload.Cwd) != "" {
			return raw.Payload.Cwd, true
		}
	}
	return "", false
}

func ReadPaneID(pidFile string) (string, error) {
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func copyFileIfExists(srcPath, dstPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyFile(srcPath, dstPath, info.Mode())
}

func copyFile(srcPath, dstPath string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}
