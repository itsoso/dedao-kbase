# Health Authority Pack v1.1 Design

## Goal

Make `health_authority_pack_v1` safer and more useful for `health-llm-driven` by enriching each record with review metadata, normalized source references, and machine-checkable risk reasons. This is an additive contract hardening pass; the contract name stays `health_authority_pack_v1`.

## Product Boundary

Dedao book content remains educational source material. It can help Aheng retrieve context, prepare questions, and explain concepts, but it must not become direct diagnosis, treatment, medication, dosage, emergency, pregnancy, child, oncology, or cardiovascular action support without later external evidence and review.

## Contract Additions

Each pack record should include:

- `source_refs`: normalized `book_id`, `book_title`, `chapter_id`, `chapter_title`, `claim_id`, `citations`, and `source_hash`.
- `review_status`: `needs_review`, `education_only`, `assistive_context`, or `blocked`.
- `risk_reason`: deterministic explanation for downgrade or block decisions.
- `entity_candidates`: health entity hints suitable for matching, not automatic entity creation.
- `allowed_uses` and `blocked_uses`: explicit downstream permission boundaries.

Existing flat fields remain for backward compatibility.

## Health Import Behavior

`health-llm-driven` should accept both current flat records and new nested `source_refs`. Dry-run import must:

- reject unknown contracts;
- reject missing stable source refs;
- block medical action claims even when a record has citations;
- preserve entity candidates and risk reasons for reviewer triage;
- keep `would_write = False`.

## Web Visibility

The KBase Health Authority panel should surface pack quality at a glance: total records, reviewable records, blocked records, and top risk reasons. It should not add new navigation or imply clinical readiness.

## Verification

Use TDD. Add failing tests for Dedao pack metadata and Health dry-run importer compatibility before implementation. Run focused tests first, then repository-level verification. Do not deploy if either side fails.
