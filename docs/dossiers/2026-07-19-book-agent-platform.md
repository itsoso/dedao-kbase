# Book Agent Platform Dossier

**Status:** COMPLETE — G1-G6 PASS; production pilot verified on KBase, Proofroom, and Health

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
- **G3 Test: PASS.** Final clean-main matrices passed on KBase `6643420`,
  Proofroom `7ed622fa`, and Health `f58925b8` after all production-discovered
  citation and trace remediations.
- **G4 Review: PASS.** Independent architecture and cross-consumer reviews
  returned GO. The final Proofroom legacy citation resolver also received an
  independent GO with no Critical, High, Medium, or Low finding.
- **G5 Deployment health: PASS.** All three exact main revisions are deployed;
  public health, service status, restart counts, and product-specific health
  checks pass.
- **G6 Online verification: PASS.** The evaluated v1.1.0 pilot is published,
  searchable, executable, cited, and traced; Proofroom imports it idempotently
  and closes bounded feedback; Health holds it outside serving because its
  authorized usage policy is not `evidence_only`.

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

## G4 remediation checkpoint 5: Health review workspace

**Decision: PASS after correction and independent re-review.** The normal and
symbolic-link configured-directory paths now use one physical workspace and
one lock identity. Health retains review and safety ownership. Fresh
cross-repository G3 and G4 remain pending. No branch was pushed and no
deployment was attempted.

Delivered in Health revisions `1ea87f2d8` and `9a8f5f44f`:

- the admin review, adjudication, verification, finalize, preview, and publish
  APIs select either the configured release workspace or its fixed
  `agent-packages` child;
- public service functions no longer accept arbitrary artifact-directory
  overrides, and an `agent-packages` symlink escape is rejected;
- for a normal real configured root, release and Agent Package workspaces share
  one lock, and a parent release rebuild preserves the child workspace and its
  fingerprint;
- Agent Packages remain draft-only until Health-owned adjudication,
  finalization, and explicit publication; preview does not mutate serving data
  or personal health state.

TDD and exact verification results from the Health worktree with its project
virtual environment active:

- the four-test focused regression command covering parent rebuild, shared
  locking, service signatures, and child-symlink escape was RED with three
  failures before the implementation;
- `python -m pytest -o addopts='' -q` over the five focused lifecycle and API
  regressions — PASS, `5 passed, 6 warnings in 1.92s`;
- `python -m pytest -o addopts='' -q backend/tests/test_dedao_kbase_release_consumer.py backend/tests/test_dedao_kbase_export_importer.py backend/tests/test_system_knowledge_lifecycle.py backend/tests/test_system_knowledge_ingest.py backend/tests/test_kb_reconciliation.py backend/tests/test_kb_reconciliation_e2e.py backend/tests/test_kb_reconciliation_evalcase.py backend/tests/test_kb_reconciliation_judge.py backend/tests/test_kb_reconciliation_merge.py backend/tests/test_safety_failloud_consumers.py backend/tests/test_system_knowledge_phase0.py`
  — PASS, `224 passed, 6 warnings in 33.43s`;
- `python -m py_compile` over the five changed service/task/test files — PASS;
- `ruff check` over the changed workspace, lifecycle, and test files — PASS;
- `ruff check --ignore E402 backend/app/services/system_knowledge_service.py`
  — PASS; the project virtual environment did not contain a `ruff` executable,
  so the repository's installed formatter was used for both successful runs;
- `python scripts/check_doc_drift.py` — PASS. The change did not alter route or
  source inventory, so the Health system map was not regenerated;
- the Health repository has no `scripts/privacy-smoke.sh`; the added-line scan
  found no machine path, credential, key, or token literal, and
  `git diff --check` passed before commit;
- all commit hooks passed for `9a8f5f44f`.

The first mandatory independent safety re-review of `9a8f5f44f`: **NO-GO**. Review and
publish resolve the configured root, while release and Agent Package sync still
use the expanded but unresolved configured path. With a symlinked configured
root, those paths lock different files and parent replacement can replace the
symlink entry rather than the resolved workspace. Required upstream correction:
normalize sync and service paths to one resolved root, or reject a symlinked
configured root, then add a root-symlink lock/replacement regression and repeat
the safety review.

Final correction in Health revision `bff01c3b2`:

- `_dedao_kbase_review_artifact_dir()` now resolves the configured path before
  either release or Agent Package synchronization derives a target or lock;
- the root-symlink regression proves sync and review resolve to the same
  physical path, their locks serialize, a full release rebuild replaces the
  physical workspace, the configured link remains a link, and the rebuilt
  workspace is visible to review.

Exact final correction commands and results from the Health worktree with its
project virtual environment active:

- `python -m pytest -o addopts='' -q backend/tests/test_dedao_kbase_release_consumer.py::test_configured_release_root_symlink_uses_one_workspace_for_sync_and_review`
  — RED first, `1 failed, 6 warnings in 0.55s`, because sync returned the link
  path while review returned the physical path; GREEN after the one-line path
  normalization, `1 passed, 6 warnings in 1.18s`;
- the six-test focused command covering parent rebuild, parent/child locking,
  configured-root locking/replacement, workspace selection, service signatures,
  and child-link escape — PASS, `6 passed, 6 warnings in 2.61s`;
- the same eleven-file Health release matrix recorded above — PASS,
  `225 passed, 6 warnings in 33.50s`;
- `python -m py_compile backend/app/tasks/system_knowledge_lifecycle.py backend/tests/test_dedao_kbase_release_consumer.py`
  — PASS;
- `ruff check backend/app/tasks/system_knowledge_lifecycle.py backend/tests/test_dedao_kbase_release_consumer.py`
  — PASS;
- `python scripts/check_doc_drift.py` — PASS. No structural inventory changed,
  so the Health system map was not regenerated;
- the added-line privacy scan found no machine path, credential, key, or token
  literal, `git diff --check` passed, and every commit hook passed.

Mandatory independent safety re-review of `bff01c3b2`: **GO**. The reviewer
confirmed one resolved physical workspace and coordination lock across sync,
review, finalize, and publish; preservation of the fixed child during parent
replacement; rejection of arbitrary and escaping workspace paths; and no
serving, personal-health, diagnosis, prescription, dosage, or tool-execution
mutation before Health-owned review and explicit publication.

## Task 10 checkpoint: G3 revalidation

**Decision: PASS on the exact current three-repository heads.** G4 remains a
separate pending Gate. No branch was pushed and no deployment was attempted.

Verified revisions:

- KBase: `30c5b30`;
- Proofroom: `c6618c3d`;
- Health: `bff01c3b2`.

Exact commands and results:

- `go test ./...` in KBase — PASS;
- `npm run build` in `frontend/` — PASS. Vite reported existing bundle-size
  and dependency `eval` warnings but returned success after transforming and
  rendering the production bundle;
- `node --check frontend-web/app.js` and every
  `frontend-web/scripts/*.mjs` smoke — PASS;
- `bash scripts/knowledge-contract-smoke.sh`,
  `bash scripts/knowledge-eval-smoke.sh`,
  `bash scripts/proof-consumer-contract-smoke.sh`,
  `bash scripts/health-evidence-smoke.sh`,
  `bash scripts/source-agent-packaging-smoke.sh`, and
  `bash scripts/wcplus-agent-packaging-smoke.sh` — PASS;
- `jq empty contracts/agent-package-v1.schema.json contracts/agent-evaluation-v1.schema.json contracts/agent-trace-v1.schema.json`
  — PASS;
- Proofroom's six-suite command covering package consumption, decision engine,
  claim-verifier routing and quota/cache, knowledge runtime, and legacy KBase
  sync — PASS, `128 passed in 8.53s`;
- Proofroom `python -m py_compile` over its four changed Python files — PASS;
- the eleven-file Health release matrix — PASS,
  `225 passed, 6 warnings in 33.41s`;
- Health `python -m py_compile` over the seven changed Python files, `ruff
  check` over the normal changed files, and `ruff check --ignore E402` over the
  large service module — PASS;
- `python scripts/check_doc_drift.py` in Health and
  `bash scripts/system-map-smoke.sh` in KBase — PASS. No structural source
  inventory changed after the last generated artifacts, so neither system map
  was regenerated;
- `bash scripts/privacy-smoke.sh` in KBase — PASS. Proofroom and Health have no
  privacy-smoke script; their branch added-line scans found no machine path,
  credential, key, or token literal;
- `git diff --check` — PASS in all three repositories, and all three worktrees
  were clean after verification.

## Task 10 checkpoint: fresh G4 re-review

**Decision: NO-GO on exact heads `99b7414`, `c6618c3d`, and `bff01c3b2`.** Both
independent reviewers rejected release. G3 remains passed but does not override
G4. No files were changed by reviewers, and no branch was pushed or deployed.

Release blockers returned upstream:

1. **Critical — the evaluation Gate is immutable but not behavioral.** The
   trusted evaluator enumerates pinned IDs and declared policies but does not
   execute retrieval queries, grounded chat, abstention, tool selection, tool
   arguments, latency, or cost cases. A broken runtime can still score a
   perfect report and publish. Evaluation fixtures also contain expected IDs
   rather than executable inputs and expected observations.
2. **Critical — completed chat is not tied to citations actually present in the
   generated answer.** The runtime accepts model text without parsing citation
   markers, then attaches every retrieved citation to the response and trace.
   Missing or invented answer citations can therefore appear grounded. The
   citation-required no-evidence path also needs consistent trace persistence.
3. **High — supersession is not safely propagated or enforced.** KBase mutates
   the old list record, while cursor consumers positioned after that record see
   only the new version. Proofroom and Health can retain stale local lifecycle
   state. Exact-version HTTP runtime and MCP reads also fail to reject a
   superseded package, and MCP does not re-run the trusted evaluation Gate.
4. **High — Proofroom production feedback does not close every required
   outcome.** The production verifier emits `used`, `rejected`, and `conflict`,
   while `stale` and `zero_hit` are only reachable through the low-level helper
   or direct tests. Zero-hit cannot be emitted from the current post-candidate
   call site.
5. **Important — declared retrieval and cost policies are not faithfully
   executed.** Accepted lexical, vector, hybrid, and graph strategies currently
   share one lexical implementation while the response reports the declared
   strategy; `max_cost_usd` is validated but not enforced at runtime.
6. **Minor — MCP required-argument validation accepts some non-string values
   before later string conversion empties them.** Server-side schema rejection
   is weaker than the published strict tool contract.

Confirmed closed by both reviewers:

- authorized-source usage enforcement and bounded citation serialization;
- immutable evaluator provenance and publisher/consumer token separation;
- exact package-version routing, shared Book App package routes, and bounded
  trace hashes/replay;
- Proofroom verdict ownership and the feedback outcomes already wired;
- Health's resolved workspace, shared parent/child lock, atomic replacement,
  evidence-only draft review ownership, and absence of pre-review serving or
  personal-health mutation;
- privacy fixtures contain no downloaded source bodies, secrets, or local
  source paths.

Independent verification results:

- architecture reviewer: focused KBase package/evaluation/MCP/trace/runtime/HTTP
  tests — PASS; Proofroom six-suite matrix — PASS,
  `128 passed in 7.38s`; Health focused consumer/lifecycle matrix — PASS,
  `55 passed, 6 warnings in 19.37s`;
