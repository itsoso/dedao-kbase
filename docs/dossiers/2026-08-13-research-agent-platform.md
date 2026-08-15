# Research Agent Platform Dossier

**Date:** 2026-08-13

**Status:** Layer E implementation complete; G1–G5 passed; G6 remediation in progress

**Approved design:**
[Research Agent Platform Design](../plans/2026-08-13-research-agent-platform-design.md)

**Implementation plan:**
[Research Agent Platform Implementation Plan](../plans/2026-08-13-research-agent-platform.md)

## Objective and boundaries

Build a platform-level, dual-mode Research Agent runtime. Quick mode answers a
bounded question against a selected immutable knowledge package. Deep mode can
coordinate versioned knowledge, prior verified Research Runs, and a local
read-only Chatlog Worker to resolve identities, reconstruct timelines, compare
cases, detect conflicts, and publish a citation-verified report.

Complete Chatlog data remains on the local machine. The server may persist a
structured run, opaque locators and hashes, and only the minimal evidence
excerpts selected for the report. The Worker uses the existing shared Worker
token. This feature does not add request signing, message sending, message
editing, message deletion, bulk private-data export, or automatic publication.

## Delivery layers

1. Layer A: run contracts, durable store, and evidence privacy boundary.
2. Layer B: local Chatlog Worker and restricted macOS delivery.
3. Layer C: retrieval tools and role-separated orchestration.
4. Layer D: versioned Agent policy and Chinese operator workspace.
5. Layer E: gold evaluation, controlled release, and production proof.

## Gate decisions

| Gate | Status | Evidence |
|---|---|---|
| G1 Admission | PASS | Approved design, explicit modes, source boundaries, transition rules, budgets, and typed deep-research escalation are captured by Task 1 tests. |
| G2 Feasibility and risk | PASS | The macOS Chatlog Worker, shared-token control plane, bounded evidence promotion, typed outcomes, resumable orchestration, and private-data persistence boundaries have focused tests. A content-free loopback probe on 2026-08-14 returned HTTP 200 with a valid aggregate response. |
| G3 Tests | PASS | On 2026-08-14 the complete Task 14 suite passed without output truncation: module verification, vet, all Go tests, frontend build, every Web smoke, Research process smoke, Chatlog packaging smoke, direct-deployment smokes, system-map drift, privacy, and diff checks. The first attempt exposed a brittle mobile CSS smoke selector; its root cause was fixed and the complete suite was rerun from the beginning. |
| G4 Review | PASS | The seventh independent review of the exact staged integration candidate reported Critical 0, Important 0, Ready to merge Yes. It verified public v3/v4 schemas, HTTP/create-and-resume v4 enforcement, direct/Worker/derived tool authorization, policy-denied terminal behavior, and isolated process smoke. |
| G5 Deployment health | PASS | Reviewed revision `23375066acd429ae164e8f4bf2496503db9efc93` was deployed on 2026-08-15. Public and loopback health reported that exact revision; KBase, book-job Worker, three evolution Workers, and the macOS Chatlog Worker were active with zero failed start status. Installed binary hashes matched the staged candidates and the post-cutover warning window was empty. Recoverable server and evolution backups were retained under the revision-scoped deployment batch. |
| G6 Online verification | IN PROGRESS | The first production compile against real immutable Releases exposed two legacy-compatibility blockers before any Research Run was created: an older Dedao Release had an empty persisted source type despite a controlled Dedao identifier, and a real WeChat article Release carried `evidence_only` while study compilation forced `standard`. The narrowly scoped compiler remediation passed production-data read-only diagnostics and independent review; deployment and real quick/deep Runs remain required. |

## Current checkpoint

Tasks 1–12 are complete in the isolated `codex/research-agent-platform`
worktree. The platform now has durable Research Runs, privacy-bounded evidence,
the local Chatlog Worker protocol, role-separated orchestration, versioned v4
package policy, authenticated Research APIs, and the Chinese Research
workspace.

