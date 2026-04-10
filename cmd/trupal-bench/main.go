package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yuxuan/trupal/internal/bench"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if err := runSingle(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-paired":
		if err := runPaired(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-all":
		if err := runAll(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "prepare-swebench":
		if err := prepareSWEBench(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "eval-swebench":
		if err := evalSWEBench(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-swebench":
		if err := runSWEBench(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "run-swebench-paired":
		if err := runSWEBenchPaired(repoRoot, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func prepareSWEBench(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("prepare-swebench", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	swebenchDir := fs.String("swebench-dir", filepath.Join(repoRoot, "bench", "swebench-sample"), "directory containing a SWE-bench manifest snapshot")
	manifest := fs.String("manifest", "", "path to a local SWE-bench task manifest JSON file")
	instance := fs.String("instance", "", "SWE-bench instance id")
	checkout := fs.Bool("checkout", false, "clone the task repository into the prepared workspace and checkout base_commit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		SWEBenchDir:  *swebenchDir,
	})
	if err != nil {
		return err
	}

	task, workspace, err := runner.PrepareSWEBenchTask(*manifest, *instance)
	if err != nil {
		return err
	}
	if *checkout {
		if err := runner.PrepareSWEBenchWorkspace(task, workspace); err != nil {
			return err
		}
	}
	fmt.Printf("instance_id=%s\n", task.InstanceID)
	fmt.Printf("repo=%s\n", task.Repo)
	fmt.Printf("clone_source=%s\n", task.CloneSource())
	fmt.Printf("base_commit=%s\n", task.BaseCommit)
	fmt.Printf("workspace=%s\n", workspace)
	return nil
}

func evalSWEBench(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("eval-swebench", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	swebenchDir := fs.String("swebench-dir", filepath.Join(repoRoot, "bench", "swebench-sample"), "directory containing a SWE-bench manifest snapshot")
	manifest := fs.String("manifest", "", "path to a local SWE-bench task manifest JSON file")
	instance := fs.String("instance", "", "SWE-bench instance id")
	evalCmd := fs.String("eval-cmd", "", "evaluation command to run after applying test_patch")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		SWEBenchDir:  *swebenchDir,
	})
	if err != nil {
		return err
	}

	task, workspace, err := runner.PrepareSWEBenchTask(*manifest, *instance)
	if err != nil {
		return err
	}
	if err := runner.PrepareSWEBenchWorkspace(task, workspace); err != nil {
		return err
	}
	out, err := runner.EvaluateSWEBenchTask(task, workspace, *evalCmd)
	if err != nil {
		fmt.Print(out)
		return err
	}
	fmt.Print(out)
	return nil
}

func runSWEBench(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("run-swebench", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	swebenchDir := fs.String("swebench-dir", filepath.Join(repoRoot, "bench", "swebench-sample"), "directory containing a SWE-bench manifest snapshot")
	manifest := fs.String("manifest", "", "path to a local SWE-bench task manifest JSON file")
	instance := fs.String("instance", "", "SWE-bench instance id")
	arm := fs.String("arm", "control", "benchmark arm to run (control or steer)")
	evalCmd := fs.String("eval-cmd", "", "evaluation command to run after applying test_patch")
	keepTemp := fs.Bool("keep-temp", false, "keep the temp project directory after the run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		SWEBenchDir:  *swebenchDir,
		KeepTemp:     *keepTemp,
	})
	if err != nil {
		return err
	}
	result, err := runner.RunSWEBenchTask(*manifest, *instance, bench.BenchmarkArm(*arm), *evalCmd)
	if err != nil {
		return err
	}
	fmt.Println(result.Artifacts.ReportPath)
	return nil
}

func runSWEBenchPaired(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("run-swebench-paired", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	swebenchDir := fs.String("swebench-dir", filepath.Join(repoRoot, "bench", "swebench-sample"), "directory containing a SWE-bench manifest snapshot")
	manifest := fs.String("manifest", "", "path to a local SWE-bench task manifest JSON file")
	instance := fs.String("instance", "", "SWE-bench instance id")
	evalCmd := fs.String("eval-cmd", "", "evaluation command to run after applying test_patch")
	keepTemp := fs.Bool("keep-temp", false, "keep the temp project directory after the run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		SWEBenchDir:  *swebenchDir,
		KeepTemp:     *keepTemp,
	})
	if err != nil {
		return err
	}
	control, err := runner.RunSWEBenchTask(*manifest, *instance, bench.ArmControl, *evalCmd)
	if err != nil {
		return err
	}
	steer, err := runner.RunSWEBenchTask(*manifest, *instance, bench.ArmSteer, *evalCmd)
	if err != nil {
		return err
	}
	comparisonPath := filepath.Join(*resultsDir, fmt.Sprintf("%s-vs-control-steer.md", control.SWEBenchTask.Slug()))
	if err := bench.WriteSWEBenchComparisonReport(comparisonPath, control, steer); err != nil {
		return err
	}
	fmt.Println(comparisonPath)
	fmt.Println(control.Artifacts.ReportPath)
	fmt.Println(steer.Artifacts.ReportPath)
	return nil
}

func runSingle(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	codexCmd := fs.String("codex-cmd", "", "optional shell command for Codex baseline audit")
	keepTemp := fs.Bool("keep-temp", false, "keep the temp project directory after the run")
	arm := fs.String("arm", "", "benchmark arm to run (control or steer)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: trupal-bench run [flags] <scenario>")
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		CodexCmd:     *codexCmd,
		KeepTemp:     *keepTemp,
	})
	if err != nil {
		return err
	}

	var result *bench.RunResult
	if *arm != "" {
		result, err = runner.RunScenarioArm(fs.Arg(0), bench.BenchmarkArm(*arm))
	} else {
		result, err = runner.RunScenario(fs.Arg(0))
	}
	if err != nil {
		return err
	}
	fmt.Println(result.Artifacts.ReportPath)
	return nil
}

func runPaired(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("run-paired", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	codexCmd := fs.String("codex-cmd", "", "optional shell command for Codex baseline audit")
	keepTemp := fs.Bool("keep-temp", false, "keep the temp project directory after the run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: trupal-bench run-paired [flags] <scenario>")
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		CodexCmd:     *codexCmd,
		KeepTemp:     *keepTemp,
	})
	if err != nil {
		return err
	}

	results, err := runner.RunScenarioPair(fs.Arg(0))
	if err != nil {
		return err
	}
	if len(results) < 2 {
		for _, result := range results {
			fmt.Println(result.Artifacts.ReportPath)
		}
		return nil
	}
	var control, steer *bench.RunResult
	for _, result := range results {
		switch result.Arm {
		case bench.ArmControl:
			control = result
		case bench.ArmSteer:
			steer = result
		}
	}
	if control == nil || steer == nil {
		return fmt.Errorf("paired run requires control and steer arms")
	}
	comparisonPath := filepath.Join(*resultsDir, fmt.Sprintf("%s-vs-%s-%s.md", control.Scenario.ID, control.Arm, steer.Arm))
	if err := bench.WriteComparisonReport(comparisonPath, control, steer); err != nil {
		return err
	}
	fmt.Println(comparisonPath)
	for _, result := range results {
		fmt.Println(result.Artifacts.ReportPath)
	}
	return nil
}

func runAll(repoRoot string, args []string) error {
	fs := flag.NewFlagSet("run-all", flag.ExitOnError)
	resultsDir := fs.String("results-dir", filepath.Join(repoRoot, "bench", "results"), "directory for benchmark artifacts")
	codexCmd := fs.String("codex-cmd", "", "optional shell command for Codex baseline audit")
	keepTemp := fs.Bool("keep-temp", false, "keep the temp project directory after the run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runner, err := bench.NewRunner(bench.RunnerOptions{
		RepoRoot:     repoRoot,
		ResultsDir:   *resultsDir,
		ScenariosDir: filepath.Join(repoRoot, "bench", "scenarios"),
		CodexCmd:     *codexCmd,
		KeepTemp:     *keepTemp,
	})
	if err != nil {
		return err
	}

	results, err := runner.RunAll()
	if err != nil {
		return err
	}
	summaryPath := filepath.Join(*resultsDir, "latest-summary.md")
	if err := bench.WriteAggregateReport(summaryPath, results); err != nil {
		return err
	}
	fmt.Println(summaryPath)
	for _, result := range results {
		fmt.Println(result.Artifacts.ReportPath)
	}
	return nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := wd
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not find repo root from %s", wd)
		}
		current = parent
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  trupal-bench run [flags] <scenario>")
	fmt.Fprintln(os.Stderr, "  trupal-bench run-paired [flags] <scenario>")
	fmt.Fprintln(os.Stderr, "  trupal-bench run-all [flags]")
	fmt.Fprintln(os.Stderr, "  trupal-bench prepare-swebench [flags]")
	fmt.Fprintln(os.Stderr, "  trupal-bench eval-swebench [flags]")
	fmt.Fprintln(os.Stderr, "  trupal-bench run-swebench [flags]")
	fmt.Fprintln(os.Stderr, "  trupal-bench run-swebench-paired [flags]")
}
