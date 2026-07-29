# KBase Release Kit Design

## Context

The browser-session rollout proved the application gates but also exposed
release work that still depended on operator memory: Git archives omit the
embedded Vue output, configuration validity was discovered only at process
start, Linux root changes permission-test behavior, and the deployed Nginx
version may differ from a developer machine.

The release path must be repeatable without publishing host paths, credentials,
or production identifiers.

## Options Considered

### Repository-native release kit

Keep the existing systemd and Nginx architecture. Add source assembly,
configuration diagnosis, Linux preparation, transactional installation, and
smoke tests as repository scripts. This is the recommended option because it
hardens the current deployment without changing the runtime platform.

### Container image

Build and deploy one OCI image containing the server and Web assets. This
improves binary reproducibility but requires migrating persistent paths,
systemd ownership, Nginx integration, and operational rollback. It is larger
than the current need.

### External orchestration

Move deployment into Ansible or a hosted CD system. This centralizes operations
but puts repository correctness behind a separate private control plane and
does not improve local release verification.

## Decision

Implement the repository-native release kit. CI will exercise it without
production secrets. Real installation remains an explicit, separately
authorized operation.

## Architecture

The release has three immutable stages:

1. **Assemble** verifies a clean Git revision, creates an exact source archive,
   and writes a public manifest containing the revision and archive digest.
2. **Prepare** verifies that manifest on Linux, builds the locked Vue frontend,
   runs Go and Web gates, builds the CGO server, runs the real Nginx proxy
   smoke, and emits an install bundle with component digests.
3. **Install** verifies the prepared manifest, runs the candidate server's
   configuration doctor, snapshots all replacement targets, switches the
   binary/Web/configuration as one transaction, and rolls back on service,
   health, or Nginx failure.

The server gains `--check-config`. It validates the same token separation,
browser-session, public-Origin, HTTP-listen, and retry-signing contracts used
at startup, but it does not bind a port, create a database, or print secrets.

## Safety Contract

- Release manifests contain relative artifact names, revision IDs, digests,
  and schema versions only.
- Host paths and service names are required install-time inputs; they are not
  committed as defaults.
- The install script refuses an unverified bundle, incomplete target set,
  writable-by-others secret file, or missing rollback capacity.
- Every mutation has a corresponding snapshot before the first replacement.
- Rollback restores the complete target set and rechecks service and Nginx.
- CI has no deployment job and receives no production credential.

## Verification

- Go tests prove `--check-config` succeeds without opening listeners and fails
  closed for invalid session or token contracts.
- Shell smokes prove archive tampering is rejected, missing embedded frontend
  output is built during preparation, success installs all components, and an
  injected health failure restores all components.
- CI runs Go as the normal runner user and repeats the permission-focused test
  as root, then runs the proxy smoke against the runner's Nginx.
- Full repository tests, privacy smoke, and system-map drift checks remain the
  final local gate.

