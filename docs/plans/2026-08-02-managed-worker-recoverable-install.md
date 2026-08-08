# Recoverable Managed Worker Installation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Task 9 macOS worker installation stdin-secret-safe, backward compatible, crash recoverable, and fully rollbackable.

**Architecture:** Keep installer orchestration in the two existing scripts, add one shared shell library for the worker/updater pair transaction, and make narrowly scoped Go changes for configuration validation, source-only legacy loading, and signal-aware startup. Tests are added before each production change.

**Tech Stack:** Bash 3.2, macOS launchd and Keychain CLI, Go 1.23, existing shell smoke harnesses.

---

### Task 1: Secure stdin-only installer startup

**Files:**
- Modify: `scripts/source-agent-packaging-smoke.sh`
- Modify: `scripts/wcplus-agent-packaging-smoke.sh`
- Modify: `scripts/install-source-agent-macos.sh`
- Modify: `scripts/install-wcplus-agent-macos.sh`

1. Add direct-execution hostile-environment fixtures for `BASH_ENV`, `ENV`, inherited xtrace, admin and secret aliases, fake `grep`, and stdin-only Keychain input.
2. Run both packaging smokes and confirm they fail at the new assertions.
3. Add the clean `env -S` shebang, builtin-only startup cleanup, stdin read, and ASCII/length validation.
4. Run both packaging smokes and confirm the startup fixtures pass.

### Task 2: Source legacy fallback and signal-aware loaders

**Files:**
- Modify: `cmd/source-agent/main.go`
- Modify: `cmd/source-agent/main_test.go`
- Modify: `cmd/source-agent/keychain_store_darwin.go`
- Modify: `cmd/source-agent/keychain_store_darwin_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`
- Modify: `internal/sourceagentsecret/*`

1. Add failing tests for source-only fixed-missing fallback, corrupt-fixed fail-closed behavior, bounded/redacted legacy values, and cancellation of blocked loaders.
2. Run focused tests and confirm the expected failures.
3. Implement the minimal fixed-then-legacy source loader and pass caller contexts from both entrypoints.
4. Run focused and race tests.

### Task 3: Strict configuration-only validation

**Files:**
- Modify: `backend/app/source_agent_client.go`
- Modify: `backend/app/source_agent_client_test.go`
- Modify: `cmd/source-agent/main.go`
- Modify: `cmd/source-agent/main_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`
- Modify: both installer scripts and packaging smokes

1. Add failing URL, enrollment-address, no-Keychain, and vendor-blocked configuration-check tests.
2. Implement shared strict URL normalization and limited `check-config` commands.
3. Replace shell URL patterns with pre-mutation binary checks and rerun tests.

### Task 4: Shared recoverable pair transaction

**Files:**
- Create: `scripts/lib/managed-worker-pair.sh`
- Create: `scripts/managed-worker-pair-smoke.sh`
- Modify: both build scripts and both installer scripts
- Modify: both packaging smokes

1. Add a failing fault matrix for backup, first/second rename, removal, sync, rollback, SIGTERM, SIGKILL recovery, missing old sides, hashes, and cleanup.
2. Implement fixed-basename locking, bounded journal parsing, recovery, rollback, and commit.
3. Replace four duplicated pair functions and rerun the matrix plus both packaging smokes.

### Task 5: Complete installer rollback

**Files:**
- Modify: both installer scripts
- Modify: both packaging smokes

1. Add failing fake-install matrices for every updater, Keychain, plist, validation, and launchctl boundary, plus concurrent installers.
2. Add the shared-account lock and retain pair/plist/Keychain/service rollback state until final health confirmation.
3. Verify every injected failure restores old state and that success removes all transaction state.

### Task 6: Temporary-file uniqueness and final verification

**Files:**
- Modify: both installer scripts and packaging smokes
- Regenerate if changed: `docs/_generated/system-map.json`

1. Add failing consecutive-temp-name checks and move `XXXXXX` to the suffix.
2. Run focused/race tests, all transaction and packaging smokes, real temporary builds and fake installs, backend-safe tests, vet, frontend build, system-map generation and drift check, privacy smoke, and `git diff --check`.
3. Review and stage only Task 9 files, then commit the verified changes.
