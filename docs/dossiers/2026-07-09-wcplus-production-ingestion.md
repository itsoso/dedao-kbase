# WC Plus Production Ingestion Dossier

## Status

- **Current stage:** S7 - verification and rollout
- **Delivery status:** Control plane and Agent deployed; G6 new-content acquisition blocked by WC Plus authorization
- **Architecture decision:** approved
- **Last updated:** 2026-07-10

## S0 Intake

User requests:

> WC Plus相关的功能 还有哪些没有完成的 形成新规划

> 采用 本地 Agent + 在线 KBase 控制面

Primary user: the operator who collects public-account content on a local
computer and needs it available as durable online knowledge.

Problem: WC Plus is local, while KBase is online. Existing proxy and import
features cover many APIs but do not provide a secure, restart-safe, automated
local-to-online synchronization path.

Current workaround: manually import text/files or run KBase where it can reach
the local WC Plus service.

## S1 Current-State Findings

Reusable capabilities:

- WC Plus account/article listing, pagination, search, content preview, and
  exports.
- Single, account, nickname-batch, and raw article import.
- Existing Bearer-protected KBase HTTP server and online Web workbench.
- Existing `BookKnowledgeStore` and SQLite pattern.
- Existing REST-style links to books and knowledge results.

Confirmed gaps:

- The online service cannot reach a local loopback WC Plus endpoint.
- Current task models omit documented task-family fields.
- Nickname batch import uses the wrong upstream task endpoint semantics.
- Completion polling can misclassify a task that disappears from the
  unfinished-task list.
- Sync runs, items, retries, leases, and recent imports are not durably modeled.
- Reimports do not report accurate new/updated/skipped outcomes.
- WC Plus articles are written as a single unbounded chunk.
- No local agent, durable outbox, subscription scheduler, or Agent status UI.

Hard constraints:

- Never expose the unauthenticated WC Plus local API publicly.
- Never send WeChat cookies or WC Plus request-parameter records online.
- Preserve existing direct proxy and manual import during migration.
- Preserve unrelated dirty-worktree changes and stage only feature files.

## G1 Admission

**Decision:** PASS

Reason: secure and automated source ingestion directly supports the repository's
role as a private knowledge workbench. The smallest end-to-end slice is one
local agent heartbeat followed by one idempotent article upload.

## S2 Product Definition

The feature provides:

- a local outbound-only WC Plus agent
- an online durable source control plane
- idempotent article ingestion and knowledge normalization
- observable agents, subscriptions, runs, items, and failures
- conservative scheduling and explicit retries

It does not reproduce WC Plus licensing, proxy/certificate management, payment,
AI prompts, or other administrative functions.

Design:

- [`../plans/2026-07-09-wcplus-production-ingestion-design.md`](../plans/2026-07-09-wcplus-production-ingestion-design.md)

## G2 Feasibility and Risk Review

**Decision:** PASS WITH HARD CONSTRAINTS

Approved approach: local Agent plus online KBase control plane.

Key risks and controls:

- Unauthenticated local API: loopback only; outbound agent transport.
- Token scope: separate source-agent token and route group.
- Duplicate delivery: server-computed content hash and idempotency receipt.
- Agent/server restart: SQLite outbox, leases, and conditional transitions.
- Upstream contract drift: sanitized real fixtures and capability heartbeat.
- Partial content: validation, partial run state, and item-level errors.
- Privacy leakage: normalized content only, redacted logs, privacy smoke.

## S3 Plan

Implementation plan:

- [`../plans/2026-07-09-wcplus-production-ingestion.md`](../plans/2026-07-09-wcplus-production-ingestion.md)

Delivery phases:

1. Correct WC Plus task contracts.
2. Persist source agents, subscriptions, runs, items, documents, and receipts.
3. Add dedicated source-agent HTTP authentication and APIs.
4. Normalize and idempotently persist source articles.
5. Build local client and durable outbox.
6. Execute leased WC Plus runs end to end.
7. Add scheduling.
8. Add the Web control plane.
9. Package the macOS Agent.
10. Pass full verification, review, deployment, and production validation.

## S4 Requirement Decomposition

The task-level file changes, failing tests, verification commands, and commit
boundaries are defined in the implementation plan. Implementation must stop at
checkpoints A-E when evidence is missing or a gate is red.

## S5 Implementation Progress

Completed commits:

- `c3ebac5` corrects per-account task creation, task normalization, completion
  verification, and the Web diagnostics contract.
- `be49404` adds the SQLite source agents, subscriptions, runs, items,
  documents, receipts, leases, retries, and terminal-state model.
- `93d64c3` separates source-agent authentication from browser/admin
  authentication and adds the source control APIs and request limits.
