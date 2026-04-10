# Implementation Plan: swe-bench-small

Date: 2026-04-10

## Naming

Use the name **`swe-bench-small`** for TruPal's small real-task benchmark slice.
Do not use `swe-bench-lite`, since that name is already used in the official SWE-bench ecosystem.

## Purpose

`swe-bench-small` is the **small real-task lane** for regular benchmarking of TruPal steering.
It sits between:
- the local synthetic 5-scenario suite
- larger pilot / nightly real-task slices

## Size

Recommended size:
- **3-5 real SWE-bench tasks**

This is intentionally small enough for:
- regular regression checks
- repeated manual runs during benchmark development
- fast steering iteration with some real-task grounding

## Source of Tasks

### Preferred source
- **SWE-bench Pro**

Reason:
- better current benchmark quality / ground truth than SWE-bench Verified
- less risk of contamination / flawed tests relative to Verified

### Short-term bridge
- SWE-bench Verified is acceptable while we are still building runner compatibility and environment support.
- But the long-term target for `swe-bench-small` should be **Pro-backed tasks**, not Verified-backed tasks.

## Recommended Lane Structure

### Local suite
- 5 synthetic TruPal scenarios
- very fast regression lane

### swe-bench-small
- 3-5 real tasks
- regular real-task lane
- ideal for weekly / pre-merge benchmark checks when time allows

### SWE-bench pilot/nightly slice
- 10-20 real tasks
- slower but broader real-task lane
- not the full benchmark universe; still just a representative slice

## Selection Criteria for swe-bench-small

Pick 3-5 tasks that are:
- reproducible on the current machine
- not extremely heavy to set up
- rich in steering opportunities
- diverse by failure mode

Prioritize tasks involving:
1. verification drift
2. runtime/API contract mistakes
3. stale/shared state bugs
4. parsing/validation bugs
5. shortcut / suppression behavior

Avoid initially:
- flaky tasks
- multi-service orchestration monsters
- repos requiring too much bespoke environment work

## Current Candidate Principles

Until Pro integration is ready, use Verified only as a temporary bridge and pick tasks that:
- already have a working setup command in our manifests
- can be evaluated with a narrow test selection
- expose a plausible steering moment

## Success Criteria

`swe-bench-small` is ready when:
- the name is used consistently in scripts/docs/plans
- there is a documented 3-5 task slice
- each task has a manifest with setup/eval metadata
- the slice can be run end-to-end from the repo
- results are reported in one aggregate markdown summary

## Recommended Next Action

Rename the current language from generic `lite/full` where needed:
- keep `run-lite` for the local synthetic suite
- introduce `swe-bench-small` terminology for the 3-5 real-task lane
- keep 10-20 tasks labeled as `pilot` or `nightly`, not `full`
