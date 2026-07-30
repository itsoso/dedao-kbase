# KBase Release Kit Delivery Dossier

## Status

G1-G4 passed. G5-G6 require a new, explicit release
authorization and have not started.

No release-kit commit has been pushed, merged, or deployed.

## Requirement

Convert the proven KBase release procedure into a repeatable repository
capability so future releases do not rediscover frontend build order,
configuration requirements, Linux permission behavior, Nginx compatibility,
or rollback steps during production deployment.

## Gate Decisions

### G1 - Admission

PASS. The preceding rollout required multiple operator-only corrections before
the candidate could pass Linux and Nginx preflight. Keeping those corrections
outside the repository would repeat the same risk and delay.

### G2 - Feasibility And Risk

PASS with controls.

- Keep the current systemd and Nginx runtime architecture.
- Split release work into immutable assemble, prepare, and install stages.
- Require explicit host inputs; do not publish host paths or credentials.
- Keep production installation outside CI and behind explicit authorization.
- Test both successful replacement and complete rollback before deployment.

### G3 - Verification

PASS on 2026-07-29 after the final hardening changes.

- `bash -n` passed for all release, install, Nginx, and smoke scripts.
- `go mod verify`, `go vet ./...`, `go test ./...`, and
  `go test -race ./...` passed.
- `npm run build` and every `frontend/scripts/*-smoke.mjs` check passed.
- `node --check frontend-web/app.js` and every
  `frontend-web/scripts/*smoke*.mjs` check passed.
- `node --check` passed for the archive listing, bounded staging, and filesystem
  sync helpers.
- `deploy/kbase/archive-list-smoke.sh` and
  `deploy/kbase/stage-files-smoke.sh` passed.
- `deploy/kbase/release-manifest-smoke.sh` passed.
- `deploy/kbase/install-release-smoke.sh` passed.
- `scripts/privacy-smoke.sh`, `scripts/system-map-smoke.sh`, and
  `git diff --check` passed.
- The generated system map was regenerated from the final route locations.

The first sandboxed Go run was rejected by sandbox policy for loopback
listeners, the existing public m3u8 fixture, macOS Keychain, and user
configuration access. It did not contain assertion failures. Both normal and
race suites were rerun with host test permission and passed; the temporary
Keychain entry is test-managed and removed by cleanup.

### G4 - Independent Review

PASS after correctness and security re-review.

The first CI/documentation review rejected the candidate with three findings:

- Build `frontend/dist` before whole-repository Go vet and tests on a clean
  checkout.
- Pin official GitHub Actions to reviewed commit SHAs.
- Make the emergency restore runbook fail fast and validate all snapshots.

Those findings were remediated and the specification re-review passed. A
complete correctness/security review rejected the next candidate because
integrity checks lacked external authenticity, mutable release inputs could
change after verification, the privileged installer evaluated the environment
file as shell, health checks were not bound to KBase, build and root-command
boundaries were too broad, and archives lacked resource ceilings.

The remediation adds:

- Detached RSA/SHA-256 signatures verified against external trusted public
  keys for both source and prepared manifests.
- Private `0700` staging, staged-byte verification/consumption, and trusted
  backup-parent ancestry.
- Non-executing allowlisted environment parsing and a fixed root `PATH`.
- A loopback backend `/health` URL plus exact `dedao-kbase` response contract.
- Non-root release preparation, absolute trusted Go use under `sudo`, pinned
  Actions, and compressed/member/per-file/expanded archive quotas.
- Negative smoke coverage for wrong keys, tampering, TOCTOU replacement,
  environment injection, unrelated health endpoints, invalid health bodies,
  unsafe archives, excessive archives, and untrusted staging parents.

A later correctness/security review rejected the candidate again with concrete
power-loss and trust-boundary gaps:

- The crash window after moving the old Web directory could not recover because
  preflight rejected the now-missing target.
- Concurrent installers could share transaction and backup state.
- Journal persistence did not fsync snapshots, installed targets, and all
  rename parent directories.
- Health checks could be served from an intermediary cache.
- Candidate repository scripts still received a prepared-release private key,
  leaving key-exfiltration and signing TOCTOU risk.
- Root child tools inherited Node, OpenSSL, Tar, Python, dynamic-linker, and
  shell override variables.
- Source and prepared artifacts could consume disk or memory before bounded
  validation.

The current remediation:

- Allows only the journal-backed missing-Web recovery state and verifies it with
  a forced `SIGKILL` fixture.