Task 13 adds the privacy-safe `research-agent-v1` synthetic suite. Its
deterministic gates cover retrieval scope, identity resolution and ambiguity,
timeline precision/recall, non-monotonic numeric trends, direct advice,
intervention/conflict extraction, case-transfer warnings, material-claim
citations, insufficiency, private projection, latency, and cost. Fabricated
recovery, ambiguous identity use, unsafe amount transfer, unsupported
conclusions, trend misclassification, and private projection are hard failures
that cannot be overridden by an aggregate score.

The trusted-suite path now accepts v4, stores the trusted-suite hash, recomputes
the report at persistence and publication time, and permits publication only
after the immutable evaluation passes. A v4 package that also declares an
evidence policy must pass both Research and evidence-audit hard gates before the
existing evidence runner accepts it. v1/v2 behavior remains on the existing
evaluation paths, while v3 remains reserved for WeChat/publication collection
packages introduced on canonical `dedao-kbase/main`.

The repeatable process-level smoke starts an isolated KBase server, fake
Chatlog/TokenPlan loopback service, and real `chatlog-agent once` process. It
creates a deep run, waits with a bounded timeout, and verifies the Worker
heartbeat, completed job, promoted Chatlog evidence, searched/cited scope,
verified conclusion support, and durable event chain. The same smoke also runs
the deterministic coordinator restart/resume, Worker-offline,
identity-ambiguous, trusted-evaluation, and v4-publication cases. Process output
is checked for private sentinel leakage.

## Real-data acceptance checkpoint

An authorized, content-free probe confirmed that the real Chatlog service is
reachable on exact loopback and returns a valid aggregate response. No message,
identity, locator, or source content was printed or committed.

The complete real-data acceptance remains **PENDING** because this worktree has
no running Research-enabled KBase instance, no locally discoverable published
collection package, and no configured TokenPlan credential. Consequently no
real run ID or content hash is recorded, and G5/G6 remain pending. This is not
treated as a passing production proof; Task 14 must supply the reviewed clean
revision, selected immutable package, runtime credential, deployment, and
online verification.

No later gate may be marked PASS before its stated evidence exists, and a
failed gate returns to the responsible upstream task.

## G4 NO-GO history

The first production-readiness review covered the full feature range from
`4b0ea5b9` through `fbc285bf` plus the Task 14 worktree diff. It made no code
changes and returned **NO-GO**.

Critical findings:

1. Chatlog search marked every returned message as selected, persisted all
   excerpts remotely, and exposed raw conversation/message references instead
   of locally opaque locators. This violates the approved minimal-evidence
   boundary.
2. Deep search discarded knowledge/prior-run search results before the model
   could observe them and request bounded fetches, so real retrieval could end
   as `zero_hit` even when search returned matches.
3. Identity resolution, timeline construction, numeric trend classification,
   and case comparison existed as isolated deterministic functions but were not
   invoked by the production orchestrator.

Important findings:

1. Worker failure responses cleared the lease owner but the client required it,
   terminal failure replay was not idempotent, and expired leases could report
   failure.
2. Identity confirmation wrote an isolated row without persisted candidates or
   an orchestrator resume path.
3. The Research detail API returned only the run shell while the UI expected
   evidence, analysis, conflicts, identities, conclusions, and citation links.
4. quoted-character, total-cost, and deep evidence-count budgets were
   configured but not enforced at runtime.
5. coordinator run leases were not renewed during long model calls and stage
   commits did not bind to a lease epoch.
6. per-role model selection was absent from cache identity and audit records,
   permitting stale cross-model reuse and incorrect provenance.

This failed review is permanent dossier history. Each remediation will append
its test evidence here; the NO-GO entry must not be removed or rewritten as if
the first review passed.

### Remediation progress

The first remediation batch closed three Important findings without changing
the G4 verdict:

- Worker fail/requeue now accepts owner-cleared queued/failed responses,
  rejects an expired lease, performs bounded conditional mutation, and supports
  identical terminal/requeue replay by target agent plus request hash.
- The resolved per-role model ID now participates in durable request identity,
  cache isolation, invocation audit, and the actual model call. Changing model
  configuration cannot reuse a response generated by another model.