- `4a7ac3d` adds server-computed content hashes, idempotency receipts,
  new/updated/skipped outcomes, bounded chunks, and per-chunk provenance.
- `fa37893` accepts the current WC Plus account-list response shape.
- `4684138` adds the outbound-only local Agent client and durable SQLite
  outbox.
- `5a7b02c` executes leased article and reading-data runs end to end.
- `ad41258` adds conservative subscription scheduling and lease recovery.
- `c627941` adds the wide Web source control plane with agent,
  subscription, run, retry, and REST knowledge-document workflows.
- `fd2f7f5` packages the native macOS Agent as a bounded user LaunchAgent.
- `7f40949` adds atomic knowledge writes, concurrent-ingestion protection,
  diagnostic limits, and source/admin token separation checks.
- `cf1efa4` aligns article-list defaults with WC Plus 9.483 and reports the
  observed WC Plus version through Agent heartbeats.
- `de25e3d` parses the live account image field and supplies WC Plus's required
  `Img` value for article-link, content, and reading-data tasks.
- `5124c66` normalizes numeric task IDs returned by WC Plus 9.483 before the
  Agent begins task polling.
- `8283f91` rejects vanished tasks when the article list did not change,
  preventing existing content from becoming false success evidence.
- `b2d1173` treats WC Plus's numeric task ID `0` as an authorization or
  activation block and reports it immediately to the control plane.

Checkpoint evidence:

- **Checkpoint A: PASS.** A local WC Plus 9.483 instance was observed on its
  displayed loopback port (`5002`). Account, article-list, and article-content
  calls succeeded. The live article-list contract requires `sort=p_date` and
  `direction=desc`; both are now defaulted and covered by regression tests.
  Only response field shapes were inspected; no account name, article title,
  body, cookie, or request-parameter record was retained.
- **Checkpoint B: PASS for the server slice.** Source state, token isolation,
  request limits, restart persistence, idempotent replay, update preservation,
  failed-write behavior, search discovery, and chunk citations are covered by
  passing tests.
- **Checkpoint C: PASS for the isolated end-to-end path.** One real local
  existing article was processed through `wcplus-agent once` into an isolated
  KBase control plane. The run finished `succeeded` with `uploaded=1`,
  `failed=0`, `outbox_remaining=0`, and server counters `new=1`, `updated=0`,
  `skipped=0`, `failed=0`. The resulting private knowledge document contained
  one chapter, one bounded chunk, one citation, and complete provenance. All
  temporary article and knowledge data was deleted after validation.
- **Checkpoint D: PASS.** Desktop and mobile browser checks covered agent
  health/version, subscriptions, runs, item failures, retry/cancel actions,
  detail drawers, and REST-style knowledge links. Legacy direct-proxy tools
  remain collapsed under diagnostics.

Verification executed through Task 9 and the live-contract correction:

```text
go test ./... -count=1
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/wcplus-agent -count=1
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/kbase-token-header-smoke.mjs
node --check frontend-web/app.js
bash scripts/wcplus-agent-packaging-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The final matrix was rerun after all production-discovered contract fixes. The
Vue build emitted its existing eval and large-chunk warnings but returned zero;
the race run emitted macOS linker warnings but all three tested packages
returned `ok`.

## Release Artifacts

- Source commit: `b2d1173ae71a4379b34de0b1edac0cbbced015e4`
- Clean source archive SHA-256:
  `3fa39180f79094f5dc14b37958ecc2aa0826c55a5441b5c3374bb5d5bc94bd6e`
- Native macOS `arm64` Agent SHA-256:
  `67602da40381d73fc95c8bcb4ee2817bffa8d908abbab7558c418e66b207dec7`
- Linux `x86-64` server SHA-256:
  `634250e5a06b2400793e510ce83f3bf7bac24d6bfbd5014fec97dec810bb3f15`
- Linux build used Go 1.23.0 after checking the toolchain archive against
  SHA-256 published in the official Go release metadata. The initial module
  download through `proxy.golang.org` timed out; the successful retry used an
  HTTPS Go module mirror without disabling `go.sum` verification.
- Final pre-deploy backup identifier: `20260711095218`.

## G4 Independent Review

**Decision:** PASS

The review covered dedicated-token route isolation, lease ownership and expiry,
SQLite transactions, idempotency receipts, upload/request limits, diagnostic
redaction and truncation, crash-safe outbox replay, atomic knowledge manifests,
and rollback behavior. Findings were fixed in `7f40949`; the race detector then
passed for the ingestion server and Agent packages.

## G5 Deployment Evidence

**Decision:** PASS

The server and static control plane were deployed from the checked release
artifacts. The service returned to `active` with the expected Linux binary hash.
Before enabling source authentication, both loopback and public source-agent
routes returned `503` as designed. Existing health, books, search, reader, WC
Plus diagnostics, public Bearer API access, and control-plane static assets all
passed production probes.

The dedicated source-agent token is separate from the admin token.
Unauthenticated source requests return `401`; an authenticated empty lease
preflight returns `200`. The token was rotated during rollout and the previous
value was verified invalid. The native Agent is installed with a `0600`
LaunchAgent plist and the recorded binary hash. Production reports one Agent
at version `0.1.0`, WC Plus `9.483`, and `wcplus_healthy=true`. The final server
is `active`, public health returns `200`, and admin book access returns `200`.

## G6 Production Validation

**Decision:** BLOCKED

The staged production sequence completed without retaining account names,
article titles, bodies, cookies, or WC Plus request parameters in release
evidence:

- `doctor` confirmed local WC Plus, remote authentication, and Agent version.
- `run_bf1533af450cae52` imported one selected existing article with `new=1`
  and no failures.
- `run_3a1bfe07932d0c07` replayed the same article with `skipped=1`, proving
  unchanged idempotency.
- `run_899428a145a9e124` bounded an account sync to three items and returned
  `new=2`, `skipped=1`, and no failures.
- Live task creation exposed required `Img`, numeric `task_id`, and vanished
  task contract differences. All were fixed with regression tests and
  redeployed. An earlier apparent `sync_links` success was invalidated after
  WC Plus logged an authorization failure while returning task ID `0`.
- `run_166892b1e16aee3e` recovered an expired five-second lease after an Agent
  restart and completed successfully.
- `run_a115ead40717f75e` retained one upload after a simulated HTTP `503` in
  the local SQLite outbox, then replayed it after recovery; pending returned
  from one to zero and the run completed successfully.
- A temporary five-second schedule automatically created
  `run_96d34172c2145d0f`, which completed successfully. The subscription was
  then verified restored to its original `manual` and `sync_links` settings.

The three newly created knowledge documents all returned `200` from their REST
detail routes. Existing knowledge was preserved; validation replays produced
skips rather than duplicate documents.

The final new-content acquisition probe, `run_2887bca1d59aab6b`, failed
immediately with `task creation returned invalid task_id 0; check WC Plus
authorization or activation`. This matches the vendor console's authorization
failure. KBase cannot activate or bypass a paid WC Plus license. G6 remains
blocked until WC Plus accepts a nonzero task, after which the same bounded probe
must be rerun.

A sanitized license-status check reports `is_active=false`, `expire_time=0`,
and no configured license key. The vendor UI exposes purchased-license and
limited trial activation. Neither was triggered automatically because trial
activation starts a time-limited entitlement and requires an operator decision.

## Gate Ledger

| Gate | Status | Evidence | Next action |
| --- | --- | --- | --- |
| G1 Admission | PASS | User confirmed local Agent plus online control plane | Preserve approved scope |
| G2 Feasibility/risk | PASS WITH CONSTRAINTS | Design documents auth, loopback, privacy, idempotency, and rollback | Start Task 1 |
| G3 Tests | PASS | Fresh Go tests/vet/race, Vue build, Web smokes, packaging, privacy, and diff checks passed | Preserve tested commit and artifact hashes |
| G4 Review | PASS | Auth, leases, transactions, idempotency, limits, redaction, outbox, atomic writes, and rollback reviewed; findings fixed in `7f40949` | Preserve release diff |
| G5 Deploy health | PASS | Backup `20260711095218`; expected server and Agent hashes; public health, admin access, dedicated auth, and healthy heartbeat verified | Retain rollback backup |
| G6 Production validation | BLOCKED | Existing imports, replay, recovery, outbox, scheduling, and REST access pass; WC Plus rejects new task creation with ID `0` and an authorization error | Activate WC Plus, then rerun one bounded `sync_links` probe |

## Pending Decisions

No KBase design decision remains open. The external decision is to activate a
valid WC Plus license or approve a separate non-WC Plus acquisition adapter.
Until then, existing locally collected articles can be imported, but new
WC Plus task families remain intentionally blocked rather than reported as
successful.

## Rollback

- Disable source-agent token configuration and scheduler leasing.
- Unload the local Agent while preserving its outbox.
- Restore server/static/env state from backup `20260711095218` if G5 regresses.
- Keep existing imported knowledge and source-sync audit records.
- Continue using direct local proxy or manual import.

## Completion Record

The production control plane and local Agent are shipped. Existing article
ingestion, idempotent replay, bounded processing, lease recovery, outbox replay,
scheduling, and REST knowledge access are complete. Full WC Plus acquisition is
not complete because the paid upstream currently rejects task creation; G6 must
be rerun after activation.
