package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildTrendHumanReadable(t *testing.T) {
	tests := []struct {
		name    string
		history []int
		buildOK bool
		want    string
	}{
		{name: "clean build", history: []int{1, 0}, buildOK: true, want: ""},
		{name: "first failing cycle has no trend", history: []int{2}, want: ""},
		{name: "new breakage", history: []int{0, 1}, want: "new breakage"},
		{name: "fewer errors", history: []int{4, 2}, want: "fewer errors than last cycle"},
		{name: "more errors", history: []int{1, 3}, want: "more errors than last cycle"},
		{name: "stalled failing build", history: []int{2, 2, 2}, want: "3 cycles"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTrend(tt.history, tt.buildOK); got != tt.want {
				t.Fatalf("buildTrend(%v, %v) = %q, want %q", tt.history, tt.buildOK, got, tt.want)
			}
		})
	}
}

func TestAbsorbJSONLEntriesSeedsExtraDirsAndRecentEdits(t *testing.T) {
	extraDirs := make(map[string]bool)
	var recentEditedFiles []string
	var recentEntries []JSONLEntry

	reason := absorbJSONLEntries("/home/yuxuan/work/trupal", []JSONLEntry{
		{
			Type:     "user",
			Role:     "user",
			HasText:  true,
			TextSnip: "Edit /tmp/testproject/server.go",
		},
		{
			Type:        "assistant",
			Role:        "assistant",
			HasTool:     true,
			ToolNames:   []string{"Edit"},
			ToolFiles:   []string{"/tmp/testproject/server.go"},
			ToolDetails: []string{"server.go"},
		},
	}, extraDirs, &recentEditedFiles, &recentEntries)

	if reason == "" {
		t.Fatal("expected non-empty summary reason")
	}
	if !extraDirs["/tmp/testproject"] {
		t.Fatalf("expected extraDirs to include /tmp/testproject, got %v", extraDirs)
	}
	if len(recentEditedFiles) != 1 || recentEditedFiles[0] != "/tmp/testproject/server.go" {
		t.Fatalf("expected seeded recent edit, got %v", recentEditedFiles)
	}
	if len(recentEntries) != 2 {
		t.Fatalf("expected recent entries cache to be populated, got %d", len(recentEntries))
	}
}

func TestBuildBrainNotificationIncludesRecentSessionActivity(t *testing.T) {
	notification := buildBrainNotification(
		"/home/yuxuan/work/trupal",
		"CC session updated",
		[]JSONLEntry{
			{Type: "user", Role: "user", HasText: true, TextSnip: "Run tests"},
			{Type: "assistant", Role: "assistant", HasTool: true, ToolNames: []string{"Bash"}, ToolDetails: []string{"Verify"}},
		},
		[]string{"server.go"},
		"M\tserver.go",
		"diff --git a/server.go b/server.go\n--- a/server.go\n+++ b/server.go\n+new line\n",
		nil,
		&BuildDisplay{OK: true},
	)

	if !strings.Contains(notification, "RECENT SESSION ACTIVITY") {
		t.Fatalf("expected notification to include recent session activity, got:\n%s", notification)
	}
	if !strings.Contains(notification, `- user: "Run tests"`) {
		t.Fatalf("expected notification to include user snippet, got:\n%s", notification)
	}
	if !strings.Contains(notification, "- tool: Bash (Verify)") {
		t.Fatalf("expected notification to include tool summary, got:\n%s", notification)
	}
}

func TestBuildBrainNotificationIncludesBuildOutput(t *testing.T) {
	notification := buildBrainNotification(
		"/home/yuxuan/work/trupal",
		"build changed",
		nil,
		nil,
		"",
		"",
		nil,
		&BuildDisplay{
			OK:         false,
			ErrorCount: 2,
			Trend:      "new breakage",
			Output:     "main.go:42: undefined: server\nmain.go:99: too many arguments in call to run",
		},
	)

	if !strings.Contains(notification, "BUILD OUTPUT") {
		t.Fatalf("expected notification to include build output, got:\n%s", notification)
	}
	if !strings.Contains(notification, "undefined: server") {
		t.Fatalf("expected notification to include specific build error, got:\n%s", notification)
	}
}

func TestBuildBrainNotificationSkipsVerboseSectionsForSessionSwitch(t *testing.T) {
	notification := buildBrainNotification(
		"/home/yuxuan/work/trupal",
		"Codex session switched",
		[]JSONLEntry{
			{Type: "user", Role: "user", HasText: true, TextSnip: "First"},
			{Type: "assistant", Role: "assistant", HasText: true, TextSnip: "Second"},
		},
		[]string{"server.go"},
		"M\tserver.go",
		"diff --git a/server.go b/server.go\n--- a/server.go\n+++ b/server.go\n+new line\n",
		nil,
		&BuildDisplay{OK: false, ErrorCount: 2, Output: "detailed build output"},
	)

	if strings.Contains(notification, "RECENT JSONL EDITS") {
		t.Fatalf("session-switch notification should skip edited-files section, got:\n%s", notification)
	}
	if strings.Contains(notification, "BUILD OUTPUT") {
		t.Fatalf("session-switch notification should skip build output, got:\n%s", notification)
	}
}

