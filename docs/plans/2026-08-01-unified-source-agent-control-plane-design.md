# Unified Source Agent Control Plane Design

**Status:** Approved

**Date:** 2026-08-01

**Decision:** Evolve the existing KBase source-agent control plane into one
operator-controlled management layer. WeChat and WC Plus remain independent
workers with isolated state and failure domains. The protocol is
cross-platform, while the first implementation targets macOS.

## Context

KBase already has most of the reliable collection substrate: agent
heartbeats, capability health, task leases, subscriptions, scheduling, run and
item history, idempotent ingestion, durable local outboxes, cursors, retry, and
recovery. These capabilities are split across source-specific pages and worker
entry points, so operators do not have one trustworthy view of worker state,
manual control, diagnostics, or software version.

The next product sequence is:

1. close source collection through a unified Agent management layer;
2. improve knowledge quality;
3. improve everyday product experience.

This design covers the first item. It consolidates the existing substrate
rather than replacing it.

## Goals

- Provide one `/sources/agents` control surface for all source workers.
- Keep WeChat and WC Plus in independent processes with independent state,
  outbox, logs, and failure domains.
- Support explicit operator actions: run, pause, resume, cancel, retry,
  diagnose, and constrained software upgrade.
- Keep all worker communication outbound-only over authenticated HTTPS.
- Preserve local credential boundaries: WeChat credentials stay in the local
  platform secret store, and WC Plus stays behind a loopback-only API.
- Define platform-neutral worker, capability, command, and upgrade contracts.
- Deliver the first updater, installer integration, and process supervision on
  macOS while leaving clean Windows and Linux extension points.
- Deliver WeChat and WC Plus worker integration in the same feature batch with
  layered production acceptance.

## Non-Goals

- Arbitrary remote commands, scripts, environment variables, or download URLs.
- A remote terminal or generic process-management API.
- Inbound connections from KBase to an operator machine.
- Moving source credentials, cookies, QR payloads, or downloaded source bodies
  into KBase.
- Automatic scheduling in the first production rollout.
- Replacing the existing source sync, lease, subscription, run, outbox, cursor,
  or ingestion implementation.
- Shipping Windows or Linux installers in the first version.
- Reimplementing or bypassing WC Plus licensing, host security, or vendor
  restrictions.
- Reintroducing an external artifact-signing workflow.

## Architecture

```text
/sources/agents Web control surface
  -> KBase Source Control Plane
     -> Worker Registry
     -> Source subscriptions and runs
     -> constrained Command Queue
     -> private Artifact Catalog
     -> bounded operation audit

Independent WeChat Worker -- outbound HTTPS --> KBase
  -> local platform secret store
  -> WeChat MP session and public article adapter
  -> private state, cursor, and outbox

Independent WC Plus Worker -- outbound HTTPS --> KBase
  -> loopback-only WC Plus API
  -> private state, cursor, and outbox
```

The implementation extends `SourceSyncStore`, the existing heartbeat and lease
protocol, and existing administrative APIs. It does not create a second source
collection system.

### KBase control plane

KBase owns:

- the latest worker registration projection;
- desired worker state;
- capability health and bounded diagnostics;
- subscriptions, manual runs, item failures, and run history;
- constrained commands and their state transitions;
- private update-artifact metadata and downloads;
- an operator-visible audit trail.

KBase never owns local source credentials or a generic execution channel.

### Independent workers

Each worker has one stable operator-assigned `agent_id` and one worker type.
It owns its local adapter, state database, outbox, logs, and platform
integration. One worker failing or upgrading must not stop the other worker.

The first version continues to use the shared `KBASE_SOURCE_AGENT_TOKEN`.
Consequently, `agent_id` is an operational identifier, not a cryptographically
trusted device identity. A token leak requires rotating every source worker,
and KBase must not treat self-reported device identity as non-forgeable audit
evidence. The protocol may add per-worker credentials later without changing
source task contracts.

## Worker and Capability State

Each worker has a stored desired state and a derived observed state.

Desired state:

- `active`: eligible to lease compatible work;
- `paused`: ineligible for new work.

Observed state:

- `online`: recent heartbeat and all required capabilities healthy;
- `degraded`: recent heartbeat with a non-blocking capability failure;
- `requires_action`: recent heartbeat with an operator-remediable blocker;
- `offline`: heartbeat outside the bounded freshness window;
- `upgrading`: one active upgrade command owns the worker lifecycle.

Capabilities use stable codes rather than UI parsing of free text. Initial
codes include:

- `login_required`;
- `vendor_blocked`;
- `dependency_unavailable`;
- `config_invalid`;
- `upgrade_required`;
- `throttled`.

