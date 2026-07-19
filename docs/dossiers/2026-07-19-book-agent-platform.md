# Book Agent Platform Dossier

**Status:** BLOCKED at G4 review; Tasks 1-9 implemented and G3 passed

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
- **G3 Test: PASS.** The full KBase, Proofroom, and Health release matrix passed
  on the integrated feature heads recorded below.
- **G4 Review: NO-GO.** Independent architecture and cross-consumer safety
  review found release-blocking policy, evaluation, authorization, runtime,
  lifecycle, feedback, and citation-disclosure gaps. The implementation has
  returned upstream; no push or deployment is permitted.
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

## Checkpoint: Tasks 5-6

**Decision: PASS for the Task 5-6 batch.** The proof consumer imports Agent
Packages without assuming that retrieval proves a claim, and the proof contract
fixture closes bounded feedback into gap or reverification candidates without
automatic publication. G3 remains pending until the health consumer, traces,
shared Book App, and full cross-repository suites pass. No deployment was
attempted.

Delivered:

- cursor-based Proofroom package import with stable idempotent release receipts;
- preserved package, release, claim, chunk, and citation identities and links;
- user-scoped package evidence retrieval that remains `unknown` until
  Proofroom's judge assigns support or contradiction;
- bounded `used`, `rejected`, `stale`, `conflict`, and `zero_hit` feedback with
  opaque event IDs and no prompt or user-data fields;
- a synthetic proof fixture covering supported, contradicted, insufficient,
  stale, and citation-missing outcomes;
- contract smoke coverage proving feedback creates a gap or reverification
  candidate while `automatic_publication` remains false.

Exact commands and results:

- `python -m pytest -q tests/test_kbase_release_consumer.py` (from the isolated
  Proofroom feature worktree and its project virtual environment)
  — RED case-by-case before implementation; final result `7 passed in 1.72s`.
- `python -m pytest -q tests/test_kbase_release_consumer.py tests/test_decision_engine.py tests/test_claim_verifier_routing.py tests/test_claim_verifier_quota_query_cache.py tests/test_knowledge_runtime.py tests/test_dedao_kbase_sync.py`
  (from the isolated Proofroom feature worktree and its project virtual
  environment)
  — PASS, `126 passed in 10.75s`.
- `python -m py_compile rpa_llm/kbase_release_consumer.py rpa_llm/claim_verifier.py rpa_llm/decision_engine.py tests/test_kbase_release_consumer.py`
  — PASS.
- `bash scripts/proof-consumer-contract-smoke.sh` — RED first because the proof
  fixture was absent; GREEN after adding the synthetic fixture.
- `bash scripts/knowledge-contract-smoke.sh` — PASS with the proof pilot included.
- `bash scripts/privacy-smoke.sh` — PASS before the Proofroom commit, run from
  the KBase feature worktree because the Proofroom repository has no privacy
  smoke script; the Proofroom feature diff also passed a targeted path, key,
  and token scan.
- `git diff --check` — PASS in both feature worktrees.

Proofroom commit: `7adc63fb feat(kbase): consume agent packages for proof`.

## Checkpoint: Task 7

**Decision: PASS after an initial independent NO-GO and two corrective
reviews.** Health imports only evidence-only packages into an isolated draft
workspace; its serving, domain review, and safety ownership remain outside
KBase. G3 remains pending until traces, replay, the shared Book App, and the
full release suites pass. No deployment was attempted.

Delivered:

- KBase package detail responses now include the persisted evaluation report
  and fail closed if the matching publication-gate evidence is unavailable;
- Health verifies evaluation identity, suite, input hash, evaluator version,
  timestamp, passing status, and required threshold metrics rather than
  inferring a pass from publication state;
- stale, conflicting, unevaluated, and non-evidence-only packages remain held;
- package imports write only draft review artifacts and audit receipts, never
  the serving index or personal health state;
- fingerprinted cursor handling selects cumulative incremental sync or a full
  immutable replay when the workspace is missing, stale, or based on a changed
  canonical seed;
- the explicit, unscheduled Agent Package task uses a separate
  `agent-packages` review workspace;
- package lineage is preserved across overlapping packages in the same batch
  and across later incremental batches, including artifact rows and both
  manifests.

TDD and review trail:

- the first interface suite was RED with seven missing Agent Package consumer
  behaviors, then GREEN with `43 passed`;
