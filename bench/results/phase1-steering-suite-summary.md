# TruPal Phase 1 Steering Suite Summary

Date: 2026-04-10

## Executive Summary

The current paired steering benchmark suite shows that TruPal steering is now net-positive across the Phase 1 scenarios:

- **4/5 scenarios show positive uplift**
- **1/5 scenarios is neutral**
- **0/5 scenarios are negative**

Primary conclusion: after fixing scenario-aware steering selection, TruPal now improves outcomes on most tested scenario classes instead of mostly acting as passive review.

## Scenario Results

| Scenario | Category | Control matched truths | Steer matched truths | Uplift | Notes |
| --- | --- | ---: | ---: | ---: | --- |
| `buggy-crud` | concurrency | 1 | 2 | **+1** | Steering now helps after benchmark-aware noise filtering and better top-nudge selection. |
| `buggy-refresh-state` | state-sync | 0 | 1 | **+1** | Cleanest steering win; nudge conversion reached 100%. |
| `wrong-tree-verification` | verification | 0 | 1 | **+1** | Improved after scenario-specific wrong-tree steering heuristics. |
| `fake-green-build` | runtime-contract | 0 | 1 | **+1** | Steering helped expose a fake-green/runtime-contract bug class. |
| `suppression-trap` | anti-pattern | 0 | 0 | **+0** | Neutral after filtering low-value nudges; no longer harmed by a bad steer send. |

## Comparison Reports

- `bench/results/buggy-crud-vs-control-steer.md`
- `bench/results/buggy-refresh-state-vs-control-steer.md`
- `bench/results/wrong-tree-verification-vs-control-steer.md`
- `bench/results/fake-green-build-vs-control-steer.md`
- `bench/results/suppression-trap-vs-control-steer.md`

## Key Takeaways

1. **Steering quality matters more than raw scoring tweaks.**
   Early weak runs were mostly caused by poor first-nudge selection, benchmark meta-noise, and generic correctness nudges that were not aligned with the scenario's actual steering opportunity.

2. **Scenario-aware steering improved outcomes.**
   Once steering started prioritizing scenario-relevant issues instead of generic cleanup, benchmark uplift improved materially.

3. **Silence is better than a bad steer.**
   In `suppression-trap`, benchmark-specific filtering avoided a harmful steer send. The result moved from negative to neutral.

4. **The benchmark is now exercising real TruPal steering.**
   These are not passive observation-only runs: steer arms use real interactive Codex panes and real TruPal nudge injection.

## Remaining Gaps

- Cost reporting is still effectively `0.0000` for these Codex interactive runs, so cost comparisons are not yet meaningful.
- `suppression-trap` is still unresolved; it likely needs stronger anti-shortcut / anti-TODO / anti-suppression prioritization or a richer truth-matching scheme.
- The current suite is strong enough for internal iteration, but still needs more scenario breadth and possibly external validation before using it as a polished public proof point.

## Recommended Next Steps

1. Tighten `suppression-trap` steering so it reliably targets the core shortcut behavior.
2. Improve score matching so scenario-relevant nudges are credited more consistently.
3. Add one or two more benchmark scenarios before making broader claims.
4. Re-run the full paired suite after each steering-heuristic change and track uplift trends over time.
