# Unified Source Agent Control Plane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the existing KBase source-agent substrate into one operator-controlled management plane for independent WeChat and WC Plus workers, including bounded diagnostics and a constrained, rollback-capable macOS upgrade protocol.

**Architecture:** Extend the existing `SourceSyncStore`, heartbeat, lease, subscription, run, outbox, and HTTP boundaries instead of replacing them. Add durable worker state, constrained commands, a private file-backed artifact catalog, a fixed-function updater, and `/sources/agents` overview/detail views. Keep both workers outbound-only and independent; preserve shared-token compatibility while treating `agent_id` as an operational identifier rather than a trusted device identity.

**Tech Stack:** Go 1.21, SQLite, `net/http`, macOS Keychain and LaunchAgent, Vue-independent static Web UI in `frontend-web`, Node smoke scripts, shell packaging checks, generated system map, privacy gates.

**Required skills during execution:** `@test-driven-development` for every behavior change, `@systematic-debugging` for unexpected failures, `@app-update-delivery` for Tasks 6-9, `@privacy-guard` before every commit or push, `@requesting-code-review` after Tasks 5, 9, and 13, and `@verification-before-completion` before G4/G5/G6 claims.

---

### Task 1: Open the delivery dossier and freeze the first-version boundary

**Files:**
- Create: `docs/dossiers/2026-08-01-unified-source-agent-control-plane.md`
- Reference: `docs/plans/2026-08-01-unified-source-agent-control-plane-design.md`
- Reference: `docs/system-map/INDEX.md`

**Step 1: Create the lifecycle dossier**

Record:

```markdown
# Unified Source Agent Control Plane Dossier

**Status:** Definition complete; delivery not started

## G1 - Admission
**Decision: PASS**

## G2 - Feasibility and Risk
**Decision: PASS with explicit shared-token and remote-update boundaries**

## G3 - Test
**Decision: PENDING**

## G4 - Review
**Decision: PENDING**

## G5 - Deployment Health
**Decision: PENDING**

## G6 - Online Verification
**Decision: PENDING**
```

Include the approved constraints: independent workers, manual scheduling, shared worker token, browser-only upgrade creation, no arbitrary remote execution, macOS first, and layered WC Plus acceptance.

**Step 2: Run documentation privacy checks**

Run:

```bash
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: both PASS; no machine-specific path, token, cookie, or downloaded content appears.

**Step 3: Commit the lifecycle skeleton**

```bash
git add docs/dossiers/2026-08-01-unified-source-agent-control-plane.md
git commit -m "docs(kbase): open source agent control plane delivery"
```

### Task 2: Evolve the Worker Registry schema compatibly

**Files:**
- Modify: `backend/app/source_sync.go`
- Modify: `backend/app/source_sync_test.go`
- Create: `backend/app/source_agent_control.go`
- Create: `backend/app/source_agent_control_test.go`

**Step 1: Write failing legacy-migration and heartbeat tests**

Add tests that create the pre-feature `source_agents` table, run `migrateSourceSyncDB`, and assert the old row remains readable with safe defaults.

The new heartbeat shape must be additive:

```go
type SourceAgentHeartbeat struct {
    AgentID          string                            `json:"agent_id"`
    WorkerType       string                            `json:"worker_type,omitempty"`
    Platform         string                            `json:"platform,omitempty"`
    Architecture     string                            `json:"architecture,omitempty"`
    Version          string                            `json:"version,omitempty"`
    ProtocolVersion  string                            `json:"protocol_version,omitempty"`
    Capabilities     []string                          `json:"capabilities,omitempty"`
    CapabilityHealth map[string]SourceCapabilityHealth `json:"capability_health,omitempty"`
    CurrentRunID     string                            `json:"current_run_id,omitempty"`
    CurrentCommandID string                            `json:"current_command_id,omitempty"`
    OutboxPending    int                               `json:"outbox_pending,omitempty"`
    DeadLetterCount  int                               `json:"dead_letter_count,omitempty"`
    LastSuccessAt    string                            `json:"last_success_at,omitempty"`

    // Legacy compatibility only.
    WCPlusHealthy bool   `json:"wcplus_healthy"`
    WCPlusVersion string `json:"wcplus_version,omitempty"`
    LastError     string `json:"last_error,omitempty"`
}
```

Add a stable code to capability health:

```go
type SourceCapabilityHealth struct {
    Healthy        bool   `json:"healthy"`
    Code           string `json:"code,omitempty"`
    Version        string `json:"version,omitempty"`
    LastError      string `json:"last_error,omitempty"`
    RequiresAction string `json:"requires_action,omitempty"`
}
```

Tests must cover valid platform-neutral fields, negative counter rejection, bounded strings, unknown diagnostic codes, and old WC Plus-only heartbeat mapping.

**Step 2: Run tests to verify RED**

Run:

```bash
go test ./backend/app -run 'TestSourceAgent(RegistryMigration|HeartbeatRuntimeMetadata|HeartbeatBounds|CapabilityCode)' -count=1
```

Expected: FAIL because the fields, migration, and validation do not exist.

**Step 3: Add columns through the existing migration helper**

Add columns with safe legacy defaults:

```text
worker_type TEXT NOT NULL DEFAULT 'legacy'
platform TEXT NOT NULL DEFAULT ''
architecture TEXT NOT NULL DEFAULT ''
protocol_version TEXT NOT NULL DEFAULT ''
desired_state TEXT NOT NULL DEFAULT 'active'
current_run_id TEXT NOT NULL DEFAULT ''
current_command_id TEXT NOT NULL DEFAULT ''
outbox_pending INTEGER NOT NULL DEFAULT 0
dead_letter_count INTEGER NOT NULL DEFAULT 0
last_success_at TEXT NOT NULL DEFAULT ''
```

Call `ensureSourceSyncColumn` sequentially and stop on the first error. Update every `INSERT`, `UPDATE`, `SELECT`, and scanner together.

**Step 4: Implement normalization and stable constants**

In `source_agent_control.go`, define:

```go
const (
    SourceAgentDesiredActive = "active"
    SourceAgentDesiredPaused = "paused"

    SourceAgentObservedOnline         = "online"
    SourceAgentObservedDegraded       = "degraded"
    SourceAgentObservedRequiresAction = "requires_action"
    SourceAgentObservedOffline        = "offline"
    SourceAgentObservedUpgrading      = "upgrading"
)

