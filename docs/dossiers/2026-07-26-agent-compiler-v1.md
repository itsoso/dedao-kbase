# Agent Compiler v1 Delivery Dossier

## Status

Implementation and repeated Gate 3 verification complete. Gate 4 passed after
two review/remediation rounds and a focused final re-review.

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

Pending.

### G6 - Online Verification

Pending.

## Rollback

Remove the compiler route and Web panel, then restore the prior binary and
static snapshot. No stored artifact rollback is required because compilation is
read-only.
