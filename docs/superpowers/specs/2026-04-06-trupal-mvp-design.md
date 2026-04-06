# TruPal MVP Design Spec

## Overview

TruPal is a display-only tmux sidecar that watches a coding agent's work in real-time. It tracks trajectory signals (whack-a-mole, error trends, fix-then-break) and flags suspicious diff patterns (error swallowing, deleted tests, lint suppression). No communication with the agent — human reads both panes.

## Commands

```bash
trupal start [project-dir]   # split tmux pane to the right (30%), start watching
trupal stop                  # kill watcher, close pane
```

- Must be run inside a tmux session. Errors otherwise.
- `project-dir` defaults to cwd. Walks up to find `.git/`.
- `trupal start` writes its pane ID to `<project-dir>/.trupal.pid`.
- `trupal stop` reads `.trupal.pid`, kills that tmux pane (`tmux kill-pane -t <id>`), removes the pid file. If no pid file, prints "trupal is not running."
- `.trupal.pid` should be added to `.gitignore`.

## Watch Loop

Polls every 3 seconds (configurable). Each cycle:

1. Run `git diff --name-only HEAD` and `git diff --name-status HEAD` to get changed files.
2. Run `git diff HEAD` to get full diff content.
3. Compare diff content per file against previous cycle to determine if file was re-edited (not just still dirty).
4. Update session state: increment per-file edit count for files with new changes.
5. If build command is configured and relevant files changed, run it. Append error count to history.
6. Grep added lines in diff for high-precision patterns.
7. Evaluate trajectory signals against session state.
8. Clear screen and redraw display.

## Detections

### Trajectory signals (stateful, across cycles)

| Signal | Detection | Trigger |
|--------|-----------|---------|
| **Whack-a-mole** | Per-file edit count | Same file's diff content changed in 3+ cycles |
| **Error stall** | Build error count flat/increasing | Error count not decreasing over 3+ consecutive cycles |
| **Fix-then-break** | Build error count drops then rises | Decrease followed by increase in error history |

### Diff pattern signals (stateless, per cycle)

| Signal | Detection | Scope |
|--------|-----------|-------|
| **Error swallowing** | Regex on added lines: empty catch/except blocks, `_ = err`, `_ := err` | All languages |
| **Deleted tests** | `git diff --name-status` shows D on files matching `test`, `spec`, `_test.go` | All languages |
| **Lint/type suppression** | Regex on added lines: `@ts-ignore`, `eslint-disable`, `# type: ignore`, `nolint`, `NOSONAR`, `@SuppressWarnings` | All languages |

### Pattern details

Error swallowing patterns (on added lines only):
```
catch\s*\(\w*\)\s*\{\s*\}          # JS/TS/Java empty catch
except:\s*pass                      # Python bare except pass
except\s+\w+:\s*pass               # Python typed except pass
_\s*,?\s*=\s*\w+                   # Go ignored error (_ = err, _, _ = ...)
```

Lint/type suppression patterns (on added lines only):
```
@ts-ignore
@ts-expect-error
eslint-disable
# type:\s*ignore
nolint
NOSONAR
@SuppressWarnings
# nosec
```

Deleted test patterns (on file status, not diff content):
```
D\s+.*test.*                        # any file with "test" in name
D\s+.*spec.*                        # any file with "spec" in name
D\s+.*_test\.go                     # Go test files
```

## Session State

In-memory only. Resets on `trupal start`.

```go
type Session struct {
    StartTime     time.Time
    ProjectDir    string
    FileEditCount map[string]int    // filename -> cycles where diff changed
    ErrorHistory  []int             // build error count per cycle (append-only)
    LastDiffHash  map[string]string // filename -> hash of last seen diff content
}
```

### Per-file edit counting

Each cycle, hash the diff content for each changed file. Compare against `LastDiffHash`. If different (or new), increment `FileEditCount` for that file and update the hash. This distinguishes "file was edited again" from "file is still dirty from one edit."

### Error trend evaluation

Over the last 10 entries in `ErrorHistory`:
- **Stall**: last 3+ entries are equal and > 0, or trending upward.
- **Fix-then-break**: a decrease followed by an increase (min, then rise).
- **Progress**: strictly decreasing. No warning.

## Configuration

Optional file at `<project-dir>/.trupal.toml`. Hand-parsed (3 fields, no TOML library).

```toml
# Command to run for build checking. If empty or missing, no build check.
build_cmd = "tsc --noEmit"

# File extensions that trigger the build check. If empty, any file change triggers it.
build_extensions = [".ts", ".tsx"]

# Poll interval in seconds. Default: 3.
poll_interval = 3
```

If no `.trupal.toml` exists, TruPal runs without build checking — trajectory tracking and diff pattern matching still work.

On startup, print active configuration so the user knows what's running.

## Display

Clear and redraw each cycle. Raw ANSI escape codes to stdout.

### Active state (changes detected):
```
--- TruPal ----------------------------
watching ~/work/my-project
session: 12m

  changed: auth.py, token.py, utils.py

  build: [green]clean[/green]
  build: [red]3 errors[/red] (was 5)
  build: [red]3 errors (stalled x4)[/red]
  build: [yellow]3 errors (was 1)[/yellow]

  [yellow]! auth.py edited 4 times -- whack-a-mole?[/yellow]
  [yellow]! empty except block (token.py:+23)[/yellow]
  [yellow]! deleted test_auth.py[/yellow]

  last check: 2s ago
----------------------------------------
```

### Idle state (no changes):
```
--- TruPal ----------------------------
watching ~/work/my-project
session: 12m

  watching...

  last check: 1s ago
----------------------------------------
```

### Colors:
- Green: build clean, positive signals
- Red: build failing
- Yellow: trajectory warnings, pattern matches
- Dim/gray: metadata (session time, last check, file list)

### Line number references

For diff pattern matches, show the diff line reference as `+N` (the line number in the new file from the diff hunk header). Parsed from the `@@` line in unified diff format: `@@ -a,b +c,d @@` — count added lines from `c` to determine the line number.

## Project Structure

```
trupal/
  main.go        # CLI entry (start/stop), tmux pane management
  watcher.go     # Poll loop, git commands, orchestrates each cycle
  session.go     # Session state struct, trajectory signal evaluation
  checker.go     # Build command runner, diff pattern regex matching
  display.go     # ANSI rendering to stdout
  config.go      # Parse .trupal.toml
  go.mod
```

Estimated ~400 lines total. Single binary: `go build -o trupal .`

## Dependencies

Zero external dependencies. Stdlib only:
- `os/exec` — shell out to git, tmux, build command
- `regexp` — diff pattern matching (pre-compiled at init)
- `fmt`, `strings`, `strconv` — output and parsing
- `time` — polling, session duration
- `os`, `path/filepath` — file paths, project root detection
- `crypto/md5` or `hash/fnv` — cheap diff content hashing for edit detection
- `bufio` — line-by-line diff parsing

## Platform Support

macOS and Linux. No OS-specific code. Requires `git` and `tmux` in PATH.

## What is NOT in MVP

- No JSONL parsing (V1)
- No send-keys / buddy mode (V1)
- No bidirectional communication with CC (V1)
- No trust score computation (V1)
- No finding lifecycle tracking (V1)
- No plan drift detection (V1)
- No claim-action verification (V1)
- No persistence across sessions
- No fsnotify / file watching (polling is sufficient)
- No external dependencies

## Success Criteria

Use TruPal alongside CC for one full day of real work. Did it catch at least one real issue (whack-a-mole, error stall, error swallowing, deleted test) that you would have missed or noticed late? If yes, the MVP proved its value.