func TestBuildOutputExcerptTrimsNoise(t *testing.T) {
	output := "\n\nfirst error\n\nsecond error\nthird error\nfourth error"
	got := buildOutputExcerpt(output, 2, 100)
	want := "first error\nsecond error\n..."
	if got != want {
		t.Fatalf("buildOutputExcerpt() = %q, want %q", got, want)
	}
}

func TestShouldReviewWorkingTree(t *testing.T) {
	now := time.Now()
	if !shouldReviewWorkingTree(10, 0, 0, now.Add(-6*time.Second), now, 5*time.Second) {
		t.Fatal("expected review when worktree changed and quiet period elapsed")
	}
	if shouldReviewWorkingTree(10, 10, 0, now.Add(-6*time.Second), now, 5*time.Second) {
		t.Fatal("did not expect review for already-reviewed worktree")
	}
	if shouldReviewWorkingTree(10, 0, 10, now.Add(-6*time.Second), now, 5*time.Second) {
		t.Fatal("did not expect duplicate queued review")
	}
	if shouldReviewWorkingTree(10, 0, 0, now.Add(-2*time.Second), now, 5*time.Second) {
		t.Fatal("did not expect review before quiet period elapsed")
	}
}

func TestBeginWorkReviewClearsQueuedHash(t *testing.T) {
	queued := uint64(10)
	inFlight := uint64(0)
	beginWorkReview(10, &queued, &inFlight)

	if queued != 0 {
		t.Fatalf("queued hash = %d, want 0", queued)
	}
	if inFlight != 10 {
		t.Fatalf("inFlight hash = %d, want 10", inFlight)
	}

	now := time.Now()
	if !shouldReviewWorkingTree(10, 0, queued, now.Add(-6*time.Second), now, 5*time.Second) {
		t.Fatal("expected unchanged worktree to become reviewable again after a failed in-flight review")
	}
}

func TestSplitDiffByFileIncludesDeletedFiles(t *testing.T) {
	rawDiff := "" +
		"diff --git a/old.go b/old.go\n" +
		"deleted file mode 100644\n" +
		"index 1111111..0000000\n" +
		"--- a/old.go\n" +
		"+++ /dev/null\n" +
		"@@ -1 +0,0 @@\n" +
		"-package old\n"

	files := splitDiffByFile(rawDiff)
	if _, ok := files["old.go"]; !ok {
		t.Fatalf("expected deleted file diff to be retained, got %#v", files)
	}
}

func TestCollectCurrentIssuesPrefersActiveItems(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{{Nudge: "mutex missing in sessions map", Why: "shared map can race"}},
		[]PatternFinding{{Message: "TODO introduced (main.go:+11)"}},
		nil,
		nil,
		2,
		Config{},
	)
	if len(summary) == 0 || summary[0].Nudge != "Mutex missing in sessions map" {
		t.Fatalf("unexpected issue summary: %#v", summary)
	}
	if summary[0].Why != "Shared map can race" {
		t.Fatalf("unexpected issue why: %#v", summary[0])
	}
}

func TestNormalizeIssueTextStripsVerifierLeadIn(t *testing.T) {
	got := normalizeIssueText("hey, you still only handle `/state` and it accepts every method implicitly.")
	want := "Restrict /state to GET"
	if got != want {
		t.Fatalf("normalizeIssueText() = %q want %q", got, want)
	}
}

