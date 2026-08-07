# Unified Source Agent Control Plane Dossier

**Date:** 2026-08-01

**Status:** Tasks 1-15 implemented and reviewed; Task 16 G5/G6 rollout pending

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

Tasks 1 through 10A are implemented, reviewed, released as v0.2.0, and installed
locally as two independent macOS Workers with separately supervised updaters.
Tasks 11 and 12 add the unified `/sources/agents` overview and stable Worker
details; Task 13 adds a deterministic two-Worker integration Gate to normal CI.
Those Web and integration changes are not yet merged or deployed. Task 14
synchronized operator documentation and the generated system map. Task 15
completed independent review and candidate verification; the next handoff is
the clean-main deployment and layered G5/G6 verification in Task 16.

## 2026-08-07 Tasks 11-13 Checkpoint

The overview exposes the approved manual controls without adding arbitrary
URLs, scripts, environment fields, bulk actions, silent rollout, or scheduling.
The detail route validates and binds `agent_id`, protects against stale
responses, keeps source credentials out of the page, and links each Worker to
its source-owned workspace. Artifact and command panel failures remain
independent so one unavailable surface does not erase the other state.

The integration smoke starts a loopback KBase server with generated temporary
credentials, loads sanitized WeChat and WC Plus heartbeat fixtures, and proves
independent registration, targeted pause isolation, bounded diagnosis,
truthful `vendor_blocked`, artifact catalog loading, same-byte SHA-256
promotion, rollout kill-switch behavior, command-bound download checks, and
updater recovery coverage. It was first RED because the fixtures were absent,
then GREEN after the deterministic fixtures and CI step were added.

Checkpoint evidence:

```text
node --check frontend-web/app.js
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
node frontend-web/scripts/web-navigation-url-smoke.mjs
node frontend-web/scripts/wechat-source-ui-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
PASS

bash scripts/source-agent-control-plane-smoke.sh
PASS

bash scripts/privacy-smoke.sh
git diff --check
PASS
```

Production KBase was separately reconciled to clean `main` revision
`f4c6a079a7eb43a19fb0844fd6ec46c251514ae7`; service health, public static
routes, and anonymous API rejection passed after a startup-race attempt safely
rolled back and the bounded readiness retry succeeded. This is baseline health
evidence only: Tasks 11-14 are not in that revision, the production artifact
catalog is still disabled, and the complete feature therefore does not pass
G5 or G6 yet.

## 2026-08-07 Task 15 Review Checkpoint

Independent review covered the four required risk areas. It found no arbitrary
execution, updater concurrency/rollback, credential/content privacy, or WC Plus
truthfulness blocker. The first review classified cross-Worker impersonation
under the shared token as High; the final review corrected that classification:
the Worker class is intentionally mutually trusted under the owner-approved,
frozen shared-token design. This remains a documented residual risk and token
rotation blast radius, not an implementation deviation or a claim of
per-Worker identity.

The review also found a genuine Medium lease-authority gap: a Worker could ask
for an operation it had not registered, an empty health table could pass, and
a heartbeat could change capability/health between precheck and claim. The
remediation was developed RED/GREEN. Lease requests are now intersected with
the registered capability set, non-legacy Workers require non-empty healthy
capability state, and the exact capability/health JSON snapshot is rechecked
inside the final atomic SQL claim. The targeted final re-review found no
Critical, High, or Medium blocker.

Candidate verification evidence:

```text
npm --prefix frontend ci --registry=https://registry.npmjs.org --no-audit
npm --prefix frontend run build
go mod verify
go vet ./...
go test ./... -count=1
bash scripts/source-agent-control-plane-smoke.sh
bash scripts/source-agent-artifact-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
all frontend/scripts/*-smoke.mjs
node --check frontend-web/app.js
all frontend-web/scripts/*smoke*.mjs
git diff --check
PASS
```

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

## 2026-08-02 Task 10A Commit 3 Checkpoint

