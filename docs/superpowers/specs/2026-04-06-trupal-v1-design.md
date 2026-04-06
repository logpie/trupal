# TruPal V1 Design Spec

## Overview

V1 adds intelligence to TruPal via a persistent Claude Code brain subprocess. The brain reads CC's JSONL session files and codebase autonomously, detects claim-action gaps, trajectory problems, and process quality issues, and generates contextual nudges with visible reasoning. Nudges are displayed in the tmux pane for human inspection — no send-keys injection.

## What changes from MVP

| Component | MVP | V1 |
|-----------|-----|-----|
| Analysis | Regex patterns on git diff | LLM brain with full tool use |
| JSONL | Not read | Watched via fsnotify, brain reads directly |
| Nudges | None | Brain generates, displayed with reasoning |
| Finding lifecycle | None | new → shown → resolved/waived |
| Dependencies | Zero | fsnotify |
| Diff patterns | Regex in checker.go | Removed — brain handles this better |

## Architecture

```
┌─────────────────────────┐
│ trupal (Go binary)      │
│                         │
│ ┌─────────────────────┐ │     ┌──────────────────────────┐
│ │ JSONL watcher       │ │     │ CC brain (subprocess)    │
│ │ (fsnotify)          │─│────>│                          │
│ │ detects: new msgs,  │ │     │ claude -p --stream-json  │
│ │ tool calls, idle    │ │     │ --model sonnet           │
│ └─────────────────────┘ │     │ --effort high            │
│                         │     │ --dangerously-skip-perms  │
│ ┌─────────────────────┐ │     │                          │
│ │ git poller (3s)     │─│────>│ On notification:         │
│ │ build runner        │ │     │  · reads CC JSONL        │
│ │ trajectory tracker  │ │     │  · reads source files    │
│ └─────────────────────┘ │     │  · runs git diff, grep   │
│                         │<────│  · returns nudges        │
│ ┌─────────────────────┐ │     └──────────────────────────┘
│ │ display renderer    │ │
│ │ tmux pane + log     │ │
│ └─────────────────────┘ │
└─────────────────────────┘
```

The Go binary handles: tmux pane lifecycle, JSONL watching, git polling, build checks, trajectory tracking, display rendering, brain subprocess management.

The CC brain handles: reading JSONL + codebase with tools, analyzing CC's behavior, generating nudges with reasoning.

## Brain subprocess

### Spawning

On `trupal start`, the Go binary spawns a persistent CC subprocess:

```bash
claude -p \
  --input-format stream-json \
  --output-format stream-json \
  --verbose \
  --system-prompt "<brain system prompt>" \
  --model <brain_model> \
  --effort <brain_effort> \
  --dangerously-skip-permissions \
  --no-session-persistence \
  --allowed-tools "Read,Bash,Grep,Glob"
```

Key flags:
- `--dangerously-skip-permissions` — brain runs autonomously
- `--no-session-persistence` — brain's own session doesn't need saving
- `--allowed-tools "Read,Bash,Grep,Glob"` — brain can observe but NOT edit. Read-only.
- `--verbose` — required for stream-json output

The subprocess stays alive for the entire trupal session. Go writes JSON messages to its stdin, reads JSON responses from its stdout.

### Communication protocol

Go sends a user message to the brain's stdin:

```json
{"type":"user","message":{"role":"user","content":"<notification text>"}}
```

Brain responds with assistant messages on stdout as stream-json events. The response flow is:

1. `{"type":"system","subtype":"init",...}` — turn start (ignore)
2. `{"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}` — brain's response. The `text` field contains the brain's reasoning and nudge JSON. The brain may also make tool calls (Read, Bash, Grep) which appear as additional assistant/user message pairs in the stream.
3. `{"type":"result",...}` — turn complete. Go can now parse the accumulated text content for the final JSON response.

