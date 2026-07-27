# Agent Compiler v1 Delivery Dossier

## Status

Released to production from runtime revision `0d21ffc` on 2026-07-27.
G1-G6 are complete.

## Requirement

Compile deterministic study and evidence Agent Package candidates from strict
Release Assembly state, in dual, evidence, and study modes.

## Gate Decisions

### G1 - Admission

PASS. Package validation, evaluation, publication, and runtime already exist.
Manual package policy authoring is now the primary gap between Releases and
per-book Agents.

### G2 - Feasibility And Risk

PASS with controls.

- Pure read-only compiler.
- Fixed policy profiles rather than arbitrary policy input.
- No automatic trusted evaluation or publication.
- Evidence mode fails closed without independent support.
- Existing package schemas and runtime remain authoritative.

### G3 - Tests

PASS.

- `go test ./... -count=1` passed outside the restricted sandbox. The first
  sandbox run failed only because local `httptest` listeners, the macOS
  keychain backend, and the repository's existing network test were denied.
- The final full run used an official Go 1.23 arm64 toolchain because the
  workstation's existing x86_64 Go, Node, and Python processes entered an
  unrelated Rosetta uninterruptible state. The native run passed all packages.
- `go test -race ./backend/app ./cmd/kbase-server -count=1` passed outside the
  restricted sandbox. The macOS linker emitted its existing malformed
  `LC_DYSYMTAB` warning, but both test packages passed.
- `go vet ./...`, `go mod verify`, and `cd frontend && npm run build` passed.
  Vite reported only the repository's existing eval and large-chunk warnings.
- Knowledge contract, evaluation, Proofroom consumer, Health evidence,
  source-agent packaging, WC Plus agent packaging, system-map, and privacy
  smoke checks passed.
- All existing desktop and Web Node smoke scripts passed, including the Agent
  Compiler workspace checks.
- A real Chrome Playwright check exercised dual and study compilation at
  1440x1000 and rendered the workspace at 390x844. It verified request
  isolation, candidate rendering, stable responsive controls, and zero console
  or page errors. The repeated check also delayed a dual response behind a
  study response and confirmed the stale result was discarded. A Release-list
  `500` left the published package list visible and disabled only the compiler.
- `GET /api/knowledge/releases?latest=true` has focused coverage for per-book
  latest selection, newest-first ordering, and cursor pagination.
- `git diff --check` passed.

The compiler route uses the normal authenticated API session because
compilation is read-only and non-persistent. The browser never receives the
publisher credential. Evaluation and publication retain their dedicated
publisher-only boundary.

The browser check found and prevented one release regression: Chrome rejected
the first SemVer `pattern` under HTML Unicode Sets rules, and the generic input
selector enlarged support checkboxes. Both defects now have static regression
coverage and passed the repeated Chrome and full Web smoke checks.

### G4 - Review

First review: FAIL. It found eight issues:

- explicit unrelated support was accepted;
- a global 500-cluster projection could be mistaken for complete evidence;
- every compile rebuilt and loaded the global Assembly;
- the Web selector loaded historical rather than latest Releases;
- Package and Release-list loading were coupled;
- stale compile responses could overwrite a newer selection;
- package-validation errors could expose a machine path;
- the response schema did not constrain package shape strongly enough.

Remediation is implemented and Gate 3 was repeated:

- every support source must be independent and related;
- Assembly construction is scoped to at most 17 selected Releases;
- automatic support discovery is bounded to 500 latest Release records;
- publication identity and relationships use complete selected Releases rather
  than the paginated cluster projection;
- the Web selector uses latest-per-book pagination and `Promise.allSettled`;
- compile responses are request-sequenced;
- validation failures return a generic bounded issue;
- schema and runtime validation enforce candidate state and `study → v1`,
  `evidence → v2`.

Second review: FAIL. It found three remaining contract and scale issues:

