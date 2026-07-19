# Book Agent Platform Dossier

**Status:** Delivery in progress; Tasks 1-4 checkpoints passed

## Objective

Compile authorized books and related articles into immutable, evaluated Agent
Packages that can power a shared book application and supply proof-oriented and
health evidence consumers without transferring consumer policy into KBase.

## Scope

In scope: package schema and registry, pinned releases, retrieval and model
policies, read-only MCP tools, deterministic authorization, evaluation gates,
consumer adapters, traces, feedback closure, and a manifest-driven product
shell.

Out of scope for the first pilot: per-book code forks, autonomous purchases or
publication, unrestricted redistribution of source text, health diagnosis,
prescription or dosage decisions, and personal-data write tools.

## Gate Record

- **G1 Admission: PASS.** Existing source, release, and consumer contracts make
  the objective incremental rather than a platform rewrite.
- **G2 Feasibility and risk: PASS WITH BOUNDARIES.** GitHub research supports the
  package/runtime/tool/evaluation separation. Paid-source usage policy,
  deterministic tool authorization, citation resolution, and consumer-owned
  high-risk review are mandatory.
- **G3 Test: PENDING.** Requires package schema, store, policy, evaluation, and
  cross-consumer contract tests.
- **G4 Review: PENDING.** Requires architecture and safety review after the proof
  pilot passes.
- **G5 Deployment health: PENDING.** No implementation has been deployed.
- **G6 Online verification: PENDING.** Requires exact-revision verification in
  KBase and both consumer environments.

## Checkpoint: Tasks 1-2

**Decision: PASS for the Task 1-2 batch.** G3 remains pending until the
evaluation, MCP policy, consumer, trace, and shared Book App suites also pass.
No deployment was attempted.

Delivered:

- `agent-package.v1` JSON Schema and Go contract;
- deterministic content hashing that excludes lifecycle timestamps;
- validation of pinned published releases, citation resolution, model,
  retrieval, tool, safety, evaluation, prompt-profile, and UI policies;
- atomic artifact publication with manifest-last visibility;
- persistent idempotency, immutable version reuse, supersession metadata, and
  stable versioned URLs;
- bearer-protected operator publication plus package list/detail APIs.

Exact commands and results:

- `go test ./backend/app -run AgentPackage -count=1` — RED first: contract
  symbols were undefined; GREEN after implementation.
- `go test ./backend/app -run 'AgentPackage|KBaseHTTPHandlerPublishesAndReadsAgentPackages' -count=1`
  — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` —
  PASS; regenerated because new durable types and HTTP routes changed structural
  source inventory.
- `bash scripts/system-map-smoke.sh` — PASS after regeneration.
- `go test ./...` — PASS.
- `bash scripts/privacy-smoke.sh` — PASS.
- `git diff --check` — PASS.

Task 1 commit: `6e09f2f feat(kbase): define agent package contract`.

## Checkpoint: Tasks 3-4

**Decision: PASS for the Task 3-4 batch.** G3 remains pending until the proof
and health consumer contract suites, traces, and shared Book App also pass. No
deployment was attempted.

Delivered:

- versioned synthetic evaluation suites for retrieval, citations,
  faithfulness, abstention, tool choice, and tool arguments;
- deterministic evaluation input hashing and persisted evaluator provenance;
- publication blocking for missing, mismatched, or below-threshold reports;
- package/release-scoped metadata, search, citation, and claim MCP resources;
- deterministic `allow`, `require_confirmation`, and `block` policy;
- read-only tool catalog, strict argument schemas, and audit fingerprints that
  omit argument values.

Exact commands and results:

- `go test ./backend/app -run AgentPackageEvaluation -count=1` — RED first:
  evaluator and persistence symbols were undefined; GREEN after implementation.
- `go test ./backend/app -run 'AgentToolPolicy|BookKnowledgeMCP' -count=1` —
  RED first: policy evaluation and scoped resources were undefined; GREEN after
  implementation.
- `go test ./backend/app -run KBaseHTTPHandlerPublishesAndReadsAgentPackages -count=1`
  — RED first: the default HTTP publisher rejected the built-in scoped tool;
  GREEN after wiring the read-only catalog.
- `go test ./backend/app -run 'AgentPackageEvaluation|AgentToolPolicy|BookKnowledgeMCP|KBaseHTTPHandlerPublishesAndReadsAgentPackages' -count=1`
  — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` —
  PASS; regenerated for new durable evaluation/audit objects and MCP operations.
- `bash scripts/system-map-smoke.sh` — PASS.
- `go test ./...` — PASS.
- `bash scripts/privacy-smoke.sh` — PASS.
- `git diff --check` — PASS.

## Decisions

1. KBase remains the knowledge authoring and release control plane.
2. Agent execution is a separate runtime concern.
3. Book apps are generated from manifests and shared components.
4. The proof pilot runs before the health pilot.
5. Health receives evidence-only drafts and retains all domain approval.
6. Feedback can trigger analysis or reverification but never publication.

## References

- `docs/research/2026-07-19-book-agent-platform-github-research.md`
- `docs/plans/2026-07-19-book-as-agent-platform-design.md`
- `docs/plans/2026-07-19-book-as-agent-platform.md`
- `docs/contracts/knowledge-supply-v1.md`