Go collects all `text` content blocks from the assistant messages in a turn, then parses the final JSON (`{"reasoning":..., "nudges":..., "resolved_findings":...}`) from the last text block.

### Restart on crash

If the brain subprocess exits unexpectedly, Go restarts it after a 5-second backoff. The brain starts fresh (no persistent session) so restart is clean.

### Brain system prompt

```
You are TruPal, a verification agent watching another Claude Code session work.
You are a peer to CC — a pair programmer specializing in catching mistakes,
verifying claims, and keeping the other agent honest.

CC's session JSONL: {jsonl_path}
Project directory: {project_dir}

ACTIVE FINDINGS (unresolved):
{findings_json}

When notified of activity, investigate autonomously:

1. Read the recent JSONL entries to understand what CC just did
2. Check git diff to see what actually changed
3. Look for:
   - CLAIM-ACTION GAPS: CC said it did something but JSONL shows no
     corresponding tool call (e.g. "verified tests pass" but no test command)
   - TRAJECTORY PROBLEMS: same file edited repeatedly, errors not decreasing,
     scope drifting from original task
   - PROCESS QUALITY: did CC read before editing? did CC verify after changing?
     root cause vs symptom patching?
   - STRUCTURAL ISSUES: error swallowing, deleted tests, coupling increase
4. Check if any active findings have been resolved by CC's recent actions

Respond with JSON:
{
  "reasoning": "what you checked and what you found (be specific, quote evidence)",
  "nudges": [
    {"severity": "warn|error", "message": "short nudge under 120 chars"}
  ],
  "resolved_findings": ["<finding_id>", ...]
}

If nothing noteworthy: {"reasoning": "checked X, Y, Z — nothing to flag", "nudges": [], "resolved_findings": []}

Guidelines:
- Be specific: quote file names, line numbers, tool names, JSONL timestamps
- Be concise: nudges display in a narrow tmux pane (under 120 chars)
- Don't nag: minor style issues are not worth flagging
- High precision: only flag things you're confident about
- Check your active findings: if CC addressed one, mark it resolved
```

The `{findings_json}` placeholder is populated with active (unresolved) findings so the brain can track whether CC addressed previous nudges.

## JSONL watching

### Finding CC's session file

CC stores session JSONL at `~/.claude/projects/<encoded-path>/<session-id>.jsonl` where `<encoded-path>` is the project directory with `/` replaced by `-` (e.g. `/home/yuxuan/work/myproject` → `-home-yuxuan-work-myproject`).

On startup, trupal:
1. Encodes the project dir to find the CC projects directory
2. Finds the most recently modified `.jsonl` file
3. Starts watching it

If the session file changes (CC starts a new session), trupal detects this and switches to watching the new file.

### Tiered watching (from Sidecar)

- **HOT tier**: fsnotify on the session directory. 100ms debounce. Used when session is active (JSONL modified within last 5 minutes).
- **COLD tier**: 30-second file stat poll. Used when session is inactive.
- Auto-promote to HOT on activity, auto-demote to COLD after 5 minutes of silence.

### Incremental parsing

Track byte offset into the JSONL file. On each fsnotify event:
1. Seek to last offset
2. Read new bytes to EOF
3. Parse each complete line as JSON
4. Update offset
5. Classify each new entry

### Entry classification

Each JSONL entry is classified as:

| Type | Role | Content | Meaning |
|------|------|---------|---------|
| `user` | user | string | Human typed a prompt |
| `assistant` | assistant | text block | CC made a claim or statement |
| `assistant` | assistant | tool_use block | CC called a tool |
| `user` | user | tool_result | Tool returned a result |

### Status detection (from Sidecar)

Two-tier status detection:

1. **Fast path**: if JSONL mtime < 30s ago → CC is active
2. **Slow path**: scan backwards in JSONL for last entry
   - Last entry is `user` (human message or tool result being processed) → CC is thinking
   - Last entry is `assistant` → CC is idle/waiting for input

