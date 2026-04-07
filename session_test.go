package main

import "testing"

func TestEvalTrajectoryBuildErrorsIncreasing(t *testing.T) {
	session := NewSession("/tmp/trupal")
	session.ErrorHistory = []int{0, 1, 1}

	findings := session.EvalTrajectory()
	if len(findings) != 1 {
		t.Fatalf("EvalTrajectory() returned %d findings, want 1", len(findings))
	}
	if got, want := findings[0].Message, "build errors increasing"; got != want {
		t.Fatalf("EvalTrajectory() message = %q, want %q", got, want)
	}
}
