# KBase Direct Deployment Dossier

**Date:** 2026-07-31

**Completed:** 2026-08-01

**Status:** Delivered and verified

**Design:** `docs/plans/2026-07-31-kbase-direct-deployment-design.md`

**Implementation plan:** `docs/plans/2026-07-31-kbase-direct-deployment.md`

## Outcome

KBase returned to the direct Linux deployment model used before the signed
release-kit pipeline. Active release signing, prepared bundles, and the
transactional installer are retired. Normal application build, test, privacy,
system-map, and Nginx gates remain active.

Revision `58239c14ebd5465d616ec42a7ed1bc07aa4d7eb6` is deployed and passed
G5/G6. Only the server binary and static Web tree changed. The previous
revision remains available in one root-owned backup batch.

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

**Decision: PASS**

TDD contract evidence:

- RED: `bash scripts/kbase-direct-deployment-smoke.sh` rejected the first
  remaining active release-kit file.
- GREEN: after retiring the release kit and adding normal build gates, the same
  smoke passed.
- `bash scripts/privacy-smoke.sh` passed.
- `git diff --check` passed.
- `bash scripts/system-map-smoke.sh` passed.
- `npm ci`, the Vue production build, and every `frontend` smoke passed.
- `node --check frontend-web/app.js` and every `frontend-web` smoke passed.
- `go mod verify`, `go vet ./...`, and `go test ./...` passed. The complete Go
  suite was rerun outside the local sandbox because its tests require local
  listeners, external DNS, and macOS Keychain access.
- GitHub `KBase Build Gates` run `30692347489` passed for the exact canonical
  revision.
- The Linux host repeated frontend, Web, Go, CGO build, and real Nginx proxy
  smoke as the unprivileged service account. The proxy smoke passed; Nginx
  emitted its known non-root error-log warning while validating the private
  fixture configuration.

## G4 - Review

**Decision: PASS after one remediation**

Review confirmed that no active signing, manifest, prepared-release, or
transactional installer dependency remains. Normal CI retains frontend, Web,
Go, Nginx, privacy, and generated-system-map gates. Historical plans and
dossiers remain unchanged.

The first review rejected the README replacement block because it described
recovery without installing an automatic error trap. A new RED contract check
required `rollback_direct_deployment`, the runbook added the trap and handled
the missing-Web replacement window, and the contract returned GREEN. The final
diff and workflow syntax checks passed with no remaining release-kit reference
in active scripts, CI, or current runbook instructions.

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

- Canonical and deployed revision:
  `58239c14ebd5465d616ec42a7ed1bc07aa4d7eb6`
- Source archive SHA-256:
  `2f7a63951f8188e2e74f6bbb7f94b1f524cd681283172024a24095d4afd009d2`
- Linux candidate and installed binary SHA-256:
  `bb40b69f45bef50ad2c4c1f8e6c2a015ba32ab39fd5349786a1c39717b4c4751`
- Pre-deployment production revision:
  `0dcc0f5c45af6c8a9bf47f6025635bddee7b6857`
- Pre-deployment binary SHA-256:
  `1728f96854878b26810d3b5c763bb750e4a7b0e93ddbcaea921c934ab09805e6`

## G5 - Deployment Health

**Decision: PASS**

- Immediately before mutation, loopback and public health returned the previous
  revision; the service was active with `ExecMainStatus=0` and `NRestarts=0`.
- No concurrent build process was running under the service account.
- The old binary and Web tree were copied into
  `/opt/dedao-kbase/backups/direct-58239c1-20260801T090514Z` before either
  target changed. The backup is root-owned with mode `0700`; its binary hash
  matches the recorded previous binary.
- Candidate bytes were staged on the target filesystem and the candidate hash
  was reverified before switching.
- The direct replacement command installed an automatic error trap that would
  restore both backup targets and require the previous revision's loopback
  health on any replacement, restart, active-state, hash, or health failure.
- The replacement did not trigger the rollback path. Loopback health returned
  the exact target revision, the installed hash matched the Linux candidate,
  and the service was `active/running` with `ExecMainStatus=0` and
  `NRestarts=0`.

## G6 - Online Verification

**Decision: PASS**

- Public `/health` returned the exact target revision and `dedao-kbase`
  service contract.
- `/`, `/book-knowledge`, `/sources/dedao/courses`, and `/app.js` returned
  HTTP 200.
- Anonymous `/api/books` and `/browser/session` returned HTTP 401.
- A root-local authenticated `/api/book-chat` probe read the API token in
  process memory without printing it or placing it in process arguments. A
  deliberately absent book returned HTTP 404, contained `book not found`, and
  contained no `manifest.json`, `book_knowledge`, or production-root path.
- `nginx -t` passed. Existing duplicate-name warnings for unrelated virtual
  hosts remain visible and did not affect KBase.
- The deployment-window service log contained no panic, fatal error,
  segmentation fault, or failed-start pattern.
- The final installed binary hash remained exact; the service remained
  `active/running` with `ExecMainStatus=0` and `NRestarts=0`.
- The now-unused active release-tool directory had no systemd or cron
  reference. It was moved, not deleted, to
  `/opt/dedao-kbase/release-tools.retired-20260801T090514Z`, which remains
  root-owned.

## Rollback

The rollout retains this root-owned backup batch:

`/opt/dedao-kbase/backups/direct-58239c1-20260801T090514Z`

It contains:

- the previously installed KBase server binary;
- the previously installed `frontend-web` tree.

To roll back:

1. restore both targets from that same backup;
2. restart the KBase service;
3. require successful loopback health;
4. require successful public health;
5. verify the restored revision and recent logs.

The previous Web directory also remains at
`/opt/dedao-kbase/.frontend-web.0dcc0f5-20260801T090514Z.previous` as secondary
recovery evidence. The backup batch above is the authoritative rollback input.

No environment, Nginx, credential, or knowledge-data rollback is part of this
rollout. If the retired tools must be restored for audit, move
`release-tools.retired-20260801T090514Z` back to the inactive
`release-tools` name only after confirming that the current runbook still does
not invoke it.
