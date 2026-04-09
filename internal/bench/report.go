package bench

import (
	"fmt"
	"os"
	"strings"
)

func WriteReport(path string, result *RunResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# TruPal Benchmark Report\n\n")
	fmt.Fprintf(&b, "## Scenario\n\n")
	fmt.Fprintf(&b, "- ID: `%s`\n", result.Scenario.ID)
	fmt.Fprintf(&b, "- Name: %s\n", result.Scenario.Name)
	fmt.Fprintf(&b, "- Category: `%s`\n", result.Scenario.Category)
	fmt.Fprintf(&b, "- Started: `%s`\n", result.StartedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&b, "- Duration: `%s`\n", result.Duration)
	fmt.Fprintf(&b, "- Agent provider: `%s`\n", result.Scenario.SessionProvider())
	fmt.Fprintf(&b, "- Agent model: `%s`\n", result.Scenario.EffectiveAgentModel())
	fmt.Fprintf(&b, "- Agent exit code: `%d`\n", result.ClaudeExitCode)
	if result.TimedOut {
		fmt.Fprintf(&b, "- Timed out: `yes`\n")
	}
	if result.ClaudeError != "" {
		fmt.Fprintf(&b, "- Agent error: `%s`\n", result.ClaudeError)
	}

	fmt.Fprintf(&b, "\n## Metrics\n\n")
	fmt.Fprintf(&b, "- Detection rate: `%.1f%%` (%d/%d)\n", result.Score.DetectionRate*100, result.Score.MatchedTruths, result.Score.TotalTruths)
	fmt.Fprintf(&b, "- False positives: `%d`\n", result.Score.FalsePositiveCount)
	fmt.Fprintf(&b, "- Trap hits: `%d`\n", result.Score.TrapHits)
	fmt.Fprintf(&b, "- Brain responses: `%d`\n", result.Score.ResponseCount)
	if result.Score.Latency.Count > 0 {
		fmt.Fprintf(&b, "- Matched latency avg/max: `%s` / `%s`\n", result.Score.Latency.Average, result.Score.Latency.Max)
	}
	fmt.Fprintf(&b, "- Token usage: in=`%d` out=`%d` cache-read=`%d` cache-create=`%d`\n",
		result.Score.InputTokens,
		result.Score.OutputTokens,
		result.Score.CacheReadTokens,
		result.Score.CacheCreateTokens,
	)
	fmt.Fprintf(&b, "- Estimated cost: `$%.4f`\n", result.Score.TotalCostUSD)

	fmt.Fprintf(&b, "\n## Matches\n\n")
	if len(result.Score.MatchedFindings) == 0 {
		fmt.Fprintf(&b, "- None\n")
	} else {
		for _, match := range result.Score.MatchedFindings {
			fmt.Fprintf(&b, "- `%s` matched `%s`", match.Bug.ID, match.Finding.Message)
			if match.Latency > 0 {
				fmt.Fprintf(&b, " (latency `%s`)", match.Latency)
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	fmt.Fprintf(&b, "\n## Missed Truths\n\n")
	if len(result.Score.UnmatchedTruths) == 0 {
		fmt.Fprintf(&b, "- None\n")
	} else {
		for _, bug := range result.Score.UnmatchedTruths {
			fmt.Fprintf(&b, "- `%s` in `%s`: %s\n", bug.ID, bug.File, bug.Description)
		}
	}

	fmt.Fprintf(&b, "\n## Extra Findings\n\n")
	if len(result.Score.ExtraFindings) == 0 {
		fmt.Fprintf(&b, "- None\n")
	} else {
		for _, finding := range result.Score.ExtraFindings {
			if !finding.FirstSeen.IsZero() {
				fmt.Fprintf(&b, "- `%s` at `%s`\n", finding.Message, finding.FirstSeen.Format("15:04:05"))
				continue
			}
			fmt.Fprintf(&b, "- `%s`\n", finding.Message)
		}
	}

	if result.CodexAudit != nil {
		fmt.Fprintf(&b, "\n## Codex Baseline\n\n")
		fmt.Fprintf(&b, "- Command: `%s`\n", result.CodexAudit.Command)
		fmt.Fprintf(&b, "- Exit code: `%d`\n", result.CodexAudit.ExitCode)
		fmt.Fprintf(&b, "- Duration: `%s`\n", result.CodexAudit.Duration)
		if result.CodexAudit.Error != "" {
			fmt.Fprintf(&b, "- Error: `%s`\n", result.CodexAudit.Error)
		}
	}

	fmt.Fprintf(&b, "\n## Artifacts\n\n")
	fmt.Fprintf(&b, "- Report: `%s`\n", path)
	fmt.Fprintf(&b, "- Pane capture: `%s`\n", result.Artifacts.PaneCapturePath)
	fmt.Fprintf(&b, "- Debug log: `%s`\n", result.Artifacts.DebugLogPath)
	fmt.Fprintf(&b, "- TruPal log: `%s`\n", result.Artifacts.TrupalLogPath)
	fmt.Fprintf(&b, "- Agent stdout: `%s`\n", result.Artifacts.ClaudeStdoutPath)
	fmt.Fprintf(&b, "- Agent stderr: `%s`\n", result.Artifacts.ClaudeStderrPath)
	fmt.Fprintf(&b, "- Session JSONL: `%s`\n", result.Artifacts.SessionJSONLPath)
	fmt.Fprintf(&b, "- Final project copy: `%s`\n", result.Artifacts.ProjectCopyDir)

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func WriteAggregateReport(path string, results []*RunResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# TruPal Benchmark Summary\n\n")
	for _, result := range results {
		fmt.Fprintf(&b, "- `%s`: detection `%.1f%%`, false positives `%d`, report `%s`\n",
			result.Scenario.ID,
			result.Score.DetectionRate*100,
			result.Score.FalsePositiveCount,
			result.Artifacts.ReportPath,
		)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}
