# TruPal Benchmark Report

## Scenario

- ID: `buggy-crud`
- Name: CRUD API with race conditions
- Category: `concurrency`
- Started: `2026-04-07 16:52:46 PDT`
- Duration: `1m30.730624639s`
- Claude model: `haiku`
- Claude exit code: `0`

## Metrics

- Detection rate: `0.0%` (0/8)
- False positives: `0`
- Trap hits: `0`
- Brain responses: `1`
- Token usage: in=`18` out=`4262` cache-read=`45655` cache-create=`49121`
- Estimated cost: `$0.0873`

## Matches

- None

## Missed Truths

- `race-users` in `main.go`: global users slice accessed without mutex
- `race-nextid` in `main.go`: nextID incremented without lock
- `missing-405` in `main.go`: handlers do not return 405 for unsupported methods
- `swallowed-json-errors` in `main.go`: json encode or decode errors are ignored
- `unbounded-rate-limit-map` in `main.go`: rate limiter map grows forever without cleanup
- `stale-cache` in `main.go`: cache is not invalidated on update or delete
- `route-parsing-bug` in `main.go`: manual path parsing accepts invalid user routes
- `auth-ordering` in `main.go`: auth middleware does not consistently protect all CRUD endpoints

## Extra Findings

- None

## Artifacts

- Report: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/report.md`
- Pane capture: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/pane.txt`
- Debug log: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/trupal.debug`
- TruPal log: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/trupal.log`
- Claude stdout: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/claude.stdout.log`
- Claude stderr: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/claude.stderr.log`
- Session JSONL: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/session.jsonl`
- Final project copy: `/home/yuxuan/work/trupal/bench/results/20260407-165246-buggy-crud/project`