Existing `healthy`, `last_error`, and `requires_action` heartbeat fields remain
readable during migration. New fields are additive, bounded, and normalized.
Older workers appear as legacy workers requiring upgrade rather than becoming
unavailable immediately.

Heartbeats add platform-neutral runtime metadata and bounded statistics:

- worker type, platform, architecture, binary version, and protocol version;
- current run and current command identifiers;
- pending outbox and dead-letter counts;
- last successful synchronization time;
- normalized capability state and stable diagnostic code.

They never include tokens, source bodies, cookies, QR data, arbitrary local
paths, or complete upstream responses.

## Operator Actions

### Source work

- **Run:** create a run from an existing subscription. A compatible active
  worker leases it through the current lease protocol.
- **Pause:** stop issuing new leases to the worker. An already-running task is
  allowed to reach a safe checkpoint.
- **Cancel:** cancel one run. The worker stops at a safe checkpoint and keeps
  its cursor and outbox truth.
- **Retry:** retry only failed or canceled terminal work using the original
  subscription and bounded operation parameters.
- **Resume:** make the worker eligible for new leases. It does not implicitly
  start historical work.

New subscriptions default to `manual`. Scheduled intervals remain represented
by the existing contract but cannot be enabled as part of the first rollout.
A later activation Gate may enable one subscription at a time.

### Diagnostics

`diagnose` is a fixed command type. Workers run only built-in checks and return
structured component state, stable codes, timestamps, and bounded operator
guidance. They do not return full logs, environment dumps, source bodies,
tokens, or arbitrary file paths.

## Constrained Command Queue

The durable command store accepts an explicit enum. The first protocol only
needs `diagnose` and `upgrade`; pause and resume are desired-state changes, and
source synchronization continues to use source runs.

Commands have one target worker, creation and expiry times, expected current
version where applicable, an idempotency identity, and a bounded state history.
The server rejects unknown command types, unknown fields, stale targets,
duplicate active upgrades, invalid transitions, expired commands, and version
conflicts.

Upgrade states are:

```text
queued -> claimed -> downloading -> verified -> installing
       -> restarting -> verifying -> succeeded
                              `-> rollback -> rolled_back

