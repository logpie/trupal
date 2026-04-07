# TruPal Benchmark Report

## Scenario

- ID: `buggy-crud`
- Name: CRUD API with race conditions
- Category: `concurrency`
- Started: `2026-04-07 16:57:48 PDT`
- Duration: `1m8.005901189s`
- Claude model: `haiku`
- Claude exit code: `0`

## Metrics

- Detection rate: `12.5%` (1/8)
- False positives: `4`
- Trap hits: `0`
- Brain responses: `1`
- Matched latency avg/max: `29.691s` / `29.691s`
- Token usage: in=`18` out=`2138` cache-read=`45657` cache-create=`48782`
- Estimated cost: `$0.0763`

## Matches

- `unbounded-rate-limit-map` matched `Your rate limit map has no cleanup—expired entries pile up in `s.rateLimit` forever. After a minute, the entry should be evicted, but you're just letting it sit. Either delete expired entries when you detect them, or use a time-based cleanup goroutine.` (latency `29.691s`)

## Missed Truths

- `race-users` in `main.go`: global users slice accessed without mutex
- `race-nextid` in `main.go`: nextID incremented without lock
- `missing-405` in `main.go`: handlers do not return 405 for unsupported methods
- `swallowed-json-errors` in `main.go`: json encode or decode errors are ignored
- `stale-cache` in `main.go`: cache is not invalidated on update or delete
- `route-parsing-bug` in `main.go`: manual path parsing accepts invalid user routes
- `auth-ordering` in `main.go`: auth middleware does not consistently protect all CRUD endpoints

## Extra Findings

- `Hey, you're not checking the error from json.NewEncoder(w).Encode() calls. Hit lines 112, 139, 154, 173, 211. If encoding fails (e.g., circular reference, invalid type), the response body is incomplete and the caller gets no error signal. Check the error and log/report it.` at `16:58:56`
- `Tell me what the actual build error is. Run `go build main.go` and paste the output—the code looks structurally sound but something's failing. Are you getting an import error, a method signature mismatch, or something else?` at `16:58:56`
- `Build is failing with 1 error but the code structure appears syntactically valid—need to see the actual error message to diagnose.` at `16:58:56`
- `JSON encoding errors are silently swallowed across all handler methods (GetUsers, CreateUser, GetUser, UpdateUser)—clients get incomplete responses if serialization fails.` at `16:58:56`

## Artifacts

- Report: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/report.md`
- Pane capture: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/pane.txt`
- Debug log: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/trupal.debug`
- TruPal log: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/trupal.log`
- Claude stdout: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/claude.stdout.log`
- Claude stderr: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/claude.stderr.log`
- Session JSONL: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/session.jsonl`
- Final project copy: `/home/yuxuan/work/trupal/bench/results/20260407-165748-buggy-crud/project`