- Holds an exclusive lock from recovery through commit/rollback and rejects a
  concurrent installer.
- Fsyncs retained snapshots, candidate and installed targets, transaction
  journals, and parent directories around rename boundaries.
- Sends `no-cache` health requests and marks `/health` as `no-store` through
  both application and Nginx contracts.
- Removes private-key options from source assembly and release preparation.
  Production signing is an external KMS/offline protocol that never gives
  repository scripts a production key; repository signing is CI-fixture only.
- Clears dangerous inherited variables, runs candidate commands through
  `env -i`, fixes root `PATH`, and documents a clean production invocation.
- Uses a shared `O_NOFOLLOW`, byte-bounded, fsynced staging helper and streamed
  artifact hashing. Per-artifact limits now cover manifests, signatures,
  binaries, Web archives, Nginx templates, renderers, public keys, and
  environment files.

The final correctness review passed with no P0/P1 findings. It retained three
explicit residual risks: not every rename/fsync instruction has an individual
SIGKILL injection, tests cannot emulate a real storage-controller power loss,
and schema v1 has no monotonic anti-rollback generation.

The parallel security review rejected the candidate with two additional P1
findings:

- Root could execute repository helpers or tool overrides from user-writable
  paths.
- A sticky writable transaction parent allowed a symlink race between lock
  validation and Bash opening/chmod of the lock path.

That rejection returned the feature to implementation. The installer now:

- Runs as root only from `/opt/dedao-kbase/release-tools`.
- Canonicalizes every external executable and checks the install script,
  repository helpers, and resolved tools for root ownership, non-symlink
  ancestry, and no group/other write permission.
- Rejects group/other-writable transaction and lock parents without a sticky
  directory exception.
- Adds smoke coverage that a writable transaction parent fails before any lock
  file or target mutation, plus a CI root check that a checkout-based install
  is rejected.

The first targeted security re-review confirmed the lock fix but found one
remaining P1: root canonicalized a user-provided `--node-bin`, then executed
that Node to validate its own ownership. An attacker-controlled executable
could therefore run before the trust decision.

Root mode now rejects every executable override before starting Node or any
other overridable child. It resolves only default command names through the
fixed system `PATH`; non-root fixture tests retain overrides. CI installs the
reviewed helper set under the trusted `/opt` directory, supplies an executable
sentinel as `--node-bin`, and requires rejection without creating the sentinel
marker.

The second targeted security re-review passed with no P0/P1 findings. It
confirmed that root rejects overrides before starting an overridable child and
that the non-writable lock-parent contract closes the non-privileged symlink
race. G4 is PASS.

The root sentinel behavior is encoded in the Ubuntu CI workflow. A local
`sudo -n` attempt could not execute because this workstation requires a sudo
password; no system mutation occurred. The non-root transaction installer
suite, including writable-parent rejection, passed locally.

Known residual risk: release schema v1 authenticates a revision but does not
carry a monotonic release generation. A correctly signed older release can
therefore be installed with operator approval. Anti-rollback counters and
emergency downgrade policy are deferred to a versioned release schema rather
than being implied by Git SHA ordering.

### G5 - Deployment Health

IN PROGRESS after explicit release authorization on 2026-07-29.

The first `main` release-gate run for `91df112` stopped before production
mutation. Ubuntu GNU tar omitted the numeric timezone from `--full-time`
listing output for a Git archive member, while the bounded archive parser
required that optional display field. The Linux gate therefore rejected the
unsafe-symlink fixture during strict listing parsing instead of reaching the
intended safety rejection.

The remediation adds a Linux-format regression fixture that was observed RED
before the parser change, accepts GNU listings both with and without the
display timezone, and keeps all path, type, size, control-character, timeout,
and byte-limit checks unchanged. Local archive-list and complete release
manifest smokes pass. A new `main` gate run must pass before installation
continues.

### G6 - Online Verification

PENDING successful G5.

## Delivery Commits

- `554fba7` - release automation design and implementation plan
- `757383f` - side-effect-free server configuration doctor
- `b7dca15`, `96384e8` - immutable and atomic source releases
- `e3c55d7`, `02a1ed2`, `b9a275f` - verified Linux preparation and archive
  hardening
- `ca47747`, `52adc85`, `80665cd` - transactional install, rollback recovery,
  and doctor validation
- `3e256d5`, `fefddcb` - secret-free CI release gates and review remediation
- `b62e4ec`, `948bba9` - authenticated artifacts, immutable staging, bounded
  archives, non-executing environment parsing, and trusted staging ancestry
