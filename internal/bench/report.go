package bench

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func WriteReport(path string, result *RunResult) error {
	var b strings.Builder
	if result.SWEBenchTask != nil {
		return writeSWEBenchReport(path, result)
	}
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
	if result.StopReason != "" {
		fmt.Fprintf(&b, "- Stop reason: `%s`\n", result.StopReason)
	}
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
	fmt.Fprintf(&b, "- Generated nudges: `%d`\n", result.GeneratedNudges)
	fmt.Fprintf(&b, "- Sent nudges: `%d`\n", result.SentNudges)
	fmt.Fprintf(&b, "- Unsent nudges: `%d`\n", result.UnsentNudges)
	if result.GeneratedNudges > 0 {
		fmt.Fprintf(&b, "- Sent/generated ratio: `%.1f%%`\n", float64(result.SentNudges)*100/float64(result.GeneratedNudges))
	}
	if result.FirstGeneratedNudge > 0 {
		fmt.Fprintf(&b, "- First generated nudge: `%s`\n", result.FirstGeneratedNudge)
	}
	if result.FirstSentNudge > 0 {
		fmt.Fprintf(&b, "- First sent nudge: `%s`\n", result.FirstSentNudge)
	}
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
	fmt.Fprintf(&b, "- Benchmark status: `%s`\n", result.Artifacts.BenchmarkStatusPath)
	fmt.Fprintf(&b, "- Final project copy: `%s`\n", result.Artifacts.ProjectCopyDir)

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func writeSWEBenchReport(path string, result *RunResult) error {
	var b strings.Builder
	task := result.SWEBenchTask
	fmt.Fprintf(&b, "# TruPal SWE-bench Report\n\n")
	fmt.Fprintf(&b, "## Task\n\n")
	fmt.Fprintf(&b, "- Instance: `%s`\n", task.InstanceID)
	fmt.Fprintf(&b, "- Repo: `%s`\n", task.Repo)
	fmt.Fprintf(&b, "- Arm: `%s`\n", result.Arm)
	fmt.Fprintf(&b, "- Started: `%s`\n", result.StartedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&b, "- Duration: `%s`\n", result.Duration)
	fmt.Fprintf(&b, "- Agent exit code: `%d`\n", result.AgentExitCode)
	if result.StopReason != "" {
		fmt.Fprintf(&b, "- Stop reason: `%s`\n", result.StopReason)
	}
	if result.TimedOut {
		fmt.Fprintf(&b, "- Timed out: `yes`\n")
	}
	fmt.Fprintf(&b, "- Solved: `%v`\n", result.SWEBenchSolved)
	fmt.Fprintf(&b, "- Steering events: `%d`\n", len(result.SteeringEvents))
	fmt.Fprintf(&b, "- Generated nudges: `%d`\n", result.GeneratedNudges)
	fmt.Fprintf(&b, "- Sent nudges: `%d`\n", result.SentNudges)
	fmt.Fprintf(&b, "- Unsent nudges: `%d`\n", result.UnsentNudges)
	if result.GeneratedNudges > 0 {
		fmt.Fprintf(&b, "- Sent/generated ratio: `%.1f%%`\n", float64(result.SentNudges)*100/float64(result.GeneratedNudges))
	}
	if result.FirstGeneratedNudge > 0 {
		fmt.Fprintf(&b, "- First generated nudge: `%s`\n", result.FirstGeneratedNudge)
	}
	if result.FirstSentNudge > 0 {
		fmt.Fprintf(&b, "- First sent nudge: `%s`\n", result.FirstSentNudge)
	}
	if result.SWEBenchEvalCommand != "" {
		fmt.Fprintf(&b, "- Eval command: `%s`\n", result.SWEBenchEvalCommand)
	}

	fmt.Fprintf(&b, "\n## Artifacts\n\n")
	fmt.Fprintf(&b, "- Report: `%s`\n", path)
	fmt.Fprintf(&b, "- Eval log: `%s`\n", result.Artifacts.EvalOutputPath)
	fmt.Fprintf(&b, "- Pane capture: `%s`\n", result.Artifacts.PaneCapturePath)
	fmt.Fprintf(&b, "- Debug log: `%s`\n", result.Artifacts.DebugLogPath)
	fmt.Fprintf(&b, "- TruPal log: `%s`\n", result.Artifacts.TrupalLogPath)
	fmt.Fprintf(&b, "- Steer log: `%s`\n", result.Artifacts.SteerLogPath)
	fmt.Fprintf(&b, "- Session JSONL: `%s`\n", result.Artifacts.SessionJSONLPath)
	fmt.Fprintf(&b, "- Benchmark status: `%s`\n", result.Artifacts.BenchmarkStatusPath)
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
	if control.StopReason != "" || steer.StopReason != "" {
		fmt.Fprintf(&b, "| Stop reason | %s | %s |\n", control.StopReason, steer.StopReason)
	}
	fmt.Fprintf(&b, "| Generated nudges | %d | %d |\n", control.GeneratedNudges, steer.GeneratedNudges)
	fmt.Fprintf(&b, "| Sent nudges | %d | %d |\n", control.SentNudges, steer.SentNudges)
	fmt.Fprintf(&b, "| Unsent nudges | %d | %d |\n", control.UnsentNudges, steer.UnsentNudges)
	fmt.Fprintf(&b, "| Steering events | %d | %d |\n", control.Score.SteeringEventCount, steer.Score.SteeringEventCount)
	fmt.Fprintf(&b, "| Bugs fixed after nudge | %d | %d |\n", control.Score.BugsFixedAfterNudge, steer.Score.BugsFixedAfterNudge)
	fmt.Fprintf(&b, "| Nudge conversion | %.1f%% | %.1f%% |\n", control.Score.NudgeConversionRate*100, steer.Score.NudgeConversionRate*100)

	fmt.Fprintf(&b, "\n## Reports\n\n")
	fmt.Fprintf(&b, "- control: `%s`\n", control.Artifacts.ReportPath)
	fmt.Fprintf(&b, "- steer: `%s`\n", steer.Artifacts.ReportPath)
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func WriteSWEBenchComparisonReport(path string, control, steer *RunResult) error {
	var b strings.Builder
	task := control.SWEBenchTask
	fmt.Fprintf(&b, "# TruPal SWE-bench Comparison\n\n")
	fmt.Fprintf(&b, "## Task\n\n")
	fmt.Fprintf(&b, "- Instance: `%s`\n", task.InstanceID)
	fmt.Fprintf(&b, "- Repo: `%s`\n", task.Repo)
	fmt.Fprintf(&b, "| Metric | control | steer |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Solved | %t | %t |\n", control.SWEBenchSolved, steer.SWEBenchSolved)
	fmt.Fprintf(&b, "| Agent exit code | %d | %d |\n", control.AgentExitCode, steer.AgentExitCode)
	if control.StopReason != "" || steer.StopReason != "" {
		fmt.Fprintf(&b, "| Stop reason | %s | %s |\n", control.StopReason, steer.StopReason)
	}
	fmt.Fprintf(&b, "| Duration | %s | %s |\n", control.Duration, steer.Duration)
	fmt.Fprintf(&b, "| Generated nudges | %d | %d |\n", control.GeneratedNudges, steer.GeneratedNudges)
	fmt.Fprintf(&b, "| Sent nudges | %d | %d |\n", control.SentNudges, steer.SentNudges)
	fmt.Fprintf(&b, "| Unsent nudges | %d | %d |\n", control.UnsentNudges, steer.UnsentNudges)
	fmt.Fprintf(&b, "| Steering events | %d | %d |\n", len(control.SteeringEvents), len(steer.SteeringEvents))
	fmt.Fprintf(&b, "\n## Reports\n\n")
	fmt.Fprintf(&b, "- control: `%s`\n", control.Artifacts.ReportPath)
	fmt.Fprintf(&b, "- steer: `%s`\n", steer.Artifacts.ReportPath)
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func WriteSWEBenchRepeatSummaryReport(path string, controls, steers []*RunResult) error {
	var b strings.Builder
	if len(controls) == 0 || len(steers) == 0 {
		return fmt.Errorf("repeat summary requires control and steer runs")
	}
	task := controls[0].SWEBenchTask
	controlSolved := countSolved(controls)
	steerSolved := countSolved(steers)
	fmt.Fprintf(&b, "# TruPal SWE-bench Repeat Summary\n\n")
	fmt.Fprintf(&b, "## Task\n\n")
	fmt.Fprintf(&b, "- Instance: `%s`\n", task.InstanceID)
	fmt.Fprintf(&b, "- Repo: `%s`\n", task.Repo)
	fmt.Fprintf(&b, "- Repeats: `%d`\n", minInt(len(controls), len(steers)))
	fmt.Fprintf(&b, "\n## Aggregate\n\n")
	fmt.Fprintf(&b, "| Metric | control | steer |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: |\n")
	fmt.Fprintf(&b, "| Solved runs | %d/%d | %d/%d |\n", controlSolved, len(controls), steerSolved, len(steers))
	fmt.Fprintf(&b, "| Pass rate | %.1f%% | %.1f%% |\n", ratio(controlSolved, len(controls))*100, ratio(steerSolved, len(steers))*100)
	fmt.Fprintf(&b, "| Avg generated nudges | %.2f | %.2f |\n", avgGenerated(controls), avgGenerated(steers))
	fmt.Fprintf(&b, "| Avg sent nudges | %.2f | %.2f |\n", avgSent(controls), avgSent(steers))
	fmt.Fprintf(&b, "| Avg duration | %s | %s |\n", avgDuration(controls), avgDuration(steers))
	fmt.Fprintf(&b, "\n## Repeats\n\n")
	fmt.Fprintf(&b, "| Repeat | control solved | steer solved | control sent | steer sent | control generated | steer generated |\n")
	fmt.Fprintf(&b, "| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for i := 0; i < minInt(len(controls), len(steers)); i++ {
		fmt.Fprintf(&b, "| %d | %t | %t | %d | %d | %d | %d |\n", i+1, controls[i].SWEBenchSolved, steers[i].SWEBenchSolved, controls[i].SentNudges, steers[i].SentNudges, controls[i].GeneratedNudges, steers[i].GeneratedNudges)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func countSolved(results []*RunResult) int {
	count := 0
	for _, result := range results {
		if result.SWEBenchSolved {
			count++
		}
	}
	return count
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func avgGenerated(results []*RunResult) float64 {
	if len(results) == 0 {
		return 0
	}
	total := 0
	for _, result := range results {
		total += result.GeneratedNudges
	}
	return float64(total) / float64(len(results))
}

func avgSent(results []*RunResult) float64 {
	if len(results) == 0 {
		return 0
	}
	total := 0
	for _, result := range results {
		total += result.SentNudges
	}
	return float64(total) / float64(len(results))
}

func avgDuration(results []*RunResult) time.Duration {
	if len(results) == 0 {
		return 0
	}
	var total time.Duration
	for _, result := range results {
		total += result.Duration
	}
	return total / time.Duration(len(results))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
