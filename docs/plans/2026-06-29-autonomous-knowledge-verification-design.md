# Autonomous Knowledge Verification Design

## Goal

Give 阿衡 (`health-llm-driven`) and Proofroom stronger machine verification so they can consume Dedao book knowledge without keeping a human in the realtime loop.

## Decision

Use a verification gateway inside `dedao-gui` instead of directly promoting book claims into downstream systems. The gateway keeps Dedao KBase as the source asset service, produces deterministic verification reports, and leaves downstream systems responsible for their own final action gates.

## Non-Goals

- Do not auto-write to health or Proofroom production stores in this slice.
- Do not replace human review for medical, legal, financial, or other high-risk actions.
- Do not call external LLMs from the verification path yet; the first version must be deterministic and cheap.

## Verification Model

Each project claim receives:

- `verification_score`: numeric confidence from provenance, citation, evidence, and project policy checks.
- `risk_tier`: `auto_usable`, `assistive_only`, `needs_human`, or `blocked`.
- `decision`: `allow`, `assist`, `queue`, or `block`.
- `checks`: machine-readable check results.
- `failure_reasons`: explicit reasons preventing automatic use.
- `allowed_uses` and `blocked_uses`: downstream-safe usage hints.
- `provenance`: stable source identifiers and a source hash.

## Health Policy

阿衡 can automatically use verified book claims for health education, context retrieval, and question preparation. It must not turn book claims into diagnosis, treatment, medication, emergency guidance, or personalized medical instructions. Health-sensitive claims are downgraded to `assistive_only` or `blocked`.

## Proofroom Policy

Proofroom can use verified book claims more broadly as source-pack material and argument drafts. It still needs citation presence and provenance. Unsupported claims stay in `needs_human` and should not enter default argument maps.

## API Shape

Add a Bearer-protected endpoint:

```text
GET /api/projects/{project}/verification-report?limit=20
```

The response is read-only and includes project metadata, autonomy policy, tier counts, decision counts, and verified items.

## Product Behavior

The Web project panel shows a compact verification summary beside the existing review queue. Users can see how many claims are automatically usable, assistive-only, queued for review, or blocked. This makes the autonomous path auditable without requiring human approval for every item.

## Future Extensions

- Add cross-source contradiction checks across books.
- Add Model Council verification for Proofroom argument quality.
- Add guideline and PubMed-style evidence checks for health.
- Add persistent project collections and explicit downstream export jobs.
- Add async sampling review so humans inspect a percentage of auto-used claims after the fact.