- A coordinator renews its run lease throughout `Advance`; loss of renewal
  cancels the stage context before it can continue a long model call.

Focused verification passed:

```text
go test ./backend/app -count=1 -run 'TestResearchWorker|TestResearchOrchestrator'
go test -race ./backend/app -count=1 -run 'TestResearchOrchestrator|TestResearchCoordinator'
go test ./cmd/chatlog-agent ./cmd/kbase-server -count=1 -run 'TestChatlogAgent|TestResearchServer'
git diff --check
```

G4 remains **NO-GO** until the remaining privacy, retrieval, deterministic
analysis, budget, detail API, and identity-resume blockers are fixed and a new
independent review passes.

The second remediation batch closed the runtime-budget and public-detail
contracts and replaced direct Chatlog search promotion with a two-step private
candidate boundary:

- Evidence insertion now enforces each run's item and quoted-character limits
  atomically. Model responses that would exceed the cumulative USD budget are
  audited as `budget_exhausted` and are not written to the response cache.
- Verified conclusions retain their citation IDs. The authenticated run-detail
  response now exposes only redacted evidence, allowlisted analysis fields,
  verified conclusions with bounded same-origin citation links, and identity
  binding status. It does not expose identity IDs/hashes, raw locators, arbitrary
  analysis attributes, actors, or private failure messages.
- Confirming an identity binding atomically selects the internal subject and
  resumes a run stopped specifically for `identity_ambiguous`; unrelated
  terminal outcomes are not resumed.
- Chatlog search now uploads only unselected `sha256:` candidate references and
  occurrence times. Raw conversation/message references remain in a bounded
  Worker-local `0600` locator cache. The control plane stores only opaque
  candidates and schedules bounded `fetch_chat_message` jobs; only those
  fetched messages can become evidence.

Focused verification passed:

```text
go test ./backend/app -count=1 -run 'TestResearch(Evidence|Worker|Orchestrator|Analysis|Tool|Run|KBaseHTTPResearch)'
go test ./backend/app -count=1 -run 'TestKBaseHTTPResearchRunLifecycleBearerCompatibilityAndRedaction|TestResearchOrchestratorPersistsVerifiedConclusionCitations'
go test ./cmd/chatlog-agent -count=1
node frontend-web/scripts/book-knowledge-web-smoke.mjs
git diff --check
```

G4 remains **NO-GO**. Candidate-backed identity persistence, non-Chatlog search
observation/fetch, and production wiring for deterministic analysis remain in
the next remediation batch.

The third remediation batch connected the remaining retrieval and deterministic
analysis paths to production orchestration:

- `resolve_chat_identity` now returns only opaque candidate IDs and match flags.
  The control plane validates that boundary, persists candidate bindings, and
  automatically resolves exactly one strong match. Multiple plausible matches
  still fail closed and use the authenticated manual-confirmation path.
- Fact extraction now precedes deterministic timeline construction. Grounded
  timeline events, numeric trend classification, direct-advice conflicts, and
  historical/current case differences are persisted as typed analysis records
  and included in synthesizer/verifier context.
- Typed extractor output now supports measurements and cases with mandatory
  evidence references. A sequence such as `24,25,18` is classified as mixed
  with a downward net direction; it cannot be mislabeled monotonic.
- Deep knowledge search immediately fetches bounded evidence from the pinned
  release instead of discarding matches. Verified prior-run conclusions are
  promoted as prior-run evidence, while underlying private excerpts still
  require explicit locator verification.
- Prior-run search is scoped to the same HTTP owner (or the same internal
  no-owner scope), preventing cross-user reuse of private research results.
- Typed insufficiency can be recorded from every active stage, so zero-hit and
  identity-ambiguity outcomes no longer fail while attempting to persist their
  terminal state.

Verification passed:

```text
go test ./backend/app ./cmd/chatlog-agent ./cmd/kbase-server -count=1 -run 'TestResearch|TestKBaseHTTPResearch|TestChatlogAgent|TestResearchServer'
go test -race ./backend/app ./cmd/chatlog-agent -count=1 -run 'TestResearch(Worker|Orchestrator|Evidence)|TestKBaseHTTPResearchRunLifecycleBearerCompatibilityAndRedaction|TestChatlogAgent'
node frontend-web/scripts/book-knowledge-web-smoke.mjs
cd frontend && npm run build
git diff --check
```