var allowedSourceCapabilityCodes = map[string]struct{}{
    "": {}, "login_required": {}, "vendor_blocked": {},
    "dependency_unavailable": {}, "config_invalid": {},
    "upgrade_required": {}, "throttled": {},
}
```

Normalize worker type, platform, architecture, versions, IDs, counters, and timestamps with explicit length/format bounds. Do not silently keep invalid values.

**Step 5: Run focused and package tests**

```bash
go test ./backend/app -run 'TestSourceAgent' -count=1
go test ./backend/app -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_sync.go backend/app/source_sync_test.go backend/app/source_agent_control.go backend/app/source_agent_control_test.go
git commit -m "feat(kbase): evolve source worker registry"
```

### Task 3: Derive observed state and enforce pause at the lease boundary

**Files:**
- Modify: `backend/app/source_agent_control.go`
- Modify: `backend/app/source_agent_control_test.go`
- Modify: `backend/app/source_sync.go`
- Modify: `backend/app/source_sync_test.go`

**Step 1: Write the observed-state truth-table tests**

Cover these ordered rules:

```text
active upgrade command -> upgrading
stale heartbeat -> offline
any capability with requires_action/code requiring operator action -> requires_action
any unhealthy non-blocking capability -> degraded
otherwise -> online
```

Use a fixed `now` and a bounded heartbeat freshness duration. Add a test proving the stored desired state is independent from observed health.

**Step 2: Write pause/lease failing tests**

Create one queued run bound to a paused agent and assert `LeaseNextRun` returns no work. Resume it and assert the same run becomes leasable. Also prove an already-running run remains unchanged when the worker is paused.

**Step 3: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgentObservedState|TestSourceLeaseRejectsPausedAgent' -count=1
```

Expected: FAIL.

**Step 4: Implement the minimal state and pause methods**

Add:

```go
func (s *SourceSyncStore) SetAgentDesiredState(agentID, desired string) (SourceAgent, error)
func DeriveSourceAgentObservedState(agent SourceAgent, now time.Time, freshness time.Duration, upgradeActive bool) string
```

At the beginning of `LeaseNextRun`, load the worker and return no work unless `desired_state == active`. Keep capability matching and existing run transaction semantics unchanged.

**Step 5: Run focused and regression tests**

```bash
go test ./backend/app -run 'TestSourceAgentObservedState|TestSourceLease' -count=1
go test ./backend/app -run 'TestSource(Sync|Scheduler|Agent)' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_agent_control.go backend/app/source_agent_control_test.go backend/app/source_sync.go backend/app/source_sync_test.go
git commit -m "feat(kbase): control source worker lease eligibility"
```

### Task 4: Add the durable constrained Command Queue

**Files:**
- Create: `backend/app/source_agent_command.go`
- Create: `backend/app/source_agent_command_test.go`
- Modify: `backend/app/source_sync.go`

**Step 1: Write failing store and transition tests**

Define only:

```go
const (
    SourceAgentCommandDiagnose = "diagnose"
    SourceAgentCommandUpgrade  = "upgrade"
)
```

Test command creation, target validation, idempotency, bounded payload, claim by the target agent, progress transitions, terminal completion, expiry, duplicate claim, duplicate completion, and one-active-upgrade-per-agent.

The upgrade payload is fixed:

```go
type SourceAgentUpgradeSpec struct {
    ArtifactID            string `json:"artifact_id"`
    ExpectedCurrentVersion string `json:"expected_current_version"`
}
```

