package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SWEBenchTask struct {
	InstanceID             string   `json:"instance_id"`
	Repo                   string   `json:"repo"`
	RepoURL                string   `json:"repo_url"`
	BaseCommit             string   `json:"base_commit"`
	EnvironmentSetupCommit string   `json:"environment_setup_commit"`
	ProblemStatement       string   `json:"problem_statement"`
	FailToPass             []string `json:"FAIL_TO_PASS"`
	PassToPass             []string `json:"PASS_TO_PASS"`
	TestPatch              string   `json:"test_patch"`
	Patch                  string   `json:"patch"`
	Version                string   `json:"version"`
	SetupCommand           string   `json:"setup_command"`
	EvalCommand            string   `json:"evaluation_command"`
	DockerImage            string   `json:"docker_image"`
	DockerEvalCommand      string   `json:"docker_evaluation_command"`
	ManifestPath           string   `json:"-"`
}

func LoadSWEBenchTask(manifestPath, instanceID string) (SWEBenchTask, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return SWEBenchTask{}, fmt.Errorf("read SWE-bench manifest: %w", err)
	}

	var single map[string]any
	if err := json.Unmarshal(raw, &single); err == nil && len(single) > 0 {
		task, err := taskFromRawMap(single)
		if err == nil && strings.TrimSpace(task.InstanceID) != "" {
			task.ManifestPath = manifestPath
			if instanceID == "" || task.InstanceID == instanceID {
				return validateSWEBenchTask(task)
			}
			return SWEBenchTask{}, fmt.Errorf("instance %q not found in %s", instanceID, manifestPath)
		}
	}

	var many []map[string]any
	if err := json.Unmarshal(raw, &many); err != nil {
		return SWEBenchTask{}, fmt.Errorf("parse SWE-bench manifest %s: %w", manifestPath, err)
	}
	for _, item := range many {
		task, err := taskFromRawMap(item)
		if err != nil {
			return SWEBenchTask{}, fmt.Errorf("parse SWE-bench manifest %s: %w", manifestPath, err)
		}
		task.ManifestPath = manifestPath
		if instanceID == "" || task.InstanceID == instanceID {
			return validateSWEBenchTask(task)
		}
	}
	return SWEBenchTask{}, fmt.Errorf("instance %q not found in %s", instanceID, manifestPath)
}

func validateSWEBenchTask(task SWEBenchTask) (SWEBenchTask, error) {
	task.InstanceID = strings.TrimSpace(task.InstanceID)
	task.Repo = strings.TrimSpace(task.Repo)
	task.BaseCommit = strings.TrimSpace(task.BaseCommit)
	task.EnvironmentSetupCommit = strings.TrimSpace(task.EnvironmentSetupCommit)
	task.ProblemStatement = strings.TrimSpace(task.ProblemStatement)
	task.TestPatch = strings.TrimSpace(task.TestPatch)
	task.Patch = strings.TrimSpace(task.Patch)
	task.Version = strings.TrimSpace(task.Version)
	task.SetupCommand = strings.TrimSpace(task.SetupCommand)
	task.EvalCommand = strings.TrimSpace(task.EvalCommand)
	task.DockerImage = strings.TrimSpace(task.DockerImage)
	task.DockerEvalCommand = strings.TrimSpace(task.DockerEvalCommand)
	if task.InstanceID == "" {
		return SWEBenchTask{}, fmt.Errorf("missing instance_id")
	}
	if task.Repo == "" {
		return SWEBenchTask{}, fmt.Errorf("task %s missing repo", task.InstanceID)
	}
	if task.BaseCommit == "" {
		return SWEBenchTask{}, fmt.Errorf("task %s missing base_commit", task.InstanceID)
	}
	if task.ProblemStatement == "" {
		return SWEBenchTask{}, fmt.Errorf("task %s missing problem_statement", task.InstanceID)
	}
	return task, nil
}

func (t SWEBenchTask) Slug() string {
	slug := strings.NewReplacer("/", "-", ":", "-", " ", "-").Replace(strings.TrimSpace(t.InstanceID))
	if slug == "" {
		return "swebench-task"
	}
	return slug
}

func (t SWEBenchTask) WorkspaceRoot(baseDir string) string {
	return filepath.Join(baseDir, t.Slug())
}

func (t SWEBenchTask) CloneSource() string {
	if strings.TrimSpace(t.RepoURL) != "" {
		return strings.TrimSpace(t.RepoURL)
	}
	repo := strings.TrimSpace(t.Repo)
	if repo == "" {
		return ""
	}
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "/") || strings.HasPrefix(repo, ".") {
		return repo
	}
	return "https://github.com/" + repo + ".git"
}

func taskFromRawMap(raw map[string]any) (SWEBenchTask, error) {
	task := SWEBenchTask{
		InstanceID:             firstString(raw, "instance_id"),
		Repo:                   firstString(raw, "repo"),
		RepoURL:                firstString(raw, "repo_url"),
		BaseCommit:             firstString(raw, "base_commit"),
		EnvironmentSetupCommit: firstString(raw, "environment_setup_commit"),
		ProblemStatement:       firstString(raw, "problem_statement"),
		FailToPass:             firstStringSlice(raw, "FAIL_TO_PASS", "fail_to_pass"),
		PassToPass:             firstStringSlice(raw, "PASS_TO_PASS", "pass_to_pass"),
		TestPatch:              firstString(raw, "test_patch"),
		Patch:                  firstString(raw, "patch"),
		Version:                firstString(raw, "version"),
		SetupCommand:           firstString(raw, "setup_command", "before_repo_set_cmd"),
		EvalCommand:            firstString(raw, "evaluation_command"),
		DockerImage:            firstString(raw, "docker_image", "dockerhub_tag"),
		DockerEvalCommand:      firstString(raw, "docker_evaluation_command"),
	}
	return task, nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func firstStringSlice(raw map[string]any, keys ...string) []string {
	for _, key := range keys {
		v, ok := raw[key]
		if !ok {
			continue
		}
		switch vv := v.(type) {
		case []any:
			var out []string
			for _, item := range vv {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return out
		case string:
			var out []string
			if err := json.Unmarshal([]byte(vv), &out); err == nil {
				return out
			}
			if strings.TrimSpace(vv) != "" {
				return []string{strings.TrimSpace(vv)}
			}
		}
	}
	return nil
}