- cross-consumer safety reviewer: focused KBase suites and Book App smoke —
  PASS; Proofroom project-environment matrix — PASS,
  `128 passed in 7.11s`; Health eleven-file matrix — PASS,
  `225 passed, 6 warnings in 32.56s`;
- KBase privacy smoke and `git diff --check` — PASS; all three worktrees stayed
  clean throughout review.

## G4 remediation checkpoint 6: behavioral Gates and cursor consumers

**Decision: NO-GO.** KBase and Proofroom remediations passed their focused and
release-level checks, but the mandatory independent Health safety review found
two blocking retirement defects in Health revision `544adb16a`. G3
revalidation was stopped at that failed Gate. No branch was pushed and no
deployment was attempted.

Exact current revisions:

- KBase: `2e0ee67` (`0cc307b` behavioral runtime/evaluation hardening plus
  `2e0ee67` incremental supersession contract regression);
- Proofroom: `018d8d99`;
- Health: `544adb16a`.

Delivered and verified before the failed safety Gate:

- KBase now executes deterministic retrieval, citation, faithfulness,
  abstention, tool-selection, tool-argument, latency, and cost evaluation cases;
  runtime and MCP require a currently published package and a passing trusted
  evaluation; generated answer citation markers are validated before completed
  traces; retrieval strategy and model-cost declarations are enforced; strict
  MCP required arguments reject non-string values.
- `go test ./...` in KBase — PASS, including `backend/app` in `15.432s`;
  `npm run build` in `frontend/` — PASS with the existing Vite `eval` and large
  chunk warnings only.
- `go test ./backend/app -run
  'TestAgentPackageStoreSupersedesVersionsWithoutMutatingArtifacts' -count=1`
  — PASS; the regression confirms a consumer positioned after v1 sees only v2
  and receives `supersedes: package@v1`.
- KBase `bash scripts/privacy-smoke.sh` and `git diff --check` — PASS before
  both remediation commits.
- Proofroom cursor-only supersession, stale-match, and zero-hit tests were RED
  before production wiring and GREEN afterward; the focused adapter file —
  PASS, `12 passed in 3.90s`.
- Proofroom's six-suite release matrix in the repository virtual environment —
  PASS, `131 passed in 9.07s`; `python3 -m py_compile`, the added-line privacy
  scan, and `git diff --check` — PASS before commit. An earlier diagnostic run
  with system Python failed `22` async tests because `pytest-asyncio` was absent;
  it was not counted as a Gate pass and was rerun successfully in the project
  environment.
- Health's cursor-only supersession regression was RED because both old and new
  release artifacts remained, then GREEN with the two related lifecycle tests,
  `3 passed, 6 warnings in 2.57s`.
- The Health eleven-file release matrix was split only to preserve a trustworthy
  process exit code: group one — PASS, `102 passed, 6 warnings in 17.23s`;
  group two — PASS, `124 passed, 6 warnings in 17.56s`. `ruff check`, project-
  environment `python scripts/check_doc_drift.py`, `python -m py_compile`, the
  added-line privacy scan, and `git diff --check` — PASS. No structural source
  inventory changed, so the Health system map was not regenerated. The first
  commit attempt was correctly blocked when its hook used dependency-missing
  system Python; retrying with the repository virtual environment first in
  `PATH` passed every hook without bypass.

Mandatory independent Health safety review of `544adb16a`: **NO-GO**.

1. A mixed cursor page can contain a held replacement plus an unrelated
   eligible package. Supersession references are currently collected before
   Health eligibility assessment, so the held replacement can retire the prior
   draft even though Health rejected the replacement. Retirement must be
   derived only from an eligible, identity-validated replacement detail.
2. Removing the final Agent Package lineage currently drops the artifact row.
   If package ingestion replaced a canonical row with the same `doc_id`, this
   deletes shared canonical evidence instead of restoring its baseline form.
   The correction must restore canonical rows and add canonical-collision plus
   mixed held/eligible regressions.

The reviewer confirmed that the candidate-workspace lock/atomic replacement,
draft-only review boundary, explicit Health publication ownership, lack of
personal-health writes, and privacy boundary remain intact. Remediation and a
fresh independent safety review are required before G3/G4 can resume.

## G4 remediation checkpoint 7: Health retirement safety re-review

**Decision: NO-GO after partial correction.** Health revision `171d42f55`
closed both checkpoint-6 findings, but its fresh independent safety review found
one remaining record/detail identity-consistency blocker. G3/G4 remain stopped;
no branch was pushed and no deployment was attempted.

Corrections delivered in `171d42f55`:

- retirement authority is now derived only after the fetched replacement
  passes Health's evidence-only eligibility assessment;
- malformed, cross-package, self-version, and explicit list/detail
  supersession conflicts are held rather than applied;
- removing the final Agent Package lineage restores a matching canonical
  document by `doc_id` or canonical relation by
  `(src_doc_id, relation, dst_doc_id)`; rows shared by another package retain
  the remaining lineage;
- all changes remain inside the locked candidate workspace and preserve
  draft-only Health review, finalization, and explicit publication ownership.

Exact TDD and verification results:

- the mixed held/eligible cursor-page regression — RED because `release-one`
  disappeared when the held v2 shared a page with an unrelated eligible package;
- the canonical-collision regression — RED with a missing canonical page after
  final-lineage retirement;
- the four focused retirement/preservation regressions after correction — PASS,
  `4 passed, 6 warnings in 5.17s`;
- Health release matrix group one — PASS,
  `104 passed, 6 warnings in 19.25s`; group two — PASS,
  `124 passed, 6 warnings in 18.73s`;
- `ruff check`, project-environment `python -m py_compile`, project-environment
  `python scripts/check_doc_drift.py`, the added-line privacy scan, and
  `git diff --check` — PASS; all commit hooks passed. No structural source
  inventory changed, so the Health system map was not regenerated.
- KBase's six contract/consumer packaging smokes — PASS individually with
  trustworthy exit code `0`; all Web client smokes, schema JSON validation,
  system-map smoke, privacy smoke, and `git diff --check` — PASS. The combined
  diagnostic command's output session did not expose a trustworthy final exit
  code, so every release smoke was rerun individually before being counted.

Mandatory independent safety re-review of `171d42f55`: **NO-GO**.

The cursor record and fetched detail must agree exactly on package ID, version,
content hash, lifecycle state, and supersession field before the replacement can
authorize retirement. The current implementation does not compare content hash
or lifecycle, and permits an empty record supersession when detail supplies one.
A mismatched pair can therefore retire the prior draft. Required correction:
add mixed-page regressions for every mismatched identity/lifecycle/supersession
class and hold the replacement on any disagreement, then repeat the independent
safety review.

The reviewer reconfirmed canonical restoration, shared-lineage preservation,
workspace locking/atomicity, draft-only review ownership, and absence of serving,
personal-health, diagnosis, prescription, dosage, or tool-execution mutation.

## G4 remediation checkpoint 8: Health identity safety GO

**Decision: PASS for the mandatory Health safety Gate on revision
`e33beedfa`.** Full cross-repository G4 review remains pending. No branch was
pushed and no deployment was attempted.

Corrections and TDD evidence:

- cursor record and fetched detail are normalized and must agree on package ID,
  version, content hash, lifecycle state, and supersession before retirement
  authority is considered;
- missing list-record supersession now conflicts with a non-empty detail
  supersession instead of trusting the detail unilaterally;
- a mixed-page parameterized regression covers package-ID, version,
  content-hash, lifecycle, explicit-supersession, and missing-supersession
  disagreement while an unrelated eligible package proceeds;
- before correction the parameterized test was RED in the three missing classes:
  content hash, lifecycle, and missing list supersession (`3 failed, 3 passed`);
  after correction all retirement and preservation cases were GREEN,
  `9 passed, 6 warnings in 7.21s`.

Exact Health verification on `e33beedfa`:

- Agent Package consumer file — PASS, `59 passed, 6 warnings in 17.04s`;
- export/lifecycle/ingest/reconciliation group — PASS,
  `51 passed, 6 warnings in 6.20s`;
- reconciliation/safety/phase-0 group — PASS,
  `124 passed, 6 warnings in 18.72s`;
- total Health release matrix — `234 passed`;
- `ruff check`, project-environment `python -m py_compile`, project-environment
  `python scripts/check_doc_drift.py`, added-line privacy scan, and
  `git diff --check` — PASS; all commit hooks passed. No structural inventory
  changed, so the system map was not regenerated.

Mandatory independent safety re-review: **GO**. The reviewer confirmed exact
record/detail agreement, eligible-only retirement authority, malformed/cross/self
supersession holds, canonical document/relation restoration, shared-lineage
preservation, locked atomic candidate replacement, draft-only artifacts, Health-
owned adjudication/finalization/publication, and no serving, personal-health,
diagnosis, prescription, dosage, or tool mutation. The reviewer's own focused
pytest attempt could not collect because its shell lacked SQLAlchemy; it reported
that environment limitation accurately and relied on code inspection plus
successful compile/diff checks. The executor's project-environment release
matrix above provides the runtime test evidence.

## Task 10 checkpoint: fresh G4 after Health safety GO

**Decision: NO-GO on exact clean heads `a636820a1a7d343dad8da07e8df34ad1335b6ca6`,
`018d8d997d47620d35ab2a8bc7bbbfd550125f98`, and
`e33beedfa952f8d4e5d672a5f113e9b21c5c5e20`.** Both independent G4 reviewers
rejected release. No branch was pushed and no deployment was attempted.

Fresh G3 evidence on those revisions remained green:

- KBase `go test ./...` — PASS; `frontend/npm run build` — PASS with the
  pre-existing dependency `eval` and bundle-size warnings;
- all six KBase contract/consumer packaging smokes, Web client smokes, schema
  JSON validation, system-map smoke, privacy smoke, and `git diff --check` —
  PASS;
- Proofroom six-suite matrix — PASS, `131 passed in 11.32s`; compile and diff
  checks — PASS;
- Health release matrix — PASS, `234 passed`; lint, compile, document drift,
  privacy scan, and diff checks — PASS;
- reviewers independently reran focused KBase, Proofroom, and Health matrices
  successfully and confirmed all three worktrees were clean.

Release blockers returned upstream:

1. **Critical — no supported production surface creates the trusted evaluation
   sidecar required for publication.** Publication requires a persisted trusted
   report, but only tests call deterministic evaluation/save helpers. The HTTP
   publish test pre-seeds it with a test-only helper. A clean production pilot
   therefore cannot publish its first package through API, CLI, or task.
2. **Critical — content hashing sorts runtime-significant order.** Model
   fallbacks and prompt profiles are sorted during hash normalization while the
   runtime executes index zero. Two packages can therefore share one immutable
   content hash but execute different model or prompt behavior.
3. **High — the evaluation Gate is still only partially behavioral.** Retrieval
   executes, but faithfulness synthesizes an answer from expected evidence;
   tool choice selects a declared tool for any non-empty input; retrieval lacks
   precision measurement; latency/cost are local/static; task completion is not
   evaluated. The design's package-specific answer/tool/task/model observations
   are not yet established.
4. **High — declared vector/hybrid retrieval does not match the approved
   semantic-vector plus reranker design.** Current vector is token-frequency
   cosine, hybrid averages that with lexical overlap, no reranker exists, and
   graph fails closed. The declared strategy is materially stronger than the
   implementation.