Reject unknown JSON fields by decoding into the typed spec and requiring full consumption.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgentCommand' -count=1
```

Expected: FAIL because the store does not exist.

**Step 3: Add command tables and indexes**

Create `source_agent_commands` and `source_agent_command_events`. Persist command ID, target, type, typed spec JSON, state, expected/actual versions, stable result code, bounded message, timestamps, claim owner, and expiry. Add a partial unique index preventing more than one non-terminal upgrade for an agent.

**Step 4: Implement explicit transition validation**

Use one transition map rather than scattered conditionals:

```go
var sourceAgentCommandTransitions = map[string]map[string]struct{}{
    "queued":      {"claimed": {}, "canceled": {}, "expired": {}},
    "claimed":     {"downloading": {}, "failed": {}, "expired": {}},
    "downloading": {"verified": {}, "failed": {}},
    "verified":    {"installing": {}, "failed": {}},
    "installing":  {"restarting": {}, "rollback": {}, "failed": {}},
    "restarting":  {"verifying": {}, "rollback": {}},
    "verifying":   {"succeeded": {}, "rollback": {}},
    "rollback":    {"rolled_back": {}, "failed": {}},
}
```

Give `diagnose` a separate short transition set: `queued -> claimed -> succeeded|failed|expired`.

**Step 5: Run focused tests and race-sensitive transaction tests**

```bash
go test ./backend/app -run 'TestSourceAgentCommand' -count=1
go test -race ./backend/app -run 'TestSourceAgentCommand' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_agent_command.go backend/app/source_agent_command_test.go backend/app/source_sync.go
git commit -m "feat(kbase): persist constrained source worker commands"
```

### Task 5: Expose browser-management and worker-command HTTP contracts

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `backend/app/source_agent_client.go`
- Modify: `backend/app/source_agent_client_test.go`

**Step 1: Write failing administrative API tests**

Cover:

```text
GET  /api/source-agents/{agent_id}
POST /api/source-agents/{agent_id}/desired-state
POST /api/source-agents/{agent_id}/commands
GET  /api/source-agents/{agent_id}/commands
```

Assert:

- pause/resume and diagnose accept normal authenticated management requests;
- upgrade rejects ordinary Bearer authentication;
- upgrade accepts a valid browser cookie plus CSRF token;
- unknown command type, unknown fields, invalid agent ID, stale version, and duplicate active upgrade fail explicitly;
- API messages contain no local path or command spec internals.

**Step 2: Write failing worker API/client tests**

Add typed client calls and routes for:

```text
POST /api/source-agent/commands/claim
POST /api/source-agent/commands/{command_id}/progress
POST /api/source-agent/commands/{command_id}/complete
```

The request always includes `agent_id`; the store verifies the command target. Test that a shared-token caller cannot claim another agent's target command merely by reusing a command ID.

**Step 3: Verify RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerSourceAgent(Control|Commands)|TestSourceAgentClientCommands' -count=1
```

Expected: FAIL with route-not-found or missing method errors.

**Step 4: Implement path parsing and auth checks**

Extend `isSourceSyncAdminPath` to include `/api/source-agents/`. Reuse the request auth stored by `requestWithKBaseAuth`. In the upgrade branch, require:

```go
auth, ok := kbaseRequestAuthFromContext(r.Context())
if !ok || auth.Method != kbaseAuthMethodCookie {
    writeHTTPError(w, http.StatusForbidden, "browser management session required")
    return
}
```

Do not weaken the existing global unsafe-method CSRF enforcement.

**Step 5: Implement worker command methods in `SourceAgentClient`**

Add typed methods:

```go
func (c *SourceAgentClient) ClaimCommand(ctx context.Context) (*SourceAgentCommand, error)
func (c *SourceAgentClient) ReportCommand(ctx context.Context, commandID, state, code, message, actualVersion string) (SourceAgentCommand, error)
```

Keep bounded JSON decoding and the current rule that HTTP error bodies are not surfaced as secrets.

**Step 6: Run focused and full HTTP/client tests**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerSourceAgent|TestSourceAgentClient' -count=1
go test ./backend/app -count=1
```

Expected: PASS.

**Step 7: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go backend/app/source_agent_client.go backend/app/source_agent_client_test.go
git commit -m "feat(kbase): expose source worker management API"
```

### Task 6: Build a private file-backed Artifact Catalog

**Files:**
- Create: `backend/app/source_agent_artifact.go`
- Create: `backend/app/source_agent_artifact_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`
- Create: `scripts/source-agent-artifact-smoke.sh`

**Step 1: Write failing catalog validation tests**

Use a private root containing a checked-in-format `catalog.json` only in test fixtures. Define:

```go
type SourceAgentArtifact struct {
    ID               string `json:"id"`
    WorkerType       string `json:"worker_type"`
    Platform         string `json:"platform"`
    Architecture     string `json:"architecture"`
    Revision         string `json:"revision"`
    Version          string `json:"version"`
    ProtocolVersion  string `json:"protocol_version"`
    MinimumVersion   string `json:"minimum_version,omitempty"`
    Channel          string `json:"channel"`
    ReleaseNotes     string `json:"release_notes,omitempty"`
    Size             int64  `json:"size"`
    SHA256           string `json:"sha256"`
    StorageKey       string `json:"storage_key"`
    BuildGate        string `json:"build_gate"`
    AllowedForRollout bool  `json:"allowed_for_rollout"`
}
```