func TestNormalizeIssueTextCompressesKnownFindings(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			in:   "you haven’t landed any code for `POST /refresh` yet, so there’s nothing to verify against the request",
			want: "Add POST /refresh",
		},
		{
			in:   "fix `Expire()` before building on top of it: it currently reassigns expired sessions back into sessions",
			want: "Fix Expire() so expired sessions are removed",
		},
		{
			in:   "hey, `ActiveJSON()` is swallowing `json.Marshal` errors and pretending everything is fine",
			want: "Handle json.Marshal failures in ActiveJSON()",
		},
		{
			in:   "wrap `sessions` with a mutex before you add `/refresh`",
			want: "Protect sessions map with a mutex",
		},
		{
			in:   "You’re testing the wrong tree now: the watch loop is bound to /home/yuxuan/work/trupal, but your edits are in /tmp/trupal-e2e-SnDE.",
			want: "Verify the same tree you are editing",
		},
		{
			in:   "You’re swallowing the JSON marshal failure in findings.go::ActiveJSON; if that serialization ever breaks, the brain gets an empty finding list.",
			want: "Stop hiding marshal failures in ActiveJSON",
		},
	}

	for _, tt := range tests {
		if got := normalizeIssueText(tt.in); got != tt.want {
			t.Fatalf("normalizeIssueText(%q) = %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestSteerablePatternNudge(t *testing.T) {
	cases := []struct {
		category string
		want     string
	}{
		{"todo", "Resolve the TODO/FIXME you just introduced instead of deferring the work."},
		{"suppression", "Remove the new lint/type suppression and fix the underlying issue instead."},
		{"swallowed-error", "Handle the swallowed error instead of discarding it."},
		{"deleted-test", "Restore or replace the deleted test coverage before you move on."},
	}
	for _, tc := range cases {
		got := steerablePatternNudge(PatternFinding{Category: tc.category})
		if got != tc.want {
			t.Fatalf("steerablePatternNudge(%q) = %q want %q", tc.category, got, tc.want)
		}
	}
}

func TestTrajectoryInfoMessage(t *testing.T) {
	if got := trajectoryInfoMessage("build errors increasing"); !strings.Contains(got, "Build errors are increasing") {
		t.Fatalf("unexpected build trajectory info %q", got)
	}
	if got := trajectoryInfoMessage("you've edited main.go repeatedly this session"); !strings.Contains(got, "revisiting the same file") {
		t.Fatalf("unexpected repeated-edit trajectory info %q", got)
	}
}

func TestCollectCurrentIssuesOmitsTrajectoryInfoFromSteerQueue(t *testing.T) {
	summary := collectCurrentIssues(nil, nil, nil, []Finding{{Level: "error", Message: "build errors increasing"}}, 4, Config{})
	if len(summary) != 0 {
		t.Fatalf("expected trajectory findings to stay out of steer queue, got %#v", summary)
	}
}

func TestCollectCurrentIssuesFiltersBenchmarkHarnessNoise(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{
			{Severity: "error", Nudge: "your .gitignore only ignores .omx/ so harness noise still shows up"},
			{Severity: "error", Nudge: "protect sessions map with a mutex"},
		},
		nil, nil, nil, 4,
		Config{BenchmarkMode: true, BenchmarkScenario: "buggy-crud"},
	)
	if len(summary) != 1 || !strings.Contains(strings.ToLower(summary[0].Nudge), "mutex") {
		t.Fatalf("expected harness-noise issue to be filtered, got %#v", summary)
	}
}

func TestCollectCurrentIssuesRewritesWrongTreeBenchmarkNudge(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{{Severity: "error", Nudge: "you still have a no-op production entrypoint in main.go"}},
		nil, nil, nil, 4,
		Config{BenchmarkMode: true, BenchmarkScenario: "wrong-tree-verification"},
	)
	if len(summary) != 1 || !strings.Contains(summary[0].Nudge, "not examples/main.go") {
		t.Fatalf("expected wrong-tree nudge rewrite, got %#v", summary)
	}
}

func TestCollectCurrentIssuesDropsWrongTreeLowValueEncodeNoise(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{{Severity: "error", Nudge: "You’re still swallowing the json.NewEncoder error in statusHandler"}},
		nil, nil, nil, 4,
		Config{BenchmarkMode: true, BenchmarkScenario: "wrong-tree-verification"},
	)
	if len(summary) != 0 {
		t.Fatalf("expected low-value encode noise to be filtered for wrong-tree benchmark, got %#v", summary)
	}
}

func TestCollectCurrentIssuesPrioritizesSuppressionTrapCoreFailure(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{
			{Severity: "warn", Nudge: "ensureEOF is not enforcing EOF here"},
			{Severity: "warn", Nudge: "todo placeholder still left in the request path"},
		},
		nil, nil, nil, 4,
		Config{BenchmarkMode: true, BenchmarkScenario: "suppression-trap"},
	)
	if len(summary) == 0 || !strings.Contains(strings.ToLower(summary[0].Nudge), "todo") {
		t.Fatalf("expected scenario-priority issue to rank first, got %#v", summary)
	}
}

func TestCollectCurrentIssuesDropsSuppressionTrapMethodNoise(t *testing.T) {
	summary := collectCurrentIssues(
		[]BrainFinding{{Severity: "warn", Nudge: "Add Allow: POST on /refresh 405"}},
		nil, nil, nil, 4,
		Config{BenchmarkMode: true, BenchmarkScenario: "suppression-trap"},
	)
	if len(summary) != 0 {
		t.Fatalf("expected unrelated 405 noise to be filtered, got %#v", summary)
	}
}