5. **High — multi-release citation identity is ambiguous.** Package validation
   allows duplicate citation IDs across pinned releases. Runtime initially
   resolves `(release_id, citation_id)` but drops release identity, answer
   markers use only citation ID, selection maps overwrite collisions, and trace
   authorization can attribute one marker to multiple evidence items. Required
   correction: release-qualified citation identity end to end or reject
   cross-release citation-ID collisions with regressions.
6. **High — Proofroom applies list-record supersession before validating
   detail.** It retires the old projection, then fetches detail and checks only
   detail lifecycle; it never reconciles list/detail ID, version, content hash,
   lifecycle, or supersession. An adversarial reproduction retired live v1 and
   projected an unrelated package. Proofroom must adopt the same reconcile-
   before-retire discipline and mixed-page mismatch tests now used by Health.

Both reviewers confirmed closed: answer-bound citation markers and abstention;
trace persistence; published/evaluation enforcement in runtime and MCP; strict
MCP argument types; cursor-only supersession transport; all five Proof feedback
outcomes; Health exact eligible-only retirement/canonical restoration/review
ownership; authorized-source usage, separated publisher authentication, and
privacy boundaries.

G4 remediation is required before any push or deployment. The next execution
must begin with TDD for the production evaluation surface, hash-order invariant,
cross-release citation collision, and Proof list/detail reconciliation, then
resolve the evaluation/retrieval design gaps without weakening the approved
Gate thresholds.

## G4 remediation checkpoint 9: executable evaluation and semantic retrieval

**Decision: implementation PASS; G3/G4 revalidation pending.** The six findings
from the previous fresh G4 review are now addressed in local revisions KBase
`f7ee6d9` and Proofroom `0942fb7c`. Neither branch was pushed and no deployment
was attempted.

Delivered in KBase:

- publisher-authenticated `POST /api/agent-packages/evaluate` creates and
  immutably stores the first trusted suite/report sidecar, supports exact replay,
  and leaves publication gated on recomputation;
- package hashing preserves runtime-significant fallback-model and prompt-profile
  order;
- package validation rejects a citation ID pinned from multiple releases;
- deterministic golden evaluation now runs the shared grounded-chat execution
  path using suite model outputs, evaluates model-proposed tools and arguments,
  measures retrieval recall and exact precision separately, and adds task
  completion plus observed request/response cost and runtime latency;
- `vector` and `hybrid` retrieval now require an explicitly authorized
  OpenAI-compatible semantic embedder, persist a content-addressed numeric vector
  index, and use a distinct reranking stage. Missing embedding configuration
  fails closed instead of substituting lexical term frequency;
- the evaluation/package schemas, synthetic fixture, operator documentation,
  and generated system map were updated. No downloaded source body or secret was
  added.

Delivered in Proofroom:

- published list records are reconciled against detail on package ID, version,
  content hash, lifecycle, and supersession before any local mutation;
- supersession is applied only after the replacement projection succeeds;
- a six-case mixed-page regression proves mismatched replacements cannot retire
  the live version and do not prevent an unrelated valid package from importing.

TDD and exact verification results:

- KBase cross-release citation regression — RED with
  `cross-release citation collision error = <nil>`, then GREEN together with the
  production evaluation and hash-order regressions;
- KBase behavioral evaluation regressions — RED at compile time because
  `ModelOutput` and `ProposedTool` did not exist, then GREEN after shared-runtime
  execution and the new precision/task metrics;
- KBase semantic retrieval regressions — RED because no semantic embedder/index
  API existed, then GREEN with `ok .../backend/app 1.385s`; missing configuration
  also fails closed;
- `go test ./backend/app -count=1` — PASS in `15.048s`;
- `go test ./...` — PASS, including `backend/app` in `14.823s` and
  `cmd/kbase-server` in `1.766s`;
- `frontend/npm run build` — PASS with the pre-existing dependency `eval` and
  bundle-size warnings only;
- all six KBase contract/consumer packaging smokes — PASS; all Web client and
  desktop markdown/Book Knowledge smokes — PASS; schema JSON validation and
  `bash scripts/system-map-smoke.sh` — PASS;
- `bash scripts/privacy-smoke.sh` and `git diff --check` — PASS before the KBase
  commit; only the 17 listed feature files were staged;
- Proofroom mixed-page reconciliation regression — RED in all six parameterized
  cases, then GREEN with `8 passed, 10 deselected` including existing lifecycle
  cases;
- Proofroom six-suite release matrix in the project environment — PASS,
  `137 passed in 23.98s`; four-file `py_compile`, current-diff added-line privacy
  scan, and `git diff --check` — PASS before commit;
- a diagnostic Proofroom run with system Python returned `22 failed, 115 passed`
  because `pytest-asyncio` was not installed. It was not treated as Gate evidence
  and was rerun with the repository `.venv`, producing the passing result above.

The generated system map was regenerated because the semantic retrieval source
file and publisher evaluation route changed structural inventory. Full Health
revalidation and fresh independent G4 review remain required before push or
deployment.

## Task 10 checkpoint: final G3 after remediation 9

**Decision: G3 PASS on clean local heads KBase `4400baa`, Proofroom
`0942fb7c`, and Health `e33beedfa`.** G4 remains pending, so no push or
deployment was attempted.

Exact final evidence:

- KBase `go test ./...` — PASS; all six contract/consumer packaging smokes,
  every Web client smoke, both desktop frontend smokes, schema JSON validation,
  system-map smoke, privacy smoke, and `git diff --check` — PASS;
- the KBase production frontend build on the same implementation revision —
  PASS with only the existing dependency `eval` and bundle-size warnings;
- Proofroom six-suite release matrix in the repository environment — PASS,
  `137 passed in 9.09s`; four-file `py_compile`, branch added-line privacy scan,
  `git diff --check`, and clean-worktree check — PASS;
- Health eleven-file release matrix in the project backend environment — PASS,
  `234 passed, 6 warnings in 39.68s`; four-file `py_compile`, document drift,
  normal changed-file `ruff check`, service-module `ruff check --ignore E402`,
  branch added-line privacy scan, `git diff --check`, and clean-worktree check —
  PASS;
- the first Health command selected a nonexistent worktree-local environment
  and failed collection with `ModuleNotFoundError: No module named 'sqlalchemy'`.
  It was diagnostic only, was not counted as Gate evidence, and the identical
  matrix passed with the repository backend environment above;
- the first Health lint path pointed at an environment without a `ruff` binary
  and returned exit `127`; it was not counted, and the repository's lint
  environment then passed both required commands.

All three feature worktrees were clean at the checkpoint. Fresh independent G4
architecture and consumer-safety review is the next required Gate.

## Task 10 checkpoint: fresh G4 after remediation 9

**Decision: G4 NO-GO on exact clean heads KBase
`ed17c9185d4f295a1edbde1b323f5a4b996ae018`, Proofroom
`0942fb7c9d625e55f905bb070ee7a0e1d379d10b`, and Health
`e33beedfa952f8d4e5d672a5f113e9b21c5c5e20`.** Both independent reviewers
rejected release. G3 remains passed but cannot override G4. No branch was pushed
and no deployment was attempted.

Release blockers returned upstream:

1. **High — collision rejection does not cover every runtime-reachable
   citation.** Validation checks only citation IDs explicitly listed on each
   package release reference. Runtime search can return a matched claim's full
   citation list even when an ID is outside that reference list. Two releases
   can therefore pin different valid decoy IDs while their searchable claims
   expose the same unlisted ID, reaching the bare-ID answer selector and trace
   attribution. Required correction: reject collisions across all reachable
   release/claim citations and enforce the reference allowlist at retrieval, or
   carry release-qualified citation identity end to end. Add the adversarial
   unique-ref/duplicate-claim regression.
2. **High — semantic retrieval identity is not bound to the immutable package.**
   The embedder endpoint/provider/model and reranker version are selected outside
   the hashed policy. The same evaluated package hash can retrieve differently
   across deployments, and providers sharing a model label can reuse an
   incompatible index. Pin authorized embedder provider/model/version and
   reranker version in hashed package policy, and preserve that identity in
   evaluation, trace, and vector-index provenance.
3. **Medium — deterministic evaluation mixes in wall-clock latency.** Evaluation
   uses elapsed host time while persisted reports are later recomputed and
   compared exactly. A near-threshold case can pass creation but fail publication
   because of host load. Separate observed performance evidence from
   deterministic recomputation or inject a stable timing observation.

The reviewers independently reran focused KBase, Proofroom, and Health suites.
KBase full/focused Go checks, system-map/privacy/diff checks, Proofroom's
six-suite matrix (`137 passed`), and Health focused consumer/lifecycle checks all
passed; every worktree remained clean. They confirmed Proofroom reconciliation,
Health evidence-only ownership, publisher-token separation, read-only MCP
policy, hash-order preservation, production evaluation API, semantic vector
index/reranker presence, and privacy boundaries are otherwise closed.

Execution stops at this failed Gate. The next continuation must begin with TDD
for the three findings above and repeat full G3 plus fresh independent G4 before
any push or deployment.

## G4 remediation checkpoint 10: citation scope and retrieval provenance

**Decision: implementation PASS in KBase revision `74dce4e`; full cross-repo G3
and fresh G4 pending.** No branch was pushed and no deployment was attempted.

Corrections:

- chat/runtime retrieval filters every claim citation through the package
  release reference allowlist and drops claims with no authorized citation;
- MCP search applies the same filter, citation resolution rejects IDs outside
  the allowlist, and claim lookup returns only authorized citations or fails;
- package validation continues rejecting cross-release collisions among allowed
  IDs and now also rejects duplicate citation IDs inside one pinned release;
- semantic provider, model, model version, exact endpoint URL fingerprint, and
  reranker version are required fields in the hashed retrieval policy;
- runtime configuration must match that immutable identity; vector indexes,
  evaluation reports, and runtime traces record the same embedder/reranker
  provenance;
- golden latency uses an immutable recorded observation while still executing
  the shared runtime path. Publication recomputation no longer compares a
  wall-clock measurement affected by host load.

TDD and exact results:

- unique reference IDs plus duplicate unlisted claim citations — RED with two
  ambiguous runtime results, then GREEN after allowlist enforcement;
- MCP unlisted claim citation regression — RED with the forbidden claim in the
  result, then GREEN together with the existing pinned-release MCP test;
- semantic retrieval identity hash regression — RED at compile time because the
  five identity fields did not exist, then GREEN after adding them to policy;
- trace provenance regression — RED at compile time because
  `RetrievalRoute`/`AgentTraceRetrievalRoute` did not exist, then GREEN after
  trace contract and runtime persistence changes;
- stable latency regression — RED at compile time because
  `RecordedLatencyMS` did not exist, then GREEN with deterministic repeated
  reports;
- duplicate citation within one release — RED with validation error `<nil>`,
  then GREEN together with cross-release and runtime-scope regressions;
- combined seven-regression command — PASS, `backend/app 1.500s`;
- `go test ./backend/app -count=1` — PASS in `15.196s` before the final duplicate
  citation addition; the subsequent focused duplicate/collision/scope command
  also passed;
- `go test ./...` — PASS, including `backend/app` in `13.170s` and
  `cmd/kbase-server` in `1.340s`;
- frontend production build — PASS with the existing dependency `eval` and
  bundle-size warnings only;
