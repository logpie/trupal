package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"| Steering events | 0 | 2 |",
		"| Nudge conversion | 0.0% | 50.0% |",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("comparison report missing %q in:\n%s", want, text)
		}
	}
}