- automatic support discovery returned no result when the catalog exceeded 500
  latest Releases;
- runtime and JSON Schema allowed contradictory mode/candidate or
  status/candidate combinations;
- request Release IDs were unbounded although response IDs were schema-bounded.

The follow-up remediation sorts latest-per-book Release records newest first
and scans only the first 500, bounds every Release ID to 128 Unicode code points,
and enforces exact candidate kinds and aggregate status in both Go validation
and JSON Schema. Regression tests use 501 latest Releases and execute the
composed schemas offline with all `$id` dependencies registered.

Focused final re-review: PASS. It confirmed newest-first bounded discovery,
runtime and Schema mode/status agreement, and 128-code-point Release ID bounds.
No P0-P2 findings remain.

### G5 - Deployment Health

PASS.

- Production `dedao-kbase/main` remained at the reviewed base `76b2648`, so it
  fast-forwarded by 11 commits to exact runtime revision `0d21ffc`.
- The exact source archive SHA-256 was
  `42490ac0f58296b0ba593f6e1a93a06a60f879c7aee533581bb59edc438befd7`.
- Linux preflight rebuilt the Vue frontend, passed every Web static smoke,
  `go test ./...`, the race detector for `backend/app` and
  `cmd/kbase-server`, `go vet`, module verification, knowledge contract and
  evaluation checks, source-agent packaging, generated system-map drift,
  privacy, and diff checks.
- The first archived-tree preflight was blocked because `git archive` omits
  `.git`, which the drift checks require. The retry created an isolated Git
  baseline from the exact archive before running tests. A later command stopped
  on an incorrect packaging-script filename, and the corrected Linux run
  stopped when the macOS-only WC Plus packaging check correctly rejected
  Linux. No production file had changed. The WC Plus and source-agent packaging
  checks then passed on macOS, while all Linux-applicable checks and the final
  CGO build passed on the server.
- The installed binary SHA-256 is
  `78036f11b9ad59ddca095b025d8c5fdc918bb6bb954662f7f07c58b39e8f5ce7`.
- Only the service binary and static Web bundle changed. Knowledge data,
  configuration, and secrets were preserved.
- Rollback snapshot: `0d21ffc-20260727T063004Z`.
- Post-deploy state was `active/running`, `ExecMainStatus=0`, `NRestarts=0`,
  and the local health endpoint returned
  `{"ok":true,"service":"dedao-kbase"}`.

### G6 - Online Verification

PASS.

- Public `https://kbase.executor.life/health` returned
  `{"ok":true,"service":"dedao-kbase"}`.
- The protected browser route and compiler API each returned `401` without
  authentication.
- The deployed static bundle contains the Agent Compiler endpoint and workspace
  markers.
- Authenticated latest-per-book Release listing returned the requested bounded
  page with a continuation cursor.
- Authenticated, read-only production requests exercised `study`, `evidence`,
  and `dual` and returned `agent-compilation.v1` with the exact candidate kinds
  and bounded status relationships required by the contract.
- A production Release generated a ready `study` candidate containing
  `agent-package.v1`. Compiling the same Release in `dual` mode returned
  `partial`: the study candidate was ready and evidence failed closed.
- No evidence-ready pair was found in the bounded sample of 20 newest Releases.
  Those requests returned the expected `supporting_release_required` outcome;
  this is an independent-support data gap, not a runtime failure.
- An invalid historical Release returned the generic `release_invalid` code
  without exposing a machine path.
- The service remained `active` with `ExecMainStatus=0` and `NRestarts=0`.
  Recent logs contained no panic, fatal error, failed request, or error-level
  event.

## Rollback

Restore
`/opt/dedao-kbase/bin/kbase-server.backup-0d21ffc-20260727T063004Z` and
`/opt/dedao-kbase/frontend-web.backup-0d21ffc-20260727T063004Z`, then restart
`dedao-kbase`. No stored artifact rollback is required because compilation is
read-only.