- all six contract/consumer packaging smokes, all Web and desktop frontend
  smokes, schema JSON validation, regenerated system map and drift check,
  privacy smoke, and `git diff --check` — PASS;
- only the 18 feature files listed by the staged diff were committed.

The system map was regenerated because the trace contract added a structural
retrieval-route type. Proofroom and Health code did not change in this
checkpoint; both must still be included in the next full G3 run.

## Task 10 checkpoint: final G3 after remediation 10

**Decision: G3 PASS on clean local heads KBase `8a12351`, Proofroom
`0942fb7c`, and Health `e33beedfa`.** G4 remains pending; no push or deployment
was attempted.

Exact evidence:

- KBase `go test ./...` — PASS; all six contract/consumer packaging smokes,
  every Web and desktop frontend smoke, schema JSON validation, system-map drift,
  privacy smoke, and `git diff --check` — PASS;
- KBase frontend production build — PASS with only the existing dependency
  `eval` and bundle-size warnings;
- Proofroom six-suite release matrix — PASS, `137 passed in 12.22s`; four-file
  `py_compile`, branch added-line privacy scan, `git diff --check`, and clean
  status — PASS;
- Health eleven-file release matrix — PASS,
  `234 passed, 6 warnings in 40.70s`; four-file `py_compile`, document drift,
  both required `ruff` commands, branch added-line privacy scan,
  `git diff --check`, and clean status — PASS;
- all three exact worktrees were clean after verification.

Fresh independent architecture and consumer-safety G4 review is now required on
these remediated heads before any release action.

## Task 10 checkpoint: fresh G4 after remediation 10

**Decision: G4 NO-GO on exact clean heads KBase `a6c870d`, Proofroom
`0942fb7c`, and Health `e33beedfa`.** The independent architecture reviewer
returned GO with no Critical, High, or Medium blockers. The independent
consumer-safety reviewer found a High persisted-package integrity gap, so the
Gate failed and release progression stopped. No branch was pushed and no
deployment was attempted.

Architecture review evidence:

- focused KBase backend and server suites — PASS in `8.240s` and `0.962s`;
- KBase `go test ./...`, schema validation, system-map drift, privacy smoke,
  `git diff --check`, and clean status — PASS;
- Proofroom release matrix — PASS, `137 passed in 13.12s`;
- Health focused consumer/lifecycle matrix — PASS,
  `64 passed, 6 warnings in 17.57s`;
- all earlier citation allowlist, retrieval identity, deterministic latency,
  consumer-owned review, and evidence-only Health boundaries were accepted;
- one Low advisory remains non-blocking: replay canonical input and OTLP
  attributes do not yet include the already persisted `RetrievalRoute`.

Consumer-safety review evidence:

- exact heads and clean status — confirmed;
- KBase citation/MCP/evaluation/trace suite — PASS in `2.791s`;
- Proofroom focused reconciliation — PASS, `8 passed, 10 deselected`; full
  six-suite matrix — PASS, `137 passed`;
- Health consumer suite — PASS, `59 passed`;
- High blocker: `loadAgentPackageRecordUnlocked` read the content-addressed
  artifact without recomputing its hash or validating the package. Runtime and
  MCP then ran only the evaluation Gate. An altered persisted retrieval policy
  could therefore retain the manifest hash and reach execution without a
  complete package-contract check. The reviewer process was interrupted by an
  automated platform classifier after reporting the finding and expected
  NO-GO; the finding was independently reproduced locally and is treated as a
  failed Gate.

## G4 remediation checkpoint 11: persisted package integrity

**Decision: focused remediation PASS; full G3 and fresh G4 pending.** Public
package reads now verify artifact identity, recompute and compare the
deterministic content hash, verify immutable publication metadata, release the
store lock, and then rerun the complete Agent Package contract. Runtime, MCP,
HTTP, and consumer reads therefore share the same fail-closed boundary. The
route set did not change, but the generated route source locator moved when the
read boundary grew, so the system map was regenerated after its drift Gate
failed. No branch was pushed and no deployment was attempted.

TDD and focused evidence:

- `go test ./backend/app -run TestAgentPackageStoreRejectsTamperedArtifactOnLoad -count=1`
  — RED: the altered persisted retrieval policy loaded with `<nil>` error;
- the same regression plus atomic publication, supersession, and mutable
  version tests — GREEN, `ok .../backend/app 1.388s`;
- `go test ./backend/app -run 'AgentPackage|AgentRuntime|BookKnowledgeMCP|AgentTrace' -count=1`
  — PASS, `ok .../backend/app 3.608s`;
- `go test ./cmd/kbase-server -count=1` — PASS,
  `ok .../cmd/kbase-server 1.025s`;
- the first full KBase G3 attempt completed Go, race, frontend, and smoke
  checks, but `bash scripts/system-map-smoke.sh` reported the generated route
  locator changing from line `282` to `304`; G3 was marked failed rather than
  allowing later commands to mask it;
- `go run ./cmd/system-map --root . --out docs/_generated/system-map.json`,
  `bash scripts/system-map-smoke.sh`, and `git diff --check` — PASS after
  regeneration. A fresh full KBase G3 run remains required.

## Task 10 checkpoint: final G3 after remediation 11

**Decision: G3 PASS on KBase implementation revision `a508a32`, Proofroom
`0942fb7c`, and Health `e33beedfa`.** Fresh independent G4 remains pending, so
no branch was pushed and no deployment was attempted.

Exact final evidence:

- KBase `go test ./...` — PASS;
- KBase `go test -race ./backend/app ./cmd/kbase-server -count=1` — PASS,
  backend `17.660s` and server `1.911s`; the macOS linker emitted the existing
  non-fatal `LC_DYSYMTAB` warnings;
- KBase frontend production build — PASS with the existing dependency `eval`
  and bundle-size warnings;
- every `frontend/scripts/*.mjs` and `frontend-web/scripts/*.mjs` smoke plus
  `node --check frontend-web/app.js` — PASS;
- all six contract/consumer packaging smokes, all three Agent JSON Schemas,
  system-map drift, privacy smoke, and `git diff --check` — PASS;
- Proofroom six-suite matrix — PASS, `137 passed in 9.16s`; four-file
  `py_compile`, branch added-line privacy scan, `git diff --check`, and clean
  status — PASS;
- Health eleven-file release matrix — PASS,
  `234 passed, 6 warnings in 41.88s`; seven-file `py_compile`, normal changed
  file `ruff`, service-module `ruff --ignore E402`, document drift, branch
  added-line privacy scan, `git diff --check`, and clean status — PASS.

The successful KBase rerun used `set -euo pipefail`, so any failing command
would stop the Gate instead of being hidden by a later successful check. The
only KBase working-tree changes after verification are this dossier checkpoint
and the regenerated source-locator metadata in the system map.

## Task 10 checkpoint: refresh consumers onto current main

**Decision: G3 remains PASS on deployable local heads KBase `7d285a9`,
Proofroom `0942fb7c`, and Health `a762fa99b`.** Read-only remote refresh showed
KBase canonical `dedao-kbase/main` at `dd6bc9c` and Proofroom `origin/main` at
`9b155400`; both are direct ancestors of their feature heads and merge-tree
checks were clean. Health `origin/main` had advanced to `e135100d`, so the
feature branch was refreshed before final G4. No branch was pushed and no
deployment was attempted.

Health integration evidence:

- `git merge-tree --write-tree origin/main HEAD` — correctly reported one
  conflict limited to `docs/_generated/system-map.json`;
- `git merge --no-ff --no-commit origin/main` — reproduced only that generated
  conflict; `python scripts/dump_system_map.py` regenerated the merged source
  inventory and the subsequent document-drift check passed;
- the eleven-file Health matrix on the merged source — PASS,
  `234 passed, 6 warnings in 39.79s`; seven-file `py_compile`, both required
  `ruff` commands, document drift, and `git diff --check` — PASS;
- the first merge-commit attempt was rejected by the doc-drift hook because
  system Python lacked `sqlalchemy` and `pydantic`; it was not bypassed. The
  same commit was rerun with the repository backend environment on `PATH`, and
  every pre-commit hook passed;
- KBase privacy smoke, Health staged added-line privacy scan, and
  `git diff --check` — PASS immediately before merge commit `a762fa99b`;
- the Health worktree was clean and `11` commits ahead of current
  `origin/main` after the merge.

Fresh independent architecture and cross-consumer G4 review is required on
these exact deployable heads.

## Task 10 checkpoint: final G4 after persisted-package remediation

**Decision: G4 NO-GO on exact clean heads KBase `c50c3af`, Proofroom
`0942fb7c`, and Health `a762fa99b`.** Architecture review returned GO with no
Critical, High, or Medium blockers. Cross-consumer review found one Medium
citation-scope blocker, so release progression stopped. No branch was pushed
and no deployment was attempted.

Architecture evidence:

- KBase focused package/runtime suite, focused race suite, and server suite —
  PASS in `4.694s`, `3.304s`, and `0.919s`;
- KBase system map, privacy smoke, `git diff --check`, and clean status — PASS;
- Proofroom matrix — PASS, `137 passed in 9.88s`;
- Health focused consumer/lifecycle matrix — PASS,
  `64 passed, 6 warnings in 17.84s`; document drift and clean status — PASS;
- persisted artifact integrity, post-unlock full validation, citation
  allowlists inside KBase, semantic retrieval identity, deterministic
  evaluation, read-only MCP, traces, and consumer-owned review were accepted;
- a Low non-blocking recommendation remains: canonicalize package-list detail
  URLs instead of trusting stored list metadata.

Cross-consumer NO-GO evidence:

- KBase runtime filtered claims and citations against each hashed release
  reference's `citation_ids`, but Proofroom and Health validated the references
  and then projected each complete release;
- a package allowing citation A from a release containing A and B could
  therefore place B in both consumer projections. Proofroom adjudication and
  Health human review reduced serving risk but did not preserve deterministic
  package scope;
- KBase focused checks and Proofroom's `18` focused tests passed. The reviewer's
  Health shell lacked project dependencies, so its Health pytest and drift
  attempts were not counted; the primary G3 environment had already passed
  both and the Medium code finding was independently reproduced.

## G4 remediation checkpoint 12: consumer citation scope

**Decision: implementation and consumer matrices PASS; fresh G4 pending.**
Proofroom now intersects release citations and claim citation IDs with the
hashed package reference, projects only chunks reached by allowed citations,
and drops claims with no allowed citation. Health now deep-copies and scopes
each release before compilation, removes unlisted citations, chunks, claims,
and release-wide summary content, and still emits draft-only artifacts for
Health-owned review. No source inventory was added; Health document drift
passed without regenerating its system map. No branch was pushed and no
deployment was attempted.

TDD and verification evidence:

- Proofroom focused import regression — RED: the package authorizing one
  citation projected `2` claims, `2` citations, and `2` chunks; GREEN after the
  fix, `1 passed in 0.79s`, with the existing test's exact `1/1/1` assertions;
- Health focused package compiler regression — RED: projected claim IDs were
  `claim-1` and `claim-unlisted`; GREEN after the fix,
  `1 passed, 6 warnings in 0.06s`, followed by the strengthened full-projection
  assertion passing in `0.31s`;
- Proofroom six-suite matrix — PASS, `137 passed in 8.26s`; four-file
  `py_compile`, `git diff --check`, and clean status — PASS; commit
  `c3324fec`;
