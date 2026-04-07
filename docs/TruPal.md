---
title: "TruPal — Trust Layer for Coding Agents"
type: product-idea
status: exploring
created: 2026-04-05
tags: [agents, trust, verification, product]
---

# TruPal — Trust Layer for Coding Agents

## The Problem

AI agents say "done" — but did they actually do the work correctly? Today's agents have zero skin in the game. No penalty for hallucination, shortcuts, or incomplete work. Users either blindly trust or manually verify everything.

This is the trust problem of AI agents:
- Agent claims it fixed a bug → actually suppressed the error
- Agent claims tests pass → wrote trivial tests or didn't run them
- Agent claims it refactored safely → missed references in 3 files
- Agent claims it reviewed the code → surface-level pattern matching

Trust is not a feature of any single agent. It's a **missing layer** in the agent stack.

## Core Thesis

**Measure trust. Detect problems. Nudge the agent to do better. Learn which nudges work.**

TruPal doesn't just verify — it steers. It's a nudge engine that watches, detects, intervenes, and learns.

Trust is a measurable, optimizable property. We can:
1. Decompose it into orthogonal signals
2. Build a verification protocol that measures those signals
3. Optimize the protocol itself (cost vs. catch rate)
4. Make the system resistant to gaming (Goodhart's Law)

## Trust as a Metric

```
Trust = f(correctness, craftsmanship, process)
```

### Three dimensions:

| Dimension | What it measures | Why it matters |
|-----------|-----------------|----------------|
| **Correctness** | Does it work? | The floor — necessary but not sufficient |
| **Craftsmanship** | Is it done well? | Separates junior from senior. Sloppiness compounds. |
| **Process** | Was the approach sound? | Bad process predicts future correctness failures |

Most tools only measure correctness. Craftsmanship and process are what a senior dev pair programmer notices. Trust measures all three.

### Correctness signals:
| Signal | Measurement |
|--------|-------------|
| Type-check passes | `tsc --noEmit` zero errors |
| Tests pass | test suite before vs. after |
| No regressions | existing tests still green |
| Completeness | % of spec items addressed (adversarial review) |
| Honesty | calibration: did agent's confidence match actual accuracy? |

### Craftsmanship signals:
| Signal | Detection |
|--------|-----------|
| Dead code | Unused imports, commented-out blocks, orphaned functions in diff |
| Inconsistent naming | `getUserData` next to `fetch_account_info` in same file |
| Copy-paste duplication | Same 10 lines added in 3 places instead of extracting |
| TODO/HACK/FIXME introduced | Agent deferred instead of solving |
| Error swallowing | Empty catch blocks, `_ = err` |
| Type coercion | `any`, `as unknown as`, `@ts-ignore` introduced |
| Magic numbers/strings | Hardcoded values instead of constants |
| Test quality | Assertions that check nothing meaningful |
| Oversized functions | 200-line function when 3 smaller ones would be clearer |
| Style inconsistency | Doesn't match existing codebase patterns |

All detectable from `git diff` + pattern matching. No LLM needed. High precision.

### Process signals (trajectory-based):
| Signal | Detection |
|--------|-----------|
| Whack-a-mole | Same file edited 3+ times in one session |
| Error chasing | Error count not decreasing across iterations |
| Piling on | Additions but no deletions — patching, not refactoring |
| Circling | Same function touched repeatedly |
| Symptom shifting | Test A passes → test B fails → fix B → test C fails |
| Patch-not-fix | Added `if` guard, `try/catch` wrapper, `@ts-ignore`, `sleep/retry` instead of root cause |
| Test gaming | Changed test expectation to match wrong behavior |
| Plan drift | Files being edited don't relate to plan's target; scope creeping beyond plan; unplanned "cleanup" work |
| Scope creep | Plan said 3 files, CC is on file 7; new subtasks appearing that weren't in plan |

**Plan drift detection** — one of the most common agent failures. CC starts on task A, gets distracted by a tangential issue, spends 20 minutes refactoring something unrelated. Trust keeps the plan visible in its pane and flags when CC's actions stop aligning:
```
Plan:  fix auth token expiry bug (auth.py, token.py)
CC:    editing logger.py, refactoring log format
Trust: ⚠️ drifted from plan — current work (logging refactor) 
       doesn't relate to task (auth token expiry)
```
Sources for plan awareness: plan.md files, original user prompt (first JSONL message), task descriptions. Trust displays plan as a checklist in its pane, crossing off items as they complete.

Process signals require **trajectory visibility** — seeing the journey, not just the final diff. This is the unique value of a companion that watches continuously. Code review tools see the end result. Trust sees how the agent got there.

**Key insight:** bad process that happens to produce passing tests NOW will break next week. A whack-a-mole session is a ticking time bomb. Trust catches it before it ships.

Each signal is 0-1. Tracked per model, per task type, over time.

## Core Analysis Engine: Three-Way Comparison

Trust's power comes from comparing three data sources simultaneously:

```
What CC said    (JSONL assistant messages)  ← claims
What CC did     (JSONL tool calls)          ← actions
What resulted   (git diff, tsc, tests)     ← outcomes
```

| Mismatch | What it means | Example |
|----------|--------------|---------|
| Claims ≠ actions | Self-deception or lying | "I verified tests pass" but no test command in tool calls |
| Actions ≠ outcomes | Incompetence or bad approach | Edited the file but tsc still fails |
| Outcomes degrading | Whack-a-mole | Error count flat/increasing across iterations |
| Claims without actions | Talking out of hard problems | "I checked all callers" but no grep/search in tool calls |

### Critical failure mode: agent self-deception

The sneakiest agent failure. Agent hits something hard and rationalizes:
- "This approach won't work, let me try something simpler" → implements lesser solution
- "This is a known limitation" → didn't verify
- "The existing code handles this" → it doesn't
- "Tests pass" → never ran them
- "I've verified this works" → no verification evidence in tool calls
- "This is out of scope" → avoiding the hard part

**Claim-action verification** detects all of these:
```
CC says: "I've verified the tests pass"
JSONL shows: no bash tool call with test command
Trust: ⚠️ CC claimed verification but never ran tests
```

```
CC says: "I've checked all callers of this function"
JSONL shows: no grep/glob/search tool calls
Trust: ⚠️ CC claimed caller check but never searched
```

This is the killer feature only JSONL analysis enables. No other tool does this.

### Structural degradation detection

Agent solves the immediate problem but degrades the codebase:
- **Complexity creep** — function grew by 50+ lines, cyclomatic complexity increasing
- **Coupling increase** — new cross-module imports, reaching into private internals
- **God function emerging** — one function growing across multiple edits
- **Inconsistent error handling** — some paths handle errors, others swallow
- **Abstraction violation** — layer skipping, bypassing interfaces

Lightweight detection (no full AST needed for MVP):
- Count lines per changed function (regex-based)
- Count imports added vs removed
- Detect new cross-package imports
- Flag functions exceeding size threshold

### What trust watches (continuous background monitoring)

**From CC's JSONL (fsnotify):**
- User messages → what was requested
- Assistant messages → what was claimed
- Tool calls → what was actually done (read, edit, bash, grep)
- Tool results → did commands succeed or fail?
- Retry patterns → same tool called repeatedly
- Read-before-edit → did CC read a file before modifying it?
- Token usage → context window approaching limit
- Cost accumulating → session spend tracking
- Sub-agent activity

**From git (3s polling):**
- Files changed, created, deleted
- Diff content — additions, removals, modifications
- Edit count per file within session (whack-a-mole signal)
- Diff direction — shrinking then growing = fix-then-break cycle

**From build system (triggered on file change):**
- Type-check errors (tsc)
- Lint warnings
- Build failures
- Error count trend (decreasing = progress, flat/increasing = mole)

**Security (on every diff):**
- `.env`, credentials, API keys in diff
- New dependencies added
- `eval()`, `exec()`, `dangerouslySetInnerHTML`
- Permission changes

**Craftsmanship (on every diff):**
- `any`, `@ts-ignore`, `eslint-disable` introduced
- Empty catch blocks, TODO/HACK/FIXME
- Console.log in non-test files
- Commented-out code blocks
- Duplicated code across files

## Search Spaces for Optimization

The trust score is the objective. What are the tunable parameters?

| Space | What's tuned | Type |
|-------|-------------|------|
| **Prompt** | Instructions/rules given to agent | Text (searchable via OPRO/DSPy) |
| **Tool** | Available tools, mandatory tools, ordering | Discrete set |
| **Verification** | Which checks, when, for which task types | Pipeline DAG |
| **Model** | Which model for which job | Routing table |
| **Decomposition** | Task granularity | Scalar (chunk size) |
| **Context** | What info the agent sees | Inclusion/exclusion set |
| **Architecture** | Single / pipeline / parallel / adversarial | Topology |
| **Autonomy** | When to ask human, when to proceed | Policy |
| **Constraint** | Sandbox scope, permissions | Permission set |
| **Memory** | What persists across sessions | Retention policy |
| **Persona** | Role framing | Text |

These interact — it's a joint optimization, not 11 independent knobs.

### Optimization Approaches

**Text space (prompt rules):**
- OPRO-style: meta-LLM proposes rule mutations based on trupal log
- Evolutionary: mutate rules, select highest-trust variants
- DSPy: parameterize prompt as template, optimize fields

**Tool/verification space:**
- Contextual bandit: task features → verification subset
- Each task is a trial, reward = bugs caught / cost spent
- Learn a routing policy over time

**Architecture space:**
- NAS-style search over agent topologies
- Single agent vs. consensus vs. adversarial

## Goodhart's Law Defenses

"When a measure becomes a target, it ceases to be a good measure."

The agent will game any metric it can observe. Structural defenses:

### 1. Orthogonal Metrics (multi-objective)
Gaming one metric hurts another. Writing trivial tests improves "tests pass" but tanks "completeness" when adversarial review catches the gap. Pareto front is hard to fake.

### 2. Evaluator-Executor Separation
Agent A does the work. Agent B evaluates. Different model families = different blind spots. A can't game B's criteria because A doesn't control B. This is structurally identical to blockchain validators.

### 3. Latent Metrics
The agent doesn't know the full evaluation criteria. Public metrics (tsc, lint) plus hidden metrics (human readability score, 30-day bug escape rate). Can't optimize what you can't observe.

### 4. Spot-Check Randomization (tax audit principle)
```
always:     tsc, lint              (cheap, deterministic)
p=0.3:      adversarial review     (expensive, deep)
p=0.1:      human deep audit       (very expensive, ground truth)
```
Agent doesn't know which tasks get audited. Can't selectively perform well.

### 5. Calibration over Accuracy
Measure: does the agent know when it's uncertain? An agent that says "60% sure" and is right 60% of the time is perfectly calibrated — trustworthy even when wrong. An agent that always says "done, all good" and fails 30% of the time is dangerous.

### System Design (Goodhart-Resistant)
```
                    +----------+
                    | Human    | <-- random spot checks
                    | (ground  |     calibrates everything
                    |  truth)  |
                    +----+-----+
                         |
                    +----v-----+
        +---------->| Model B  |<----------+
        |           |(evaluator)|           |
        |           +----+-----+           |
        |                |                  |
   orthogonal      latent metrics     different
   metrics         (B decides what     model family
   (multi-obj)      to check)          (diverse
        |                |              blind spots)
        |           +----v-----+           |
        +---------->| Model A  |<----------+
                    |(executor) |
                    +----------+
```

## Product Form Factor: Nudge Engine

**TruPal is a companion** — a tmux sidecar that sits next to Claude Code, watches what CC does, and nudges it as a peer. Not a daemon you forget about. Not a tool you invoke. A pair programmer that's always visible, always watching, and speaks up when something's off.

```
┌─────────────────────────┬──────────────────────┐
│ Claude Code             │ TruPal               │
│                         │                      │
│ claude: editing auth.py │ watching...           │
│ claude: [shows diff]    │ ⚠ claimed caller     │
│                         │   check, never ran   │
│                         │   grep               │
│ [TRUPAL] you claimed    │                      │
│ you checked callers but │ nudge sent ✓         │
│ never searched. verify. │                      │
│                         │                      │
│ claude: you're right,   │ ✓ CC acknowledged    │
│ let me grep...          │ ✓ grep tool called   │
│                         │                      │
│ you: (watching, sipping │ plan: ██░░ 2/5 done  │
│       coffee)           │ trust: 0.87          │
└─────────────────────────┴──────────────────────┘
```

### The relationship:
```
Human (supervisor)
  ↕ watches, intervenes on judgment calls
TruPal ↔ CC (peers, nudging each other)
```

TruPal is CC's peer, not the human's advisor. The default is: TruPal and CC work it out. The human supervises. This shifts the human from reviewer to supervisor — way more leverage.

### What it is:
- **A nudge engine** — lightweight, timely, directional, ignorable
- **A meta-harness** — CC harnesses the model, TruPal harnesses the agent
- **A pair programmer** — that specializes in verification, process quality, and keeping CC honest

### Usage:
```bash
trupal start                    # display-only, watches and shows findings
trupal start --buddy            # display + nudges CC directly via send-keys
trupal score                    # last session's trust score
trupal report                   # trends over time
trupal log                      # full audit trail
trupal learn                    # mine patterns, propose improvements
```

### MVP: raw tmux split-pane
TruPal runs in a visible tmux pane next to CC. Simple, no TUI framework needed for MVP.

### Production: full TUI (Sidecar-style)
CC runs in a detached tmux session. TruPal captures its output via `capture-pane` and re-renders inside its own Bubble Tea TUI alongside findings, plan status, and trust score. Interactive mode forwards keystrokes to CC.

### What it audits (three levels):

| Level | What | When | Value |
|-------|------|------|-------|
| **Trajectory** | Did the agent take reasonable steps? | During execution | Early detection ("going in circles", "deleting tests") |
| **Code** | Is the output correct, safe, complete? | At completion | Immediate — "is this PR safe to merge?" |
| **Product** | Does the thing actually work for users? | Lagging (days/weeks) | Ground truth — calibrates the trust score itself |

**Start with code-level** — immediate value, easiest to measure. A few checks (type-checker, tests, adversarial review) give you a trust score today.

**Add trajectory monitoring** — catches problems during execution, not after. A bad trajectory (agent going in circles, suppressing errors, deleting tests instead of fixing them) predicts bad output before you even look at the code.

**Use product-level as calibration** — did our trupal score actually predict real-world outcomes? If we scored 0.9 but a bug escaped to production, that's the most valuable learning signal. Updates the whole system.

### TruPal as driver, not just verifier

TruPal's biggest value isn't catching mistakes after they happen. It's **steering the agent in real-time** to produce better work. Agents stop, drift, give up, or take shortcuts because nobody pushes them. TruPal does.

**The nudge vocabulary** (strategy types, not literal messages):

| Strategy | When | Effect |
|----------|------|--------|
| **Keep going** | Agent wants to stop early or hand back to user | Pushes through to completion |
| **Challenge** | Agent makes a suspicious claim | Questions the assertion with evidence |
| **Demand reasoning** | Agent does something without explaining | Asks for the "why" behind the decision |
| **Redirect to root cause** | Agent patches a symptom | Points to the actual underlying issue |
| **Encourage persistence** | Agent gives up on a hard problem | Pushes through the difficulty |
| **Force context re-read** | Agent is whack-a-moling | Tells it to step back and re-read the full function/file |
| **Redirect to plan** | Agent is drifting to unrelated work | Quotes the plan, shows where it diverged |
| **Demand evidence** | Agent claims verification without evidence | Asks for concrete proof (test output, grep results) |
| **Simplify** | Agent is overcomplicating | Questions unnecessary abstraction |

These are **strategy labels**, not literal text. TruPal generates rich, contextual nudges that quote specific code, errors, and context:

```
[TRUPAL] You've edited auth.py 4 times now and the error count 
hasn't decreased. The failing test is test_token_refresh (line 82) 
which expects session.valid == True after refresh. Step back — 
re-read the full refresh_token() function before the next edit. 
The root cause might be upstream of where you're patching.
```

The **strategy** (when to push vs challenge vs redirect) is learned. The **content** (which files, errors, line numbers to quote) is synthesized from live context (JSONL + git diff + build output). A nudge is: `strategy(trajectory_state) + content(live_context)`.

### Learned steering policy

Which nudge works best for which situation is **learnable**:

```
State:    agent stuck on same error 3 iterations
Action:   "step back" (re-read the full function)
Reward:   trust score improved from 0.5 → 0.9
Learned:  when stuck 3+ iterations → "step back" > "keep going"

State:    agent claims verification without evidence
Action:   "prove it" (show me the test output)
Reward:   trust score improved from 0.6 → 0.9
Learned:  for unverified claims → "prove it" > "really?"
```

This is a **steering policy** optimized over time:
```
policy:   (trajectory_state) → nudge_type
reward:   trust score after nudge
state:    agent's trajectory (edit count, error trend, drift, claims vs actions)
action:   which nudge to send
optimize: which nudge works best for which situation
```

The action space is the set of nudge types. The reward is the trust score. The state is the trajectory. It's RL on steering — the nudge vocabulary is the action space, trust score is the reward signal.

### Three levels of product evolution:

```
Level 1:  Passive watcher     → shows findings in sidecar pane
Level 2:  Active nudger       → sends findings + nudges to CC via send-keys
Level 3:  Learned steering    → optimizes which nudges work, adapts per project/model
```

Each level builds on the last. Level 1 is the MVP. Level 3 is the moat — a steering policy trained on real usage data that knows when to push, when to challenge, and when to stay silent.

### Trust score as reward signal

The trust score isn't just for the human — it's the reward signal for TruPal's own steering policy. TruPal hill-climbs on it:
- Try nudge A in situation X → trust 0.7
- Try nudge B in situation X → trust 0.9
- Learn: nudge B is better for situation X

Key tension: transparent score lets the agent game it. Resolution: **give CC the score but not the evaluation criteria.** CC knows it scored 0.7 but not why. Must improve genuinely rather than game the metric. TruPal knows the criteria (it runs the checks) but CC doesn't.

## Data Flywheel

The product gets better with usage:
```
More tasks → more trust data → better verification routing
→ lower cost per verification → more users → more tasks
```

Key data to collect per task:
```jsonl
{"task_id": "...", "agent": "claude-opus", "task_type": "refactor",
 "checks_run": ["tsc", "grep", "adversarial"], "checks_cost_s": [3, 1, 45],
 "issues_found": [{"check": "adversarial", "type": "missed-rename", "severity": "high"}],
 "escaped_bugs": 0, "trust_score": 0.87}
```

Escaped bugs (found later by humans) are the most valuable signal — they reveal holes in the verification pipeline.

## Multi-Agent Consensus = Blockchain Analogy

| Blockchain | TruPal |
|-----------|-------------------|
| Nodes validate transactions | Models verify each other's work |
| Different implementations (Bitcoin Core, btcd) | Different model families (Claude, GPT, Gemini) |
| 51% attack requires controlling majority | Gaming requires fooling multiple diverse models |
| Proof of work = expensive to fake | Adversarial review = expensive to pass with bad code |
| Merkle tree = verifiable history | Verification log = auditable trail |
| Consensus = agreement on state | Trust score = agreement on correctness |

The core insight: **multi-model consensus is the biggest trust lever**, just as multi-node consensus is blockchain's core mechanism. One model can hallucinate. Three different models hallucinating the same way is astronomically unlikely.

## Implementation Architecture (informed by Sidecar + Codex critique)

### Prior art: Sidecar by Haplab
[Sidecar](https://sidecar.haplab.com/) (Go, Bubble Tea TUI) proves the tmux-based pattern works in production. Key architectural note: Sidecar does NOT use side-by-side tmux panes. It runs CC in a **detached (invisible) tmux session**, captures its output via `capture-pane -p -e`, and **re-renders it inside its own Bubble Tea TUI** as a panel. Interactive mode forwards keystrokes back to the hidden session via `send-keys`. It's embed-a-mirror, not split-pane.

**For our MVP: raw tmux split-pane is fine.** The Sidecar approach (full TUI embedding CC's output) is the production/polished version for later.

Key lessons from Sidecar:

**Observation layer — two channels simultaneously:**
1. `tmux capture-pane -p -e -t <session> -S -600` — rendered terminal output, adaptive polling (200ms active, 500ms visible, 2s idle, 10-20s background)
2. CC's JSONL session files at `~/.claude/projects/` — structured conversation data (what CC was asked, what it claimed, what tools it called, what model it used). Watched via fsnotify, parsed incrementally from last byte offset.

Using BOTH is key. JSONL gives intent + structured data. Terminal capture gives real-time visual state. Neither alone is sufficient.

**Interaction layer — tmux send-keys:**
Sidecar uses `tmux send-keys -l` (literal mode) for keystroke injection. Production-validated. Handles edge cases: semicolons via hex encoding (`-H`), multi-line paste via `tmux load-buffer`, special keys by name.

**Status detection — hybrid approach:**
1. JSONL mtime check (fast path: modified within 30s = active)
2. JSONL tail parse (last message role: "user" = thinking, "assistant" = idle)
3. Terminal pattern matching (approval prompts, completion messages)
4. Sub-agent detection (checks `subagents/agent-*.jsonl`)

### Trust sidecar architecture

```
┌─────────────────────────────────────────────────────┐
│ tmux window                                         │
│ ┌───────────────────────┐ ┌───────────────────────┐ │
│ │ Claude Code (pane 1)  │ │ Trust sidecar (pane 2)│ │
│ │                       │ │                       │ │
│ │ user ↔ CC as normal   │ │ ✓ tsc: clean          │ │
│ │                       │ │ ⚠ deleted test file   │ │
│ │                       │ │ ⚠ no migration update │ │
│ │ ← send-keys (buddy)  │ │ trust: 0.87           │ │
│ └───────────────────────┘ └───────────────────────┘ │
│                                                     │
│ Observation:                                        │
│   CC JSONL files (fsnotify) ← intent + claims       │
│   git diff polling (3s)     ← what actually changed │
│   tmux capture-pane         ← visual state (backup) │
│                                                     │
│ Verification:                                       │
│   tsc --noEmit --incremental (on .ts change)        │
│   diff pattern analysis (any, deleted tests, etc.)  │
│   git diff vs JSONL claims (intent verification)    │
│                                                     │
│ Communication (bidirectional):                      │
│   TruPal → CC: tmux send-keys -l with guardrails     │
│   CC → TruPal: .claude/trust-responses.md            │
│   Shared:     .claude/trust-channel.jsonl           │
│   Display:    own pane (always, human sees all)      │
└─────────────────────────────────────────────────────┘
```

### Relationship model: peers, not advisor

TruPal is NOT an advisor to the human. TruPal is a **peer to CC** — a pair programmer that specializes in verification, process quality, and keeping CC honest. The human is the supervisor.

```
Human (supervisor)
  ↕ watches, intervenes when needed
TruPal ↔ CC (peers, nudging each other)
```

The default is: TruPal and CC work it out. The human watches, sips coffee, steps in on judgment calls.

### Bidirectional communication:

```
TruPal → CC:    tmux send-keys (direct injection, [TRUPAL] prefix)
CC → TruPal:    .claude/trust-responses.md (CC writes, trust reads via fsnotify)
```

What CC talking to trust enables:
- **Explain**: "I deleted that test intentionally — it tested deprecated API" → trupal stops flagging, marks waived
- **Disagree**: "That's not a bug, it's by design" → trupal marks disputed, human decides
- **Ask for help**: "Can you check if this function is called elsewhere?" → trupal runs analysis
- **Request verification**: "I think I'm done, verify?" → trupal checks on demand
- **Report confidence**: "70% sure this fixes it" → trupal factors into score
- **Acknowledge**: "Got it, fixing now" → finding lifecycle: delivered → acknowledged

CLAUDE.md rule for CC:
```
When you see a [TRUPAL] message, respond to findings by writing to 
.claude/trust-responses.md. Format: {"finding_id": "...", "action": 
"acknowledged|disputed|explained|fixed", "detail": "..."}.
Trust reads this file. The human can read it too — full observability.
```

The shared channel (`.claude/trust-channel.jsonl`) is their conversation log. Human reads it in trust's pane — full visibility into what trust found, what CC said, whether CC acted on it.

### Two modes:
```bash
trupal start                    # display-only, watches and shows findings
trupal start --buddy            # display + bidirectional communication with CC
```

### Send-keys guardrails (buddy mode):
```
Before injecting, check:
  1. Is CC at input prompt? (not mid-response, not mid-tool-call)
  2. Is CC showing a permission dialog? (never inject during [y/n])
  3. Is the user currently typing? (don't corrupt their input)
  4. Rate limit — max one injection per 30 seconds
  5. Is CC in slash-command mode, copy mode, or vim mode? (skip)
If ANY check fails → display only, don't inject. Finding is never lost.
```

### JSONL-powered intent verification (Layer 3):
CC's JSONL files tell us what CC was asked and what it claimed to do. Trust can compare claims vs reality:
- CC says "fixed the race condition" → trust checks: did the diff actually add locking?
- CC says "added error handling to all endpoints" → trust checks: how many endpoints changed vs total?
- CC says "tests pass" → trust checks: did CC actually run the test command?

This is the unique advantage over pure filesystem watching. JSONL gives us the conversation — the WHY behind the changes.

### Finding lifecycle (from Codex critique):
```
new → delivered → acknowledged → acted_on → resolved
                                          → waived (user dismissed)
```
Without lifecycle tracking, we can't know if findings were addressed. Display shows state. Buddy mode can re-inject unresolved findings.

### Codex critique (key takeaways):
1. **Output evidence, not scores.** "Build passes, tests deleted, touched auth path" > "trust: 0.82"
2. **Use CC hooks as primary event stream** for triggering checks (PostToolUse, Stop), not just polling
3. **Start with high-precision checks only** — tsc failures are zero false-positive. Earn credibility before expanding scope.
4. **Finding lifecycle is essential** — without acknowledgment tracking, trust is fire-and-forget
5. **Start as dashboard, evolve to active nudger** — display-only first, then buddy mode with steering

### Competitive landscape:
| Tool | Approach | Gap trust fills |
|------|----------|----------------|
| [TmuxAI](https://tmuxai.dev/) | Watches panes, proactive suggestions | No verification engine, no trust scoring |
| [TMAI](https://github.com/trust-delta/tmai) | Agent monitor + auto-review on completion | Review at end, not continuous |
| [Sidecar](https://sidecar.haplab.com/) | Dev dashboard, streams output, workspaces | No verification, no findings, no trust |
| [claude-review-loop](https://github.com/hamelsmu/claude-review-loop) | Stop-hook cross-model review | Blocking, only at completion |
| [adversarial-review](https://github.com/alecnielsen/adversarial-review) | Multi-round debate (Claude vs Codex) | Post-hoc, not real-time |
| [CodeRabbit](https://coderabbit.ai/) | PR review + inline feedback | PR-time, not dev-time |
| CC Monitor (ours) | Session status monitoring | No verification, no findings |

**TruPal's unique position:** real-time nudge engine that watches, verifies, steers, and learns. JSONL-powered intent verification (claim vs action gaps) + trust scoring + active steering with learned nudge policy + self-improvement loop. Nobody does continuous trust-scored verification AND active steering during development.

## Open Questions

1. **What's the minimum viable verification pipeline?** What 3 checks give 80% of the trust value?
2. **How to handle long-horizon trust?** Agent's change looks fine now, causes bug in 2 weeks. Feedback loop is too slow for real-time optimization.
3. **Can trust be transferable?** If Agent A is trusted on Python refactors, does that transfer to Go refactors? Probably not — trust should be contextual.
4. **Pricing model?** Per-verification? Per-seat? Open core (CLI free, dashboard paid)?
5. **How to bootstrap?** Need trust data to be useful, need users to get data. Start with power users (people who already do adversarial review manually)?
6. **Agent-as-evaluator alignment:** What if the evaluator model is also untrustworthy? Need evaluator diversity + human calibration.
7. **Text space optimization in practice:** Has anyone successfully OPRO'd agent system prompts for behavioral metrics (not just accuracy)? Literature gap?
8. **Trust composability:** When agents call sub-agents, how does trust propagate? Is trust of a pipeline = min(trust of each stage)?

## Next Steps

### Language: Go
Always-on sidecar on a RAM-constrained machine (16G, 9G swap). Needs: low memory (~5-10MB vs ~50MB+ for Python), natural concurrency (goroutines for watching + polling + checking + display), fast subprocess spawning (hundreds of tmux/git/tsc calls), long-running stability (hours/days, predictable memory), single binary distribution. Bubble Tea available for TUI upgrade later, but that's a bonus, not the reason.

### MVP — Build This First

**Goal:** Prove that a tmux sidecar catches real issues while working with CC. One day of dogfooding should answer: is this useful?

**Scope:** One binary. Two commands. Watch git. Run tsc. Flag patterns. Show in pane. Nothing else.

**Commands:**
```bash
trupal start [project-dir]    # split tmux pane to the right, start watching
trupal stop                   # kill watcher, close pane
```

**Architecture:**
```
main.go          # CLI entry point, tmux pane management
watcher.go       # git diff polling loop (every 3s)
checker.go       # run checks on detected changes
display.go       # color-coded output to stdout (the pane IS stdout)
```

**What `trupal start` does:**
1. `tmux split-window -h -l 40%` — creates a right pane for trupal
2. Starts the Go binary in that pane
3. Detects project root (walk up to find `.git/`)
4. Enters the watch loop

**Watch loop (every 3 seconds):**
```go
for {
    // 1. Check what changed
    changed := gitDiffFiles()      // git diff --name-only HEAD
    untracked := gitUntrackedFiles() // git ls-files --others --exclude-standard
    
    if noChanges(changed, untracked) {
        displayStatus("watching...")
        sleep(3s)
        continue
    }
    
    // 2. Show what changed
    displayChangedFiles(changed, untracked)
    
    // 3. Run checks
    if hasTypeScriptChanges(changed) {
        tscResult := runTsc()       // tsc --noEmit --incremental
        displayTscResult(tscResult) // green or red
    }
    
    diff := gitDiffContent()        // git diff HEAD
    findings := checkPatterns(diff)  // grep for patterns
    displayFindings(findings)
    
    sleep(3s)
}
```

**Three checks (and ONLY these three):**

1. **Type-check** — `tsc --noEmit --incremental` when any `.ts`/`.tsx` file changed.
   Zero false positives. If this fails, CC broke the build. Most valuable single check.

2. **Sloppiness grep** — scan `git diff HEAD` for:
   ```
   \bany\b                    # new "any" type
   @ts-ignore                 # type suppression
   eslint-disable             # lint suppression
   TODO|FIXME|HACK            # deferred work
   console\.log               # debug left in (non-test files only)
   ```
   Yellow warnings, not errors. Low false-positive for these patterns.

3. **Suspicious deletions** — scan `git diff --name-status HEAD` for:
   ```
   D.*test.*                  # deleted test file
   D.*spec.*                  # deleted spec file
   ```
   Red warning. Almost always wrong when CC does this.

**Display (just println with ANSI colors):**
```
─── TruPal ───────────────────────
watching ~/work/my-project

  changed: auth.py, token.py
  
  ✓ tsc: clean
  ⚠ new "any" type (auth.py:47)
  ⚠ TODO introduced (token.py:12)
  
  last check: 2s ago
──────────────────────────────────
```

No TUI framework. No Bubble Tea. Just `fmt.Printf` with ANSI escape codes. Clear screen and redraw on each cycle.

**What to skip in MVP (do NOT build):**
- ~~JSONL parsing~~ (V1)
- ~~Send-keys / buddy mode~~ (V1)
- ~~Bidirectional communication~~ (V1)
- ~~Trust score computation~~ (V1)
- ~~Finding lifecycle~~ (V1)
- ~~Plan drift detection~~ (V1)
- ~~Claim-action verification~~ (V1)
- ~~Learned steering~~ (V2)
- ~~trupal learn / report / log~~ (V2)
- ~~.claude/trust-findings.md output~~ (V1)
- ~~CC hooks integration~~ (V1)
- ~~fsnotify for JSONL~~ (V1)
- ~~Adaptive polling intervals~~ (V1)

**Success criteria:** Use trupal alongside CC for one full day. Did it catch at least one real issue you would have missed? If yes, everything else is worth building.

**Estimated size:** ~300 lines of Go. Single file is fine for MVP.

### V1 (buddy mode + steering)
- [ ] Send-keys injection with guardrails (prompt detection, rate limit)
- [ ] JSONL parsing for intent verification (claims vs actual changes)
- [ ] Finding lifecycle tracking (new → delivered → resolved)
- [ ] CC hooks integration (PostToolUse, Stop) for event-driven checks
- [ ] Active steering nudges (keep going, root cause, prove it, step back, stay on plan)
- [ ] Bidirectional communication (CC responds via .claude/trust-responses.md)
- [ ] Keep agents going — prevent premature stopping
- [ ] `trupal start --buddy`

### V2 (learned steering + self-improvement)
- [ ] Trust log (append-only, per-task scores + findings + nudges sent + outcomes)
- [ ] Learned steering policy: which nudge works best for which trajectory state
- [ ] `trupal learn` — mine patterns, propose CLAUDE.md rule changes
- [ ] Autoresearch-style ratchet: keep improvements, discard regressions
- [ ] Multi-model adversarial verification (call Codex/Gemini for review)

### Reference: CC JSONL parsing
[kieranklaassen/token-usage-analyzer](https://gist.github.com/kieranklaassen/7b2ebb39cbbb78cc2831497605d76cc6) — Python script that parses CC's JSONL session files. Key patterns to reference for TruPal V1:
- **Session files at** `~/.claude/projects/{encoded-path}/*.jsonl`
- **Subagent files at** `{session-id}/subagents/*.jsonl`
- **Message types:** `type: "user"|"assistant"`, content as string or list of blocks
- **Tool calls:** content blocks with `type: "tool_use"` (name, input) and `type: "tool_result"` (output, is_error)
- **Human vs tool messages:** filter by `isSidechain`, `userType`, and whether all content blocks are `tool_result`
- **Usage data:** `message.usage.{input_tokens, cache_creation_input_tokens, cache_read_input_tokens, output_tokens}`

TruPal reads the same files but incrementally (fsnotify + tail from last offset, like Sidecar) instead of batch. And analyzes claims vs tool calls instead of counting tokens.

### Research
- [ ] Study TmuxAI Watch Mode implementation in detail
- [ ] Study TMAI's 3-tier state detection architecture
- [ ] Study Sidecar's JSONL adapter code (Go) for CC session parsing
- [ ] Prototype trupal sidecar against CC on a real project, measure catch rate
- [ ] Write up as blog post / paper draft
