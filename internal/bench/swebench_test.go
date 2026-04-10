package bench

import (
	"os"
	"path/filepath"
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