- the first independent safety review returned NO-GO because missing
  evaluation evidence was treated as passed, a later batch could replace
  unreviewed drafts while advancing the cursor, and overlapping packages lost
  lineage;
- producer and consumer regressions were added before the fixes; focused KBase
  tests and the Health suite returned GREEN;
- the second review returned NO-GO for one remaining case: two overlapping
  packages arriving in separate `limit=1` calls;
- that exact regression failed with only the first package in the final claim
  lineage, then passed after existing and generated rows were merged by release;
- the final independent safety review returned GO and confirmed no serving
  import, diagnosis, prescription, dosage, tool execution, or personal-data
  write.

Exact final commands and results:

- `go test ./backend/app -run 'AgentPackage|KBaseHTTPHandlerPublishesAndReadsAgentPackages' -count=1`
  — PASS.
- `go test ./...` — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` —
  PASS; regenerated because the HTTP source locations tracked by the generated
  inventory changed.
- `bash scripts/system-map-smoke.sh` — PASS.
- `python -m pytest backend/tests/test_dedao_kbase_release_consumer.py -q`
  (from the isolated Health worktree and its project virtual environment) —
  PASS, `48 passed, 6 warnings in 30.47s`.
- `python -m pytest -o addopts='' -q backend/tests/test_dedao_kbase_release_consumer.py backend/tests/test_dedao_kbase_export_importer.py backend/tests/test_system_knowledge_lifecycle.py backend/tests/test_system_knowledge_ingest.py backend/tests/test_kb_reconciliation.py backend/tests/test_kb_reconciliation_e2e.py backend/tests/test_kb_reconciliation_evalcase.py backend/tests/test_kb_reconciliation_judge.py backend/tests/test_kb_reconciliation_merge.py backend/tests/test_safety_failloud_consumers.py`
  — PASS, `176 passed, 6 warnings in 25.96s`.
- `ruff check backend/app/integrations/dedao_kbase_release_consumer.py backend/app/tasks/system_knowledge_lifecycle.py backend/tests/test_dedao_kbase_release_consumer.py`
  — PASS.
- `python -m py_compile backend/app/integrations/dedao_kbase_release_consumer.py backend/app/tasks/system_knowledge_lifecycle.py backend/tests/test_dedao_kbase_release_consumer.py`
  — PASS.
- `python scripts/check_doc_drift.py` — PASS; structural counts remained
  consistent, so the Health system map was not regenerated after the
  corrective edits.
- `bash scripts/privacy-smoke.sh` — PASS before every KBase and cross-repository
  feature commit; the Health diffs also passed a targeted path, credential, and
  key scan.
- `git diff --check` — PASS in both feature worktrees before each commit.

KBase corrective commit: `fc6abc4 fix(kbase): expose package evaluation evidence`.

Health commits:

- `9ed97637a feat(kbase): hold health agent packages for review`;
- `07cfa8eda fix(kbase): fail closed on health package review`;
- `8463c3b61 fix(kbase): retain incremental package lineage`.

## Task 8: Bounded traces and replay

Delivered:

- a versioned trace contract for package/release identities, evidence ranks,
  model routing, policy decisions, tool outcomes, citations, and fingerprints;
- persisted payload redaction for credentials, source bodies, private prompts,
  and consumer user identifiers;
- immutable, idempotent trace persistence with conflicting identity rejection;
- deterministic replay over stored evidence hashes and mocked model/tool
  outcomes;
- an allowlisted OTLP/OpenInference-style span projection for optional tracing
  backends.

Exact commands and results:

- `go test ./backend/app -run AgentTrace -count=1` — RED first because trace
  symbols were undefined; GREEN after implementation.
- The immutable trace regression was RED before conflict detection and GREEN
  after the store rejected a second payload for the same trace ID.
- `jq empty contracts/agent-trace-v1.schema.json` — PASS.
- `go test ./backend/app -run 'AgentTrace|AgentToolPolicy|AgentPackageEvaluation' -count=1`
  — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` —
  PASS; regenerated because trace types changed structural inventory.
- `bash scripts/system-map-smoke.sh`, `bash scripts/privacy-smoke.sh`, and
  `git diff --check` — PASS.

Task 8 commit: `acf913d feat(kbase): trace and replay agent runs`.

