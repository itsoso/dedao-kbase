# KBase Release Kit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the verified KBase release procedure into a repository-native,
testable assemble/prepare/install pipeline with automatic rollback.

**Architecture:** An exact Git source archive becomes a verified Linux install
bundle. The server exposes a side-effect-free configuration check, and a
generic installer consumes explicit host inputs rather than committed
production paths.

**Tech Stack:** Go 1.23, Bash, npm/Vite, systemd, Nginx, GitHub Actions.

---

### Task 1: Side-effect-free server configuration doctor

**Files:**
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`

1. Add failing tests for `--check-config` success, invalid secret separation,
   invalid public Origin, no listener creation, and JSON output without secret
   values.
2. Run the focused tests and confirm they fail because the flag is absent.
3. Refactor startup validation into a shared function and implement
   `--check-config`.
4. Run focused tests, then `go test ./cmd/kbase-server -count=1`.
5. Commit only the two server files.

### Task 2: Exact source assembly

**Files:**
- Create: `deploy/kbase/assemble-release.sh`
- Create: `deploy/kbase/release-manifest-smoke.sh`

1. Write smoke assertions for clean revision enforcement, manifest schema,
   relative artifact paths, digest verification, and tamper rejection.
2. Run the smoke and confirm it fails because the assembler is absent.
3. Implement exact Git archive assembly and a deterministic public manifest.
4. Run the smoke and confirm success.
5. Commit the assembler and smoke.

### Task 3: Linux release preparation

**Files:**
- Create: `deploy/kbase/prepare-release.sh`
- Modify: `deploy/kbase/release-manifest-smoke.sh`

1. Extend the smoke with a fixture proving source-digest rejection and prepared
   component manifest generation.
2. Confirm the new assertions fail.
3. Implement locked Vue build, Web smokes, Go gates, CGO server build, proxy
   smoke, and install-bundle hashing. Allow explicit command paths for CI.
4. Run the fixture mode and real local preparation where dependencies exist.
5. Commit the preparation stage and updated smoke.

### Task 4: Transactional installer

**Files:**
- Create: `deploy/kbase/install-release.sh`
- Create: `deploy/kbase/install-release-smoke.sh`

1. Create fake service, health, and Nginx commands plus temporary install
   targets. Assert a successful install replaces every component.
2. Confirm the smoke fails because the installer is absent.
3. Implement manifest verification, configuration doctor invocation,
   snapshots, replacement, health validation, and rollback.
4. Add a forced-health-failure case and prove all targets are restored.
5. Commit installer and smoke.

### Task 5: CI release gates

**Files:**
- Create: `.github/workflows/kbase-release-gates.yml`
- Modify: `README.md`

1. Add a static smoke that requires normal-user Go tests, root permission-test
   coverage, frontend build, release smokes, privacy smoke, and system-map
   smoke in the workflow.
2. Confirm it fails before the workflow exists.
3. Add a secret-free workflow using pinned major toolchain versions and the
   runner's Nginx.
4. Document assemble, prepare, install, and rollback commands with placeholders
   only.
5. Commit workflow and documentation.

### Task 6: Final gates and review

**Files:**
- Modify: `docs/_generated/system-map.json`
- Create: `docs/dossiers/2026-07-29-kbase-release-kit.md`

1. Regenerate the system map because the server command surface changed.
2. Run focused release smokes, full Go tests, race tests, `go vet`, module
   verification, Vue build, all Web smokes, privacy smoke, and drift checks.
3. Request independent correctness and security reviews; remediate every
   actionable P0-P2 finding.
4. Record G1-G4 evidence and leave G5-G6 pending explicit release authorization.
5. Commit generated metadata and dossier.

