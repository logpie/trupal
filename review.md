# Implementation Gate — 2026-04-06 — TruPal V1 full repo review

## Round 1 — Codex

- [CRITICAL] Brain mutex blocks shutdown — `Notify` held mutex during blocking `scanner.Scan()`, preventing `Stop` from killing subprocess. **Fixed**: replaced mutex with `atomic.Bool`, `Stop` closes stdin to unblock scanner.
- [CRITICAL] Session rollover does not restart brain — new JSONL path but brain kept old system prompt. **Fixed**: stop old brain and start new one on session switch.
- [IMPORTANT] Unsynchronized brain/jsonlPath mutations — goroutine and main loop both wrote brain state. **Fixed**: goroutine reports via channels, main loop is sole owner.
- [IMPORTANT] JSONL watcher ignores truncation/replacement — only handled Write events, stale offset on truncation. **Fixed**: handle Create/Rename, check file size < offset in ReadNew.
- [IMPORTANT] Brain scanner errors silently swallowed — EOF collapsed to "(no response)". **Fixed**: check scanner.Err(), return actual error.
- [IMPORTANT] Build command not shell-safe — strings.Fields breaks on quotes/pipes. **Fixed**: run via `sh -c`.
- [IMPORTANT] Stop command doesn't confirm exit — 500ms sleep, no verification. **Acknowledged**: acceptable for MVP, pid file removed regardless.
- [IMPORTANT] All git failures silently return empty — runGit returns "" on error. **Deferred**: acceptable for MVP, surface in debug log.
- [IMPORTANT] .git detection rejects worktrees — only accepted directories. **Fixed**: accept file or directory.
- [NOTE] Provider config misleading — `brain_provider` exposed but only `claude` implemented. **Deferred**: by design, Codex provider planned for next iteration.
- [REFACTOR] Finding state machine has unreachable states — `new` and `waived` never produced. **Deferred**: `waived` is planned for V2 with buddy mode.

## Round 2 — Codex re-reviewed fixes

- [IMPORTANT] Brain restart from goroutine during shutdown — goroutine could spawn new brain after SIGINT. **Fixed**: added `shuttingDown` flag, goroutine reports errors via channel, main loop decides restart.
- [IMPORTANT] JSONL offset race — watchLoop and ReadNew both wrote offset. **Fixed**: removed offset mutation from watchLoop, ReadNew is sole owner.
- [IMPORTANT] Session switch ignores errors — NewJSONLWatcher/StartBrain errors dropped. **Fixed**: check errors, only replace old instance after new one succeeds.
- [IMPORTANT] ReadNew doesn't check scanner.Err — advances offset on scan failure. **Fixed**: check scanner.Err() before committing offset.
- [NOTE] Pipe leak on StartBrain failure — stdin not closed if stdout/start fails. **Fixed**: close stdin on error paths.

## Round 3 — Smoke test (pre-Codex MCP)

All fixes verified. Build clean, vet clean, start/stop lifecycle correct, brain responds, no pane death, no window kills.

## Round 4 — Codex MCP review

- [CRITICAL] Session switch while brain busy — stale brain kept analyzing old JSONL. **Fixed by Codex**: brainStale flag, instance reference in result channels, force restart.
- [IMPORTANT] Debounced triggers dropped when brainBusy — activity lost permanently. **Fixed by Codex**: pendingTrigger + mergeTriggerReason, replayed when brain finishes.
- [IMPORTANT] Active findings only in brain system prompt at startup — brain can't resolve new findings. **Fixed by Codex**: brain.Notify() now takes (reason, findingsJSON), findings sent every turn.
- [IMPORTANT] ShouldRunBuild ignores untracked files. **Fixed by Codex**: takes (changedFiles, untrackedFiles, extensions).
- [IMPORTANT] brain_provider parsed but never used — always calls claude. **Fixed by Codex**: brainCommand() validates provider, Config.Validate() fails fast.
- [IMPORTANT] runGit silently returns "" on all errors. **Fixed by Codex**: returns (string, error), failures logged via Debugf.
- [NOTE] .trupal.toml parser is not real TOML — deferred.
- [NOTE] Rendering inconsistencies with BrainLastMsg/CCStatus — deferred.
- [REFACTOR] Dead fields/helpers (printLine, wrapPrint, unused struct fields) — deferred.

## Round 5 — Codex re-reviewed fixes

- [IMPORTANT] Config validation after pid file write — stale .trupal.pid on bad config. **Fixed by Codex**: moved validation before pid file creation.
- APPROVED. No new issues.