- Health eleven-file matrix — PASS,
  `235 passed, 6 warnings in 38.31s`; seven-file `py_compile`, both required
  `ruff` commands, document drift, and `git diff --check` — PASS; feature
  commit `110947e2d`;
- Health `origin/main` then advanced by one documentation commit. The clean
  merge tree passed the same eleven-file matrix and document drift,
  `235 passed, 6 warnings in 40.47s`; final merged head `b7b1a610d`;
- KBase privacy smoke, both consumer added-line privacy scans, every
  pre-commit hook, and `git diff --check` — PASS before the commits;
- final G3 deployable heads are KBase `c50c3af`, Proofroom `c3324fec`, and
  Health `b7b1a610d`. KBase source was unchanged from its passing full G3;
  Proofroom and Health passed their complete feature release matrices above.

Fresh independent architecture and cross-consumer G4 review is required on
these exact clean heads before any push or deployment.

## Task 10 checkpoint: final G4 after consumer citation remediation

**Decision: G4 PASS on exact clean heads KBase `eabd98e`, Proofroom
`c3324fec`, and Health `b7b1a610d`.** Both independent reviewers returned GO
with no Critical, High, or Medium blockers. Push, clean-main integration, and
deployment may now proceed under the remaining privacy, G5, and G6 Gates.

Architecture review evidence:

- Proofroom intersects package/release citations, projects only reachable
  chunks and claims with allowed citations, and retrieval serves only projected
  claim rows;
- Health scopes a deep-copied release before compilation, removes unlisted
  citations, sources, claims, and release-summary content, and retains
  draft/review-required/not-serving metadata;
- persisted package integrity, post-lock validation, read-only MCP,
  deterministic evaluation, citation-bounded runtime, traces, lifecycle, and
  consumer-owned review remain intact;
- KBase focused backend/server tests, system-map drift, privacy smoke,
  `git diff --check`, canonical-main ancestry, and all three clean-worktree
  checks — PASS.

Cross-consumer review evidence:

- the A+B fixtures prove only citation A, its reachable chunk, its claim, and
  its scoped summary survive when the package authorizes only A;
- KBase focused package/runtime checks — PASS in `3.768s`; privacy and
  system-map checks — PASS;
- Proofroom focused consumer suite — PASS, `18 passed in 2.88s`; compile,
  commit diff, and clean status — PASS;
- Health focused Agent Package suite — PASS,
  `23 passed, 37 deselected, 6 warnings in 30.68s`; document drift, compile,
  feature-range diff, and clean status — PASS;
- record/detail reconciliation, explicit Proofroom adjudication, Health's six
  mismatch holds, evidence-only human approval, authorized-source boundaries,
  and fixture privacy remain intact.

One Low non-blocking defense-in-depth note remains: canonicalize package-list
detail URLs at delivery and construct same-origin detail URLs in consumers
rather than relying on stored list metadata. It does not alter the G4 GO.

## Release checkpoint: publish reviewed branches

**Decision: reviewed revisions are present on their canonical GitHub remotes;
clean-main integration remains pending for KBase and Proofroom.** No deployment
was attempted.

Exact results:

- KBase privacy smoke, `git diff --check`, and clean status — PASS; explicit
  push created `dedao-kbase/codex/book-agent-platform` at `cd95d87`;
- Proofroom privacy and diff checks — PASS. Its `origin` has two push URLs. The
  GitHub push created `refs/heads/codex/book-agent-consumer` at exact reviewed
  commit `c3324fec`, while the subsequent company-network mirror connection
  failed and made the combined command exit `128`. An explicit GitHub
  `ls-remote` confirmed the reviewed commit, and the failed mirror is not used
  by the production deployment;
- Health privacy and diff checks — PASS. Repository configuration
  `remote.origin.push=HEAD:refs/heads/main` mapped the requested feature push
  directly to `main`, advancing GitHub main from `bb68c0ae5` to exact reviewed
  head `b7b1a610d`. GitHub reported that expected status checks were bypassed by
  repository permissions. The pushed revision had already passed G3 and both
  G4 reviews, but all remaining pushes will use explicit refspecs to avoid
  implicit mappings;
- `git ls-remote` confirmed Health `refs/heads/main` at `b7b1a610d`; its local
  feature worktree is clean and tracks that exact remote main.

G5 and G6 remain pending. KBase and Proofroom must next be integrated and
reverified from clean `main` clones before explicit main pushes.

## Release checkpoint: clean main integration and production preflight

**Decision: all three reviewed revisions are on canonical GitHub `main`, but
deployment is BLOCKED before G5 by missing production secrets.** No service was
restarted and no production artifact, environment file, knowledge package, or
consumer state was mutated.

Canonical main revisions:

- KBase `bfb82fd` on `dedao-kbase/main`;
- Proofroom `c3324fec` on `origin/main`;
- Health `b7b1a610d` on `origin/main`.

Clean-main integration and verification:

- fresh temporary clones checked out actual `main`; KBase and Proofroom were
  fast-forwarded with `--ff-only` from their reviewed remote feature branches;
  Health cloned directly at the reviewed main revision;
- KBase's first clean-clone attempt ran Go before generating the ignored
  `frontend/dist` embed input and failed with `pattern all:frontend/dist: no
  matching files found`. It was not counted. The rerun used the required order:
  `npm ci`, production frontend build, `go test ./...`, race tests, every Web
  and desktop smoke, all six contract/packaging smokes, Schema validation,
  system-map drift, privacy smoke, and `git diff --check` — PASS. Race tests
  passed in `18.807s` and `2.662s`; only the existing macOS linker warnings,
  frontend `eval`/bundle warnings, and dependency audit findings were reported;
- Proofroom clean-main six-suite matrix — PASS,
  `137 passed, 2 warnings in 8.56s`; compile, diff, and clean status — PASS;
- Health clean-main eleven-file matrix — PASS,
  `235 passed, 6 warnings in 40.66s`; document drift, diff, and clean status —
  PASS;
- immediately before each main push, the remote main was fetched, ancestry and
  clean status were checked, KBase privacy smoke was rerun, and explicit
  `HEAD:refs/heads/main` refspecs were used. `ls-remote` confirmed all three
  canonical revisions above.

KBase production preflight:

- public `https://kbase.executor.life/health` — HTTP 200 with the expected
  service payload;
- authoritative DNS resolved the host to the production server; SSH confirmed
  systemd unit `dedao-kbase` is active and runs
  `/opt/dedao-kbase/bin/kbase-server` as the dedicated service user;
- `/opt/dedao-kbase` is an artifact/data installation rather than a Git
  checkout, and the server has no Go toolchain. A clean-main local cross-build
  produced a static Linux amd64 server binary with SHA-256
  `74cb1871949701a0e4f311e361253b17d3573e8e4e634516e9afb4bc1c975e8f`;
- environment presence checks (values were never printed) found
  `KBASE_AUTH_TOKEN` present but `KBASE_AGENT_PUBLISHER_TOKEN`,
  `KBASE_EMBEDDING_BASE_URL`, `KBASE_EMBEDDING_PROVIDER`,
  `KBASE_EMBEDDING_MODEL`, `KBASE_EMBEDDING_VERSION`, and
  `KBASE_EMBEDDING_API_KEY` missing;
- these values are required to keep publication authority separate and to
  reproduce the hash-bound semantic evaluation/retrieval identity. Creating or
  selecting production credentials requires explicit human secret ownership,
  so deployment and G6 verification stopped before any mutation.

Unblock requirement: configure an independently governed publisher token and
an approved, pinned embedding endpoint/provider/model/version/API key in the
production KBase secret store, then resume G5 deployment from the clean main
revision and run the full package publication, Proofroom import, Health
hold/import, citation, receipt, and feedback-closure online checks.

### Blocker recheck after requested resume

The public health probe and production environment presence-only check were
repeated on 2026-07-20. Exact results:

- `/health` returned `{"ok":true,"service":"dedao-kbase"}`;
- `KBASE_AUTH_TOKEN=present`;
- `KBASE_AGENT_PUBLISHER_TOKEN=missing`;
- `KBASE_EMBEDDING_BASE_URL=missing`;
- `KBASE_EMBEDDING_PROVIDER=missing`;
- `KBASE_EMBEDDING_MODEL=missing`;
- `KBASE_EMBEDDING_VERSION=missing`;
- `KBASE_EMBEDDING_API_KEY=missing`.

The blocker is unchanged. No secret values were read or printed, and no
production mutation or deployment was attempted.

## KBase production deployment checkpoint

**Decision: KBase G5 PASS after one failed attempt was automatically rolled
back and corrected upstream.** The user explicitly authorized server-local
secret generation and production configuration. Secret values were never
printed, copied into the repository, or written to this dossier.

Embedding service and secret configuration:

- production capacity check found x86-64, approximately 30 GiB RAM, 368 GiB
  free disk, and active Docker; port `11434` and container name
  `kbase-embedding` were unused;
- official Ollama `0.32.0` was pulled and pinned to image digest
  `sha256:57f573b47f1f71ebb445789f279fe3e596a8beab182f7cf486db9205bad87c5a`;
- the container uses `restart=unless-stopped`, a persistent named model volume,
  and publishes only `127.0.0.1:11434`;
- `embeddinggemma:300m-bf16` was pulled and pinned to model digest
  `85462619ee721b466c5927d109d4cb765861907d5417b9109caebc4e614679f1`
  with reported size `621875917` bytes;
- OpenAI-compatible Chinese batch probe — PASS: two results, 768 dimensions,
  all finite and non-zero;
- the server generated a random dedicated publisher token and embedding client
  key locally. The environment atomically received the loopback endpoint,
  provider `ollama-local`, pinned model/version, and both generated secrets;
  presence checks passed, the publisher token differed from admin and source
  tokens, and `/etc/dedao-kbase/kbase.env` remained `0600 root:root`.

First deployment attempt and rollback:

- a macOS cross-build made with `CGO_ENABLED=0` had SHA-256
  `5e80ceb80faa3e5bb28002ed24742d8e9a39a18845c05f58131cda73b92a7a58`;
- production startup failed closed with `go-sqlite3 requires cgo to work`;
- the rollout transaction restored both the previous environment file and
  previous binary, restarted the old service, and confirmed `/health` healthy;
  no Gate was bypassed and this attempt was not counted as G5 PASS.

Upstream correction and Linux verification:

- the exact clean-main source archive for `a0a77ed` had SHA-256
  `65346e0916c035f156c20cb25d895ded3871fa089589be3276cf84ca167f6768`;
- official Go `1.23.12` Linux amd64 toolchain archive matched published SHA-256
  `d3847fef834e9db11bf64e3fb34db9c04db14e068eeb064f49af747010454f90`;
- the first Linux test process was stopped after diagnosis showed production
  network SYN attempts to `proxy.golang.org` could not complete. Re-running
  through reachable `https://goproxy.cn,direct` kept normal `go.sum` checksum
  validation and passed `go test ./...`: backend app `11.346s`, services
  `3.173s`, utils `0.455s`, KBase server `0.078s`, source agent `0.109s`,
  system map `0.095s`, and WC Plus agent `0.163s`;
- the Linux CGO production binary had SHA-256
  `1165fd82ba978c455047a1265bb048195d99a2ce4489f42395246ba1c470298b`,
  was dynamically linked only to the production glibc surface, and passed an
  isolated service-user SQLite/health smoke before rollout.