G4 remains **NO-GO** until the complete release gate is rerun and a fresh
independent production-readiness review returns GO.

### G3 rerun history

The first complete-suite attempt was rejected for two concrete reasons:

1. `go vet ./...` was incorrectly started in parallel with the frontend build
   and observed an empty `frontend/dist`, so the Go embed check failed. The
   frontend lane completed successfully; module verification, vet, and the
   complete Go suite were rerun after the frontend artifact existed.
2. The full-stack Research smoke still assumed one Chatlog Worker job. The new
   privacy protocol intentionally requires two jobs (opaque candidate search,
   then bounded message fetch), so the fixture stopped its Worker before the
   fetch. The smoke was changed to keep polling, distinguish candidate and
   evidence counts, and assert two completed jobs.

After those owning fixes, the complete G3 suite passed:

```text
go mod verify
go vet ./...
go test ./... -timeout=600s -count=1
cd frontend && npm run build
for smoke in frontend/scripts/*-smoke.mjs; do node "$smoke"; done
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
bash scripts/research-agent-smoke.sh
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The full Go suite reported `backend/app` PASS in 121.943 seconds. The negative
deployment/packaging scenarios intentionally printed their expected timeout or
transaction-failure messages, and both smoke scripts ultimately exited zero.

### Second G4 review: NO-GO retained

The independent reviewer found no new Critical issue and confirmed that the
first review's privacy, retrieval, deterministic-analysis, detail-projection,
identity-resume, and role-model blockers were materially closed. G4 still
returned **NO-GO** because six Important release blockers remained:

1. Worker retries had no per-attempt lease epoch, allowing an ABA replay by the
   same Worker after a job was reclaimed.
2. The Chatlog Worker executed synchronously without renewing its two-minute
   job lease.
3. Coordinator renewal only canceled a context and did not fence database
   writes from an expired run lease.
4. A fetched Chatlog result was not proven to belong to the requested opaque
   candidate, run, and target Worker.
5. The USD limit was checked after a provider call rather than atomically
   reserved before it, and a missing provider cost was recorded as zero.
6. The authorized real-data acceptance matrix remained pending.

The fourth remediation batch addresses the five code-level blockers:

- Every Worker claim now receives a cryptographically random `lease_id`.
  Renew, complete, and fail operations carry that ID in both the request and
  idempotency key and validate owner, lease ID, state, and expiry in the same
  transaction. A delayed response from an earlier attempt is rejected even
  when the same Worker owns the reclaimed job.
- Chatlog job execution now renews at one third of the lease duration. Renewal
  loss cancels the local query, waits for the heartbeat goroutine to stop, and
  prohibits both completion and failure submission under the stale lease.
- Every coordinator claim now receives a distinct run `lease_epoch`.
  Orchestrator state, model reservation/audit/cache, analysis records, evidence
  promotion, drafts, verified conclusions, wait metadata, and transitions are
  guarded by owner + epoch + unexpired lease checks in their write transaction.
- `fetch_chat_message` must exactly match a candidate discovered for the same
  run and target Worker. Context expansion carries a server-issued opaque anchor,
  includes that anchor, and is bounded by the requested before/after window.
- Model-call slots and a worst-case cost are reserved atomically before calling
  the provider. Final cost settles to reported usage or a conservative token
  estimate; completely missing usage consumes the reservation instead of being
  treated as free.

Focused regression coverage includes stale Worker attempt replay, expired
renewal, long-running Worker heartbeat, renewal-loss cancellation, cross-run
candidate substitution, stale coordinator conclusion/transition writes,
missing-cost accounting, and concurrent budget reservation. G4 remains
**NO-GO** until the complete G3 suite, authorized real-data acceptance, and a
fresh independent review all pass.

The complete G3 suite was rerun after the fourth remediation batch and passed:

```text
go mod verify
go vet ./...
go test ./... -timeout=600s -count=1
go test -race ./backend/app ./cmd/chatlog-agent -timeout=600s -count=1 \
  -run 'TestResearch(Worker|Orchestrator|Coordinator|Evidence)|TestKBaseHTTPResearch|TestChatlogAgent'
