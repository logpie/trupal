## Observations

- Goal: determine whether benchmark and live qutebrowser runs match closely enough to support reliable offline TruPal evals.
- Fresh non-replay runs on 2026-04-13 matched on nudge count only:
  - benchmark: `2` nudges from [bench/results/20260413-015423-instance_qutebrowser__qutebrowser-f631cd4422744160d9dcf7a0455da532ce973315-v35616345bb8052ea303186706cec663146f0f184-steer/report.md](/home/yuxuan/work/trupal-codex/bench/results/20260413-015423-instance_qutebrowser__qutebrowser-f631cd4422744160d9dcf7a0455da532ce973315-v35616345bb8052ea303186706cec663146f0f184-steer/report.md)
  - live: `2` nudges from `/tmp/trupal-ui-live-current-qutebrowser-n_i0hl07/.trupal.steer.jsonl`
- Deterministic replay-brain runs also matched on count only:
  - benchmark replay: `1` nudge
  - live replay: `1` nudge
- Replay brain removed TruPal brain/model randomness as a cause, but the notification streams still differed:
  - benchmark replay notifications: `36`
  - live replay notifications: `24`
- Timing/phase boundaries still differed under replay brain:
  - first production-edit turn: benchmark `16`, live `14`
  - schema/configdata phase: benchmark `22`, live `15`
- The first user prompt in the watched Codex session is not identical between bench and live. The prompt body is the same shape, but the embedded `cwd` differs:
  - benchmark prompt starts with `# AGENTS.md instructions for /tmp/trupal-swebench-run-instance_qutebrowser...`
  - live prompt starts with `# AGENTS.md instructions for /tmp/trupal-ui-live-current-qutebrowser-ww2wo8ul`
- The actual benchmark task prompt text also differed before fixes:
  - benchmark Go prompt emitted `Requirements for this run:` with literal `-` bullets
  - live Python prompt emitted the same requirements as plain sentences
- Live and bench `.trupal.toml` intent is the same for the live replay repo:
  - `session_provider = "codex"`
  - `brain_provider = "codex"`
  - `brain_model = "gpt-5.4-mini"`
  - `brain_effort = "medium"`
  - `benchmark_mode = true`
  - `benchmark_arm = "steer"`
  - `benchmark_steering_mode = "continuous"`
- Notification replay command now exists and works:
  - `trupal replay-notifications --project-dir <repo> <notifications.jsonl>`
- Offline replay of recorded notifications is deterministic; fresh Codex runs are not.
- After changing benchmark workspaces to the same live-style prefix, replay benchmark moved substantially closer to live:
  - before fix: `36` notifications, first edit `16`, schema turn `22`
  - after live-style prefix fix: `28` notifications, first edit `15`, schema turn `16`
- After also aligning benchmark prompt formatting with live, a fixed-workdir benchmark experiment on `/tmp/trupal-qutebrowser-fixed` reached `20` notifications by the sampled checkpoint, showing further sensitivity to visible Codex inputs.
- Final exact-parity experiment with fixed workdir, replay brain, and aligned prompt text:
  - benchmark fixed-path final: `25` notifications, first edit `11`, schema turn `13`, `1` nudge
  - live fixed-path final: `26` notifications, first edit `13`, schema turn `13`, `1` nudge
  - both converged into the same late idle-review shape
- The “only 1 nudge” concern in the offline test was partly a real bug in the replay harness:
  - richer replay scripts with later matched turns were silently collapsing to the blank fallback after turn 1
  - root cause was `nextReplayTurn` consuming the first blank fallback turn inside the scan loop instead of only after later specific matches failed
- After fixing replay-turn fallback semantics, a richer qutebrowser replay fixture now emits `3` nudges across the same recorded notification stream.
- Richer fixed-path parity rerun after the replay bug fix:
  - benchmark fixed-path multi-nudge run reached `31` notifications and all `3` scripted nudges
  - live fixed-path multi-nudge run reached `44` notifications and all `3` scripted nudges
  - the nudge sequence matched exactly, but live took materially more review turns to reach the same later nudges

## Hypotheses