Successful rollout and G5 evidence:

- the second atomic rollout succeeded with the Linux CGO binary; local and
  public `/health` returned `{"ok":true,"service":"dedao-kbase"}`;
- systemd reported `active`, `NRestarts=0`, and logs confirmed the Agent Package
  publisher API was enabled with a dedicated token;
- authorization probes returned anonymous package list `401`, consumer package
  list `200`, consumer evaluation attempt `401`, and publisher-authenticated
  invalid evaluation payload `400`, proving the dedicated privilege boundary
  without creating an evaluation artifact;
- the live local embedding probe returned one finite 768-dimensional vector;
  Docker reported the pinned image running with the expected restart policy and
  `ss` confirmed the endpoint listened only on loopback.

KBase is healthy at G5. Proofroom and Health deployment verification plus the
pilot publication/import/feedback sequence remain pending; G6 has not been
claimed.

## Proofroom deployment and Health online-contract checkpoint

**Decision: Proofroom G5 PASS; Health deployment stopped at a real online
contract failure and returned upstream.** No Health service or production data
was mutated.

Proofroom deployment evidence:

- production was healthy but behind at `9b155400`; its 84 worktree entries were
  inspected before the repository deployment script's hard reset. Deleted
  tracked entries were generated static assets, and every untracked code/data
  path was absent from the new remote tree, so none would be overwritten;
- the existing KBase consumer token was transferred directly between the two
  servers without printing it and atomically added to
  `/etc/browser-llm/env.conf`; the file remained `0600 root:root`;
- the first local frontend preflight had no `node_modules` and failed only with
  missing-module TypeScript errors. It was not counted. After `npm ci`, the
  TypeScript/Vite production build and static verification passed on clean
  `c3324fec`;
- the locked server deployment reset to exact `main@c3324fec`, installed
  dependencies, built and verified 111 manifest assets, atomically selected
  `react-app-c3324fec`, restarted the backend, verified anonymous auth `401`,
  and verified nine public frontend assets at HTTP 200;
- online `/api/deploy-health` reported `ok=true`, DB and scheduler healthy, and
  matching Git/frontend SHAs `c3324fec`; `/api/llm/health` reported `ok=true`,
  six exposed models, and a configured token; systemd reported `active` and
  `NRestarts=0`;
- a server-side authenticated request from Proofroom to KBase returned HTTP
  200. At that point no Agent Packages had been published.

Health preflight evidence:

- after the reviewed `b7b1a610` head, Health `main` advanced to `f58925b8` by
  mobile, generated-type, and documentation commits. `b7b1a610` is an ancestor,
  and the Book Agent integration, lifecycle task, tests, and deployment script
  are unchanged between the revisions;
- the current clean `main@f58925b8` eleven-file backend/reconciliation/safety
  matrix passed: `235 passed, 7 warnings in 86.80s`; document drift, diff, and
  clean status checks passed;
- the production-form root environment authenticated to KBase with HTTP 200,
  but the response field inspection returned `packages_type=NoneType`;
- Health's consumer correctly requires `packages` to be a JSON array and would
  fail closed. Health deployment was therefore stopped before mutation.

Upstream empty-list contract remediation:

- RED: `go test ./backend/app -run
  TestKBaseHTTPHandlerListsEmptyAgentPackagesAsArray -count=1` failed with
  `empty package list encoded as null` and body
  `{"next_cursor":"","packages":null}`;
- the store now returns a non-nil empty `[]AgentPackageRecord`, preserving the
  same behavior for non-empty pages while normalizing every caller;
- focused HTTP/store tests — PASS in `3.348s`;
- `go test ./...` — PASS, including backend app `35.040s` and KBase server
  `2.624s`;
- `go test -race ./backend/app ./cmd/kbase-server` — PASS in `32.226s` and
  `4.160s`, with only the existing macOS linker warnings;
- knowledge contract/evaluation, Proof consumer, Health evidence, source-agent,
  and WC Plus packaging smokes — PASS; system-map drift, privacy smoke, and
  `git diff --check` — PASS;
- independent focused review returned GO on exact code commit `0fc2687` with no
  Critical, High, or Medium findings. Publication/pagination/empty-page tests
  passed in `2.825s`, the broader Agent Package suite passed in `7.779s`, and
  the reviewer confirmed non-empty ordering/cursor behavior and both consumer
  contracts remain intact;
- no structural inventory changed, so system-map artifacts were not regenerated.

The remediation is not yet deployed. G6 remains BLOCKED until the reviewed
hotfix is on clean main, KBase is redeployed, and the Health probe receives
`packages:[]` before Health deployment resumes.

## KBase empty-page hotfix deployment checkpoint

**Decision: KBase G5 remains PASS on reviewed `main@e5b86bb`; the online
contract blocker is closed.** No production secret was printed or changed.

Release and build evidence:

- immediately before both explicit pushes, `bash scripts/privacy-smoke.sh`,
  `git diff --check`, and clean-status checks passed; `git fetch` plus
  `git merge-base --is-ancestor` passed before the main push, and
  `git ls-remote` confirmed both `main` and `codex/book-agent-platform` at
  `e5b86bbd0c0b3c9e14bd51eddc7b941b544ceeb9`;
- the clean-main frontend production build passed. The exact source archive
  SHA-256 was
  `7975846c2f7d4de4caaec5904a41f1d832630da5b3f41e73085984f7c640c600`;
  an interrupted first transfer produced a different server hash and was
  rejected before extraction. The resumed transfer matched exactly;
- the server reused the previously checksum-verified official Go `1.23.12`
  Linux amd64 toolchain and normal module checksum verification. Linux CGO
  `go test ./...` passed: backend app `9.944s`, services `3.559s`, utils
  `0.484s`, KBase server `0.080s`, source agent `0.143s`, system map `0.114s`,
  and WC Plus agent `0.178s`;
- the reviewed Linux CGO binary SHA-256 is
  `26c4e5bc3d11489ee6748bca35597d30e4f5564d64f833b33f5dfbdb8780fcd1`.
  Its only dynamic runtime dependency is the production glibc surface;
- an isolated service-user SQLite/health smoke passed and returned
  `packages_type=list packages_count=0` before rollout;
- the atomic rollout retained the prior binary at
  `/opt/dedao-kbase/bin/kbase-server.backup-e5b86bb-20260721101132` and
  succeeded without invoking rollback.

Online verification:

- local and public health passed; systemd reported `active/running`,
  `ExecMainStatus=0`, and `NRestarts=0`;
- anonymous list, authenticated consumer list, consumer evaluation, and
  publisher-authenticated invalid evaluation returned `401`, `200`, `401`,
  and `400` respectively;
- the authenticated empty collection now returns a JSON array with zero
  elements, closing the Health fail-closed preflight blocker;
- the loopback embedding probe returned a finite, non-zero 768-dimensional
  vector. Docker reported the pinned container running with
  `restart=unless-stopped`, the pinned model digest prefix `85462619ee72`, and
  port `11434` remained loopback-only.

Health deployment may now resume. G6 remains pending until the consumer
deployment and pilot publication/import/feedback sequence pass online.

## Health consumer production deployment checkpoint

**Decision: Health G5 PASS on clean `main@f58925b8`; consumer review and
safety ownership remain in Health.** The Agent Package task remains explicit,
draft-only, and unscheduled.

Preflight and deployment evidence:

- a fresh fetch confirmed `origin/main` remained
  `f58925b8a4ad1d9abbf16ebe567f96dac818a71d`; the deployment clone was clean,
  its root environment was linked to the existing production-form secret
  source without copying values into Git, and `git diff --check` passed;
- the first deployment invocation stopped before production mutation because
  the clean clone was on a detached HEAD. It exited `128`; switching the same
  exact revision onto local branch `main` closed that precondition;
- the next attempt completed the database and environment backups, but its
  unconditional 42 MiB Git bundle upload was interrupted by the server's SFTP
  path. Production remained on the prior service revision and healthy;
- the retry used the same deployment script with a process-local `scp`
  compatibility wrapper that routed only the deploy-bundle destination through
  resumable `rsync`; environment-file transfers still used normal `scp`. No
  repository or production script was edited;
- the final backup succeeded at
  `/opt/health-app/backups/health_db_2026-07-21_10-18.sql.gz` (`40M`, mode
  `0600`), including both force-RLS data checks. The previous environment was
  backed up as `backend/.env.backup.20260721_101845`;
- the server reset its clean `main` to exact `f58925b8`; dependency install,
  managed migration validation (`applied: none`), food baseline seed, Phase 0
  seed, and V2 artifact import completed before the deployment transaction
  restarted backend, Celery worker, and Celery beat;
- the local SSH transport detached while the long V2 import was still running.
  Process inspection proved the one existing import transaction continued; no
  duplicate deployment was started. It exited and performed the planned
  service restarts.

Online verification:

- backend, Celery worker, and Celery beat are `active/running`, each with
  `ExecMainStatus=0` and `NRestarts=0`; the deployed repository is clean
  `main@f58925b8`;
- local and public `/api/v1/health` report API, database, Redis, and Celery
  healthy;
- `system_health_score.py --skip-tests --url http://localhost:8000 --json`
  returned `60/60`, `pass=true`, 11 ms health latency, 6 ms API P95, and zero
  errors in the sampled 200 log lines;
- the public skills manifest count matches the clean release: `22` local and
  `22` online;
- a production-process-form authenticated KBase probe returned HTTP 200 with
  `packages_type=list packages_count=0`, confirming the fail-closed consumer
  can now execute safely.

G6 remains pending because no pilot Agent Package has yet been evaluated,
published, imported into Proofroom, or held/imported as a Health review draft.

## G6 pilot publication preflight and citation-resolution remediation

**Decision: G6 remains BLOCKED at the evaluation input Gate; no Agent Package
was evaluated or published.** The only production Knowledge Release is
authorized for `standard` use, so the intended pilot disposition is Proofroom
import plus a Health `non_evidence_only` hold. It will not be relabeled as
medical evidence.

Read-only production inspection found one release,
`release-43a7dbb5062e51e383597c1452dfe5b187a2ce8b78690915f18cb1bc8819bcbb`,
with four release citations and five claims. No source body was printed or
copied into the repository. A transient private-server generator attempted to
build and locally pre-evaluate a lexical, read-only pilot package containing
all ten required evaluation metrics. It failed before writing its output:

```text
release has no claim with a resolvable citation
FAIL github.com/yann0917/dedao-gui/backend/app
```

Identifier-only inspection showed the historical analysis claims correctly
reference `source-4f7d471215fe4cbc-chunk-1`, while the immutable release
citations use IDs such as `source-4f7d471215fe4cbc-citation-1` and point back
to that chunk. Quality validation permits both chunk and citation references,
but the new Agent runtime filtered only direct citation IDs. The failed Gate
was therefore returned upstream; no evaluation report, package, receipt,
consumer import, or feedback event was created.

TDD remediation status:

- RED: `go test -v ./backend/app -run
  '^TestBookKnowledgeMCPReadsOnlyPinnedPackageRelease$' -count=1` failed with
  retrieval, retrieval-precision, citations, faithfulness, and task-completion
  all scoring zero after the fixture claim was changed to the production-form
  chunk reference;
