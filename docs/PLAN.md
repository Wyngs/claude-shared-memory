# Implementation plan

## Problem and success criterion

Independent Claude sessions on different laptops do not know what another session has investigated, claimed, ruled out, or discovered. During a complex debugging investigation, this can duplicate work and leave decisions based on stale context. A shared coordination service lets naturally separable investigations proceed with visible ownership and evidence.

Success is two sessions debugging one issue: they choose different investigation scopes, see each other's findings, avoid overlapping claims, and recover work if an owner disappears. Faster resolution is a hypothesis to measure, not a performance claim.

## Milestone 1: working coordination slice

- Go service and PostgreSQL durability.
- Workspace credentials and independently authenticated agent sessions.
- Workspace context, explicit findings and evidence, presence heartbeats.
- Task ownership with expiring leases and fresh ownership tokens on reclaim.
- Cooperative resource leases with owner-checked renew and release.
- Ordered durable events, paginated by cursor for disconnected clients.
- Node stdio MCP bridge with one agent per Claude process.
- Docker Compose, CI, two-session demo, race and integration tests.

This is the implemented first milestone. Task creation and claiming are separate. Creation announces a scope; a successful claim grants ownership. Finding publication and task creation are not idempotent, so an ambiguous network failure requires checking context before retrying.

## Milestone 2: event delivery and bounded storage

Normalize agents, tasks, findings, leases and events into individual PostgreSQL tables; migrate existing state. Add transactional outbox delivery into Redis Streams, long-lived event subscribers, replay and retention. PostgreSQL remains authoritative. Avoid a database-write / Redis-publish dual-write gap.

## Milestone 3: stronger collaboration

Add hypotheses, decisions, task dependencies, finding amendments, human review and context selection. Add optional Claude Code hooks that publish selected summaries with explicit opt-in, secret filtering and deduplication. A web view can make active scopes and evidence visible to humans.

## Milestone 4: hosted team service

Add user identity, invitations, token revocation, roles, rate limits, quotas, audit export, backups, TLS and deployment configuration. Evaluate official remote MCP transport for clients that cannot launch a local stdio process.

## Validation

1. Run two sessions against one workspace; claim contention has one winner.
2. Reclaim after expiry; the old ownership token cannot complete or renew work.
3. Findings and completion appear in another session's context.
4. Restart the coordinator; PostgreSQL preserves context and event cursors.
5. Prevent agent impersonation and credential disclosure through context.
6. Measure duplicate investigation minutes, elapsed resolution time and unsupported conclusions against a single-session baseline on naturally decomposable issues.

## Explicit limits

No automatic transcript capture or remote control of Claude. Sessions must consult context and publish updates through tools. Resource leases cannot prevent a collaborator from writing directly to Git, a filesystem or an external service. Lease ownership tokens are checked by this coordinator; downstream systems must implement their own fencing for actual write enforcement.

Redis and the dashboard are planned, not implemented. The first schema stores one JSON document per workspace and locks it during transactions. This is simple and safe for small debugging teams, but rewrites state and serializes all calls in a workspace. Unbounded history and context are unsuitable for long-running or high-volume production use; bounded storage is a prerequisite for that use.
