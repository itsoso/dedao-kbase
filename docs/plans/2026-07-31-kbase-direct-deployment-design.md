# KBase Direct Deployment Design

**Date:** 2026-07-31

## Decision

KBase returns to the direct deployment model used before the release-kit
pipeline. The repository will no longer require signed source manifests,
signed prepared manifests, or the transactional release installer.

This is an explicit operational trade-off. It removes the external signing
dependency and the crash-safe transaction machinery, while retaining a clean
revision boundary, Linux preflight, scoped backups, immediate failure recovery,
and post-deployment verification.

## Context

The external signing chain was introduced on 2026-07-29 after an independent
security review found that digest-only manifests did not establish artifact
authenticity. Earlier production deployments did not use that chain. They
archived a clean revision, built on the Linux host, backed up the installed
binary and Web tree, replaced those two targets, and restored them if restart
or health checks failed.

The signing boundary is not currently connected to an available KMS or offline
signer. Rather than add or rotate a production trust root, this design removes
the release-kit mechanism and restores the earlier deployment route.

## Repository Changes

Remove the KBase release-kit implementation and its dedicated CI workflow:

- source release assembly and manifest verification;
- Linux prepared-release construction;
- detached signature handling;
- transactional installer and its staging/archive/fsync helpers;
- release-kit smoke fixtures;
- release-gate workflow and release-kit runbook instructions.

Keep application tests, Web smoke tests, privacy checks, system-map drift
checks, Nginx proxy checks, and normal build checks. Application behavior and
HTTP contracts are unchanged by this migration.

Historical plans and dossiers remain intact as an audit record. They must not
be rewritten to imply that signing was never used.

## Direct Deployment Flow

1. Require a clean worktree at the exact canonical `main` revision.
2. Run repository tests, frontend builds and smokes, privacy checks, and drift
   checks.
3. Create an archive of that exact revision and record its SHA-256 digest.
4. Upload the archive to the Linux host and verify the digest again.
5. Extract into a private temporary directory owned by the unprivileged service
   account.
6. Repeat the relevant tests on Linux and build `kbase-server` with the exact
   revision embedded.
7. As root, create timestamped backups of the installed server binary and Web
   tree.
8. Replace only those two targets, restart the service, and check loopback
   health.
9. If replacement, restart, or loopback health fails, immediately restore both
   backups and restart the previous version.
10. After local success, verify public health, static routes, authentication
    boundaries, the target feature behavior, service restart counters, binary
    revision, and recent logs.

The deployment must not modify knowledge data, environment secrets, browser
credentials, Nginx configuration, or unrelated services.

## Failure and Recovery Model

The direct flow provides process-level recovery only:

- both replacement targets are backed up before either is changed;
- the deployment command restores both targets on a detected failure;
- retained backups provide a documented manual rollback path.

It does not provide the removed installer's exclusive lock, durable transaction
journal, fsync protocol, immutable staging guarantees, archive ceilings, or
automatic recovery after `SIGKILL` or host power loss. Operators must avoid
concurrent deployments and manually restore retained backups after an
interrupted replacement.

Repository scripts must fail visibly. They must not suppress build, restart,
health, or rollback errors.

## Production Cleanup

The installed release-kit tools are not used by the direct deployment.
Following a successful deployment and online verification, move the existing
tool directory to a root-owned, timestamped backup location. Do not delete it
during the rollout. This leaves a recoverable audit artifact without keeping
the retired mechanism on the active execution path.

## Verification

The change is accepted only when:

- release-kit references are absent from active scripts, CI, and current
  runbook documentation;
- remaining repository checks pass from a clean revision;
- Linux preflight and build pass;
- direct replacement either succeeds completely or restores both targets;
- loopback and public health report the exact target revision;
- the service is active with a successful main process and no unexpected
  restart increase;
- public static routes load, protected APIs remain protected, the deployed fix
  behaves as expected, and recent logs contain no startup failure.

The deployment dossier records the revision, archive and binary digests,
backup names, Gate decisions, production checks, and the manual rollback
procedure without recording credentials or local machine paths.
