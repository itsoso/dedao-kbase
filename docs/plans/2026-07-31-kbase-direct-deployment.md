# KBase Direct Deployment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the KBase signed release-kit path, restore the earlier direct Linux deployment flow, and deploy the current canonical revision with scoped backup and immediate failure recovery.

**Architecture:** Active repository automation will retain normal build, test, privacy, system-map, and Nginx gates but remove source/prepared manifests, signatures, and the transactional installer. Production delivery will archive one clean `main` revision, repeat the build on Linux as the service user, then directly replace only the server binary and static Web tree after creating timestamped backups.

**Tech Stack:** Bash, Git archive, GitHub Actions, Go 1.23, Node.js/npm, systemd, Nginx, SSH/SCP, SHA-256.

---

### Task 1: Define the active direct-deployment contract

**Files:**
- Create: `scripts/kbase-direct-deployment-smoke.sh`

**Step 1: Write the failing contract smoke**

Create an executable Bash smoke that:

- rejects every active file under `deploy/kbase/`;
- rejects `.github/workflows/kbase-release-gates.yml`;
- requires `.github/workflows/kbase-build-gates.yml`;
- rejects active README references to `MANIFEST.sig`, `release-signature.sh`,
  `prepare-release.sh`, and `install-release.sh`;
- requires README instructions for exact revision archive, SHA-256 verification,
  unprivileged Linux tests/build, scoped backup, service restart, immediate
  restore, and local/public health checks.

The script must use `set -Eeuo pipefail`, print a clear failing path or missing
contract, and exit non-zero.

**Step 2: Run the smoke and verify RED**

Run:

```bash
bash scripts/kbase-direct-deployment-smoke.sh
```

Expected: FAIL because the signed release-kit files and workflow still exist.

**Step 3: Commit the RED contract**

```bash
git add scripts/kbase-direct-deployment-smoke.sh
git commit -m "test(kbase): define direct deployment contract"
```

### Task 2: Remove the signed release kit and keep normal CI gates

**Files:**
- Delete: `.github/workflows/kbase-release-gates.yml`
- Create: `.github/workflows/kbase-build-gates.yml`
- Delete: `deploy/kbase/archive-list-smoke.sh`
- Delete: `deploy/kbase/archive-list.mjs`
- Delete: `deploy/kbase/assemble-release.sh`
- Delete: `deploy/kbase/fsync-paths.mjs`
- Delete: `deploy/kbase/install-release-smoke.sh`
- Delete: `deploy/kbase/install-release.sh`
- Delete: `deploy/kbase/prepare-release.sh`
- Delete: `deploy/kbase/release-manifest-smoke.sh`
- Delete: `deploy/kbase/release-signature.sh`
- Delete: `deploy/kbase/stage-files-smoke.sh`
- Delete: `deploy/kbase/stage-files.mjs`
- Modify: `README.md`

**Step 1: Add the normal build-gate workflow**

Create `KBase Build Gates` for pull requests and `main` pushes. Preserve these
steps from the old workflow:

- pinned checkout, Go, and Node setup actions;
- Linux Wails/CGO and Nginx dependencies;
- normal-user execution assertion;
- `go mod verify`;
- `frontend` install/build and all frontend smokes;
- `go vet ./...` and `go test ./...`;
- the root-only permission ceiling test;
- `frontend-web` syntax and all Web smokes;
- CGO server build and the real Nginx proxy smoke;
- privacy and generated system-map checks.

Do not generate keys, manifests, release archives, prepared bundles, or install
fixtures.

**Step 2: Replace the active README release runbook**

Replace `KBase release kit` with `KBase direct deployment`. Document
environment-variable placeholders and the following command sequence:

1. verify clean exact `main`;
2. `git archive` that revision;
3. calculate and re-check SHA-256;
4. upload and extract into a private host directory;
5. run Node/Go gates and build with
   `-ldflags "-X main.buildRevision=${KBASE_REVISION}"` as the service user;
6. back up only the installed binary and Web tree;
7. replace both targets and restart;
8. restore both backups immediately if restart or loopback health fails;
9. verify public health, routes, authentication boundaries, and logs.

State explicitly that this flow has no signing, durable journal, deployment
lock, fsync transaction, or power-loss recovery. Do not include a real host,
credential, token, or local absolute path.

**Step 3: Delete the retired release-kit files**

Delete only the files listed in this task. Preserve `deploy/nginx/`, historical
plans, and historical dossiers.

**Step 4: Run the contract smoke and verify GREEN**

Run:

```bash
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add \
  .github/workflows/kbase-build-gates.yml \
  .github/workflows/kbase-release-gates.yml \
  README.md \
  deploy/kbase \
  scripts/kbase-direct-deployment-smoke.sh
git commit -m "build(kbase): restore direct deployment"
```

### Task 3: Record the lifecycle and rollback contract

**Files:**
- Create: `docs/dossiers/2026-07-31-kbase-direct-deployment.md`

**Step 1: Create the dossier**

Record:

- G1 decision and the explicit selection of the direct model;
- G2 risk acceptance: no artifact authenticity, crash-safe transaction,
  concurrency lock, or power-loss recovery;
- the files and workflow retired;
- RED/GREEN evidence;
- G3/G4 results;
- exact revision and SHA-256 fields to be filled after the canonical push;
- G5/G6 deployment evidence placeholders;
- retained backup and manual rollback placeholders.

Do not rewrite the historical release-kit dossier.

**Step 2: Run documentation checks**

Run:

```bash
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: PASS.

**Step 3: Commit**

```bash
git add \
  docs/plans/2026-07-31-kbase-direct-deployment-design.md \
  docs/plans/2026-07-31-kbase-direct-deployment.md \
  docs/dossiers/2026-07-31-kbase-direct-deployment.md
