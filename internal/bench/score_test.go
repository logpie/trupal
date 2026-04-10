package bench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDebugLogSeparatesObservationsFromNudges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trupal.debug")
	content := "" +
		"12:00:00.000 [brain] observation: background context\n" +
		"12:00:01.000 [brain] nudge: fix the cache invalidation in main.go\n" +
		"12:00:02.000 [brain] 1 nudges, 0 resolved, reasoning: looked around\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	summary, err := ParseDebugLog(path, time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ParseDebugLog() error = %v", err)
	}
	if len(summary.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(summary.Observations))
	}
	if len(summary.Nudges) != 1 {
		t.Fatalf("nudges = %d, want 1", len(summary.Nudges))
	}
}

func TestMatchScoreNormalizesDottedFileNames(t *testing.T) {
	score := matchScore(
		TruthBug{File: "main.go", Description: "cache invalidation bug"},
		"you need to fix stale cache invalidation in main go before shipping",
	)
	if score < 0.35 {
		t.Fatalf("expected filename boost for dotted filename, got %f", score)
	}
}

func TestEditTouchesFileMatchesProjectRelativeAndAbsolutePaths(t *testing.T) {
	edit := EditEvent{
		Files: []string{"/tmp/trupal-bench-123/project/main.go"},
	}
	if !editTouchesFile(edit, "main.go") {
		t.Fatal("expected main.go to match absolute edited path")
	}
}

func TestParseSessionEditsRecognizesCodexCustomApplyPatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "" +
		"{\"timestamp\":\"2026-04-08T00:00:01Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"custom_tool_call\",\"name\":\"apply_patch\",\"input\":\"*** Begin Patch\\n*** Update File: foo.go\\n@@\\n-old\\n+new\\n*** End Patch\"}}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	edits, err := ParseSessionEdits(path)
	if err != nil {
		t.Fatalf("ParseSessionEdits() error = %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits len = %d, want 1", len(edits))
	}
	if edits[0].Tool != "apply_patch" {
		t.Fatalf("tool = %q, want apply_patch", edits[0].Tool)
	}
	if len(edits[0].Files) != 1 || edits[0].Files[0] != "foo.go" {
		t.Fatalf("files = %#v, want [foo.go]", edits[0].Files)
	}
}

func TestScoreFindingsIncludesSteeringMetrics(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	truth := GroundTruth{
		Bugs: []TruthBug{{
			ID:          "race-users",
			File:        "main.go",
			Description: "global users slice accessed without mutex",
			Severity:    "error",
		}},
	}
	findings := []ObservedFinding{{
		Message:   "global users slice accessed without mutex in main.go",
		FirstSeen: base.Add(20 * time.Second),
	}}
	edits := []EditEvent{{
		Time:  base.Add(10 * time.Second),
		Files: []string{"main.go"},
		Tool:  "apply_patch",
	}}
	debug := DebugSummary{}
	steering := []SteeringEvent{{
		Timestamp: base.Add(8 * time.Second),
		Message:   "global users slice accessed without mutex in main.go",
		Source:    "auto",
	}}

	score := ScoreFindings(truth, findings, edits, debug, steering)
	if score.SteeringEventCount != 1 {
		t.Fatalf("SteeringEventCount = %d, want 1", score.SteeringEventCount)
	}
	if score.BugsFixedAfterNudge != 1 {
		t.Fatalf("BugsFixedAfterNudge = %d, want 1", score.BugsFixedAfterNudge)
	}
	if score.NudgesWithFollowupEdit != 1 {
		t.Fatalf("NudgesWithFollowupEdit = %d, want 1", score.NudgesWithFollowupEdit)
	}
	if score.NudgeConversionRate != 1 {
		t.Fatalf("NudgeConversionRate = %f, want 1", score.NudgeConversionRate)
	}
	if score.FirstNudgeToEdit != 2*time.Second {
		t.Fatalf("FirstNudgeToEdit = %s, want 2s", score.FirstNudgeToEdit)
	}
}
