# Consul reliability fixes

Authorized: user requested repair of the two issues reproduced against local Docker Consul. No commit/push/deployment authorization.
Baseline: 04779cec8625598ded8cb251087b9888d6c39d22; initially clean working tree.
Executor: Claude Code, model sonnet. Risk: medium, concurrent invocation and health query semantics.

## Scope and design
- pkg/consul/invoke.go: protect RoundRobin selection counter against concurrent shared-invoker calls. Prefer atomic unsigned counter; prevent signed overflow/index panics and preserve first-instance-first ordering. Do not hold locks across network operations or change retry/other strategy behavior.
- pkg/consul/health.go: GetHealthChecks(serviceID) must query agent checks and filter exact ServiceID, returning all statuses (passing/warning/critical), not query passing services by name. This matches RegisterService's local agent scope. Keep empty-ID error and error prefix. Unknown ID returns empty results. Document local-agent scope and instance-ID semantics.
- Add automated HTTP-backed regression tests for concurrent shared default invoker, sequential round robin behavior, health checks by exact instance ID with different service name, multiple statuses and unrelated instances, empty ID, unknown ID and agent errors. Run race detector.
- Add reproducible opt-in real Consul integration tests if practical, gated by explicit environment variable; never connect external services by default. Cover fixed regressions against local 127.0.0.1:18500; use unique service IDs, cleanup, and bounded waits.
- Update README relevant semantics; do not expand unrelated feature scope.

## Verification
- go test -count=1 ./...
- go test -race -count=1 ./...
- go vet ./...
- Pi independently reviews full diff and reruns tests, including race repetitions and local Docker Consul integration.
- Independent fresh Claude Code review of concurrency and agent-local exact-ID health query contract.
- Build current candidate library archive into isolated temporary module; invoke public API against real Docker Consul, including passing/critical TTL checks and 400 concurrent calls through one default invoker.

## Boundaries
Only edit relevant implementation/tests/README. No dependency or Go version changes. No commit, push, production access, SSH, Docker manipulation, or global software install. Pi manages local Docker validation. Do not touch unrelated files. Report any failures without fallback.
