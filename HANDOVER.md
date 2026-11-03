# Consul reliability fixes — 2026-11-03

## Scope and authorization
- User authorized fixing two defects reproduced with local Docker Consul: shared RoundRobin invoker race and GetHealthChecks treating instance ID as service name/hiding critical checks.
- Baseline HEAD: `04779cec8625598ded8cb251087b9888d6c39d22`.
- User subsequently authorized commit, push, tag and GitHub Release, selecting `v0.3.1`. Target deployment and production access remain unauthorized/not performed.
- Task/design: `.pi/tasks/consul-reliability-fix.md`.

## Candidate changes
- `pkg/consul/invoke.go`: atomic unsigned round-robin counter, first-instance-first ordering preserved; network calls outside synchronization.
- `pkg/consul/health.go`: query connected local agent checks, exact ServiceID filtering, all statuses, deterministic CheckID ordering. Does not query remote agents or include node-only checks.
- `pkg/consul/reliability_test.go`: HTTP-backed concurrent/sequential round-robin and health query regression tests.
- `pkg/consul/integration_test.go`: opt-in real Consul tests using `CONSUL_INTEGRATION_ADDR`; skipped by default.
- `README.md`: instance-ID/local-agent semantics and round-robin concurrency safety.

## Pi independent verification
Go 1.27.3, Docker Consul 1.21.5 at loopback 127.0.0.1:18500.
- Full tests, race tests and `go vet ./...`: passed.
- Real Consul integration tests with race detector, repeated 5 times: passed.
- RoundRobin/GetHealthChecks regression tests with race detector, repeated 20 times: passed.
- Full suite with real integration endpoint and race detector: passed.
- Isolated copied candidate package built as Go archive; separate consumer exercised public APIs with real Consul: exact passing/critical instance checks and 400 concurrent requests through one default invoker passed under race detector.
- Isolated archive SHA256: `8a42afeaf195c30ee342c255bfcd53a72e2ed13a1406833631f51014c391727d`.
- `go build ./...`: passed.
- Default suite statement coverage: 37.5% (previously 21.8%); not full-component coverage.
- Test agent services empty after test cleanup. Log review: initial single-node Raft election warning; no ERROR logs observed. DEBUG content-type notices from SDK requests do not indicate test failures.
- Independent fresh Claude Code review completed: no blockers. SDK field mapping independently checked by Pi against consul/api v1.35.0 source; AgentCheck has no ServiceTags field. README now documents ACL filtering, absent catalog metadata and opt-in integration command.
- Pi read the full implementation diff and both new test files. Scoped local candidate acceptance passed; this is not whole-component/production certification.
- Low-risk follow-ups from review: integration test exact hit split assumes stable service ordering; counter wraparound and changing membership lack explicit tests. Mock tests use fixed ordering; all actual integration repetitions passed.

## Limits and follow-up
- Only single-node dev Consul 1.21.5 validated; no cluster HA, ACL/TLS, long-duration load or other server versions verified.
- Existing unrelated limitations remain: most Consul operations lack client deadlines; Watch config sharing and LeastConn implementation were not changed.
- Callers previously supplying service name to GetHealthChecks must use registered instance ID. ACL deployments need permission to read agent checks.
- Agent checks do not supply catalog metadata (ServiceTags/PeerName/CreateIndex/ModifyIndex), so these api.HealthCheck fields remain zero-valued; node-level checks are intentionally excluded.
- Test container and anonymous volumes removed; temporary executor evidence/candidate files cleaned after recording review and session metadata.

## Executor sessions
- Code: Claude Code, requested `sonnet`, response-proven model `claude-sonnet-5-5`, session `312bbe2d-4833-46c8-be77-ef6c9eb53ee0`, status success.
- Independent review: fresh Claude Code session `361fd5f3-5604-4acc-89ee-eb1d65084b92`, requested `sonnet`, response-proven model `claude-sonnet-5-5`, status success.
- Both sessions correspond to the task and baseline above; no resume required.
- Delivery version: `v0.3.1`, approved for commit to `main`, push to `origin`, annotated tag and GitHub Release. At this pre-commit snapshot delivery is pending; use actual Git refs and GitHub Release to verify completion, not this document alone. Not deployed.
- Final pre-release check: origin/main matches baseline, no existing v0.3.1 tag; full race suite and go vet passed again.
- Next session: “继续 Consul v0.3.1，先看 HANDOVER.md，并核实 Git 和 GitHub Release 状态”。
