package main

import (
	"strings"
	"testing"
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