### H1: The remaining mismatch is upstream Codex trajectory divergence caused by bench/live not launching truly identical Codex sessions. (ROOT HYPOTHESIS)
- Supports:
  - replay brain removes TruPal brain randomness, but bench/live notification streams still diverge
  - divergence appears before the main fix lands
  - the watched Codex prompt is not byte-identical because `cwd` / temp-path tokens differ
  - the benchmark task prompt formatting was not identical to live before fixes
  - bench and live still create separate fresh Codex sessions with separate temp repos and session ids
- Conflicts:
  - `.trupal.toml` intent is the same
  - fresh non-replay nudge counts can still match
- Test:
  - compare actual first prompts, notification logs, and replayed outputs; if they still diverge under replay brain, the mismatch is upstream of TruPal brain

### H2: The mismatch is caused mainly by TruPal brain nondeterminism.
- Supports:
  - older stale live runs had much larger nudge-count spread than benchmark
- Conflicts:
  - replay brain still shows bench/live notification divergence
  - replay brain runs matched on nudge count but not on notification trajectory
- Test:
  - force both sides onto the same replay brain and compare notification counts/turns

### H3: The mismatch is only a logging artifact in notification capture.
- Supports:
  - bench/live notification counts differ a lot (`36` vs `24`)
- Conflicts:
  - production-edit turn and schema-phase turn differ too, which points to real upstream trajectory differences
  - steer logs and debug logs tell the same story
- Test:
  - compare normalized trigger summaries and downstream replay outputs; if phase boundaries differ, it is not just logging noise

### H4: The offline eval lane only getting 1 nudge is a fixture/replay issue rather than a TruPal limitation. (ROOT HYPOTHESIS for the 1-nudge concern)
- Supports:
  - the current parity replay file only scripted one meaningful nudge
  - a richer replay script still produced only one nudge before the replay fallback bug was fixed
  - recorded notification stream clearly contains later phases that can support more than one finding
- Conflicts:
  - deterministic parity fixture did intentionally aim to be minimal
- Test:
  - create a richer replay file with later matched turns and verify replay emits multiple nudges after fixing fallback behavior

## Experiments

### E1: Force a deterministic brain on both benchmark and live
- Change:
  - added replay brain provider via `TRUPAL_BRAIN_REPLAY_PATH`
- Prediction:
  - if brain nondeterminism is the cause, bench/live should now match closely
- Result:
  - nudge counts matched (`1` vs `1`), but notification streams still diverged (`36` vs `24`)
- Conclusion:
  - rejects H2 as the primary remaining cause

### E2: Record exact brain inputs for both runs
- Change:
  - added `.trupal.notifications.jsonl` recording and artifact copying
- Prediction:
  - if bench/live differ upstream, notification trajectories will still differ under replay brain
- Result:
  - notification count, phase timing, and trigger ordering still differed
- Conclusion:
  - supports H1 over H3

### E3: Compare actual watched-Codex first prompts
- Change:
  - extracted first user message from benchmark artifact session JSONL and live repo-local isolated `CODEX_HOME` session JSONL
- Prediction:
  - if bench/live are not truly identical upstream, the prompts will differ
- Result:
  - prompt bodies matched in structure but differed in embedded `cwd` / temp repo path
- Conclusion:
  - supports H1; bench/live are not actually identical Codex inputs

### E4: Offline replay recorded notification logs
- Change:
  - implemented `trupal replay-notifications`
- Prediction:
  - if recorded notifications are a good offline eval surface, replay should be deterministic
- Result:
  - replay ran successfully and produced stable outputs
- Conclusion:
  - supports switching offline evals to recorded-notification replay rather than fresh live-vs-bench Codex runs

### E5: Align benchmark workspace path shape with live
- Change:
  - benchmark workspace prefix changed from `trupal-swebench-run-...` to live-style `trupal-ui-live-current-<repo>-...`
- Prediction:
  - if visible `cwd` tokens are part of the divergence, benchmark should move materially closer to live under replay brain
- Result:
  - replay benchmark moved from `36` notifications / first edit `16` / schema `22` to `28` / `15` / `16`
- Conclusion:
  - confirms that workspace-path/CWD visible to Codex is a real cause of mismatch

### E6: Align benchmark task prompt formatting with live
- Change:
  - benchmark prompt now emits the same plain-sentence requirements block as live instead of bullet markers
