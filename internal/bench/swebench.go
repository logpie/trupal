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
	BaseCommit             string   `json:"base_commit"`
	EnvironmentSetupCommit string   `json:"environment_setup_commit"`
	ProblemStatement       string   `json:"problem_statement"`
	FailToPass             []string `json:"FAIL_TO_PASS"`
	PassToPass             []string `json:"PASS_TO_PASS"`
	TestPatch              string   `json:"test_patch"`
	Patch                  string   `json:"patch"`
	Version                string   `json:"version"`
	ManifestPath           string   `json:"-"`
}

func LoadSWEBenchTask(manifestPath, instanceID string) (SWEBenchTask, error) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return SWEBenchTask{}, fmt.Errorf("read SWE-bench manifest: %w", err)
	}

	var one SWEBenchTask
	if err := json.Unmarshal(raw, &one); err == nil && strings.TrimSpace(one.InstanceID) != "" {
		one.ManifestPath = manifestPath
		if instanceID == "" || one.InstanceID == instanceID {
			return validateSWEBenchTask(one)
		}
		return SWEBenchTask{}, fmt.Errorf("instance %q not found in %s", instanceID, manifestPath)
	}

	var many []SWEBenchTask
	if err := json.Unmarshal(raw, &many); err != nil {
		return SWEBenchTask{}, fmt.Errorf("parse SWE-bench manifest %s: %w", manifestPath, err)
	}
	for _, task := range many {
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