cd frontend && npm run build
for smoke in frontend/scripts/*-smoke.mjs; do node "$smoke"; done
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
bash scripts/research-agent-smoke.sh
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The complete Go suite reported `backend/app` PASS in 129.075 seconds. The
focused race lane passed (`backend/app` 13.893 seconds and `cmd/chatlog-agent`
7.240 seconds). Both frontend suites, the Research process smoke, packaging,
direct-deployment behavior, generated-system-map, privacy, and diff checks
exited zero. Expected negative deployment fixtures printed their deliberate
timeout/transaction messages before the owning smoke returned success.

### Third G4 review: NO-GO retained

The third independent review confirmed the Worker attempt fence, Chatlog
heartbeat, run write fence, candidate boundary, and atomic cost reservation are
all active in production paths. It found no Critical issue, but retained
**NO-GO** for three Important blockers:

1. A model invocation reserved before a process crash could remain `running`,
   while a failed invocation could not be retried durably under a new attempt.
2. A terminal run transition committed before its failure and outcome metadata,
   allowing a crash to leave a terminal run without its typed result.
3. Deep orchestration produced search and exact-fetch jobs but never scheduled
   the mandatory bounded `expand_chat_context` stage.

The fifth remediation batch closes those code-level findings:

- Model calls now have an attempt ledger keyed by request identity and attempt.
  Reclaim under a new run lease marks an older `running` attempt `abandoned`
  while retaining its conservative cost, transient failures can advance to a
  new attempt, and stale-epoch completion cannot overwrite the new result.
- Terminal status, version, wait metadata, typed failure, orchestrator outcome,
  and transition event now commit in one lease-fenced transaction. A fault
  injected immediately before event insertion rolls back every field.
- Deep Chatlog retrieval now deterministically executes candidate search,
  exact fetch, and a two-message-before/two-message-after bounded context
  expansion for every fetched candidate. Expansion is deduplicated and its
  worst-case new evidence is checked against the remaining item budget.
- Worker HTTP responses must echo the exact request `lease_id`; audit wording
  now describes server-issued opaque anchors without implying a signature
  mechanism.

Focused regressions cover failed-then-retry, crash-after-reservation,
stale-epoch concurrent completion, atomic terminal rollback, changed Worker
lease responses, and the full search/fetch/expand process path. The synthetic
process smoke asserts three completed Worker jobs, one completed expansion, and
three persisted Chatlog evidence rows. G4 remains **NO-GO** until the complete
G3 suite passes again and a fresh independent review returns GO. Authorized
real-data acceptance remains a separate pending release gate.

The complete G3 suite was rerun after the fifth remediation batch and passed:

```text
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
cd frontend && npm run build
go mod verify
go vet ./...
go test ./... -timeout=600s -count=1
go test -race ./backend/app ./cmd/chatlog-agent -timeout=600s -count=1 \
  -run 'TestResearch(Worker|Orchestrator|Coordinator|Evidence)|TestKBaseHTTPResearch|TestChatlogAgent'
for smoke in frontend/scripts/*-smoke.mjs; do node "$smoke"; done
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
bash scripts/research-agent-smoke.sh
bash scripts/chatlog-agent-packaging-smoke.sh
bash scripts/kbase-direct-deployment-smoke.sh
bash scripts/kbase-direct-deployment-behavior-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The final complete Go rerun reported `backend/app` PASS in 129.548 seconds. The
final focused race lane passed (`backend/app` 12.044 seconds and `cmd/chatlog-agent`
5.354 seconds). Both frontend suites and every process, packaging, deployment,
system-map, privacy, and diff gate exited zero. Expected negative deployment
fixtures printed their deliberate timeout and transaction-failure messages
before their owning smoke returned success.

### Fourth G4 review: NO-GO retained

The fourth independent review confirmed all three third-review blockers were
closed, but found one Important candidate-budget defect. The planner capped a
fetch batch at eight while continuing to iterate every persisted search hit,
so a normal ninth hit incorrectly ended the run as `budget_exhausted`. It also
allocated anchors before reserving their complete context windows, allowing a
tight budget to fetch several anchors and then fail all expansion.

The sixth remediation makes candidate selection deterministic and durable:

- Before any exact fetch is created, the orchestrator budgets each candidate
  as one anchor plus a worst-case four adjacent evidence items.
- It selects at most `min(8, floor(remaining evidence / 5))` candidates in the
  persisted candidate order. Additional search hits remain unselected and do
  not fail the run.
- The selected set is represented by the durable exact-fetch jobs. After a
  restart, only those anchors are reconstructed and expanded; selection is not
  recomputed from a smaller post-fetch budget.
- Missing or foreign fetch state fails closed instead of silently skipping a
  selected anchor.

Regression coverage includes ten search hits with an eight-window cap, a
ten-item budget that selects exactly two complete windows, and restart
reconstruction that creates expansions only for the original selected set.
G4 remains **NO-GO** until the complete G3 suite passes again and another
independent review returns GO.

The complete G3 suite was rerun after the sixth remediation and passed. The
full Go suite reported `backend/app` PASS in 127.860 seconds; the focused race
lane passed (`backend/app` 12.524 seconds and `cmd/chatlog-agent` 5.792
seconds). The Vue production build, both frontend smoke suites, Research
process smoke, Chatlog packaging, direct-deployment contract and behavior,
system-map, privacy, and diff checks all exited zero. Expected negative
deployment fixtures emitted their deliberate failure messages before the
owning smoke returned success.

### Fifth G4 review: GO

The fifth independent review verified the deterministic candidate-window fix
and all prior remediations. It found no Critical, Important, or Minor issue and
returned **Ready to merge: Yes**. In particular, it confirmed worst-case
five-item preselection, the eight-window cap, harmless truncation of additional
hits, restart-stable selection through durable fetch jobs, selected-set-only
expansion, and fail-closed handling of foreign, oversized, or incomplete fetch
state. Focused tests, race detection, the process smoke, privacy, system-map,
and diff checks passed during the review.

G4 is PASS. Real-data acceptance remains pending and must be completed as part
of G5/G6 against the exact reviewed and deployed revision.

### Canonical-main integration revalidation

Before deployment, canonical `dedao-kbase/main` was merged into the reviewed
feature branch. Both lines had independently allocated `agent-package.v3`:
canonical main for WeChat/publication collection packages and this feature for
Research policy. The integration preserves canonical v3 collection semantics
and moves Research packages to `agent-package.v4`; v1 and v2 remain unchanged.
The combined schema rejects cross-version field mixing and routes trusted
evaluation, runtime descriptors, compilation, and publication through the
corresponding version-specific path.

The merged candidate passed module verification, `go vet ./...`, the complete
Go suite, the Vue production build, all desktop and Web smoke scripts, focused
Research/collection tests, combined race detection, Research process smoke,
Chatlog packaging smoke, direct-deployment contract and behavior smokes,
system-map drift, privacy, and staged/unstaged diff checks. A fresh independent
review of the merged v3/v4 boundary is required before the integration commit;
G5/G6 remain pending.

### Sixth G4 integration review: NO-GO and remediation candidate

The sixth review found two release blockers after canonical-main integration.
First, the public compilation request and response schemas did not describe
`research_enabled` or v4 packages, and no standalone v3/v4 package schemas
existed. Second, Research run creation and orchestration accepted ordinary
published packages with a `search` capability without enforcing the v4
Research opt-in, requested mode/source scope, Research tool allowlist, package
ToolPolicy, or package budget ceilings. This allowed a v1/v2 or v3 collection
package to be associated with a deep run and potentially schedule a local
Chatlog Worker job.

The remediation candidate adds public v3 collection and v4 Research package
schemas, updates compilation request/response schemas, and verifies the
version boundaries with schema-instance tests. A v3 schema permits exactly one
collection Release and rejects Research/evidence/ordinary Release fields; a v4
schema requires ResearchPolicy and rejects collection Releases.

Research requests now require a published, trusted-evaluation-passing v4
package with `search` and `deep_research` capabilities. Creation checks both
the requested and resolved mode plus requested sources. Resume/advance repeats
the package, policy, source, and budget checks. Every direct or Worker tool is
mapped to its `research/*` package tool ID and source, then checked against both
ResearchPolicy and an `allow` ToolPolicy rule before execution or job creation.
Server budgets are bounded by the package iteration, evidence, quoted-character,
and cost ceilings. Violations terminate fail-closed as `policy_denied`; the
malicious-planner regression proves no Worker job is created.

The process smoke now seeds and publishes an ordinary immutable knowledge
Release plus a trusted v4 Research package through public application APIs
before creating its real server run. The remediated candidate passed the full
Go suite and vet, focused race tests, Vue build, all frontend/Web smokes,
Research process smoke, packaging/direct-deployment smokes, system-map, privacy,
and diff checks. G4 remains pending until a fresh independent review returns
GO on this exact candidate.

### Seventh G4 integration review: GO

The seventh independent review returned **Ready to merge: Yes** with zero
Critical and zero Important findings. It confirmed that HTTP creation and
orchestrator resume both require a published, evaluated v4 Research package;
mode, source, tool, and budget bounds fail closed; and direct tools, Worker
jobs, and derived fetch/expand jobs all cross the same ResearchPolicy and
ToolPolicy authorization boundary. It also verified that a malicious planner
cannot leave a Worker job behind after `policy_denied`, and that the public
v3/v4 schemas are compiled and exercised against real instances.

During the review, a single-source `prior_runs` auto request was found to be
misrouted to the knowledge-only quick path. The route now deterministically
selects deep mode for prior-run research, while an explicit quick request
returns `deep_research_required`; focused and race regression tests passed.
The generated system map was refreshed after this routing change and its drift
check passed. The review's sole Minor test-coverage observation was also
closed: the HTTP legacy rejection now constructs and publishes an actual v1
package and proves that the Research endpoint rejects it as ineligible.

G4 is PASS. Deployment health and authorized real-data acceptance remain
separate G5/G6 gates and are not implied by this code-review decision.

## G5 deployment and G6 remediation checkpoint

Reviewed merge revision `23375066acd429ae164e8f4bf2496503db9efc93` was
published to canonical main and deployed as one revision across the KBase
server, book-job Worker, three evolution Workers, and the macOS Chatlog Worker.
The server deployment used candidate-first health probes and revision-scoped
recoverable backups. Public and loopback health, system service state, build
metadata, candidate/install hashes, and a post-cutover warning window all
passed. Research was explicitly enabled in the server environment without
changing the approved shared-token Worker transport.

The first authorized G6 compile used real production Release metadata rather
than a synthetic seed. It correctly failed closed, revealing two compatibility
gaps in legacy immutable Releases: controlled Dedao records created before
source typing could have an empty `SourceType`, and real WeChat evidence
Releases could already be restricted to `evidence_only`. The compiler
remediation reuses the existing controlled source inference, which permits a
Dedao inference only from a Dedao ID, EnID, or `dedao://ebook/` locator; all
other missing sources remain blocked. Study compilation now inherits the
Release usage policy and defaults only an actually empty policy to `standard`,
so the change cannot weaken an evidence-only Release.

The remediation passed focused compiler/HTTP/no-downgrade tests, the complete
backend package, the Research process smoke, focused race detection,
system-map drift, privacy, and diff checks. A read-only diagnostic against the
real production Release store validated both a legacy Dedao book as
`dedao_ebook`/`standard` and a real WeChat article as
`wechat_mp_article`/`evidence_only`, without printing or persisting source
content. Independent review returned Ready to merge Yes with zero Critical and
zero Important findings. G6 remains open until the exact remediation revision
is deployed and both real quick and deep Research Runs, citations, Worker
search/fetch/expand flow, persistence, and restart recovery are verified.
