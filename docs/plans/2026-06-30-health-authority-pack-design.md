# Health Authority Pack Design

## Goal

Turn dedao-kbase into a governed source supplier for health-llm-driven by exporting health-scoped authority candidates, not direct medical truth. The first target is safe import into the health System KB review workflow.

## Current Context

dedao-kbase already has book packages, book-level `quality_report.json`, a `health` project descriptor, project verification reports, persisted project collections, JSONL exports, and Bearer-protected HTTP APIs. health-llm-driven already treats System KB as reviewed-first evidence for agent synthesis and has deterministic safety boundaries. The missing layer is a health-specific contract that carries entity candidates, risk boundaries, and import decisions in a shape health can dry-run before review.

## Product Boundary

Dedao sources are strong educational material, but they are not final clinical authority. A Dedao-only claim may support health education, question preparation, and context retrieval. It must not support diagnosis, treatment plans, medication instructions, dosage advice, or emergency guidance unless health later promotes it with stronger external evidence and review.

## Data Contract

Add `health_authority_pack_v1` as a new derived export from the existing health project collection. Each record contains:

- `claim_id`: stable downstream ID, prefixed as `dedao:<book_id>:<claim_id>`.
- `claim_text`, `summary`, `book_id`, `book_title`, `chapter_id`, `chapter_title`.
- `entity_candidates`: deterministic health entity hints from title, summary, tags, and risk terms.
- `applies_when`: initially empty object for future Twin matching.
- `contraindications`: risk terms that must not become advice.
- `evidence_level`: inherited from source claim, capped to educational unless externally promoted.
- `review_status`: `needs_review`, `education_only`, `assistive_context`, or `blocked`.
- `risk_tier`: `education_only`, `assistive_context`, `needs_human`, or `blocked`.
- `allowed_uses` and `blocked_uses`.
- `source_refs`: `book_id`, `chapter_id`, `claim_id`, citations, and `source_hash`.

## Health Policy

Default rules:

- Missing citation or source hash -> `needs_human`.
- Medical high-risk terms -> at most `education_only`.
- Medication, dosage, diagnosis, treatment, emergency, pregnancy, child, oncology, cardiovascular red-flag terms -> `blocked` for action support.
- Dedao-only evidence never becomes `action_support_candidate`.
- Allowed uses are constrained to `health_education`, `question_preparation`, and `context_retrieval`.

## API Shape

Add:

- `POST /api/projects/health/authority-pack/refresh?limit=...`
- `GET /api/projects/health/authority-pack`
- `GET /api/projects/health/authority-pack/export?format=jsonl`

These endpoints reuse the existing project collection path and remain Bearer-protected. Export content type should be `application/x-ndjson`.

## health-llm-driven Integration

health imports the JSONL with a dry-run first:

1. Validate schema and reject unknown contract versions.
2. Reject records without stable source refs.
3. Map entity candidates to System KB entity candidates, without auto-creating production entities.
4. Produce an import diff: accepted-for-review, blocked, duplicates, conflicts, missing evidence.
5. Do not write to production System KB until a later reviewed import path exists.

## UI

The Web KBase project/ops surface should show a Health Authority Pack panel with record counts, blocked counts, top risk reasons, and the latest export timestamp. It should make clear that this is a review pack, not direct medical guidance.

## Verification

dedao-kbase tests must cover high-risk downgrade, stable source refs, JSONL export fields, and HTTP auth. health tests must cover dry-run validation, blocked medical-action claims, duplicate detection, and no writes in dry-run mode.

## Rollout

Phase 1 ships dedao-kbase pack generation and export. Phase 2 adds health dry-run importer and import report. Phase 3 can add reviewer promotion into System KB after dry-run quality is acceptable.
