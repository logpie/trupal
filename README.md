# TruPal

A trust layer for coding agents. TruPal sits next to Claude Code or Codex as a tmux sidecar, watches what the agent does in real-time, and nudges it when something's off — like a senior dev pair programmer who specializes in verification.

```
┌─────────────────────────────┬────────────────────────────────────────────┐
│ Claude Code                 │ TruPal                                     │
│                             │                                            │
│ claude: editing auth.py     │  trupal · myproject · 12m                  │
│ claude: [shows diff]        │  ● cc  ● 5s  ✓ build  ⚠ 2                 │
│                             │  ──────────────────────────────────────     │
│                             │  14:23 ⚠ Hey, you just added Expire()      │
│                             │           but it never removes expired      │
│                             │           entries — they pile up in         │
│                             │           memory forever.                   │
│                             │                                            │
│                             │  14:25 ✗ You said you verified all          │
│                             │           callers but I don't see a         │
│                             │           single grep in your tool calls.   │
│                             │                                            │
│                             │  ──────────────────────────────────────     │
│                             │  2 mod: auth.py config.py                  │
└─────────────────────────────┴────────────────────────────────────────────┘
```

## What it does

TruPal watches Claude Code or Codex session logs and git state, then uses a configurable brain provider to analyze what the agent is doing:

- **Claim-action gaps** — CC said "I verified all callers" but never ran grep
- **Trajectory problems** — same file edited 4 times, errors not decreasing
- **Process quality** — edited without reading first, no tests after changes
- **Structural issues** — error swallowing, deleted tests, race conditions

Nudges are conversational, like a senior dev talking across the desk:

> "Hey, your Serialize() and Load() methods are silently swallowing json errors. If Marshal fails, you return nil and the caller doesn't know."

## Install

```bash
# Requires: Go 1.24+, tmux, claude CLI or codex CLI
git clone https://github.com/logpie/trupal.git
cd trupal
go build -o trupal .
sudo mv trupal /usr/local/bin/  # or add to PATH
```

## Usage

```bash
# Start watching (splits tmux pane next to Claude Code)
trupal start

# Start watching a specific project
trupal start /path/to/project

# Stop (pane stays open for review)
trupal stop

# Stop and close pane
trupal stop --close

# View session log
trupal log
```

## Configuration

Optional `.trupal.toml` in your project root:

```toml
# Build check command (runs on file changes)
build_cmd = "go vet ./..."
build_extensions = [".go"]

# Session settings
session_provider = "claude"  # claude (default) or codex

# Brain settings
brain_provider = "claude"    # claude (default) or codex
brain_model = "sonnet"       # claude: haiku/sonnet/opus, codex: model id or omit for default
brain_effort = "high"        # low, medium, high, max

# Poll interval in seconds
poll_interval = 3
```

Without a config file, TruPal runs with sensible defaults (Claude session + Claude sonnet brain, high effort, no build check).

## TUI

Bubble Tea TUI with:

- **Fixed header** — project name, watched agent status, brain status, build status, findings count
- **Scrollable log** — nudges and events, keyboard (j/k, pgup/pgdn) scrolling
- **Mouse wheel** scrolling
- **Click-drag text selection** with clipboard copy (cmd+v)
- **Fixed footer** — modified files, scroll indicator

## Architecture

```
trupal (Go binary)
├── main.go        — CLI, tmux pane management
├── watcher.go     — main loop: git polling, JSONL events, brain triggers
├── brain.go       — provider-aware brain (`claude` stream-json or `codex exec --json`)
├── jsonl.go       — provider-aware session watcher, incremental parser
├── tui.go         — Bubble Tea TUI
├── selection.go   — mouse text selection + OSC 52 clipboard
├── session.go     — trajectory tracking (whack-a-mole, error trends)
├── findings.go    — finding lifecycle (new → shown → resolved)
├── checker.go     — build command runner
├── config.go      — .trupal.toml parser
└── debug.go       — timestamped debug logging (.trupal.debug)
```

Claude brains run as a persistent `claude -p --input-format stream-json --output-format stream-json` subprocess. Codex brains run via repeated `codex exec --json` calls in read-only mode. TruPal can also watch Codex sessions from `~/.codex/sessions` in addition to Claude Code JSONL sessions.

## Requirements

- Go 1.24+
- tmux
- `claude` CLI and/or `codex` CLI
- Access to the configured brain provider

## Status

V1 — functional but early. The brain catches real bugs and generates useful nudges. Known limitations:

- Brain is most effective when CC works on the same project trupal watches
- Brain response time varies (2-30s depending on investigation depth)
- No send-keys injection yet (display-only nudges, V2 planned)

## License

MIT