Tests reject traversal, symlinks, non-regular files, wrong size/hash, unsupported platform/architecture, malformed revision/version, failed build gate, and artifacts not allowed for rollout.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgentArtifact' -count=1
```

Expected: FAIL.

**Step 3: Implement the read-only catalog**

Add `KBASE_SOURCE_AGENT_ARTIFACT_ROOT` as an explicit optional server setting. No machine path appears in defaults or API responses. Load and validate metadata on access; open files relative to the configured root without following a path supplied by the client.

**Step 4: Add private metadata and command-bound download routes**

Management metadata may be listed through authenticated admin API. Worker download must require:

- worker token;
- agent ID;
- a claimed active upgrade command for that agent;
- the exact artifact ID stored in the command;
- compatible worker type, platform, and architecture from the current Registry row.

Serve bytes with `private, no-store`, `Content-Length`, and expected SHA-256 metadata. Never expose `storage_key` or the server root.

Treat `allowed_for_rollout` as a kill switch. Recheck it when creating a command,
when downloading, and immediately before the worker enters `installing`.
Promoting an artifact changes catalog rollout metadata; it must not rebuild or
replace the already-verified bytes.

**Step 5: Add a packaging smoke contract**

The shell smoke must prove that generated metadata contains exact revision, target triple, size, and SHA-256; it must reject an altered binary and a traversal storage key.

Run:

```bash
bash scripts/source-agent-artifact-smoke.sh
go test ./backend/app -run 'TestSourceAgentArtifact|TestKBaseHTTPHandlerSourceAgentArtifact' -count=1
go test ./cmd/kbase-server -run 'Test.*SourceAgentArtifact' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_agent_artifact.go backend/app/source_agent_artifact_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go cmd/kbase-server/main_test.go scripts/source-agent-artifact-smoke.sh
git commit -m "feat(kbase): add private source worker artifact catalog"
```

### Task 7: Add a shared worker control cycle without coupling adapters

**Files:**
- Create: `backend/app/source_agent_command_runner.go`
- Create: `backend/app/source_agent_command_runner_test.go`
- Modify: `backend/app/source_agent_runner.go`
- Modify: `backend/app/source_agent_runner_test.go`
- Modify: `backend/app/source_agent_outbox.go`
- Modify: `backend/app/source_agent_outbox_test.go`

**Step 1: Write failing heartbeat telemetry tests**

Expose bounded outbox counts through methods that do not scan or return bodies:

```go
func (o *SourceAgentOutbox) CountPending() (int, error)
func (o *SourceAgentOutbox) CountDeadLetters() (int, error)
```

Test that a runner heartbeat includes worker/runtime metadata and counts but no envelope content or state path.

**Step 2: Write failing command-cycle tests**

Create narrow interfaces:

```go
type SourceAgentCommandClient interface {
    ClaimCommand(context.Context) (*SourceAgentCommand, error)
    ReportCommand(context.Context, string, string, string, string, string) (SourceAgentCommand, error)
}

type SourceAgentDiagnoser interface {
    Diagnose(context.Context) SourceAgentDiagnosticReport
}

type SourceAgentUpdater interface {
    Upgrade(context.Context, SourceAgentCommand) SourceAgentUpgradeResult
}
```

Test no command, diagnose success/failure, upgrade handoff, command-before-lease ordering, and no source lease while an upgrade is active.

**Step 3: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgent(CommandRunner|RunnerHeartbeat|OutboxCounts)' -count=1
```

Expected: FAIL.

**Step 4: Implement one bounded control cycle**

The cycle order is:

```text
collect local bounded health -> heartbeat -> claim at most one command
-> execute/report command OR (when active and not upgrading) lease one source run
```

Do not run source and upgrade work concurrently. A diagnose command may finish before the next source lease.
An upgrade stays claimed but unapplied while a source run is active; the UI
must show that it is waiting rather than killing the run or discarding outbox
state.

**Step 5: Run focused and regression tests**

```bash
go test ./backend/app -run 'TestSourceAgent(CommandRunner|Runner|Outbox)' -count=1
go test -race ./backend/app -run 'TestSourceAgent(CommandRunner|Runner)' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_agent_command_runner.go backend/app/source_agent_command_runner_test.go backend/app/source_agent_runner.go backend/app/source_agent_runner_test.go backend/app/source_agent_outbox.go backend/app/source_agent_outbox_test.go
git commit -m "feat(agent): run bounded worker control commands"
```

### Task 8: Implement the fixed-function updater transaction

**Files:**
- Create: `backend/app/source_agent_update.go`
- Create: `backend/app/source_agent_update_test.go`
- Create: `cmd/source-agent-updater/main.go`
- Create: `cmd/source-agent-updater/main_test.go`
- Create: `cmd/source-agent-updater/platform_darwin.go`
- Create: `cmd/source-agent-updater/platform_other.go`

**Step 1: Write the transaction failure matrix first**

Use temporary directories and fake process controls. Cover:

```text
wrong expected current version
wrong target triple
size mismatch
hash mismatch
backup failure
atomic replacement failure
restart failure
ready-receipt timeout
ready receipt with wrong command/version
successful rollback
rollback restart failure surfaced as terminal failure
successful upgrade
```