- implementation resolves a claim reference through immutable release
  citations by exact citation ID first, then by chunk ID, preserving release
  order and de-duplicating IDs. Unknown references remain fail-closed at the
  existing package allowlist filter. Both `agent.search` and `agent.get_claim`
  use the same resolver;
- GREEN: the focused MCP test passed in `1.729s`; the broader Agent
  Package/MCP selection passed in `4.974s`;
- `go test ./...` passed, including backend app `18.781s`, services `7.602s`,
  utils `4.256s`, KBase server `2.948s`, source agent `2.746s`, system map
  `3.058s`, and WC Plus agent `2.682s`;
- `go test -race ./backend/app ./cmd/kbase-server` passed in `22.130s` and
  `3.111s`, with only the existing macOS linker warnings;
- knowledge contract/evaluation, Proof consumer, Health evidence,
  source-agent packaging, WC Plus packaging, and system-map smokes passed;
  privacy smoke and `git diff --check` passed;
- independent G4 review returned GO with no Critical, High, or Medium finding.
  The reviewer independently passed focused MCP tests in `2.905s`, MCP plus
  Agent Runtime tests in `1.697s`, MCP race tests in `3.239s`, `gofmt -d`,
  privacy smoke, and `git diff --check`; the only Low suggestion was to lock
  one-to-many ordering/de-duplication and direct-ID precedence in tests;
- those suggested table-driven regressions were added. Focused normal tests
  passed in `1.760s` and focused race tests passed in `2.987s`, again with only
  the existing macOS linker warning;
- no structural source inventory changed, so system-map artifacts were not
  regenerated.

The remediation has not yet been committed, pushed, or deployed. G6 remains
blocked until clean-main rollout and the complete pilot sequence pass.

## G6 pilot evaluation, publication, and runtime trace checkpoint

**Decision: G6 remains BLOCKED after publication because the real online chat
Gate failed.** No Proofroom or Health import was attempted, and no feedback
closure was claimed.

Citation-resolution rollout:

- privacy smoke, `git diff --check`, clean status, remote-main ancestry, and
  explicit feature/main pushes passed; both remote refs reached
  `0c15973a807a5248b8fded0035f7a25a98aca85f`;
- exact clean-main source archive SHA-256 was
  `e0c4736a1e34a9f71d9bfc05b52c2f68b24b633a82d6719b8a911d3bb1ed1efe`;
- server-side Linux CGO `go test ./...` passed: backend app `9.847s`, services
  `3.264s`, utils `0.462s`, KBase server `0.085s`, source agent `0.128s`,
  system map `0.118s`, and WC Plus agent `0.175s`;
- the production Linux CGO binary SHA-256 is
  `d364e4fbfe4652332a72f28c0b73d75be7278193a99adc6aba520f9b757cfe4d`;
  isolated service-user SQLite/health/empty-list smoke passed;
- atomic rollout retained
  `/opt/dedao-kbase/bin/kbase-server.backup-0c15973-20260721104359` and passed;
  production is `active/running`, `ExecMainStatus=0`, `NRestarts=0`, and public
  health is OK.

Pilot Gate sequence:

- the first regenerated evaluation input failed before file creation because
  the package declared a preferred capability without an executable fallback:
  `model_policy has no executable fallback model`;
- adding the approved configured fallback to the private transient package
  input, without changing thresholds, passed local deterministic preflight:
  one package, one release, all ten metrics, private file mode `0600`;
- publisher-only `/evaluate` returned HTTP 201 with
  `deterministic-agent-evaluator.v1`, `passed=true`, ten metrics, and no
  failures;
- publisher-only `/publish` returned HTTP 201 for
  `book-agent-clinical-trials-truth@1.0.0`, state `published`, content hash
  `sha256:eaec5a77b520cc7feeb1a5926a3391c2de3bc39d2e7b1b1eb96cf0c71cfa95a0`;
- authenticated online search returned HTTP 200, lexical strategy, one result,
  `claim-1`, and one resolved citation;
- real online chat returned HTTP 500. The configured TokenPlan service rejected
  the package fallback `qwen-plus` as `model_not_found`; while persisting the
  required failed trace, a second contract mismatch surfaced: the immutable
  legacy Knowledge Release stores its digest as 64 lowercase hex, whereas
  Agent Trace requires a `sha256:` fingerprint. The combined error was
  `model call failed ...; persist failed trace: releases[0].content_hash must be
  a lowercase sha256 fingerprint`.

The published v1.0.0 package is retained as an auditable failed pilot and will
be superseded, not overwritten, after the trace fix. The server's configured
model is `MiniMax-M2.5`; no credential value was printed.

Trace remediation TDD:

- RED: `TestAgentPackageRuntimeTraceNormalizesLegacyReleaseContentHash` failed
  with `releases[0].content_hash must be a lowercase sha256 fingerprint`;
- the trace projection now prefixes only an exact 64-character lowercase hex
  digest. Already-prefixed values are unchanged and malformed/uppercase values
  still fail trace validation. Package and Knowledge Release content hashes are
  not mutated;
- focused legacy-hash, failed-model, and completed-chat tests passed in
  `1.852s`; broader Agent Runtime/Trace tests passed in `2.550s`;
- `go test ./...` passed, including backend app `16.277s`, KBase server
  `1.115s`, source agent `3.142s`, and WC Plus agent `1.745s` (other packages
  cached); race tests passed in `19.569s` and `3.027s` with only the existing
  macOS linker warnings;
- all knowledge, evaluation, Proof consumer, Health evidence, packaging,
  system-map, privacy, and diff smokes passed; no structural inventory changed.
- independent G4 review returned GO with no Critical, High, or Medium finding.
  Focused Runtime/Trace passed in `3.243s`, full backend app in `16.217s`,
  KBase server in `0.945s`, focused race in `5.629s`, and formatting/privacy/diff
  checks passed;
- the review's only Low suggestion was converted into table-driven tests for
  already-prefixed, uppercase, non-hex, wrong-length, and legacy raw hashes,
  plus failed/completed/abstained outcomes. Focused normal and race tests passed
  in `1.639s` and `2.768s` respectively.

The trace remediation is not yet committed, pushed, or deployed. G6 remains
blocked.

## G6 trace rollout, successful runtime, and Proofroom projection remediation

**Decision: the KBase runtime portion of G6 is PASS; G6 remains BLOCKED at the
Proofroom import Gate pending the reviewed consumer fix.** The failed v1.0.0
pilot remains immutable and auditable. A new v1.1.0 package supersedes it and
passes evaluation, publication, retrieval, real model execution, citation, and
trace checks. The first Proofroom import then exposed a consumer-side legacy
identifier mismatch and was returned upstream before Health import or feedback
closure.

Trace rollout evidence:

- the reviewed trace remediation was committed as `6643420`, pushed to both
  the feature ref and canonical `main`, and deployed from an exact clean-main
  source archive with SHA-256
  `d2b6d4c105104f0458c6660646aae80d78c0f5516e8015715b92c26e2f59abde`;
- server-side Linux CGO `go test ./...` passed: backend app `9.901s`, services
  `3.338s`, utils `0.618s`, KBase server `0.070s`, source agent `0.112s`, system
  map `0.098s`, and WC Plus agent `0.145s`;
- the production binary SHA-256 is
  `5a3b2f123f01ce8f5935285586b58e19f42f87baf7ae206162c6902ba6c33eaf`;
  the retained atomic backup is
  `/opt/dedao-kbase/bin/kbase-server.backup-6643420-20260721105916`;
- systemd reported `active/running`, `ExecMainStatus=0`, and `NRestarts=0`;
  public health and authenticated empty-list checks passed;
- replaying v1.0.0 still returned the expected configured-model
  `model_not_found`, but the failed trace now persisted successfully as
  `agent-run-72ea...`, outcome `failed`, with a normalized release fingerprint
  and no `persist failed trace` secondary error.

Successful v1.1.0 pilot evidence:

- the private transient evaluator input remained server-only with mode `0600`;
  it contained no downloaded source body in Git and declared the actually
  configured `MiniMax-M2.5` model;
- deterministic preflight passed all ten required metrics; publisher-only
  `/evaluate` returned HTTP 201 for suite `pilot-book-agent-v2`, `passed=true`;
- publisher-only `/publish` returned HTTP 201 for
  `book-agent-clinical-trials-truth@1.1.0`, content hash
  `sha256:2ff1f0b5540d0aef4fcf6e46e60778e8bedd6c50c84697b755d2e67fe1c8fbd8`;
  v1.0.0 became `superseded` and v1.1.0 is `published`;
- authenticated online search returned HTTP 200 with one result and one
  citation; real chat returned HTTP 200, outcome `completed`, model
  `MiniMax-M2.5`, and one citation;
- trace `agent-run-06f5cdc259e4b72570164840cc83a876` loaded successfully with
  outcome `completed`, one citation, a normalized legacy release fingerprint,
  and none of the forbidden credential, source-body, private-prompt, or
  consumer-user-data fields.

Proofroom import Gate evidence:

- the production history database was backed up before the pilot and retained
  with mode `0600`;
- the first `sync_kbase_agent_packages` run for the configured consumer user
  projected one package, one release, four chunks, and four citations, but zero
  claims. The package projection points to v1.1.0 and the Proofroom service
  remained healthy at its exact deployed revision;
- identifier-only inspection found the same historical shape already fixed in
  KBase: Knowledge Release claims reference chunk IDs while the package
  allowlist contains citation IDs. Proofroom compared only direct citation IDs,
  so otherwise authorized claims were dropped. No claim was adjudicated and no
  feedback closure was attempted.

Proofroom TDD remediation:

- RED: `python3 -m pytest -q
  tests/test_kbase_release_consumer.py::test_agent_package_import_uses_cursor_and_preserves_evidence_identity`
  failed with expected claims `1`, actual `0`, `1 failed in 0.87s`;
- the consumer now resolves claim references by exact citation ID first, then
  by release citation `chunk_id`, preserving release order and de-duplicating;
  unknown references continue into the unchanged package allowlist filter and
  therefore remain fail-closed;
- GREEN: the focused import passed in `0.76s`; the original consumer suite
  passed `18` tests in `3.03s`;
- a defense-in-depth regression locks direct-ID precedence, one-to-many release
  ordering/de-duplication, and unknown-reference preservation. The expanded
  consumer suite passed `19 passed in 2.49s`;
- the project-environment six-suite release matrix passed
  `138 passed in 9.76s`; the four-file `python -m py_compile`, Proofroom
  added-line privacy scan, KBase privacy smoke, and `git diff --check` passed;
- independent G4 review returned GO with no Critical, High, Medium, or Low
  finding. The reviewer independently passed the two focused regressions
  (`2 passed, 17 deselected in 0.78s`), the six-suite matrix
  (`138 passed in 8.47s`), two-file compilation, added-line privacy scan, and
  `git diff --check`; it confirmed the allowlist remains fail-closed,
  projection remains idempotent, and Proofroom retains verdict ownership;
- no structural source inventory changed, so system-map artifacts were not
  regenerated.

The Proofroom fix is not yet committed, pushed, or deployed. Clean-main
rollout, idempotent import replay, delivery receipt, bounded feedback, Health
draft/hold isolation, and final G6 verification remain pending.

## Task 10 final checkpoint: G6 production pilot