Important: LLM thinking can produce 55-second gaps with no JSONL writes. Don't assume idle prematurely.

### Sub-agent awareness

Check for `{session-id}/subagents/agent-*.jsonl` files. If any sub-agent JSONL is being written to, CC is active (delegating work). Without this, trupal would wrongly think CC is idle during sub-agent execution.

## Brain triggers

### What triggers brain analysis

| Event | Source | Debounce |
|-------|--------|----------|
| CC assistant message with text | JSONL watcher | 2s |
| CC tool sequence completed | JSONL watcher (result entry after tool_use) | 2s |
| CC idle for 60s+ | Status detection | Once per idle period |
| Build status changed | Build runner | Immediate |
| Trajectory signal fired | Session tracker (whack-a-mole, error stall) | Immediate |

### Debouncing

JSONL events come in bursts (CC outputs multiple content blocks per turn). Debounce with a 2-second window: after the first new JSONL entry, wait 2s for activity to settle, then batch all new entries into one brain notification.

### What does NOT trigger brain

- Individual tool_result entries mid-sequence (too frequent, wait for sequence to complete)
- Repeated identical git state (nothing new to analyze)
- Brain's own subprocess activity (avoid feedback loops)

## Finding lifecycle

Each nudge from the brain becomes a Finding:

```go
type Finding struct {
    ID        string    // unique ID (e.g. "f-001")
    Timestamp time.Time
    Severity  string    // "warn" or "error"
    Nudge     string    // short message for display
    Reasoning string    // brain's inner monologue
    Status    string    // "new" → "shown" → "resolved" | "waived"
}
```

Lifecycle:
- **new**: brain just generated this nudge
- **shown**: displayed in the pane (immediate transition from new)
- **resolved**: brain detected CC addressed it (e.g., CC ran the grep after being flagged for not verifying callers). Brain reports `resolved_findings` in its response.
- **waived**: human dismissed it (via trupal command or keypress — stretch goal for V1, can be manual log edit)

Active (unresolved) findings are passed to the brain in each notification via the system prompt's `{findings_json}` placeholder. This lets the brain:
- Avoid re-flagging the same issue
- Detect when CC resolves a previous finding
- Escalate findings that have been open too long

Display shows finding status:
```
─── brain ────
14:23 [shown] reading JSONL...
      CC said "verified callers"
      no grep/search in tool_calls
      → no evidence of caller check

14:25 [resolved] test_auth.py
      CC re-added the test file
```

The `.trupal.log` records the full lifecycle of each finding.

## Display

### Layout

```
─── trupal ───
myproject | 12m

  modified:
  · token.py
  · auth.py

  ✓ build clean
  ▸ token.py edited 4x

─── brain ────
14:23 reading JSONL...
      CC said "verified callers"
      no grep/search in tool_calls
      → no evidence of caller check

14:25 reading git diff...
      test_auth.py deleted
      no explanation in JSONL
      → deleted test without reason

──────────────
```

Top section: status and trajectory signals from Go watcher (cheap, always-on, updates every 3s).

Bottom section: brain reasoning and nudges. Each entry shows:
- Timestamp
- Finding status indicator (if resolved/waived)
- Brain's reasoning in dim text
- Nudge in yellow (the actionable conclusion)

Nudges accumulate during the session, capped at last 10. Older nudges scroll off the display but remain in `.trupal.log`.

### When brain is thinking

While the brain subprocess is processing (reading files, analyzing):
```
─── brain ────
  thinking...
```

## Configuration

`.trupal.toml`:

```toml
# Build check (unchanged from MVP)
build_cmd = "go vet ./..."
build_extensions = [".go"]
poll_interval = 3

# Brain
# provider: "claude" (CC subprocess) or "codex" (codex CLI)
brain_provider = "claude"
# model: for claude — haiku, sonnet, opus
#        for codex — codex-mini, o3, etc.
brain_model = "sonnet"
# effort: low, medium, high, max
brain_effort = "high"
```