Assert the old executable remains or is restored at every failed boundary.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgentUpdate' -count=1
go test ./cmd/source-agent-updater -count=1
```

Expected: FAIL because the updater does not exist.

**Step 3: Implement a pure transaction core**

The core receives resolved local paths from local configuration, never from a remote command:

```go
type SourceAgentUpdateRequest struct {
    CommandID       string
    WorkerType      string
    CurrentVersion  string
    TargetVersion   string
    ExpectedSHA256  string
    ExpectedSize    int64
    StagedBinary    string
}
```

Keep filesystem and process operations behind interfaces so failure points are deterministic in tests. Use same-filesystem staging, mode `0755`, explicit sync where supported, backup before rename, and a unique local ready receipt.

The updater preserves the state database and outbox and must recheck the local
worker-idle condition plus the server rollout kill switch before applying. The
result records platform, channel, protocol/runtime version, command ID, source
revision, outcome, and duration without recording content or credentials.

**Step 4: Implement the macOS process adapter**

The adapter may invoke only fixed `launchctl` operations for the locally configured label. It must reject labels and paths outside values provided by the local installer. `platform_other.go` returns a clear unsupported-platform error while preserving the shared protocol types.

**Step 5: Run focused, race, and command-package tests**

```bash
go test ./backend/app -run 'TestSourceAgentUpdate' -count=1
go test ./cmd/source-agent-updater -count=1
go test -race ./backend/app -run 'TestSourceAgentUpdate' -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/source_agent_update.go backend/app/source_agent_update_test.go cmd/source-agent-updater
git commit -m "feat(agent): add rollback-capable worker updater"
```

### Task 9: Unify macOS secret loading and package both independent workers

**Files:**
- Create: `internal/sourceagentsecret/keychain_darwin.go`
- Create: `internal/sourceagentsecret/keychain_other.go`
- Create: `internal/sourceagentsecret/keychain_test.go`
- Modify: `cmd/source-agent/main.go`
- Modify: `cmd/source-agent/main_test.go`
- Modify: `cmd/source-agent/keychain_store_darwin.go`
- Modify: `cmd/source-agent/keychain_store_other.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`
- Modify: `scripts/build-source-agent-macos.sh`
- Modify: `scripts/build-wcplus-agent-macos.sh`
- Modify: `scripts/install-source-agent-macos.sh`
- Modify: `scripts/install-wcplus-agent-macos.sh`
- Modify: `scripts/source-agent-packaging-smoke.sh`
- Modify: `scripts/wcplus-agent-packaging-smoke.sh`

**Step 1: Write failing secret-boundary tests**

Prove both CLIs prefer an environment token only when explicitly provided, otherwise load `transport-token` from the platform secret store. Assert rendered LaunchAgent files do not contain `KBASE_SOURCE_AGENT_TOKEN` or its value.

This fixes the existing WC Plus installer behavior that places the token in the plist while preserving shared-token semantics.

**Step 2: Verify RED**

```bash
go test ./cmd/source-agent ./cmd/wcplus-agent ./internal/sourceagentsecret -count=1
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
```

Expected: at least the WC Plus secret test/smoke FAILS.

**Step 3: Move reusable Keychain transport-token access**

Keep WeChat MP session storage under its existing `SourceSecretStore`; share only transport-token lookup between command packages. The non-macOS implementation returns a typed unsupported error so later platforms can add their native secret store.

**Step 4: Build and install the updater locally**

Both build scripts produce the worker and `source-agent-updater`. Both installers copy them into a private user-controlled application directory, store the shared transport token in Keychain, omit it from plist, and pass only non-secret local configuration to LaunchAgent.

The source-agent and WC Plus worker use separate labels, install directories, state directories, and logs. Updater configuration remains local and is not accepted from KBase commands.

**Step 5: Re-run packaging and CLI tests**

```bash
go test ./cmd/source-agent ./cmd/wcplus-agent ./cmd/source-agent-updater ./internal/sourceagentsecret -count=1
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
```

Expected: PASS; no token appears in generated plist fixtures.

**Step 6: Commit**

```bash
git add internal/sourceagentsecret cmd/source-agent cmd/wcplus-agent scripts/build-source-agent-macos.sh scripts/build-wcplus-agent-macos.sh scripts/install-source-agent-macos.sh scripts/install-wcplus-agent-macos.sh scripts/source-agent-packaging-smoke.sh scripts/wcplus-agent-packaging-smoke.sh
git commit -m "feat(agent): package independent managed source workers"
```

### Task 10: Make WeChat and WC Plus report the same control protocol truthfully

**Files:**
- Modify: `backend/app/wechat_agent.go`
- Modify: `backend/app/wechat_agent_test.go`
- Modify: `backend/app/wcplus_agent.go`
- Modify: `backend/app/wcplus_agent_test.go`
- Modify: `cmd/source-agent/main.go`
- Modify: `cmd/source-agent/main_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`
- Create: `backend/app/source_agent_protocol_contract_test.go`

**Step 1: Write shared protocol contract tests**

Run the same assertions against both worker fixtures:

- stable worker type, platform, architecture, version, and protocol version;
- bounded capability health;
- no credential or source body in heartbeat/diagnostic JSON;
- pause/upgrade state prevents lease execution;
- independent state and outbox paths;
- command progress follows the same state enum.

**Step 2: Write source-specific blocker tests**

WeChat expired session must report `login_required`. WC Plus XProtect/vendor/local API failures matching the approved bounded classifier must report `vendor_blocked` or `dependency_unavailable`, lease no new WC Plus work, and never return success.

**Step 3: Verify RED**

```bash
go test ./backend/app ./cmd/source-agent ./cmd/wcplus-agent -run 'Test(SourceAgentProtocol|WeChat.*Capability|WCPlus.*Capability)' -count=1
```

Expected: FAIL until both workers use the new runtime metadata and stable codes.

**Step 4: Implement independent control-loop wiring**

Do not merge the adapter processes. Wire each CLI to the shared command runner, its own diagnoser, and its own updater configuration. Keep the existing WeChat enrollment runtime and WC Plus loopback-only validation.

**Step 5: Run source and command regressions**

```bash
go test ./backend/app ./cmd/source-agent ./cmd/wcplus-agent -run 'Test(SourceAgentProtocol|SourceAgent|WeChat|WCPlus)' -count=1
go test -race ./backend/app ./cmd/source-agent ./cmd/wcplus-agent -count=1
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/wechat_agent.go backend/app/wechat_agent_test.go backend/app/wcplus_agent.go backend/app/wcplus_agent_test.go backend/app/source_agent_protocol_contract_test.go cmd/source-agent cmd/wcplus-agent
git commit -m "feat(agent): align WeChat and WC Plus worker control"
```

### Task 10A: Wire command-bound artifact delivery to the fixed local updater

**Why this correction exists:** Task 10 deliberately left both production
workers fail-closed because the command contains only an artifact ID and the
expected current version. The existing artifact download route, staging
filesystem, ready-receipt store, and `SourceAgentUpdateTransaction` are each
safe in isolation, but no trusted local bridge connects them. This task must
finish before the Web exposes an upgrade action; a clickable control backed by
the fail-closed placeholder is not an acceptable delivered capability.

**Files:**
- Modify: `backend/app/source_agent_client.go`
- Modify: `backend/app/source_agent_client_test.go`
- Create: `backend/app/source_agent_update_bridge.go`
- Create: `backend/app/source_agent_update_bridge_test.go`
- Modify: `backend/app/source_agent_runner.go`
- Modify: `backend/app/source_agent_command_runner_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/source-agent/main.go`
- Modify: `cmd/source-agent/main_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`
- Modify: `cmd/source-agent-updater/main.go`
- Modify: `cmd/source-agent-updater/main_test.go`
- Modify: `cmd/source-agent-updater/platform_darwin.go`
- Modify: `cmd/source-agent-updater/platform_other.go`
- Modify: `scripts/source-agent-packaging-smoke.sh`
- Modify: `scripts/wcplus-agent-packaging-smoke.sh`

**Step 1: Write the real handoff contract RED tests**

Cover the full production path, not a fake `SourceAgentUpdater`:

- the authenticated, command-bound download returns artifact metadata from the
  server-side catalog snapshot together with the exact bytes;
- the worker rejects missing, duplicate, malformed, incompatible, oversized,
  or command-mismatched metadata before writing a staged executable;
- staging uses a fixed private directory on the executable filesystem, does
  not follow symlinks, has a bounded byte count, fsyncs the completed file, and
  verifies SHA-256 before publishing a local handoff;
- the local handoff is keyed by the bounded command ID and contains only
  catalog metadata plus locally derived fixed paths; no remote URL, shell,
  script, environment, label, updater path, executable path, or state path is
  accepted from the command or response;
- `source-agent-updater --apply` accepts only a known worker type and command
  ID, derives its install directory from its own executable, reads the
  protected local handoff, invokes `SourceAgentUpdateTransaction`, and writes a
  bounded durable outcome;
- after restart, the worker writes the matching ready receipt only after an
  authenticated heartbeat and only when command, attempt nonce, worker type,
  version, platform, architecture, protocol, and revision all match;
- downloading, verified, installing, restarting, verifying, rollback, success,
  and rolled-back command reports are resumable without repeating download,
  replacement, or rollback side effects;
- disabling rollout after download or after claim stops installation;
- helper crash, restart failure, ready timeout, wrong receipt, hash drift, and
  outcome persistence failure all fail or roll back truthfully.

**Step 2: Verify RED against both real worker constructors**

```bash
go test ./backend/app ./cmd/source-agent ./cmd/wcplus-agent ./cmd/source-agent-updater \
  -run 'TestSourceAgent(UpdateBridge|ArtifactHandoff|ReadyAfterHeartbeat|UpdaterApply|RealWorkerUpgrade)' -count=1
