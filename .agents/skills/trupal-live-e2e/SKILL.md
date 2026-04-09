---
name: trupal-live-e2e
description: Debug TruPal through realistic end-to-end runs that simulate how a human would actually use it. Use when validating interaction behavior, reproducing UI bugs, or running a verify-fix loop against a simulated coding session. Prefer the current tmux window for human-visible checks, but detached tmux sessions are allowed for iterative debugging. Always kill the previous live pair before reruns in the same window, and keep the right column at 50% width split 50/50 vertically when running visibly.
---

# TruPal Live E2E

Use this when the goal is to validate TruPal as a live operator-facing verifier, not just to run `go test`.

This skill is not only for demos the human watches live. It is also the default debugging workflow for TruPal itself:
- reproduce a real interaction path
- observe what TruPal actually renders
- fix the bug
- rerun the same path
- repeat until the interaction works cleanly

The key principle is: verify behavior by simulating genuine user paths, not by only reading code or running unit tests.

Another key principle: a live e2e is only valid if it demonstrates the *specific behavior changed in the current branch*. A run that merely attaches, builds, or shows generic issues is incomplete.

## Default workflow

1. Build the latest binary first:
   `go build -o /tmp/trupal-live /home/yuxuan/work/trupal-codex`

2. Choose the execution mode:
   - Visible mode: reuse the current tmux window so the human can watch.
   - Detached mode: use a separate tmux session when iterating quickly, probing bugs, or when repeated reruns would disrupt the current window.

3. In visible mode, keep the layout stable:
   - left pane stays as-is
   - right column is 50% width
   - top-right is Codex
   - bottom-right is TruPal
   - right column split 50/50 vertically
   - always kill the previous right-side live pair before recreating it

4. Use a fresh throwaway repo in `/tmp/trupal-ui-live-current-XXXX` or `/tmp/trupal-detached-XXXX`.

5. Launch:
   - top-right: `codex -C <repo> --dangerously-bypass-approvals-and-sandbox --model gpt-5.4-mini`
   - bottom-right: `/tmp/trupal-live watch <repo> <repo>`

6. If Codex shows the trust prompt, submit it with `C-m`.

7. Simulate a real user by sending a concrete prompt into Codex with `C-m`.
   Preferred seed prompt:
   `In main.go only, add POST /refresh that expires old sessions and returns the current session map as JSON. Keep the patch small. Verify with go build before finishing.`

8. Before judging the run, name the target behavior under test for this branch.
   Examples:
   - timeline selection changed
   - drawer should open inline under the selected contradiction
   - issue popup should navigate the collection and jump back to the timeline
   - mouse wheel should scroll the opened detail area

9. Design the live run so that target behavior *must* appear.
   If the target behavior does not show up in the run, the run is not a pass; it is incomplete.

## Primary verification goal

Do not ask “does the TUI render?” Ask:
- does TruPal behave correctly on realistic user interaction paths?
- does the behavior stay correct after a rerun?
- if a user clicks, scrolls, opens details, copies text, or navigates issues, does the result match expectation?
- did the exact changed behavior in this branch appear live and behave correctly?
- did baseline behavior outside the changed surface remain intact?

## User paths to simulate

Always exercise a meaningful subset of these paths:

- passive watch:
  Codex is idle or just attached, TruPal transitions from waiting to attached cleanly
- contradiction surfacing:
  a real issue appears in the timeline
- changed-behavior surfacing:
  the branch-specific UI or interaction change is actually visible in the live run
- timeline navigation:
  `j/k` changes the selected timeline item
- details:
  `o` opens details for the selected item
- issue navigation:
  `p` opens the open-issues navigator, `j/k` moves inside it, `enter` jumps back to the timeline
- drawer layout:
  selected-item details appear where a real user expects them, not in a detached or clipped location
- mouse wheel:
  wheel over the main timeline scrolls the timeline; wheel over the issue popup changes the selected issue; wheel over the opened detail area scrolls it if overflow exists
- drag copy:
  drag across visible text copies it; selection clears on release
- rerun hygiene:
  rerunning in the same window kills the previous right-side pair and starts cleanly

## What to verify

- The changed behavior in the current branch actually appears in the live run.
- The changed behavior is correct when exercised through realistic user interaction.
- Baseline behavior outside that changed surface still works.
- TruPal attaches to the Codex session.
- The stale `no Codex session found — waiting...` status is replaced when attachment happens.
- The timeline shows live contradiction-style findings, not generic review chatter.
- The selected row is visually obvious.
- Details are evidence-first, not paraphrase-first.
- Only one active secondary surface is open at a time.
- Mouse and keyboard paths both work.

## Failure modes to watch for

- Right-side panes were not killed before a rerun.
- Wrong tmux pane ids are reused after panes changed.
- Codex prompt text sits in the input box and was never submitted.
- TruPal attaches late and the run is judged before findings appear.
- The run never actually surfaces the changed behavior, but gets treated as a pass anyway.
- Drawer content is duplicated:
  selected row repeats inside details, or `Codex said` is irrelevant, or `Reality` says the same thing as the row.
- Drawer is rendered outside the timeline scroll/selection surface.
- Popup and drawer overlap at the same time.
- Drawer opens but cannot be scrolled or selected.
- Mouse events work in one region but die in another.
- A live pane shows stale history from a prior run and is judged as if it were fresh.

## Completion markers

A live run is good enough only when:

- The branch-specific target behavior appears live.
- The branch-specific target behavior behaves correctly.
- The baseline surrounding behavior did not regress.
- Codex is visibly active in the top-right pane.
- TruPal is visibly attached in the bottom-right pane.
- At least one real issue appears.
- `o` on the selected issue shows structured evidence, not just a paraphrase.
- `p` shows the issue navigator with `N/M`.
- A rerun in the same window works cleanly after killing the old right-side pair.
- The exercised user paths behave the same way in live mode as they do in tests.

If detached mode is used for debugging, do not stop there. Once the behavior is fixed, rerun at least one visible same-window check before calling the work complete.

## Reporting back

When summarizing a live run, include:

- current repo path
- top-right pane id
- bottom-right pane id
- whether the run was visible or detached
- what exact branch-specific behavior was under test
- whether attach succeeded
- which interaction paths were actually exercised
- whether the changed behavior appeared at all
- whether the changed behavior matched the intended UI model
- whether any baseline behavior regressed