**Decision: G6 PASS on exact deployed revisions KBase `6643420`, Proofroom
`7ed622fa`, and Health `f58925b8`.** All planned Tasks 1-10 and checkpoints are
complete. The pilot preserves authorized-source policy: no downloaded source
body or credential was added to Git, fixtures remain synthetic or
identifier-only, KBase tools remain read-only, Proofroom owns proof verdicts,
and Health owns domain review and serving approval.

Proofroom clean-main rollout:

- pre-commit privacy checks, `git diff --check`, the six-suite release matrix,
  and independent G4 GO passed before commit
  `7ed622fa fix(kbase): resolve chunk-backed claim citations`;
- privacy and diff checks passed again before explicit feature push. A clean
  `main` clone fast-forwarded from `c3324fec` to `7ed622fa`; its exact command
  `python -m pytest -q tests/test_kbase_release_consumer.py
  tests/test_decision_engine.py tests/test_claim_verifier_routing.py
  tests/test_claim_verifier_quota_query_cache.py tests/test_knowledge_runtime.py
  tests/test_dedao_kbase_sync.py` passed `138 passed in 9.38s`, followed by the
  four-file `python -m py_compile`, range diff, and clean-status checks;
- a fresh remote-main ancestry check passed and the explicit main push advanced
  `c3324fec..7ed622fa`; no force push or second push URL was used;
- production preflight found only the deployment script's expected prior
  frontend build residue plus untracked runtime data. The script's scope was
  inspected before `bash deploy/server-deploy.sh main` ran; it preserved
  runtime data, built and verified `111` manifest resources, atomically switched
  `static/react-app` to `react-app-7ed622fa`, verified nine public assets, and
  passed the anonymous-auth `401` check;
- public `/api/deploy-health` reports `ok=true`, database and scheduler healthy,
  and frontend/backend Git SHA `7ed622fa`; systemd reports `active`,
  `ExecMainStatus=0`, and `NRestarts=0`.

Proofroom pilot import and feedback closure:

- the first post-fix `sync_kbase_agent_packages` run returned one package, one
  release, five claims, four chunks, four citations, one skipped superseded
  package, and cursor `book-agent-clinical-trials-truth@1.1.0`;
- an immediate identical replay returned the same logical counts. The database
  remains exactly one package, one release, five claims, four chunks, four
  citations, and `19` stable links; KBase contains one imported receipt and one
  distinct receipt idempotency key for this consumer and release;
- `send_kbase_release_feedback` sent only outcome `used`, opaque `claim-1`, and
  a deterministic event fingerprint. Replaying it returned the same feedback
  ID. Assessment is `healthy`, `reverify_required=false`, with no trigger
  outcomes. The release's aggregate `used=2` includes one unrelated historical
  Health smoke from July 12 plus this Proofroom event, not a duplicate write;
- no query text, user data, source body, credential, or local path entered the
  receipt or feedback payload.

Health hold/isolation verification:

- the explicit unscheduled Celery task was invoked synchronously with
  `sync_dedao_kbase_agent_packages_draft.apply().get()` using the production
  process environment. It returned `status=held`, `mode=rebuild`,
  `package_count=0`, `serving_allowed=false`, and blocking reason
  `human_domain_review_required`;
- v1.0.0 was held for `stale` and `non_evidence_only`; published v1.1.0 was held
  for `non_evidence_only`. The canonical serving artifact fingerprint was
  identical before and after, and the Health-owned audit count advanced from
  zero to one with audit status `held`;
- no serving-index, diagnosis, prescription, dosage, tool-execution, or
  personal-data write occurred. Health review and safety ownership remained
  outside KBase;
- public `/api/v1/health` reports API, database, Redis, and Celery healthy;
  backend, Celery worker, and Celery beat are active with `ExecMainStatus=0` and
  `NRestarts=0`; `backend/scripts/system_health_score.py --skip-tests --url
  http://localhost:8000 --json` returned `60/60`, `pass=true`, and the public
  skills manifest contains the expected `22` skills.

Fresh final G3 verification:

- KBase clean `main@6643420`: `npm --prefix frontend ci --no-audit --no-fund`
  and `npm --prefix frontend run build` passed with only existing warnings;
  `go test ./...` passed, including backend app `15.372s`, services `6.416s`,
  utils `5.168s`, server `2.754s`, source agent `2.896s`, system map `3.377s`,
  and WC Plus agent `4.457s`; `go test -race ./backend/app
  ./cmd/kbase-server -count=1` passed in `19.910s` and `3.041s` with only the
  existing macOS linker warnings;
- every desktop and Web smoke, `node --check frontend-web/app.js`, all six
  knowledge/evaluation/consumer/packaging smokes, three Agent Schema parses,
  system-map drift, privacy smoke, `git diff --check`, and clean status passed;
- Proofroom clean `main@7ed622fa`: the six-suite matrix passed
  `138 passed in 9.38s`; compilation, diff, and clean status passed;
- the first Health matrix attempt was not counted because the clean deployment
  clone had no test database override and collection exited `4` against a
  nonexistent local PostgreSQL role. A diagnostic SQLite rerun inherited the
  clone's production review-directory setting and reported
  `3 failed, 232 passed`; all three failures selected that production path
  instead of the monkeypatched temporary workspace;
- the exact CI-safe Health Gate explicitly used
  `DATABASE_URL=sqlite:///:memory:`, `TZ=Asia/Shanghai`,
  `DEDAO_KBASE_REVIEW_ARTIFACT_DIR=''`, and `PYTHONPATH=backend`. The eleven-file
  release matrix then passed `235 passed, 7 warnings in 49.71s`; three-file
  compilation, both required `ruff` commands, document drift,
  `git diff --check`, and clean status passed on exact `main@f58925b8`.

Final online verification:

- KBase public health is OK and systemd is active with zero restarts. v1.0.0
  remains `superseded`; v1.1.0 remains `published` with evaluation
  `passed=true` and all ten metrics;
- a fresh authenticated search returned one result with one citation. A fresh
  real chat returned `completed` on `MiniMax-M2.5` with one citation; trace
  `agent-run-3cf7633a335759fb87fb1cc40b04af20` contains one retrieval, normalized
  release fingerprints, a completed final outcome, and none of
  `source_body`, `private_prompt`, `consumer_user_id`, or `credentials`;
- Proofroom public deploy health reports exact SHA `7ed622fa`; the final local
  projection is `1/1/5/4/4` for package/release/claim/chunk/citation;
- KBase feedback assessment remains healthy and its Proofroom imported receipt
  remains one row with one idempotency key; Health remains healthy after the
  explicit hold task with its serving fingerprint unchanged.

Post-close concurrency audit:

- after the exact `f58925b8` Health deployment and G6 checks completed, Health
  `origin/main` advanced to `0fa40389` through unrelated agenda, Agent Runtime
  write-reconciliation, deployment-script, CI, mobile, and generated-map work;
- `f58925b8` is an ancestor of `0fa40389`, and the Book Agent Health consumer,
  lifecycle task, and focused consumer test are byte-for-byte unchanged across
  that range;
- those concurrent changes were not deployed by this feature. A subsequent
  external release advanced Health production to `0fa40389`, and remote main
  then advanced to `c4be2399` through a dossier-only commit. Both ranges leave
  the Book Agent consumer, lifecycle task, and focused test unchanged;
- a post-release production read confirmed the original Health audit still has
  status `held`, `serving_allowed=false`, the same two package hold records,
  and a canonical serving fingerprint equal to the pilot's recorded base. G6
  test evidence remains tied to exact `f58925b8`; current public health was
  separately rechecked on the later production revision without attributing
  unrelated changes to this feature.

No structural source inventory changed in the final Proofroom remediation or
checkpoint documentation, so no system-map artifact was regenerated. Privacy
smoke and `git diff --check` remain mandatory for the final dossier commit and
push.

## Post-release incident: package article returns no result

**Status:** LOCAL G3 AND G4 PASS; COMMIT/DEPLOY PENDING. Production diagnosis reproduced a browser-visible
package page whose structured analysis manifest was `failed` with
`context_canceled`; the matching Nginx log showed `POST
/api/knowledge/pipeline/run` returning `504` after the configured upstream read
timeout. The service-local API could still load the package and source content,
so the failure was isolated to long synchronous TokenPlan analysis triggered by
the browser automation path, not missing package data.

Fix under test:

- Qwen3.7 TokenPlan requests from KBase now carry explicit
  `enable_thinking:false`, which matches the provider requirement for
  OpenAI-compatible structured JSON output;
- the browser "自动推进一次" action now advances one package per request instead
  of five sequential analyses, avoiding a single browser request owning several
  long model calls;
- the Web static asset version changed to force browsers to load the fixed
  JavaScript.

Exact commands and results so far:

- `go test ./backend/app -run TestGenerateBookAnalysisManifestDisablesThinkingForQwenStructuredOutput -count=1 -timeout=60s`
  — RED first: `EnableThinking` was missing from `BookTokenPlanConfig`; later
  GREEN after adding the request policy.
- `node frontend-web/scripts/book-knowledge-web-smoke.mjs` — RED first:
  pipeline automation did not include `limit: 1`; later GREEN after changing
  the browser request.
- `go test ./backend/app -run 'TestGenerateBookAnalysisManifestDisablesThinkingForQwenStructuredOutput|TestTokenPlanChatClientUsesOpenAICompatibleRequest' -count=1 -timeout=60s`
  — PASS.
- `go test ./backend/app -count=1 -timeout=120s` — PASS.
- `go test ./... -timeout=180s` — PASS.
- `node --check frontend-web/app.js && node frontend-web/scripts/book-knowledge-web-smoke.mjs`
  — PASS. A follow-up command that also named nonexistent optional scripts
  failed with `MODULE_NOT_FOUND`; it was not counted as a product verification.
- `for script in frontend-web/scripts/*smoke*.mjs; do node "$script"; done` —
  PASS for all existing Web smoke scripts.
- `npm --prefix frontend run build` — PASS with the existing Vite large-chunk
  warnings.
- `go test -race ./backend/app ./cmd/kbase-server -count=1 -timeout=180s` —
  PASS with existing macOS linker warnings.
- `bash scripts/knowledge-contract-smoke.sh && bash scripts/knowledge-eval-smoke.sh && bash scripts/proof-consumer-contract-smoke.sh && bash scripts/health-evidence-smoke.sh && bash scripts/source-agent-packaging-smoke.sh && bash scripts/wcplus-agent-packaging-smoke.sh && bash scripts/system-map-smoke.sh`
  — PASS after regenerating the system map.
- `bash scripts/privacy-smoke.sh && git diff --check` — PASS.
- G4 independent review `g4_incident_review_2` — PASS / Ready to merge: Yes;
  no Critical, Important, or Minor findings. Reviewer reran
  `go test ./backend/app -run 'TestGenerateBookAnalysisManifestDisablesThinkingForQwenStructuredOutput|TestTokenPlanChatClientUsesOpenAICompatibleRequest' -count=1 -timeout=60s`,
  `node --check frontend-web/app.js`,
  `node frontend-web/scripts/book-knowledge-web-smoke.mjs`,
  `bash scripts/privacy-smoke.sh`, and `git diff --check`.

No structural route, operation, command, or durable-object inventory changed.
`docs/_generated/system-map.json` was regenerated because adding
`BookTokenPlanConfig.EnableThinking` shifted generated Go type line numbers.

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
