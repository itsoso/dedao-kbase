# Recoverable Managed Worker Installation Design

## Scope

Harden the existing Task 9 macOS worker packaging and installation path. This
design does not add source capabilities, remote update behavior, or Task 10
functionality.

## Secure installer startup

Both installers execute through `/usr/bin/env -S`, removing `BASH_ENV`, `ENV`,
and inherited `SHELLOPTS` before `/bin/bash` starts. The first command disables
xtrace. Before any child process, the script fixes `IFS`, `CDPATH`, and locale,
rejects `KBASE_SOURCE_AGENT_TOKEN` from the environment, clears inherited secret
aliases and `KBASE_AUTH_TOKEN`, and reads one bounded token line from standard
input with Bash builtins. Printable-ASCII validation is also builtin-only.

The token remains an unexported shell variable. Keychain writes receive the
token twice on standard input; it is never placed in an argument, environment,
journal, plist, or log.

## Configuration and legacy compatibility

The source and WC Plus binaries expose a configuration-only validation command.
It validates URLs and local settings without reading Keychain, contacting a
vendor, leasing work, or opening the network. URL parsing remains compatible
with valid base paths while rejecting userinfo, query strings, fragments,
opaque or empty authorities, unsafe remote HTTP, and non-loopback WC Plus hosts.
Enrollment addresses require an explicit loopback host and numeric port in the
valid TCP range.

Normal source startup loads the fixed shared Keychain account first. It consults
the legacy `<agentID>:transport-token` account only when the fixed account is
missing, after the agent ID is validated. Invalid or corrupt fixed values fail
closed. Legacy output remains bounded, validated, and redacted. WC Plus has no
legacy fallback. Existing WeChat MP session and master-key accounts are not
changed.

Both production entrypoints create a signal-aware context before configuration
loading so a blocked Keychain command stops on SIGINT or SIGTERM.

## Recoverable artifact pair

`scripts/lib/managed-worker-pair.sh` implements the shared pair transaction used
by both build and install scripts. Callers provide one directory and fixed,
validated basenames. The local journal contains only a version, phase, old-file
presence flags, and SHA-256 hashes. It never contains a secret or executable
command.

The transaction uses a same-directory lock, backups, and durable phase changes:

1. `prepared`: journal is durable before the first rename.
2. `published`: both new artifacts are present and match the recorded hashes.
3. `committing`: recovery must finish the new pair and remove backups.

Recovery rolls non-committing transactions back to the previous pair and
finishes committing transactions forward. Rename, removal, journal, hash, and
global-sync errors are visible. SIGTERM triggers rollback; SIGKILL leaves enough
state for the next invocation to recover.

## Complete install transaction

A HOME-scoped lock serializes both installers while they read or mutate the
shared fixed Keychain account. Pair backups, the old plist state, old Keychain
value, and old launchd loaded state remain available until every installation
gate succeeds:

1. installed updater check;
2. Keychain replacement;
3. source doctor or WC Plus configuration-only validation;
4. plist publication;
5. bootout, bootstrap, kickstart, and final launchctl print.

Any failure stops a partially loaded new service, restores plist, Keychain, and
binary pair, and restarts the old service when it was previously loaded.
Rollback failures return a fixed public error without secret material. Commit
removes every backup and journal before reporting success.

## Verification

Layered RED/GREEN tests cover hostile shell startup state, stdin-only token
transport, source-only legacy fallback, cancellation, strict URL and enrollment
validation, pair fault and crash recovery, complete install rollback boundaries,
shared-account concurrency, unique temporary plist names, real temporary builds,
and fake installations. Final gates include focused and race tests, packaging
smokes, backend-safe tests, frontend build, system-map drift, privacy smoke, and
diff checks.