If no `.trupal.toml` exists, defaults:
- No build check
- `brain_provider = "claude"`
- `brain_model = "sonnet"`
- `brain_effort = "high"`

## Project structure

```
trupal/
  main.go        # CLI entry, tmux pane management (unchanged)
  config.go      # Parse .trupal.toml — add brain fields
  session.go     # Session state, trajectory evaluation (unchanged)
  watcher.go     # Poll loop — integrate JSONL watcher + brain triggers
  display.go     # ANSI rendering — add brain/nudge section
  checker.go     # Build runner only — remove regex patterns
  brain.go       # NEW: CC subprocess lifecycle, stdin/stdout protocol
  jsonl.go       # NEW: find session file, fsnotify, incremental parse
  findings.go    # NEW: finding lifecycle management
  go.mod
```

### New files

**brain.go** — manages the CC brain subprocess:
- `StartBrain(cfg Config, projectDir string, jsonlPath string) *Brain`
- `(*Brain).Notify(message string)` — send event to brain's stdin
- `(*Brain).ReadResponse() BrainResponse` — parse brain's stdout
- `(*Brain).Stop()` — kill subprocess
- Auto-restart on crash with 5s backoff

**jsonl.go** — JSONL session watching:
- `FindSessionJSONL(projectDir string) string` — find CC's active session file
- `NewJSONLWatcher(path string) *JSONLWatcher` — fsnotify + incremental reader
- `(*JSONLWatcher).Poll() []JSONLEntry` — read new entries since last poll
- `DetectStatus(path string) string` — active/thinking/idle status detection
- Sub-agent file detection

**findings.go** — finding lifecycle:
- `FindingStore` — holds all findings, provides active/resolved/waived queries
- `(*FindingStore).Add(f Finding)` — add new finding from brain
- `(*FindingStore).Resolve(ids []string)` — mark findings resolved
- `(*FindingStore).Active() []Finding` — get unresolved findings for brain context
- `(*FindingStore).Recent(n int) []Finding` — get last N for display

### Changed files

**config.go** — add `BrainProvider`, `BrainModel`, `BrainEffort` fields and parsing.

**display.go** — add brain section below status. Show reasoning (dim), nudges (yellow), finding status, "thinking..." indicator.

**watcher.go** — integrate JSONL watcher events and brain triggers into the poll loop. Debounce JSONL events (2s window). Trigger brain on significant events. Pass brain responses to display.

**checker.go** — remove all regex pattern matching (`ScanDiffPatterns`, `detectErrorSwallowing`, etc.). Keep only `RunBuildCheck`, `ShouldRunBuild`, `ScanDeletedTests`.

## Dependencies

- **fsnotify/fsnotify** — for JSONL file watching. Required for timely event detection (can't poll JSONL every 3s — need sub-second response to CC actions).

No other new dependencies. Brain uses `claude` CLI already installed on the machine.

## Platform support

macOS and Linux (unchanged). Requires `git`, `tmux`, and `claude` CLI in PATH.

## What is NOT in V1

- No send-keys / buddy mode (V2) — nudges are display-only
- No bidirectional CC ↔ TruPal communication (V2)
- No learned steering policy (V2)
- No trust score computation (V2)
- No multi-brain consensus (V2) — single brain, user chooses provider
- No plan drift detection (brain could do this but not explicitly prompted for V1)
- No Codex provider implementation (architecture supports it, Claude provider ships first)

## Success criteria

Run trupal V1 alongside CC for a real coding session. Did the brain:
1. Catch at least one claim-action gap that would have gone unnoticed?
2. Generate nudges that were actually useful (not noise)?
3. Show reasoning that made sense (not hallucinated)?
4. Resolve findings when CC addressed them?

If yes, the intelligence layer proved its value and we can move to buddy mode (V2).