git commit -m "docs(kbase): record direct deployment gates"
```

### Task 4: Run the complete repository verification

**Files:**
- Verify only; no expected source changes.

**Step 1: Run narrow direct-deployment checks**

```bash
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/privacy-smoke.sh
bash scripts/system-map-smoke.sh
git diff --check
```

Expected: PASS.

**Step 2: Run frontend checks**

```bash
cd frontend
npm ci
npm run build
for smoke in scripts/*-smoke.mjs; do node "$smoke"; done
```

Then from the repository root:

```bash
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
```

Expected: PASS.

**Step 3: Run Go and Nginx checks**

```bash
go mod verify
go vet ./...
go test ./...
CGO_ENABLED=1 go build -trimpath -o "${KBASE_LOCAL_BUILD:?}" ./cmd/kbase-server
KBASE_SERVER_BIN="${KBASE_LOCAL_BUILD:?}" \
  NGINX_BIN="${NGINX_BIN:?}" \
  bash deploy/nginx/browser-session-proxy-smoke.sh
```

Expected: PASS. If macOS cannot provide a compatible Nginx binary, record that
check as Linux-only and run it during Task 6 before production mutation.

**Step 4: Check the final tree**

```bash
git status --short
git diff --check
```

Only expected generated frontend output may exist, and it must not be
committed.

### Task 5: Publish the direct-deployment revision

**Files:**
- Update: `docs/dossiers/2026-07-31-kbase-direct-deployment.md`

**Step 1: Rebase or fast-forward against canonical `main`**

Fetch the canonical remote, confirm that the worktree is clean, and require the
candidate to contain current `main` without dropping commits.

**Step 2: Run the privacy and diff gates again**

```bash
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: PASS and clean.

**Step 3: Push the exact candidate to canonical `main`**

Push only after Tasks 1–4 pass. Record the resulting full revision in the
dossier.

**Step 4: Verify the remote revision**

Fetch again and require local `HEAD` and canonical `main` to be identical.

### Task 6: Build the exact revision on Linux

**Files:**
- Production mutation: none in this task.

**Step 1: Confirm production preflight**

Read-only checks must show:

- existing service active with successful main process;
- current loopback and public health healthy;
- enough free space;
- required Node, npm, Go toolchain, compiler, tar, and SHA-256 tools present;
- no concurrent deployment in progress.

**Step 2: Archive and upload**

Create a `git archive` from the exact canonical revision, calculate SHA-256,
upload it to a new private temporary directory, and verify the same digest on
the host.

**Step 3: Extract and assign unprivileged ownership**

Extract into a new mode `0700` directory owned by the KBase service account.
Do not reuse an old extraction directory.

**Step 4: Run Linux gates as the service account**

Run:

- `npm ci`, frontend build, and all frontend smokes;
- `node --check` and all `frontend-web` smokes;
- `go mod verify`, `go vet ./...`, and `go test ./...`;
- CGO server build with the full revision embedded;
- real Nginx proxy smoke using the candidate binary.

Use the checksummed alternate Go proxy already validated for this host only if
the default module endpoint remains unreachable.

**Step 5: Record candidate hashes**

Record the archive SHA-256 and Linux binary SHA-256. Confirm the candidate
binary reports the exact target revision through a temporary loopback health
probe before replacing production.

### Task 7: Directly deploy with scoped backup and immediate recovery

**Files:**
- Production targets: the existing KBase server binary and static Web tree.

**Step 1: Create timestamped backups**

As root, create one root-owned backup directory and copy the current binary and
Web tree into it. Verify both backups exist before any replacement.

**Step 2: Stage replacement bytes on the target filesystem**

Copy the candidate binary and candidate Web tree to root-owned temporary names
next to their final targets. Verify the candidate binary hash again.

**Step 3: Replace and restart**

Replace only the binary and Web targets, restart the service, and request
loopback `/health`.

**Step 4: Recover immediately on failure**

If replacement, restart, service state, or loopback health fails:

- restore both targets from the same backup;
- restart the previous service;
- verify previous loopback health;
- report G5 failure and stop before public promotion.

Do not continue after a failed recovery.

**Step 5: Verify G5**

Require:

- loopback health reports the exact target revision;
- service active, `ExecMainStatus=0`, and no unexpected restart increase;
- installed binary hash equals the recorded Linux candidate hash;
- the retained backup is complete.

### Task 8: Complete online verification and retire active release tools

**Files:**
- Update: `docs/dossiers/2026-07-31-kbase-direct-deployment.md`

**Step 1: Verify G6**

Require:

- public health reports the exact revision;
- `/`, `/book-knowledge`, `/sources/dedao/courses`, and `/app.js` return 200;
- anonymous protected API and browser-session requests return 401;
- an authenticated missing-book chat request returns 404 with the public
  `book not found` message and does not expose storage paths;
- recent service logs contain no panic, fatal error, segmentation fault, or
  failed startup.

Never print or pass credentials in process arguments.

**Step 2: Retire the installed release tools recoverably**

After G6 passes, move the root-owned active release-tool directory to a
timestamped root-owned backup name. Do not delete it. Confirm no service or
active runbook references the retired path.

**Step 3: Fill the dossier evidence**

Record exact revision, archive and binary hashes, backup names, G5/G6 results,
and the manual rollback procedure. Do not record credentials or machine-local
developer paths.

**Step 4: Verify and commit the rollout evidence**

```bash
bash scripts/privacy-smoke.sh
git diff --check
git add docs/dossiers/2026-07-31-kbase-direct-deployment.md
git commit -m "docs(kbase): record direct production rollout"
```

Push the evidence commit after verification.