## Task 9: Shared Book App

Delivered:

- stable `/agent-packages`, `/agents`, and `/book-apps` routes backed by one
  shared renderer;
- package/release/evaluation loading and manifest-gated reader, search,
  grounded-chat, evidence, quiz, and action-plan surfaces;
- explicit unavailable states for declared capabilities without a connected
  runtime, rather than empty links;
- responsive desktop/mobile layouts with no horizontal overflow in the bounded
  browser check.

Exact commands and results:

- `node frontend-web/scripts/book-knowledge-web-smoke.mjs` — RED first because
  the shared renderer and stable route contract were absent; GREEN after
  implementation.
- `node --check frontend-web/app.js` — PASS.
- `for smoke in frontend-web/scripts/*.mjs; do node "$smoke"; done` — PASS for
  Book Knowledge, ebook loading, token header, markdown, WC Plus control-plane
  and source, and WeChat collector and source smokes.
- An ephemeral Playwright harness ran the static Web client through the
  webapp-testing server helper — PASS at widths 1440 and 390; six declared
  capabilities rendered, two unavailable capabilities were explicit, search
  and chat interactions completed, and horizontal overflow was `0` at both
  widths.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` and
  `bash scripts/system-map-smoke.sh` — PASS; the generator produced no diff.
- `bash scripts/privacy-smoke.sh` and `git diff --check` — PASS.

Task 9 commit: `f70068a feat(web): add shared book agent shell`.

## Checkpoint: Task 10 release gates

**Decision: G3 PASS; G4 NO-GO.** Release progression stopped at the failed
review Gate. No feature branch was pushed and no deployment or online mutation
was attempted. G5 and G6 remain pending.

Integrated revisions:

- KBase `f70068a`; canonical `dedao-kbase/main` at `dd6bc9c` is its direct
  ancestor, so no main integration commit was required.
- Proofroom `815fe7c5`, merging current `origin/main` into the consumer feature.
- Health `2852250a9`, merging current `origin/main` into the consumer feature;
  the generated system map was regenerated from the merged source tree and its
  drift hook passed under the project environment.

G3 exact commands and results:

- `go test ./...` — PASS.
- `go test -race ./backend/app ./cmd/kbase-server -count=1` — PASS; the macOS
  linker emitted non-fatal `LC_DYSYMTAB` warnings.
- `cd frontend && npm run build` — PASS with existing large-chunk and `eval`
  warnings.
- `node frontend/scripts/markdown-render-smoke.mjs` and
  `node frontend/scripts/book-knowledge-ui-smoke.mjs` — PASS.
- `for smoke in frontend-web/scripts/*.mjs; do node "$smoke"; done` — PASS.
- `bash scripts/proof-consumer-contract-smoke.sh` and
  `bash scripts/knowledge-contract-smoke.sh` — PASS.
- `jq empty contracts/agent-package-v1.schema.json contracts/agent-trace-v1.schema.json`
  — PASS.
- `go test ./backend/app -run 'AgentPackage|AgentToolPolicy|BookKnowledgeMCP|AgentTrace|KBaseHTTPHandlerPublishesAndReadsAgentPackages' -count=1`
  — PASS.
- `python -m pytest -q tests/test_kbase_release_consumer.py tests/test_decision_engine.py tests/test_claim_verifier_routing.py tests/test_claim_verifier_quota_query_cache.py tests/test_knowledge_runtime.py tests/test_dedao_kbase_sync.py`
  in the Proofroom project environment — PASS after current-main integration,
  `126 passed in 9.32s`.
- `python -m py_compile rpa_llm/kbase_release_consumer.py rpa_llm/claim_verifier.py rpa_llm/decision_engine.py tests/test_kbase_release_consumer.py`
  — PASS.
- `python -m pytest -o addopts='' -q backend/tests/test_dedao_kbase_release_consumer.py backend/tests/test_dedao_kbase_export_importer.py backend/tests/test_system_knowledge_lifecycle.py backend/tests/test_system_knowledge_ingest.py backend/tests/test_kb_reconciliation.py backend/tests/test_kb_reconciliation_e2e.py backend/tests/test_kb_reconciliation_evalcase.py backend/tests/test_kb_reconciliation_judge.py backend/tests/test_kb_reconciliation_merge.py backend/tests/test_safety_failloud_consumers.py`
  in the Health project environment — PASS after current-main integration,
  `176 passed, 6 warnings in 31.53s`.
- `ruff check backend/app/integrations/dedao_kbase_release_consumer.py backend/app/tasks/system_knowledge_lifecycle.py backend/tests/test_dedao_kbase_release_consumer.py`
  and the matching `python -m py_compile` command — PASS.
- `python scripts/dump_system_map.py` and `python scripts/check_doc_drift.py` in
  Health — PASS; the generated inventory matches the merged source tree.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json`,
  `bash scripts/system-map-smoke.sh`, `bash scripts/privacy-smoke.sh`, and
  `git diff --check` in KBase — PASS. Both consumer worktrees also passed
  `git diff --check` and targeted newly-added-line privacy scans.

G4 independent review findings:

- **Critical:** package publication does not prevent a release usage-policy
  downgrade or require non-empty authorized source types.
- **Critical:** evaluation reports can be overwritten and publication does not
  recompute the input hash or pin the approved evaluator; the synthetic adapter
  also scores caller-supplied observations rather than package behavior.
- **Critical:** one bearer credential authorizes both read consumers and package
  publication, so operator-only publication is not enforced.
- **Important:** MCP calls omit package version and load mutable latest state;
  search also does not enforce the package context limit.
- **Important:** the shared Book App calls generic single-book search/chat,
  ignores additional pinned releases and package retrieval/model/safety/tool
  policy, and omits answer citation evidence.
- **Important:** Proofroom imports superseded package records without lifecycle
  or freshness filtering.
- **Important:** package feedback helpers are not wired into Proofroom or Health
  usage paths, so the planned feedback closure is not operational.
- **Important:** the isolated Health package workspace has no selector in the
  ordinary admin review API, leaving no normal human-review progression path.
- **Important:** citation resolution can return `SourceHTML`, including local
  ebook paths; trace fingerprint fields are not constrained to actual bounded
  hashes; Go package-ID validation is weaker than the JSON Schema.

The cross-consumer reviewer confirmed that Health remains draft-only,
human-review-blocked, and free of serving-index, personal-data, diagnosis,
prescription, dosage, or tool-execution writes. Proof neutrality remains intact
for non-stale candidates. These preserved boundaries do not override the failed
platform Gate.

Required return upstream before a new G4 attempt:

1. add RED regressions and enforce source-policy monotonicity, source identity,
   immutable evaluator evidence, recomputed inputs, and separated publisher
   authorization;
2. version every MCP call and enforce package retrieval/tool limits;
3. provide a package-scoped multi-release runtime with citation-bearing output,
   or mark search/chat unavailable until that runtime exists;
4. filter consumer imports to valid current lifecycle state and wire bounded
   package feedback into both consumers;
5. redact citation source paths, constrain fingerprint/hash formats and IDs,
   then rerun G3 and independent G4 review.

## G4 remediation checkpoint 1: publication trust boundary

**Decision: PASS for the first remediation batch; G4 remains NO-GO.** This batch
closes the source-policy, evaluation-provenance, package-ID, and publisher-token
findings. MCP versioning, the package-scoped runtime, consumer lifecycle and
feedback, citation redaction, trace constraints, and Health review progression
remain release blockers. No push or deployment was attempted.

Delivered:

- Package IDs now follow the JSON Schema's URL-safe identity rule.
- Every pinned release must have a non-empty authorized source type, and an
  `evidence_only` release cannot be downgraded into a `standard` package.
- Synthetic evaluation observations are derived from the package's pinned
  release claims, citations, chunks, allowed tools, abstention reasons, and
  bounded arguments rather than accepted from the suite fixture.
- Evaluation reports persist their trusted suite sidecar, recompute the input
  hash and approved evaluator output at save and publication time, and are
  immutable for a package content hash.
- `KBASE_AGENT_PUBLISHER_TOKEN` is separate from consumer and source-agent
  credentials; ordinary consumer tokens cannot call Package publication.

TDD and exact results:

- `go test ./backend/app -run 'TestAgentPackageRejectsUsagePolicyDowngrade|TestAgentPackageRejectsMissingSourceIdentity|TestAgentPackageRejectsNonURLSafePackageID|TestAgentPackageEvaluation|TestKBaseHTTPHandlerPublishesAndReadsAgentPackages' -count=1`
  — RED first because the trusted evaluator/store signatures and dedicated
  publisher configuration did not exist.
- The first GREEN attempt exposed immutable release-fixture reuse and a
  persisted `nil` versus in-memory empty failure slice; targeted regressions
  reproduced both. Test fixtures now create the intended immutable release,
  and trusted reports normalize the empty failure set before persistence.
- A second focused attempt exposed pointer-versus-value comparison inside the
  publication Gate; the persisted/recomputed identity regression failed before
  dereferencing and passed afterward.
- `go test ./backend/app ./cmd/kbase-server -run 'TestAgentPackageRejectsUsagePolicyDowngrade|TestAgentPackageRejectsMissingSourceIdentity|TestAgentPackageRejectsNonURLSafePackageID|TestAgentPackageEvaluation|TestKBaseHTTPHandlerPublishesAndReadsAgentPackages|TestDefaultAgentPublisherToken|TestValidateKBaseTokenSeparation' -count=1`
  — PASS.
- `jq empty contracts/agent-evaluation-v1.schema.json testdata/agent-evals/book-agent-v1.json`
  — PASS.
- `go test ./backend/app ./cmd/kbase-server -run 'AgentPackage|AgentEvaluation|KBaseHTTPHandlerPublishesAndReadsAgentPackages|KBaseTokenSeparation' -count=1`
  — PASS.
- `go test ./...` — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` and
  `bash scripts/system-map-smoke.sh` — PASS; regenerated because the HTTP
  configuration and evaluation store changed structural inventory.
- `bash scripts/privacy-smoke.sh` and `git diff --check` — PASS.

## G4 remediation checkpoint 2: versioned read boundary and bounded traces

**Decision: PASS for the second remediation batch; G4 remains NO-GO.** MCP
versioning, retrieval limits, citation redaction, and trace hash boundaries are
closed. The package-scoped runtime and the two consumer feedback/review paths
remain release blockers. No push or deployment was attempted.

Delivered:

- every read-only MCP resource and tool now requires `package_version` and
  loads that immutable version rather than mutable latest state;
- tool policy audit records include the version, reject missing/mismatched
  versions, and keep the version inside the deterministic argument fingerprint;
- MCP search rejects limits above `retrieval_policy.max_context_chunks` and
  bounds its default to that package limit;
- citation resolution returns an allowlisted citation view that excludes source
  HTML, source-account identity, and local-path-bearing fields;
- completed traces require retrieved evidence and final citations;
- trace/package/release/fingerprint and replay-result hashes use exact lowercase
  SHA-256 formats, trace IDs are bounded URL-safe identifiers, and trace loads
  verify the requested identity matches the stored object;
- evaluation tool-argument cases now cover the immutable package version.

TDD and exact results:

- `go test ./backend/app -run 'BookKnowledgeMCP|AgentToolPolicy|AgentTraceRejects' -count=1`
  — RED first: `package_version` was rejected as unknown while versionless calls
  still passed; raw fingerprints and ungrounded completed traces also passed.
- `go test ./backend/app -run TestAgentPackageEvaluationRequiresVersionedToolArguments -count=1`
  — RED first because a mismatched package version still scored the tool
  arguments metric as passing.
- `go test ./backend/app -run TestReplayAgentTraceIsDeterministicOverStoredEvidenceAndMockResults -count=1`
  — RED for an unbounded raw tool-result hash, then GREEN after replay validation
  required a SHA-256 value for executed results.
- `go test ./backend/app -run 'BookKnowledgeMCP|AgentToolPolicy|AgentTrace|AgentPackageEvaluation' -count=1`
  — PASS.
- `go test ./...` — PASS.
- `jq empty contracts/agent-trace-v1.schema.json` — PASS.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` and
  `bash scripts/system-map-smoke.sh` — PASS; regenerated because the scoped MCP
  citation and audit types changed structural inventory.

## G4 remediation checkpoint 3: package runtime and shared Book App

**Decision: PASS for the third remediation batch; G4 remains NO-GO.** The
package-scoped runtime and shared Book App findings are closed. Proofroom
lifecycle/feedback and Health review/feedback integration remain release
blockers. No push or deployment was attempted.

Delivered:

- package search and chat require an exact package version and pass the package
  evaluation Gate before execution;
- search covers every pinned release, preserves release/claim/citation identity,
  sorts deterministically, and rejects limits above
  `retrieval_policy.max_context_chunks`;
- grounded chat selects the package fallback model, applies its capability and
  timeout, uses the package prompt/output and safety/abstention policies, and
  returns allowlisted citations without source bodies or local source paths;
- completed, abstained, and failed model executions persist bounded SHA-256
  traces with package/release versions, retrieval ranks, model route, outcome,
  and final citation identity;
- consumer-authenticated HTTP routes expose only versioned package runtime
  search and chat;
- the shared Book App now calls those package routes, supports multi-release
  results, renders citation identities and explicit abstention, and no longer
  falls back to the generic single-book search/chat endpoints.

TDD and exact results:

- `go test ./backend/app -run AgentPackageRuntime -count=1` — RED first because
  the package runtime types and functions did not exist. The first GREEN attempt
  exposed a duplicate-version test-fixture conflict; isolating the fixture made
  all package search/chat behavior tests pass.
- `go test ./backend/app -run 'KBaseHTTPHandlerRunsVersionedAgentPackage' -count=1`
  — RED with `404 agent package not found`, then GREEN after the versioned
  package search/chat routes were added.
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs` — RED because Book
  App search still called the generic endpoint, then GREEN after package runtime
  migration and citation rendering.
- `go test ./backend/app -run 'AgentPackageRuntimeChat|AgentPackageRuntimeAbstains' -count=1`
  — RED because responses had no trace identity, then GREEN after completed and
  abstained traces were persisted.
- `go test ./backend/app -run TestAgentPackageRuntimePersistsFailedModelCall -count=1`
  — RED because a failed model call left no trace directory, then GREEN after
  fail-closed trace persistence was added.
- `go test ./backend/app -run 'AgentPackageRuntime|KBaseHTTPHandlerRunsVersionedAgentPackage' -count=1`
  — PASS.
- `go test ./...` — PASS.
- `node --check frontend-web/app.js` — PASS.
- `for smoke in frontend-web/scripts/*.mjs; do node "$smoke"; done` — PASS for
  all nine Web client smoke suites.
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json` and
  `bash scripts/system-map-smoke.sh` — PASS; regenerated because the HTTP
  runtime handler changed structural source inventory.

## G4 remediation checkpoint 4: Proofroom lifecycle and feedback

**Decision: PASS for the Proofroom remediation; G4 remains NO-GO.** Proofroom
continues to own retrieval adjudication and claim verdicts. The Health
review/feedback integration remains the final consumer blocker. No branch was
pushed and no deployment was attempted.

Delivered in Proofroom revision `c6618c3d`:

- package sync mirrors list-record lifecycle onto existing local projections,
  skips every non-`published` package before fetching its detail, and
  rechecks detail lifecycle before projection;
- projected package, release, claim, chunk, and citation records carry
  lifecycle state, while retrieval fails closed for records not currently
  marked `published`;
- the existing Proofroom claim judge remains the only component that assigns
  support or contradiction;
- after that judge runs, the production verification path asynchronously sends
  grouped, bounded KBase feedback containing only release/claim identifiers and
  outcome codes; missing configuration or feedback failure never changes the
  local proof verdict.

TDD and exact results:

- `python -m pytest -q tests/test_kbase_release_consumer.py -k 'superseded or non_published_local_versions or retrieved_package_claim'`
  — RED with a superseded detail request and both old/new versions returned,
  then GREEN with `3 passed, 6 deselected`.
- `python -m pytest -q tests/test_kbase_release_consumer.py -k 'adjudicates_package_candidates_without_counting'`
  — RED because no feedback call occurred, then GREEN after wiring the bounded
  sender into the post-judge path.
- `python -m pytest -q tests/test_kbase_release_consumer.py`
  — PASS, `9 passed in 3.34s`.
- the six-suite Proofroom contract command covering the KBase adapter,
  decision engine, claim-verifier routing/quota/cache, knowledge runtime, and
  legacy KBase sync — PASS, `128 passed in 8.74s`.
- the matching four-file `python -m py_compile` command — PASS.
- Proofroom has no `scripts/privacy-smoke.sh`; an added-line scan for
  machine-specific paths, private paths, and bearer literals found no matches,
  and `git diff --check` passed before commit.

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
