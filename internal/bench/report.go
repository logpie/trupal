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
	fmt.Fprintf(&b, "- Arm: `%s`\n", result.Arm)
	fmt.Fprintf(&b, "- Name: %s\n", result.Scenario.Name)
	fmt.Fprintf(&b, "- Category: `%s`\n", result.Scenario.Category)
	fmt.Fprintf(&b, "- Started: `%s`\n", result.StartedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&b, "- Duration: `%s`\n", result.Duration)
	fmt.Fprintf(&b, "- Agent provider: `%s`\n", result.Scenario.SessionProvider())
	fmt.Fprintf(&b, "- Agent model: `%s`\n", result.Scenario.EffectiveAgentModel())
	fmt.Fprintf(&b, "- Agent exit code: `%d`\n", result.AgentExitCode)
	if result.TimedOut {
		fmt.Fprintf(&b, "- Timed out: `yes`\n")
	}
	if result.AgentError != "" {
		fmt.Fprintf(&b, "- Agent error: `%s`\n", result.AgentError)
	}

	fmt.Fprintf(&b, "\n## Metrics\n\n")
	fmt.Fprintf(&b, "- Detection rate: `%.1f%%` (%d/%d)\n", result.Score.DetectionRate*100, result.Score.MatchedTruths, result.Score.TotalTruths)
	fmt.Fprintf(&b, "- False positives: `%d`\n", result.Score.FalsePositiveCount)
	fmt.Fprintf(&b, "- Trap hits: `%d`\n", result.Score.TrapHits)
	fmt.Fprintf(&b, "- Brain responses: `%d`\n", result.Score.ResponseCount)
	fmt.Fprintf(&b, "- Steering events: `%d`\n", result.Score.SteeringEventCount)
	if result.Score.SteeringEventCount > 0 {
		fmt.Fprintf(&b, "- Bugs fixed after nudge: `%d`\n", result.Score.BugsFixedAfterNudge)
		fmt.Fprintf(&b, "- Nudge conversion: `%.1f%%` (%d/%d)\n", result.Score.NudgeConversionRate*100, result.Score.NudgesWithFollowupEdit, result.Score.SteeringEventCount)
		if result.Score.FirstNudgeToEdit > 0 {
			fmt.Fprintf(&b, "- First nudge to relevant edit: `%s`\n", result.Score.FirstNudgeToEdit)
		}
	}
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
	fmt.Fprintf(&b, "- Steer log: `%s`\n", result.Artifacts.SteerLogPath)
	fmt.Fprintf(&b, "- Agent stdout: `%s`\n", result.Artifacts.AgentStdoutPath)
	fmt.Fprintf(&b, "- Agent stderr: `%s`\n", result.Artifacts.AgentStderrPath)
	fmt.Fprintf(&b, "- Session JSONL: `%s`\n", result.Artifacts.SessionJSONLPath)
	fmt.Fprintf(&b, "- Final project copy: `%s`\n", result.Artifacts.ProjectCopyDir)

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func WriteComparisonReport(path string, control, steer *RunResult) error {
	var b strings.Builder
	uplift := steer.Score.MatchedTruths - control.Score.MatchedTruths
	extraCost := steer.Score.TotalCostUSD - control.Score.TotalCostUSD
	costPerExtraBugFixed := 0.0
	if uplift > 0 {
		costPerExtraBugFixed = extraCost / float64(uplift)
	}

	fmt.Fprintf(&b, "# TruPal Steering Comparison\n\n")
	fmt.Fprintf(&b, "## Scenario\n\n")
	fmt.Fprintf(&b, "- ID: `%s`\n", control.Scenario.ID)
	fmt.Fprintf(&b, "- Name: %s\n", control.Scenario.Name)
	fmt.Fprintf(&b, "- Primary metric: steering uplift `%+d`\n", uplift)
	fmt.Fprintf(&b, "- Extra cost vs control: `$%.4f`\n", extraCost)
	if uplift > 0 {
		fmt.Fprintf(&b, "- Cost per extra bug fixed: `$%.4f`\n", costPerExtraBugFixed)
	}

	fmt.Fprintf(&b, "\n## Arms\n\n")
	fmt.Fprintf(&b, "| Metric | control | steer |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Matched truths | %d | %d |\n", control.Score.MatchedTruths, steer.Score.MatchedTruths)
	fmt.Fprintf(&b, "| Residual truths | %d | %d |\n", len(control.Score.UnmatchedTruths), len(steer.Score.UnmatchedTruths))
	fmt.Fprintf(&b, "| False positives | %d | %d |\n", control.Score.FalsePositiveCount, steer.Score.FalsePositiveCount)
	fmt.Fprintf(&b, "| Trap hits | %d | %d |\n", control.Score.TrapHits, steer.Score.TrapHits)
	fmt.Fprintf(&b, "| Cost (USD) | %.4f | %.4f |\n", control.Score.TotalCostUSD, steer.Score.TotalCostUSD)
	fmt.Fprintf(&b, "| Steering events | %d | %d |\n", control.Score.SteeringEventCount, steer.Score.SteeringEventCount)
	fmt.Fprintf(&b, "| Bugs fixed after nudge | %d | %d |\n", control.Score.BugsFixedAfterNudge, steer.Score.BugsFixedAfterNudge)
	fmt.Fprintf(&b, "| Nudge conversion | %.1f%% | %.1f%% |\n", control.Score.NudgeConversionRate*100, steer.Score.NudgeConversionRate*100)

	fmt.Fprintf(&b, "\n## Reports\n\n")
	fmt.Fprintf(&b, "- control: `%s`\n", control.Artifacts.ReportPath)
	fmt.Fprintf(&b, "- steer: `%s`\n", steer.Artifacts.ReportPath)
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
