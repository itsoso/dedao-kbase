# Health Evidence Review Workspace v2 Design

## Goal

Upgrade the Knowledge Operations Console from a per-package Health summary into
a focused, privacy-safe Health review queue. Operators should see which KBase
items need consumer review next, why they are in the queue, what evidence
metadata is available, and what action is safe to take.

## Scope

This release stays inside KBase operations. It does not write Health serving
state, does not mark medical content as reviewed, and does not expose source
bodies or claim statements in the operations console.

The workspace adds:

1. a queue model returned by `GET /api/knowledge/operations`;
2. deterministic priority and reason labels for Health evidence candidates;
3. frontend-web queue cards/table inside `/operations`;
4. tests that preserve the existing boundaries against source body exposure,
   Health serving promotion, and unsafe replay.

## Non-goals

- No automatic Health serving promotion.
- No Health-owned review decisions or safety ownership inside KBase.
- No source bodies, downloaded article text, prompts, tokens, cookies, or claim
  statements in the operations response.
- No new mutating replay actions beyond `analyze` and `evaluate_quality`.
- No unauthenticated public console access.

## Architecture

The existing operations aggregation layer already combines pipeline state,
quality state, releases, and Health readiness. v2 adds a derived
`health_review_queue` array to the same console response. Each queue item is
computed from the existing operations item and Health evidence metadata.

The queue is intentionally derived and read-only:

- source of truth remains existing KBase package, quality, release, and Health
  readiness files;
- queue priority is deterministic and reproducible;
- queue recommendations are phrased as KBase operator guidance, not Health
  decisions;
- `serving_allowed` is always `false`.

## Queue item contract

Each item includes only metadata:

```json
{
  "book_id": "source-example",
  "title": "Example package",
  "release_id": "release-example",
  "status": "ready_to_publish",
  "priority": 80,
  "priority_label": "review_next",
  "next_operator_action": "send_to_health_review",
  "consumer_review_required": true,
  "serving_allowed": false,
  "claim_count": 12,
  "citation_count": 18,
  "risk_counts": { "high": 1, "medium": 4 },
  "reasons": ["consumer_review_required"]
}
```

Recommended action labels:

- `send_to_health_review`: KBase has evidence-only material ready for downstream
  review, but KBase must not promote serving.
- `inspect_policy_block`: usage policy prevents Health use.
- `run_analysis`: analysis is missing or stale.
- `evaluate_quality`: quality evaluation is missing or stale.
- `inspect_quality_block`: quality failed and needs upstream correction.
- `monitor_imported_release`: KBase release exists and Health can import/review
  through its own process.

## Data flow

1. `BuildKnowledgeOperationsConsole` builds the existing console items.
2. The builder derives `HealthReviewQueue` from those items.
3. Queue entries are sorted by priority descending, then title/book ID for
   stable display.
4. The HTTP route returns the queue as part of the existing authenticated
   operations API.
5. The frontend renders a dedicated Health review queue panel before the
   package table.

## Safety boundaries

- The backend never serializes `HealthEvidenceClaim.Statement` in the operations
  console.
- The frontend does not render `publish` or `health_serving_promote` as replay
  controls.
- Queue actions are labels only; they do not mutate downstream Health state.
- Safe replay remains limited to `analyze` and `evaluate_quality`.

## Testing

- Backend test for queue derivation, priority labels, reasons, and
  `serving_allowed=false`.
- Backend serialization test to ensure queue output contains no claim
  statements.
- Frontend smoke test for queue markers and absence of unsafe action buttons.
- Existing operations tests continue covering dangerous replay rejection.

## Gates

- **G1 admission:** PASS if this is a read-only KBase review queue and not a
  Health serving workflow.
- **G2 feasibility/safety:** PASS if all data comes from existing KBase store
  contracts and no source bodies are required.
- **G3 test:** PASS only after focused backend tests, frontend smoke, privacy
  smoke, and whitespace checks pass.
- **G4 review:** PASS only if code and tests preserve no source body exposure,
  no Health serving promotion, no unsafe replay, and no consumer review
  ownership transfer.
- **G5/G6:** deployment and online verification are allowed only after G3 and
  G4 pass.
