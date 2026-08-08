# Operations Empty-State Diagnostics Design

## Goal

Make the Knowledge Operations Console explain why the Health review queue is
empty or blocked, and show safe next steps for operators.

The current production queue can legitimately return `queue=0`. Without a
diagnostic panel, that looks like missing data. v3 turns that state into an
explicit explanation.

## Scope

Add a read-only diagnostics model to `GET /api/knowledge/operations` and render
it in `/operations`.

The diagnostics answer:

- why the queue is empty;
- how many packages are in each Health readiness state;
- which safe operator actions are available next;
- which blockers are present and how many packages they affect.

## Non-goals

- No automatic Health serving promotion.
- No KBase-owned Health review decision.
- No source body, downloaded article body, or claim statement exposure.
- No new replay action beyond `analyze` and `evaluate_quality`.
- No unauthenticated console access change.

## API contract

`KnowledgeOperationsConsole` gains:

```json
{
  "health_review_diagnostics": {
    "queue_empty_reason": "no_items_match_current_limit",
    "status_counts": {
      "needs_analysis": 3,
      "needs_quality": 1
    },
    "next_safe_actions": [
      {
        "action": "increase_limit_or_filter",
        "label": "Increase console limit or inspect all packages",
        "count": 5
      },
      {
        "action": "run_analysis",
        "label": "Run analysis for packages missing Health evidence analysis",
        "count": 3
      }
    ],
    "blockers": [
      {
        "status": "needs_analysis",
        "label": "Analysis is missing or stale",
        "count": 3,
        "safe_action": "run_analysis"
      }
    ]
  }
}
```

Reason labels:

- `queue_has_items`: queue contains actionable items.
- `no_operations_items`: no packages are visible to the operations query.
- `no_health_readiness_items`: packages are visible, but none have Health
  readiness state in the current response.
- `no_items_match_current_limit`: the visible slice has no queue items; increase
  limit or inspect filters before assuming there is no work.
- `all_visible_items_need_upstream_work`: visible packages need analysis,
  quality, or policy/quality correction before review.
- `all_visible_items_ready_or_imported`: visible packages are ready/published
  and require downstream Health-owned review/import outside KBase.

## Data flow

1. `BuildKnowledgeOperationsConsole` builds the existing items and
   `health_review_queue`.
2. It derives diagnostics from the same operations items plus summary totals.
3. Diagnostics contain counts and labels only.
4. The frontend renders a panel directly under the queue, so an empty queue
   still has an explanation and safe next steps.

## Safety boundaries

- Diagnostics are derived from metadata already present in operations items.
- Action labels are guidance strings, not mutating commands.
- `publish`, `health_serving_promote`, `feedback`, and external Health writes
  remain unavailable.
- Tests assert no claim statements are serialized through the diagnostics path.

## Testing

- Backend TDD:
  - diagnostics explain an empty queue with visible upstream work;
  - diagnostics expose status counts and safe next actions;
  - diagnostics do not expose claim statements or unsafe actions.
- Frontend smoke:
  - verifies diagnostics model markers, renderer markers, empty reason copy,
    next action markers, and no unsafe replay buttons.
- Release checks:
  - system-map smoke after Go type changes;
  - privacy smoke and `git diff --check`;
  - focused backend Operations tests;
  - frontend-web syntax and smoke.

## Gates

- **G1 admission:** PASS if this remains read-only operational explanation.
- **G2 feasibility/safety:** PASS if diagnostics are derived from existing
  metadata and require no source bodies or secrets.
- **G3 test:** PASS only after focused backend tests, frontend smoke,
  system-map smoke, privacy smoke, and whitespace checks pass.
- **G4 review:** PASS only if no Health serving promotion, no unsafe replay,
  no source body exposure, and no Health ownership transfer are preserved.
- **G5/G6:** deploy only after G3/G4 pass from clean main.