- Prediction:
  - if prompt-format mismatch contributes to divergence, benchmark should move closer again
- Result:
  - fixed-workdir replay benchmark compressed further to `20` notifications by the sampled checkpoint
- Conclusion:
  - supports prompt-format mismatch as another real cause

### E7: Force benchmark and live onto the exact same visible workdir
- Change:
  - added `TRUPAL_FIXED_WORKDIR` and ran both benchmark and live sequentially against `/tmp/trupal-qutebrowser-fixed` with the same replay brain and aligned prompt text
- Prediction:
  - if the remaining divergence is mostly visible-path/session-input drift, bench and live trajectories should nearly collapse to the same shape
- Result:
  - benchmark: `25` notifications, first edit `11`, schema `13`, `1` nudge
  - live: `26` notifications, first edit `13`, schema `13`, `1` nudge
- Conclusion:
  - confirms the dominant remaining mismatch was input-shape drift, and the applied fixes bring bench and live close enough to treat the parity harness as matched for deterministic runs

### E8: Validate why the offline golden only had 1 nudge
- Change:
  - created a richer replay fixture intended to emit 3 nudges at different phases
- Prediction:
  - if the replay lane is healthy, the richer fixture should emit multiple nudges
- Result:
  - initial richer replay still emitted only 1 nudge
  - inspection found a replay bug: blank fallback turns were consumed immediately, so later specific matches were unreachable
- Conclusion:
  - confirms H4 and identifies a real replay bug

### E9: Fix replay fallback semantics and regenerate richer golden
- Change:
  - changed `nextReplayTurn` so blank fallback turns are skipped during matching and used only after later specific matches fail
- Prediction:
  - richer fixture should now emit all scripted later nudges
- Result:
  - richer qutebrowser fixture now emits `3` nudges on the same 24 recorded notifications
- Conclusion:
  - confirms the one-nudge concern was mostly a fixture/replay bug, not a fundamental limitation of the offline eval lane

### E10: Re-run fixed-path bench vs live with the richer 3-nudge replay fixture
- Change:
  - ran benchmark and live against the same fixed workdir with the same richer replay brain
- Prediction:
  - if the remaining mismatch was only the one-nudge replay bug, bench and live should now align closely on both nudge sequence and notification trajectory
- Result:
  - both runs emitted the same 3 nudges in the same order
  - benchmark reached them in `31` notifications
  - live reached them in `44` notifications
- Conclusion:
  - the one-nudge bug is fixed, but a smaller fresh-session/live-run timing/trajectory mismatch still remains upstream of TruPal’s nudging logic

## Root Cause

Benchmark and live were not actually giving Codex identical inputs. The main causes were the visible workspace path/CWD and the serialized benchmark prompt text; Codex is sensitive enough to those differences that the upstream coding trajectory diverged before TruPal could intervene. After aligning those inputs and optionally forcing both runs onto the same visible workdir, benchmark and live became near-parity in deterministic runs.

For the “only 1 nudge” concern, the root cause was separate: the deterministic parity golden used a one-nudge replay fixture, and the replay runner also had a fallback-turn bug that prevented later matched nudges from firing in richer fixtures.

After that fix, bench and live still differ on how many review turns they take to reach the same later findings, even when they now agree on the nudge sequence.

## Fix

- Implemented deterministic replay brain and notification capture/replay so offline TruPal evals can run against recorded notification streams instead of fresh Codex live-vs-bench runs.
- Made benchmark workspaces use the same live-style temp path shape as live runs.
- Made the benchmark task prompt text match the live helper prompt format.
- Added `TRUPAL_FIXED_WORKDIR` so bench and live can be forced onto the exact same visible workspace path for parity experiments.
- Fixed replay-turn fallback semantics so later matched scripted nudges are reachable.
- Added a richer qutebrowser multi-nudge golden fixture to validate that the offline lane can exercise more than a single finding.
- For deterministic parity runs, these fixes are enough to make benchmark and live effectively match.
- For richer deterministic runs, benchmark and live now match on nudge sequence but not yet on review-turn count.
- Fresh uncontrolled benchmark and live Codex sessions can still drift, but the deterministic offline eval surface now removes the dominant bench/live mismatch.
