# Verified Evidence Pack v1 Design

## Goal

Define one shared evidence-pack contract for dedao-kbase so downstream systems can consume governed knowledge without binding directly to Dedao-specific book, course, or project internals.

## Product Decision

`dedao-kbase` remains the knowledge asset service. It collects and organizes source material, runs deterministic quality and verification checks, and exports reviewable evidence packs. Health, Proofroom, and future consumers keep their own final promotion gates.

The new shared contract is `verified_evidence_pack_v1`. Domain-specific packs such as `health_authority_pack_v1` and a future `proofroom_argument_pack_v1` should be projections of this base contract, not separate data models.

## Non-Goals

- Do not auto-write into health, Proofroom, or any production consumer store.
- Do not treat book/course material as clinical, legal, or financial authority by itself.
- Do not include raw downloaded content, private tokens, cookies, or machine-local paths in exported packs.
- Do not add LLM verification in v1; the base contract is deterministic and cheap.

## Contract Shape

Each exported pack contains a metadata envelope and newline-delimited evidence records.

Envelope fields:

- `consumer_contract`: `verified_evidence_pack_v1`
- `pack_id`: stable ID derived from project, version, and source fingerprint
- `project_id`, `target_system`, `export_type`
- `generated_at`, `schema_version`, `source_fingerprint`
- `source_unchanged`: true when the current source fingerprint matches a previous artifact
- `quality_summary`: total, accepted, assistive, blocked, invalid, missing-source counts
- `policy`: default allowed and blocked uses for the target project

Record fields:

- `evidence_id`: stable downstream ID, prefixed by source family, for example `dedao:<book_id>:<claim_id>`
- `source_refs`: source type, source ID, title, section/chapter ID, claim ID, citations, and source hash
- `title`, `summary`, `normalized_claim`
- `verification_score`, `quality_status`, `risk_tier`, `decision`
- `allowed_uses`, `blocked_uses`
- `risk_flags`, `risk_reason`, `failure_reasons`
- `entities`: consumer-neutral entity candidates
- `audit`: review status, audit status, sample reason, and recommended actions

## Consumer Projections

Health uses a strict projection. Dedao-only evidence may support education, context retrieval, and question preparation. It must not support diagnosis, treatment, medication changes, dosage, emergency guidance, or personalized clinical instructions without later external evidence and review.

Proofroom uses a broader argument projection. Claims may become source leads, argument drafts, or contradiction candidates when citations and provenance are present. Unsupported or conflicting records remain review items.

Generic consumers should treat every record as source material unless their own gate promotes it.

## API Shape

Add Bearer-protected endpoints:

```text
GET /api/projects/{project}/evidence-pack
GET /api/projects/{project}/evidence-pack/export?format=jsonl
GET /api/projects/{project}/evidence-pack/diff?previous_pack_id=...
```

The JSON response returns the envelope plus a bounded record preview. The JSONL endpoint streams one record per line and includes the envelope fields needed for downstream stateless validation. The diff endpoint reports added, removed, changed, unchanged, and blocked records by stable `evidence_id`.

## UI Shape

The Project Hub should show:

- latest pack ID and source fingerprint
- source unchanged status
- quality and risk counts
- top risk reasons
- export and diff links
- recommended next actions

The UI must label packs as reviewable source material, not final downstream truth.

## Verification

Backend tests must cover deterministic pack IDs, stable source refs, JSONL export, diff behavior, unknown project errors, auth, and redaction. Web smoke tests should assert the Project Hub exposes pack status, diff, and export affordances. Health-side tests should continue validating that `health_authority_pack_v1` remains a strict projection.

## Rollout

1. Ship the base pack contract and project export endpoints.
2. Convert the health authority pack implementation to reuse the base pack builder.
3. Add Proofroom argument pack projection.
4. Add webhook or pull manifest once both consumers use the base contract.