```

Expected: FAIL because the workers still install fail-closed updater stubs and
the updater CLI only supports `--check`.

**Step 3: Bind catalog metadata to the download snapshot**

Return the selected artifact's bounded version, worker type, platform,
architecture, protocol version, revision, channel, size, and SHA-256 from the
same server-side snapshot used to stream bytes. The client must parse these as
strict single-valued response headers and compare them with its compiled
runtime identity and the claimed command. It must never infer trusted metadata
from the command payload or a filename.

**Step 4: Implement the private same-filesystem stage and handoff**

Derive the updater path, current worker executable, staging root, and handoff
root exclusively from local installation/runtime configuration. Download to a
new no-follow file, bound the copy, verify size and SHA-256, sync it, then
atomically publish a strict local handoff. A retry for the same command must
reuse only a fully matching staged identity; conflict fails closed.

**Step 5: Extend the fixed-function updater CLI**

Keep `--check`. Add only `--apply --worker-type <known-worker> --command-id
<bounded-id>`. The helper derives all paths and the fixed LaunchAgent label
locally; it cannot accept a URL, shell command, script, environment override,
label, install path, state path, or executable path. It loads the handoff,
constructs `SourceAgentUpdateRequest`, and executes the existing rollback-safe
transaction. Non-macOS builds return the typed unsupported-platform result.

**Step 6: Wire resumable command stages and authenticated readiness**

Replace both fail-closed updater stubs with the shared bridge while retaining
separate worker state, staging, outcomes, and LaunchAgent identities. Before
claiming the next command, a successfully authenticated heartbeat may satisfy
one matching local ready challenge. Map the durable local transaction stage or
outcome to exactly one allowed server command transition per cycle; never
report success based only on helper process exit.

**Step 7: Run security, recovery, packaging, and race gates**

```bash
go test ./backend/app ./cmd/source-agent ./cmd/wcplus-agent ./cmd/source-agent-updater -count=1
go test -race ./backend/app ./cmd/source-agent ./cmd/wcplus-agent ./cmd/source-agent-updater -count=1
bash scripts/source-agent-artifact-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS. Scans must confirm that no signing mechanism was introduced
and no command/response field can select a URL, path, script, environment, or
LaunchAgent label.

**Step 8: Commit**

Stage only the bridge, worker, updater, tests, packaging smoke changes, and the
regenerated system map. Commit as:

```bash
git commit -m "feat(agent): connect constrained worker upgrades"
```

### Task 11: Add the unified `/sources/agents` overview

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Create: `frontend-web/scripts/source-agent-control-plane-smoke.mjs`
- Modify: `frontend-web/scripts/web-navigation-url-smoke.mjs`

**Step 1: Write the static RED smoke**