Any applicable pre-terminal state may become failed, expired, or canceled.
```

Submitting a command is not success. The UI reports the last durable worker
acknowledgement and actual observed version.

## Constrained Upgrade Protocol

### Artifact Catalog

An update artifact must be registered in KBase before it can be selected. Its
record contains:

- worker type;
- target platform and architecture;
- exact source revision;
- binary and protocol versions;
- byte size and SHA-256;
- minimum supported source version;
- build-gate result;
- an explicit allowed-for-rollout flag.

The Web UI cannot submit a download URL. It can only select a compatible,
approved catalog item. The design intentionally uses exact revision and
SHA-256 verification without an external signing service.

Creating an upgrade command requires an authenticated browser management
session, CSRF validation, and explicit confirmation. Ordinary machine Bearer
clients cannot create upgrade commands. The shared worker token may only claim
worker commands, download the referenced compatible artifact, and report
bounded progress.

The remote payload contains only a command ID, artifact ID, expected current
version, and expiry. It cannot contain a shell command, script, environment
variable, installation path, or arbitrary URL.

### macOS updater

The first platform implementation installs a fixed-function, user-level
updater beside each worker. The updater:

1. accepts only a known worker type and command identity;
2. verifies platform, architecture, version compatibility, size, and SHA-256;
3. stages and backs up the current binary on the same filesystem;
4. atomically replaces the worker binary;
5. asks LaunchAgent to restart the worker;
6. waits for a local ready receipt containing the expected command and version;
7. confirms only after the new worker loads configuration and state and sends
   one authenticated heartbeat;
8. restores the old binary and restarts it when verification fails or expires.

The updater cannot execute arbitrary commands. Its first installation remains
a local, explicit installer action so remote control cannot bootstrap a new
execution surface by itself.

Download failure leaves the current worker running. Hash mismatch quarantines
the staging file. An unexpected current version rejects the command. A worker
does not lease source work while upgrading. Token rotation is not an upgrade
operation.

## Web Product Surface

### `/sources/agents`

The unified overview presents online, attention, offline, paused, and upgrading
groups derived from server state. Each row shows:

- agent ID and worker type;
- platform, architecture, binary version, and protocol version;
- desired and observed state;
- capability health;
- last heartbeat and last successful synchronization;
- current run or command;
- outbox and dead-letter counts;
- stable error code and concise next action.

Available actions are pause, resume, diagnose, select an approved update,
inspect runs, retry eligible failures, and open the source-specific workspace.
There is no remote terminal, custom command, custom artifact URL, or bulk force
upgrade.

### Worker details

`/sources/agents/{agent_id}` is a stable deep link. It shows worker metadata,
capabilities, bound subscriptions, recent runs and item failures, command and
upgrade history, outbox statistics, and redacted diagnostics. It links to the
WeChat or WC Plus workspace for source-specific operations.

WeChat login and WC Plus local configuration stay on the operator machine.
The online UI never asks for or stores those credentials.

## HTTP API Evolution

Existing subscription, run, retry, cancel, lease, article upload, asset upload,
and run-completion routes retain their contracts.

The administrative surface evolves additively:

- `GET /api/source-agents` returns the unified list projection;
- `GET /api/source-agents/{agent_id}` returns one bounded detail projection;
- `POST /api/source-agents/{agent_id}/desired-state` pauses or resumes;
- `POST /api/source-agents/{agent_id}/commands` accepts only supported command
  types;
- `GET /api/source-agents/{agent_id}/commands` returns bounded command history;
- worker-scoped `/api/source-agent/*` routes claim and report commands and
  download the one referenced compatible artifact;
- private catalog reads expose only artifact metadata needed by the management
  surface.

All new fields and routes must be generated into the system map from code.

## Error Handling and Privacy

- API failures have stable error codes and bounded public messages.
- Unknown workers, expired commands, duplicate completion, invalid transitions,
  and version conflicts fail explicitly.
- Agent IDs, capability names, versions, and diagnostic fields have strict
  character, length, and collection bounds.
- URL path identities are decoded and validated exactly once.
- KBase never stores local absolute paths, cookies, tokens, article bodies, QR
  payloads, full upstream responses, or arbitrary command output.
- Production logs contain IDs, states, codes, and durations, not content or
  credentials.
- WC Plus vendor or host-security failure is represented as
  `vendor_blocked`; it cannot be reported as a successful run.

## Delivery Plan

Implementation proceeds in these batches:

1. Worker Registry migration, heartbeat evolution, state derivation, and legacy
   compatibility.
2. Desired state, manual controls, constrained diagnostics, and operation
   audit.
3. Artifact Catalog, Command Queue, macOS updater, and rollback behavior.
4. WeChat and WC Plus independent-worker integration against the shared
   protocol.
5. Unified overview, worker details, source-workspace links, and responsive UI.
6. Integration preflight and layered production verification.

Both workers ship in the same feature batch. The implementation must not enable
automatic scheduling during rollout.

## Test Strategy

Test-driven delivery covers:

- schema migration and old heartbeat compatibility;
- desired and observed state derivation;
- lease rejection for paused, incompatible, or upgrading workers;
- command idempotency, transitions, expiry, duplicate claim/completion, and
  concurrent upgrade exclusion;
- authentication boundaries for browser management sessions, ordinary machine
  Bearer clients, and worker tokens;
- artifact platform, architecture, version, size, and hash validation;
- updater recovery at every staged failure boundary;
- rejection of arbitrary commands, URLs, scripts, environment variables, and
  installation paths;
- shared worker-protocol contract tests with independent WeChat and WC Plus
  fixtures;
- outbox, cursor, interruption, retry, partial failure, and dead-letter
  regression behavior;
- Web syntax, static smoke, desktop layout, narrow viewport, deep links, stale
  response guards, and accessible operation feedback;
- full Go tests, Vue build, existing Web smoke checks, real Nginx proxy smoke,
  generated system-map drift, privacy smoke, and diff checks.

## Gate Acceptance

The unified control-plane feature passes online verification only when:

- both independent workers register and report distinct capability state;
- pause prevents new leases and resume restores explicit manual operation;
- diagnosis, cancellation, and retry show actual durable outcomes;
- both workers complete one real constrained version upgrade;
- isolated upgrade failure proves automatic binary rollback;
- WeChat performs one operator-selected bounded production collection, a rerun
  skips unchanged items, interruption preserves cursor/outbox truth, and
  imported knowledge resolves to original-article citations;
- WC Plus completes fixture-based end-to-end synchronization, recovery, and
  upgrade validation; when the production dependency remains unavailable, the
  deployed worker reports `vendor_blocked`, leases no WC Plus work, and never
  claims success;
- the server, workers, Web UI, logs, API messages, and generated documentation
  expose no source credentials or private content.

Real WC Plus acquisition activation is a separate later feature and Gate. It
starts only when a legitimate vendor build is accepted by the host and can be
validated without bypassing licensing or platform security. That external
activation state does not invalidate the unified management feature when the
designed `vendor_blocked` behavior is correct.

## Subsequent Product Sequence

After this feature passes its delivery Gates:

1. design and deliver knowledge-quality improvements covering retrieval,
   deduplication, citation trust, and conflict detection;
2. design and deliver experience improvements covering initial setup,
   one-action workflows, notifications, and mobile usability.

