package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewArtifactSetUsesGoToolSafeProjectCopyDir(t *testing.T) {
	artifacts := NewArtifactSet("/tmp/results")
	if filepath.Base(artifacts.ProjectCopyDir) != "_project" {
		t.Fatalf("ProjectCopyDir = %q, want basename _project", artifacts.ProjectCopyDir)
	}
}

func TestWriteComparisonReportIncludesPrimaryMetrics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comparison.md")
	control := &RunResult{
		Scenario: Scenario{ID: "buggy-crud", Name: "CRUD API"},
		Arm:      ArmControl,
		Artifacts: ArtifactSet{
			ReportPath: filepath.Join(dir, "control.md"),
		},
		Score: Scorecard{
			MatchedTruths: 2,
			UnmatchedTruths: []TruthBug{
				{ID: "a"}, {ID: "b"},
			},
			FalsePositiveCount: 1,
			TrapHits:           0,
			TotalCostUSD:       0.11,
		},
		GeneratedNudges: 2,
		SentNudges:      0,
		UnsentNudges:    2,
	}
	steer := &RunResult{
		Scenario: control.Scenario,
		Arm:      ArmSteer,
		Artifacts: ArtifactSet{
			ReportPath: filepath.Join(dir, "steer.md"),
		},
		Score: Scorecard{
			MatchedTruths:       3,
			UnmatchedTruths:     []TruthBug{{ID: "a"}},
			FalsePositiveCount:  1,
			TrapHits:            0,
			TotalCostUSD:        0.15,
			SteeringEventCount:  2,
			BugsFixedAfterNudge: 1,
			NudgeConversionRate: 0.5,
		},
		GeneratedNudges: 5,
		SentNudges:      2,
		UnsentNudges:    3,
	}
	if err := WriteComparisonReport(path, control, steer); err != nil {
		t.Fatalf("WriteComparisonReport() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"Primary metric: steering uplift `+1`",
		"| Matched truths | 2 | 3 |",
		"| Generated nudges | 2 | 5 |",
		"| Sent nudges | 0 | 2 |",
		"| Steering events | 0 | 2 |",
		"| Nudge conversion | 0.0% | 50.0% |",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("comparison report missing %q in:\n%s", want, text)
		}
	}
}

func TestWriteSWEBenchRepeatSummaryReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repeat.md")
	task := &SWEBenchTask{InstanceID: "x", Repo: "example/repo"}
	controls := []*RunResult{
		{SWEBenchTask: task, SWEBenchSolved: false, Duration: 2 * time.Minute, GeneratedNudges: 0, SentNudges: 0},
		{SWEBenchTask: task, SWEBenchSolved: true, Duration: time.Minute, GeneratedNudges: 1, SentNudges: 0},
	}
	steers := []*RunResult{
		{SWEBenchTask: task, SWEBenchSolved: true, Duration: 90 * time.Second, GeneratedNudges: 3, SentNudges: 2},
		{SWEBenchTask: task, SWEBenchSolved: true, Duration: 80 * time.Second, GeneratedNudges: 2, SentNudges: 1},
	}
	if err := WriteSWEBenchRepeatSummaryReport(path, controls, steers); err != nil {
		t.Fatalf("WriteSWEBenchRepeatSummaryReport() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		"| Solved runs | 1/2 | 2/2 |",
		"| Pass rate | 50.0% | 100.0% |",
		"| Avg generated nudges | 0.50 | 2.50 |",
		"| Avg sent nudges | 0.00 | 1.50 |",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("repeat summary missing %q in:\n%s", want, text)
		}
	}
}