Require:

```js
ROUTES.sourceAgents = "/sources/agents"
```

The smoke must assert navigation presence, derived status groups, agent rows, version/protocol fields, capability health, last heartbeat, run/command, outbox/dead-letter counts, pause/resume, diagnose, approved-artifact upgrade controls, and links to the two source workspaces. It must reject custom URL, shell, script, environment, or force-all controls.

**Step 2: Verify RED**

```bash
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
```

Expected: FAIL because the route and UI do not exist.

**Step 3: Add state loading with stale-response protection**

Reuse the repository's request-sequencing/abort pattern. Load the unified agent list and bounded command/artifact metadata without coupling failure of one panel to the others. Poll only while the route is active and an operation/active command warrants it.

**Step 4: Render the overview and actions**

Build accessible status groups and worker cards/table. Every mutating action must show pending state and then refresh authoritative server state; never change a status optimistically. Upgrade requires a confirmation dialog showing worker ID, current version, and target version.

**Step 5: Add responsive styling and navigation**

At narrow width, place status summary first, then attention workers, then the remaining list. Preserve visible focus, semantic buttons, labels, and non-color status text.

**Step 6: Run syntax and Web smokes**

```bash
node --check frontend-web/app.js
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
node frontend-web/scripts/web-navigation-url-smoke.mjs
```

Expected: PASS.

**Step 7: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/source-agent-control-plane-smoke.mjs frontend-web/scripts/web-navigation-url-smoke.mjs
git commit -m "feat(web): add source worker control overview"
```

### Task 12: Add stable worker details and source-workspace deep links

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/source-agent-control-plane-smoke.mjs`
- Modify: `frontend-web/scripts/wechat-source-ui-smoke.mjs`
- Modify: `frontend-web/scripts/wcplus-source-ui-smoke.mjs`

**Step 1: Extend the smoke for `/sources/agents/{agent_id}`**

Require direct-load and history navigation behavior, URL-safe agent IDs, capability details, bound subscriptions, recent runs/items, command timeline, outbox statistics, redacted diagnostics, and links to the correct source-specific page.

**Step 2: Verify RED**

```bash
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
```

Expected: FAIL on missing detail behavior.

**Step 3: Implement detail routing and isolated loading**

Add one path helper that decodes and validates the agent ID. Use independent request sequencing so an old detail response cannot overwrite a newly selected worker. A missing worker gets a bounded not-found state without falling back to another agent.

**Step 4: Preserve source-specific ownership**

The detail page links to WeChat enrollment/discovery or WC Plus diagnostics; it does not render credential forms. Update existing source pages to include a return link to the stable worker detail.

**Step 5: Run all relevant Web smokes**

```bash
node --check frontend-web/app.js
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
node frontend-web/scripts/wechat-source-ui-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
```

Expected: PASS.

**Step 6: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/source-agent-control-plane-smoke.mjs frontend-web/scripts/wechat-source-ui-smoke.mjs frontend-web/scripts/wcplus-source-ui-smoke.mjs
git commit -m "feat(web): add source worker detail workspace"
```

### Task 13: Add end-to-end fixtures and upgrade rollback integration gates

**Files:**
- Create: `scripts/source-agent-control-plane-smoke.sh`
- Create: `backend/app/testdata/source-agent-artifacts/catalog.json`
- Create: `backend/app/testdata/source-agent-protocol/wechat-heartbeat.json`
- Create: `backend/app/testdata/source-agent-protocol/wcplus-heartbeat.json`
- Modify: `.github/workflows/kbase-build-gates.yml`

**Step 1: Write the failing integration smoke contract**

The smoke must start a temporary KBase server and two fixture workers with separate state directories, then verify:

1. both register independently;
2. pause prevents only the targeted worker from leasing;
3. diagnose produces a bounded structured result;
4. one worker can fail while the other continues;
5. valid artifact download is command-bound;
6. altered artifact is rejected;
7. a fake updater failure restores the old executable;
8. WC Plus `vendor_blocked` leases no work and is not success;
9. no fixture contains a real token, cookie, source body, or machine path.
10. disabling rollout after claim prevents installation;
11. the artifact promoted from staging to production has the same SHA-256.

**Step 2: Verify RED**

```bash
bash scripts/source-agent-control-plane-smoke.sh
```

Expected: FAIL until the full integration surface exists.

**Step 3: Implement sanitized deterministic fixtures**

Use generated temporary credentials and loopback servers only. Artifact fixture bytes must be a tiny test executable created during the smoke, not a committed binary.

**Step 4: Add the smoke to normal build gates**

Run it after Go tests and before privacy/system-map checks. Do not add signing or retired release-kit dependencies.

**Step 5: Run the complete local verification set**

```bash
bash scripts/source-agent-control-plane-smoke.sh
bash scripts/source-agent-artifact-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
go mod verify
go vet ./...
go test ./... -count=1
node --check frontend-web/app.js
```

Then run every repository Web smoke directly, without piping through plain `tail`.

Expected: PASS.

**Step 6: Commit**

```bash
git add scripts/source-agent-control-plane-smoke.sh backend/app/testdata/source-agent-artifacts/catalog.json backend/app/testdata/source-agent-protocol .github/workflows/kbase-build-gates.yml
git commit -m "test(kbase): gate source worker control plane"
```

### Task 14: Synchronize documentation, system map, and operational runbooks

**Files:**
- Modify: `README.md`
- Modify: `docs/system-map/product-map.md`
- Regenerate: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-08-01-unified-source-agent-control-plane.md`

