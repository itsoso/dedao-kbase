# KBase Direct Deployment Dossier

**Date:** 2026-07-31  
**Status:** Delivery in progress  
**Design:** `docs/plans/2026-07-31-kbase-direct-deployment-design.md`  
**Implementation plan:** `docs/plans/2026-07-31-kbase-direct-deployment.md`

## Outcome

KBase is returning to the direct Linux deployment model used before the signed
release-kit pipeline. Active release signing, prepared bundles, and the
transactional installer are retired. Normal application build, test, privacy,
system-map, and Nginx gates remain active.

Production deployment and online verification have not yet run.

## Scope

In scope:

- retire the active signed release-kit scripts and dedicated workflow;
- retain normal build/test CI without signing or installer fixtures;
- document exact-revision direct deployment;
- deploy only the KBase server binary and static Web tree;
- retain scoped pre-replacement backups and immediate detected-failure
  recovery;
- verify the target revision and HTTP contracts in production.

Out of scope:

- environment, token, browser credential, or Nginx configuration changes;
- knowledge data or generated knowledge artifact changes;
- history rewriting of the release-kit plans and rollout records;
- production trust-root replacement.

## G1 - Admission

**Decision: PASS**

The operator explicitly selected the direct deployment model after being shown
three alternatives:

1. remove only the external signature dependency;
2. make signing optional;
3. restore the earlier direct build, backup, replace, and restart flow.

The selected option is 3.

## G2 - Feasibility and Risk Pressure Test

**Decision: PASS with explicit risk acceptance**

The direct flow keeps:

- a clean exact canonical revision;
- SHA-256 comparison before and after upload;
- unprivileged Linux tests and build;
- scoped backups of both replacement targets;
- immediate restore when replacement, restart, or loopback health fails;
- local and public post-deployment checks.

It intentionally does not keep:

- artifact publisher authentication;
- immutable prepared-release staging;
- deployment locking;
- durable transaction journaling;
- fsync-backed replacement boundaries;
- automatic recovery after forced termination or host power loss;
- archive resource ceilings from the retired preparer and installer.

Concurrent deployments are forbidden. An interrupted replacement requires
manual restoration from the retained backup.

## Definition and Design Evidence

- The signed manifest path first entered the repository in the
  `fix(kbase): authenticate release artifacts` change on 2026-07-29.
- A later transaction-hardening change required an external KMS or offline
  signing boundary.
- Earlier production dossiers record the prior direct Linux build and scoped
  binary/Web replacement model.
- The approved replacement design is committed separately from implementation.

## G3 - Test

**Decision: IN PROGRESS**

TDD contract evidence:

- RED: `bash scripts/kbase-direct-deployment-smoke.sh` rejected the first
  remaining active release-kit file.
- GREEN: after retiring the release kit and adding normal build gates, the same
  smoke passed.
- `bash scripts/privacy-smoke.sh` passed.
- `git diff --check` passed.

Pending before delivery:

- complete frontend builds and smokes;
- Go module verification, vet, and tests;
- real Nginx proxy smoke on Linux;
- generated system-map drift check;
- clean-tree verification.

## G4 - Review

**Decision: PENDING**

Review must confirm:

- no active signing, manifest, prepared-release, or transactional installer
  dependency remains;
- normal build/test CI gates were preserved;
- the direct runbook does not include credentials or developer-local paths;
- the replacement scope is limited to binary and Web targets;
- rollback restores both targets from the same backup batch.

## Delivery Changes

Retired active surfaces:

- the KBase release-gate workflow;
- source assembly and prepared-release scripts;
- signature handling;
- transactional installation;
- release archive, staging, fsync, and installer smoke fixtures.

Replacement active surfaces:

- `.github/workflows/kbase-build-gates.yml`;
- `scripts/kbase-direct-deployment-smoke.sh`;
- the `KBase direct deployment` README runbook.

Historical plans and dossiers remain unchanged as audit evidence.

## Candidate Evidence

- Canonical revision: `PENDING`
- Source archive SHA-256: `PENDING`
- Linux candidate binary SHA-256: `PENDING`
- Pre-deployment production revision: `PENDING`

## G5 - Deployment Health

**Decision: PENDING**

Required evidence:

- pre-mutation service and loopback/public health;
- one retained backup containing the previous binary and Web tree;
- installed binary hash matching the Linux candidate;
- service active with successful main process;
- loopback health returning the exact canonical revision;
- immediate recovery evidence if any replacement step fails.

## G6 - Online Verification

**Decision: PENDING**

Required evidence:

- public health returning the exact canonical revision;
- public static entry points returning HTTP 200;
- protected anonymous endpoints returning HTTP 401;
- authenticated missing-book chat returning HTTP 404 with the public
  `book not found` message and without storage-path disclosure;
- no panic, fatal error, segmentation fault, or failed startup in the
  deployment-window logs;
- active release tools moved to a retained root-owned backup after G6.

## Rollback

The rollout will retain one timestamped root-owned backup containing:

- the previously installed KBase server binary;
- the previously installed `frontend-web` tree.

To roll back:

1. restore both targets from that same backup;
2. restart the KBase service;
3. require successful loopback health;
4. require successful public health;
5. verify the restored revision and recent logs.

Backup identity and exact verification results will be recorded after
deployment. No environment, Nginx, credential, or knowledge-data rollback is
part of this rollout.
