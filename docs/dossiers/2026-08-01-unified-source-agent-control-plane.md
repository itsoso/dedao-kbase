# Unified Source Agent Control Plane Dossier

**Date:** 2026-08-01

**Status:** Delivery in progress; Task 10A updater recovery amendment under review

**Frozen design baseline:**
`docs/plans/2026-08-01-unified-source-agent-control-plane-design.md`

**Approved implementation plan:**
`docs/plans/2026-08-01-unified-source-agent-control-plane.md`

**System map contract:** `docs/system-map/INDEX.md`

## Outcome

The approved design is frozen as the delivery baseline for a unified KBase
control plane over independent source workers. Delivery is in progress at the
Task 10A definition amendment described below. Any change to the boundaries
below must return to definition and update the design decision before
implementation proceeds.

Architecture counts must be cited only from the generated system map described
by `docs/system-map/INDEX.md`; route, command, operation, durable-object, or
other structural counts must never be hand-written into this dossier or other
narrative documentation.

## Current Stage and Next Handoff

Tasks 1 through 10 have been implemented and independently reviewed. Before
Task 11 exposes an upgrade control, delivery returned to the definition loop
because the real Workers still fail closed at the artifact-to-updater handoff.
Task 10A in the approved implementation plan is now the active Gate. The Web
overview remains blocked until that Gate passes implementation, recovery,
security, and review checks.

## 2026-08-02 Updater Recovery Definition Amendment

The first Task 10A draft was rejected because a helper forked by a Worker may
be killed with the Worker's launchd process group and because a restarted
Runner cannot recover an in-memory active command. A second draft added an
independent updater job but was rejected because it did not yet guarantee
updater crash recovery, initial-claim recovery, deterministic terminal
reconciliation, or atomic paired install/uninstall.

The current amendment requires a separately supervised updater LaunchAgent per
Worker, a durable `PathState` pending marker, restart-safe command recovery,
pre-armed ready identity, a server-authored pre-replacement guard, clean-HEAD
compiled revision identity, retained backup until server terminal
acknowledgement, and recoverable publication of the binary pair, both plists,
and protected local config. A shared per-worker kernel lifecycle lock makes
maintenance and update publication mutually exclusive across all three local
processes. These changes refine the approved macOS updater;
they do not add signing, remote paths, arbitrary execution, scheduled rollout,
or per-agent authentication claims.

## Approved Constraints

- WeChat and WC Plus remain independent workers with separate processes,
  state, outboxes, logs, and failure domains.
- Source work in the first release is manual-only: an operator must explicitly
  trigger every run, new subscriptions default to `manual`, and interval
  scheduling must remain disabled. This source-work constraint is distinct
  from the upgrade-scheduling prohibition below.
- Workers share `KBASE_SOURCE_AGENT_TOKEN`. An `agent_id` is only an
  operational identifier: it is not a trusted identity, is not non-forgeable
  audit evidence, and cannot be revoked independently. A token compromise
  requires rotation across all source workers.
- Only an authenticated browser management session, with CSRF validation and
  explicit operator confirmation, may create an upgrade command. Worker-token
  and ordinary machine Bearer clients cannot create one.
- The updater is fixed-function and constrained. There is no arbitrary remote
  command, URL, shell command, script, environment variable, or installation
  path surface.
- macOS is delivered first through user-level updater and LaunchAgent
  integration. Worker, capability, command, and upgrade protocols reserve
  clean Windows and Linux extension points without claiming those installers
  in the first release.
- WC Plus acceptance is layered: fixture-based end-to-end synchronization,
  recovery, and upgrade behavior must pass; if the legitimate production
  dependency remains unavailable, the worker must honestly report
  `vendor_blocked`, lease no WC Plus work, and never claim success.
- Each worker may have only one active upgrade command, and workers are
  upgraded individually. Bulk, broadcast, silent, and scheduled upgrades are
  forbidden.
- Every artifact is bound to an exact source revision and verified with
  SHA-256, platform, architecture, version compatibility, and byte size.
- Upgrade installation requires same-filesystem local staging and backup,
  atomic binary replacement, LaunchAgent restart, a ready receipt for the
  expected command and version, and automatic rollback when readiness or the
  authenticated post-restart heartbeat fails.
- `allowed_for_rollout` is a kill switch: disabling it prevents new upgrade
  commands and stops a claimed command before installation.
- Staging and production promote the same already-verified artifact; promotion
  must not rebuild it.
- There is no external artifact signature. SHA-256 provides byte-integrity
  evidence, not independent publisher identity, so upgrades may never be
  silent, scheduled, or broadcast.

## Gate Decisions

### G1 - Admission

**Decision: PASS**

The requirement is admitted as the next product step: consolidate the existing
source-agent substrate into one operator-controlled management layer before
knowledge-quality and everyday-experience work. The scope extends existing
registries, leases, runs, outboxes, cursors, and ingestion rather than creating
a second collection system.

### G2 - Feasibility and Risk Pressure Test

**Decision: PASS**

The design is feasible within the existing outbound HTTPS worker model and
explicitly accepts two security boundaries:

- **Shared-token boundary:** `KBASE_SOURCE_AGENT_TOKEN` authenticates the
  worker class, not an individual device. `agent_id` remains self-reported
  operational metadata, cannot establish trusted identity, and cannot support
  per-agent revocation in version one.
- **Remote-update boundary:** remote update is a browser-confirmed selection
  from a private compatible artifact catalog, implemented by a constrained
  fixed-function updater. The payload carries only bounded identifiers,
  expected version, and expiry; it cannot become a generic execution or
  arbitrary-download channel. Without external signing, all unattended,
  scheduled, silent, bulk, and broadcast rollout paths remain prohibited.

Residual risk is bounded by exact-revision and SHA-256 checks, per-worker
upgrade exclusion, the rollout kill switch, same-artifact promotion, local
backup and atomic replacement, ready-receipt verification, and automatic
rollback. The macOS-first implementation and layered WC Plus acceptance keep
platform and vendor dependencies explicit rather than presenting them as
completed production support.

### G3 - Test

**Decision: PENDING**

Delivery and test evidence have not started.

### G4 - Review

**Decision: PENDING**

Implementation review has not started.

### G5 - Deployment Health

**Decision: PENDING**

No candidate has been deployed.

### G6 - Online Verification

**Decision: PENDING**

No production verification has been performed.