**Step 1: Update operator documentation**

Document:

- `/sources/agents` and worker details;
- shared-token limitations and rotation blast radius;
- independent worker installation and local secret storage;
- manual-by-default operation;
- constrained upgrade/catalog preparation;
- automatic local rollback;
- WC Plus `vendor_blocked` layered acceptance;
- Windows/Linux protocol-only status.

Use placeholders and environment-variable contracts, never developer-local paths or real hosts/tokens.

**Step 2: Regenerate structural inventory**

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

Expected: PASS; all new routes, command entry points, and durable objects come from generated code inventory rather than hand-maintained counts.

**Step 3: Complete G3 evidence and prepare G4**

Record exact commands and results. Do not mark G4 PASS before review. Record any rejected finding and remediation in the dossier.

**Step 4: Run privacy and diff checks**

```bash
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: PASS; only intended documentation and generated inventory remain.

**Step 5: Commit**

```bash
git add README.md docs/system-map/product-map.md docs/_generated/system-map.json docs/dossiers/2026-08-01-unified-source-agent-control-plane.md
git commit -m "docs(kbase): document source worker operations"
```

### Task 15: Complete independent review and clean-main candidate verification

**Files:**
- Modify after evidence: `docs/dossiers/2026-08-01-unified-source-agent-control-plane.md`

**Step 1: Request review of four explicit risk areas**

Review must cover:

1. shared-token identity spoofing and authorization boundaries;
2. arbitrary-execution resistance of command, artifact, and updater paths;
3. updater rollback and concurrent-state correctness;
4. source credential/content privacy and WC Plus truthful blocker handling.

**Step 2: Remediate every Critical/High/Medium finding with RED/GREEN evidence**

Do not proceed while G4 is rejected or blocked.

**Step 3: Run clean-main verification**

From an isolated worktree containing the exact candidate revision:

```bash
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
git diff --check
```

Run all `frontend/scripts/*-smoke.mjs` and `frontend-web/scripts/*smoke*.mjs` directly.

Expected: every command PASS and the tree remains clean.

**Step 4: Update the dossier and commit evidence**

```bash
git add docs/dossiers/2026-08-01-unified-source-agent-control-plane.md
git commit -m "docs(kbase): record source control plane review"
```

### Task 16: Deploy in layers and perform G5/G6 verification

**Files:**
- Modify after evidence: `docs/dossiers/2026-08-01-unified-source-agent-control-plane.md`

**Step 1: Deploy KBase from a clean canonical main revision**

Use the active direct-deployment runbook. Replace only the KBase binary and Web tree, retain one root-owned binary/Web backup batch, and require exact revision health. Do not change environment, browser-session data, knowledge data, Nginx, or worker credentials in the code deployment.

**Step 2: Prepare the private artifact catalog separately**

Build macOS worker/updater artifacts from the exact reviewed revision, record size and SHA-256, validate the catalog with `scripts/source-agent-artifact-smoke.sh`, and install it through a scoped operational step. Do not expose the artifact root or write a real path into repository documentation.

Promote the same verified bytes from staging to production. Do not rebuild
between channels. Because these artifacts are deliberately unsigned, update
one worker at a time through explicit browser confirmation; never enable
scheduled, broadcast, or silent installation.

**Step 3: Locally bootstrap both macOS updaters**

Use the reviewed local installers. Confirm:

- separate LaunchAgent labels and state directories;
- shared token present only in Keychain;
- no token in plist, logs, arguments, or environment after launch;
- both workers heartbeat independently.

**Step 4: Exercise real constrained upgrades**

From `/sources/agents`, upgrade WeChat and WC Plus independently. Require actual version change, successful ready receipt, new heartbeat, and no unintended source lease during upgrade. Test rollback with a deliberately failing artifact only in an isolated local fixture environment, not by deploying a corrupt production binary.

**Step 5: Perform bounded WeChat G6**

With explicit operator enrollment and account choice:

1. run one bounded collection;
2. verify new items and original-article citations;
3. rerun and verify unchanged items skip;
4. interrupt/restart once and verify cursor/outbox truth;
5. confirm no credential or source body reaches KBase logs or diagnostics.

**Step 6: Perform layered WC Plus G6**

Run fixture end-to-end verification. On the production Mac, if the legitimate dependency remains unavailable, require `vendor_blocked`, zero leased WC Plus work, and no success claim. Do not bypass XProtect, licensing, or vendor controls. Open a separate future dossier for real WC Plus acquisition activation.

**Step 7: Verify KBase and public contracts**

Require service active/running, installed hash exact, loopback/public health exact, `/sources/agents` and static assets HTTP 200, anonymous protected routes HTTP 401, management mutations protected by cookie/CSRF, worker routes protected by the shared agent token, and no deployment-window panic/fatal/start failure.

**Step 8: Close the dossier and commit final evidence**

Mark G5/G6 PASS only when every in-scope acceptance condition is evidenced. Record rollback locations operationally without committing machine-specific absolute paths.

```bash
bash scripts/privacy-smoke.sh
git diff --check
git add docs/dossiers/2026-08-01-unified-source-agent-control-plane.md
git commit -m "docs(kbase): record source control plane rollout"
```

---

## Completion Boundary

This plan completes only phase 1 of the approved product sequence. Do not start knowledge-quality or experience implementation inside this branch. After G6, begin a new brainstorming/design cycle for:

1. retrieval, deduplication, citation trust, and conflict detection;
2. initial setup, one-action workflows, notifications, and mobile usability.
