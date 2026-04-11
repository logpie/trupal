package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSWEBenchTaskFromSingleManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, []byte(`{
  "instance_id": "sample__repo-1",
  "repo": "example/repo",
  "base_commit": "abc123",
  "problem_statement": "Fix the bug",
  "FAIL_TO_PASS": ["a"],
  "PASS_TO_PASS": ["b"],
  "test_patch": "patch"
}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	task, err := LoadSWEBenchTask(path, "sample__repo-1")
	if err != nil {
		t.Fatalf("LoadSWEBenchTask() error = %v", err)
	}
	if task.InstanceID != "sample__repo-1" || task.Repo != "example/repo" || task.BaseCommit != "abc123" {
		t.Fatalf("unexpected task: %#v", task)
	}
}

func TestLoadSWEBenchTaskFromArrayManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(`[
  {"instance_id":"a","repo":"example/a","base_commit":"111","problem_statement":"A"},
  {"instance_id":"b","repo":"example/b","base_commit":"222","problem_statement":"B"}
]`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	task, err := LoadSWEBenchTask(path, "b")
	if err != nil {
		t.Fatalf("LoadSWEBenchTask() error = %v", err)
	}
	if task.InstanceID != "b" {
		t.Fatalf("InstanceID = %q, want b", task.InstanceID)
	}
}

func TestLoadSWEBenchTaskSupportsLowercaseProFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, []byte(`{
  "instance_id": "pro-1",
  "repo": "example/pro",
  "base_commit": "abc123",
  "problem_statement": "Fix it",
  "fail_to_pass": "[\"tests::a\"]",
  "pass_to_pass": ["tests::b"],
  "before_repo_set_cmd": "echo setup",
  "dockerhub_tag": "python:3.12-slim",
  "steering_mode": "continuous",
  "steering_rounds": 3,
  "steering_cooldown": "45s"
}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	task, err := LoadSWEBenchTask(path, "pro-1")
	if err != nil {
		t.Fatalf("LoadSWEBenchTask() error = %v", err)
	}
	if len(task.FailToPass) != 1 || task.FailToPass[0] != "tests::a" {
		t.Fatalf("FailToPass = %#v", task.FailToPass)
	}
	if len(task.PassToPass) != 1 || task.PassToPass[0] != "tests::b" {
		t.Fatalf("PassToPass = %#v", task.PassToPass)
	}
	if task.SetupCommand != "echo setup" || task.DockerImage != "python:3.12-slim" {
		t.Fatalf("unexpected pro fields %#v", task)
	}
	if task.SteeringMode != "continuous" || task.SteeringRounds != 3 || task.SteeringCooldown != "45s" {
		t.Fatalf("unexpected steering fields %#v", task)
	}
}

func TestLoadSWEBenchTaskRejectsInvalidSteeringMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task.json")
	if err := os.WriteFile(path, []byte(`{
  "instance_id": "bad-mode",
  "repo": "example/pro",
  "base_commit": "abc123",
  "problem_statement": "Fix it",
  "steering_mode": "bogus"
}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadSWEBenchTask(path, "bad-mode"); err == nil {
		t.Fatal("expected invalid steering_mode to fail")
	}
}

func TestPrepareSWEBenchWorkspaceClonesAndChecksOutBaseCommit(t *testing.T) {
	repoDir := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, string(out))
		}
	}

	run(repoDir, "git", "init")
	run(repoDir, "git", "config", "user.name", "Bench Test")
	run(repoDir, "git", "config", "user.email", "bench@test")
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run(repoDir, "git", "add", ".")
	run(repoDir, "git", "commit", "-m", "first")
	baseCommitRaw, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse error = %v: %s", err, string(baseCommitRaw))
	}
	baseCommit := strings.TrimSpace(string(baseCommitRaw))
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	run(repoDir, "git", "add", ".")
	run(repoDir, "git", "commit", "-m", "second")

	runner := &Runner{}
	task := SWEBenchTask{
		InstanceID:       "sample",
		Repo:             repoDir,
		BaseCommit:       baseCommit,
		ProblemStatement: "fix it",
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := runner.PrepareSWEBenchWorkspace(task, workspace); err != nil {
		t.Fatalf("PrepareSWEBenchWorkspace() error = %v", err)
	}
	gotRaw, err := exec.Command("git", "-C", workspace, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("workspace rev-parse error = %v: %s", err, string(gotRaw))
	}
	if got := strings.TrimSpace(string(gotRaw)); got != baseCommit {
		t.Fatalf("workspace HEAD = %q, want %q", got, baseCommit)
	}
}

func TestEvaluateSWEBenchTaskAppliesTestPatchAndRunsEval(t *testing.T) {
	repoDir := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %v\n%s", args, err, string(out))
		}
	}

	run(repoDir, "git", "init")
	run(repoDir, "git", "config", "user.name", "Bench Test")
	run(repoDir, "git", "config", "user.email", "bench@test")
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module example.com/eval\n\ngo 1.24.2\n"), 0644); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc status() bool { return false }\n"), 0644); err != nil {
		t.Fatalf("WriteFile(main.go) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "main_test.go"), []byte("package main\nimport \"testing\"\nfunc TestStatus(t *testing.T) { if !status() { t.Fatal(\"want true\") } }\n"), 0644); err != nil {
		t.Fatalf("WriteFile(main_test.go) error = %v", err)
	}
	run(repoDir, "git", "add", ".")
	run(repoDir, "git", "commit", "-m", "base")
	baseCommitRaw, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse error = %v: %s", err, string(baseCommitRaw))
	}
	baseCommit := strings.TrimSpace(string(baseCommitRaw))

	task := SWEBenchTask{
		InstanceID:       "sample",
		Repo:             repoDir,
		BaseCommit:       baseCommit,
		ProblemStatement: "Fix status",
		TestPatch:        "diff --git a/main.go b/main.go\nindex 8f2de11..c3dcb44 100644\n--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,2 @@\n package main\n-func status() bool { return false }\n+func status() bool { return true }\n",
		EvalCommand:      "go test ./...",
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	runner := &Runner{}
	if err := runner.PrepareSWEBenchWorkspace(task, workspace); err != nil {
		t.Fatalf("PrepareSWEBenchWorkspace() error = %v", err)
	}
	out, err := runner.EvaluateSWEBenchTask(task, workspace, "")
	if err != nil {
		t.Fatalf("EvaluateSWEBenchTask() error = %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected go test success output, got %q", out)
	}
}

func TestSetupSWEBenchWorkspaceRunsSetupCommand(t *testing.T) {
	workspace := t.TempDir()
	task := SWEBenchTask{
		InstanceID:       "sample",
		ProblemStatement: "x",
		SetupCommand:     "printf ready > .setup-proof",
	}
	runner := &Runner{}
	if err := runner.SetupSWEBenchWorkspace(task, workspace); err != nil {
		t.Fatalf("SetupSWEBenchWorkspace() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, ".setup-proof"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(raw)) != "ready" {
		t.Fatalf("unexpected setup proof %q", string(raw))
	}
}
