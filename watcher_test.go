package main

import "testing"

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