The third reviewable Task 10A slice is implemented locally. The update
transaction durably arms `restart_requested` before restarting a Worker, and a
new Worker can publish ready only after its authenticated heartbeat using its
compiled runtime identity. The server exposes a strict Worker-authenticated
recovery contract for either the exact protected command ID or the single
active upgrade already owned by that Worker. The Runner performs this recovery
before ordinary claim or source lease, durably checkpoints the immutable
command fingerprint before any report or upgrade side effect, and reconciles a
terminal command by first clearing any stale in-memory execution state and
then writing a strict, neutral local terminal observation without re-executing
it. That observation is not a cleanup acknowledgement and carries no cleanup
or rollback action.

All local terminal outcomes, including failures before binary replacement,
now retain the backup, partial stage, and recovery journal. In commit 4 the
independent updater must bind the neutral server observation to local
journal/outcome evidence and durably choose either acknowledgement/cleanup or
rollback; the observation by itself can never authorize cleanup. Different
commands remain blocked while retained recovery material exists. Production
WeChat and WC Plus constructors still use their fail-closed updater
implementations. This intermediate branch is not deployable until protected
state, recovery, and phase mapping are wired into both constructors in commit
4; this slice does not claim production upgrade availability and does not add
a signing mechanism.

The first commit-level review rejected the candidate because a recovered terminal could
leave a stale in-memory command executable, pre-replacement durable failures
could clean recovery material before server reconciliation, cancellation was
returned through a transport wrapper, and the terminal record was named like
a cleanup acknowledgement. Remediation now clears matching in-memory command
and pending state before recording the terminal, preserves exact `ctx.Err()`,
retains every durable terminal attempt, and names the persisted record as a
neutral observation. Fresh independent specification and code-quality
re-reviews approved the remediated slice with no blocking findings.

Provisional G3 evidence for this slice:

```text
go test ./backend/app -run 'TestSourceAgent(CommandRecovery|ClientRecoversOwnedUpgrade|WorkerCommandRecovery|Runner.*Upgrade|RunnerDoesNotPublishReady|Update)' -count=1
PASS

go test ./backend/app -count=1
PASS

go test ./... -count=1
PASS

go test -race ./backend/app -count=1
PASS

go test -race ./backend/app -run 'TestSourceAgent(CommandRecovery|ClientRecovery|ClientRecoversOwnedUpgrade|WorkerCommandRecovery|Runner.*Upgrade|RunnerDoesNotPublishReady|Update)' -count=1
PASS after review remediation

go vet ./...
PASS

bash scripts/source-agent-artifact-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
PASS

bash scripts/system-map-smoke.sh
PASS

git diff --check
PASS
```

The recovery route change is reflected in the generated system map. Commit 3
passed its commit-level specification and code-quality re-reviews. Overall G4
remains pending until Task 10A commits 4 through 6 pass their required reviews.
The updater LaunchAgent smoke is not yet available because independent job
wiring belongs to commit 4; it is a remaining Task 10A Gate, not evidence for
this slice.

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

**Decision: PASS**

The final Task 10A candidate passed the full Go suite and race suite, Go vet,
module verification, frontend build and smoke checks, generated system-map
check, privacy check, both Worker packaging checks, real-updater managed
install/uninstall recovery checks, and the independent updater launchd crash
check. The lifecycle protocol tests use the compiled updater with a real
kernel lock and cover process death after durable `begin-mutation`.

### G4 - Review

**Decision: PASS**

Independent review rejected earlier candidates for lock ownership, stale
marker truncation, unbounded lock acquisition, and fixture-only crash
coverage. The final re-review confirmed that the fixed updater owns the lock
and marker, preserves prior durable phases, uses non-blocking flock with a
bounded installer deadline, cleans the marker before journal evidence, and is
exercised as a real child process. Task 15 then reviewed the complete candidate,
rejected the initial lease-authority gap, and approved its atomic snapshot
remediation. No Critical, High, or Medium finding remains.

### G5 - Deployment Health

**Decision: PENDING FOR COMPLETE CANDIDATE**

v0.2.0 was published and both macOS Workers/updaters were installed locally.
The baseline KBase server is healthy on clean main, but the unified Web UI,
integration Gate, and production artifact catalog are not deployed together.

### G6 - Online Verification

**Decision: PENDING**

No complete two-Worker production acceptance or real version-changing upgrade
has been performed. The feature must not claim G6 until Task 16 satisfies the
layered acceptance list below.
