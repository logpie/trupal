package bench

import (
	"strings"
	"testing"
)

func TestBenchmarkClaudePromptForcesNonInteractiveExecution(t *testing.T) {
	prompt := benchmarkClaudePrompt("Implement the API")

	for _, want := range []string{
		"Do not ask clarifying questions.",
		"Do not stop at planning, brainstorming, or design.",
		"implement the task end-to-end",
		"Task:",
		"Implement the API",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("benchmarkClaudePrompt() missing %q in:\n%s", want, prompt)
		}
	}
}
