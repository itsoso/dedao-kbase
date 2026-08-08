# Controlled Book Agent Wizard Dossier

## Status

Delivered and verified in production. The pilot knowledge release and controlled
Book Agent `attention-mechanism-research-assistant` version `1.0.1` are
published.

## Requirement

Provide a human-controlled path from one authorized Dedao ebook to a usable
knowledge base and Book Agent. Preserve the shared consumer token for ordinary
browser/API access, keep the dedicated Agent Package publisher credential on the
server, and prevent quality or evaluation holds from being bypassed.

## Definition Gate (G1)

Decision: PASS.

- Scope is one existing authorized ebook and the reusable platform path.
- The first Agent is read-only and supports reading, search, grounded chat, and
  evidence inspection.
- Autonomous publication, write tools, vector fallback, and browser publisher
  credentials are excluded.

## Feasibility And Risk Gate (G2)

Decision: PASS with explicit proxy requirement.

- Existing immutable Knowledge Release, Agent Package, evaluation, runtime, and
  publication contracts are reused.
- A legacy Dedao fallback package can contain complete retrieval objects while
  lacking `content_hash`; this deterministically blocks quality publication.
- The controlled browser routes require both normal API authorization and a
  constant-time verified proxy marker matching `KBASE_BROWSER_SESSION_SECRET`.
  The proxy must clear client-supplied markers on general API routes.

## Implementation

### Content identity

- Canonical SHA-256 identity covers stable book metadata, chapters, chunks,
  claims, and citations.
- Collection order and volatile timestamps do not alter identity.
- New packages receive an identity before persistence.
- Legacy repair applies only to an empty identity and invalidates stale analysis
  and quality artifacts before saving the repaired package.

### Initial knowledge release

- The review workspace exposes legacy repair when required.
- Analysis completion refreshes the quality report automatically.
- A current passing quality report enables the first immutable Knowledge
  Release action.

### Controlled Agent

- The server derives a contract-valid lexical, citation-required, read-only
  Agent Package from one published Knowledge Release.
- It generates a ten-metric deterministic suite covering retrieval recall and
  precision, citations, faithfulness, abstention, tool selection and arguments,
  task completion, latency, and cost.
- Draft, evaluation, and publication are separate explicit browser actions.
- Publication requires a passing persisted evaluation, explicit confirmation,
  a stable idempotency key, trusted browser-session provenance, and configured
  server-side publisher authority.

## Test Gate (G3)

Decision: PASS.

Focused RED/GREEN and full-suite evidence:

- canonical content-hash tests;
- missing-hash persistence and legacy repair tests;
- initial release Web smoke checks;
- generated Agent Package contract and deterministic evaluation tests;
- controlled browser-session, evaluation, confirmation, publication, and
  credential non-disclosure HTTP tests;
- three-step wizard Web smoke checks.
- regression coverage for legacy chapter-level citations and missing Dedao
  `source_type` metadata;
- `go test ./...`, Web smoke, generated system-map drift check, privacy smoke,
  and `git diff --check` all passed locally; the Linux release source also
  passed `go test ./... -timeout=180s` before every production binary build.

## Review Gate (G4)

Decision: PASS.

- The final diff preserves server-side publisher authority and requires both
  ordinary API authorization and a constant-time verified proxy secret for the
  controlled workflow.
- The general API proxy clears client-provided browser markers; the dedicated
  controlled route overwrites the marker only after Basic Auth.
- Privacy smoke passed and no token, cookie, downloaded book content, or
  machine-specific deployment value was committed.

## Deployment Health Gate (G5)

Decision: PASS.

- The release branch was pushed as `codex/controlled-book-agent-wizard`.
- Linux built the CGO-enabled server from the exact source archive after the
  complete server-side test suite passed.
- The binary, static Web directory, and Nginx include were backed up before
  replacement. Nginx configuration validation passed before reload.
- The final service is active with `ExecMainStatus=0` and `NRestarts=0`; local
  and public health probes both return the expected service payload.
- An unauthenticated public controlled-workflow request with a forged browser
  marker returns HTTP 401.

## Online Verification Gate (G6)

Decision: PASS.

1. Repaired the pilot book's empty content identity and invalidated stale
   derived artifacts.
2. Regenerated analysis with `qwen3.7-max`: seven structured claims; all six
   quality rules passed, including content-version and citation integrity.
3. Published immutable Knowledge Release
   `release-aaa4382d565653804812170c15e37295c053c19e592df934b9aa687bc7564e31`.
4. Production acceptance exposed and then fixed two legacy compatibility gaps:
   chapter-level analysis references now resolve to release citations, and
   missing source metadata is inferred only when durable Dedao ebook identity
   is present.
5. The first immutable Agent version exposed an unavailable legacy model and
   was retained for audit. Version `1.0.1` pins the production-verified
   `qwen3.7-max`, passed all ten deterministic metrics, and was explicitly
   published.
6. Runtime search returned one matching claim with two citations. A grounded
   chat completed with one evidence item, two resolved citations, a non-empty
   answer, and a persisted trace ID. An unrelated/insufficient query abstained
   without calling the model.
7. No response exposed publisher authority; the final service remained active
   with zero restarts and no new fatal/error log entries.

No Gate may advance with a failed quality, evaluation, privacy, deployment, or
online acceptance check.
