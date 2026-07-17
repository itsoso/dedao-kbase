# Health Evidence Analysis Status Counts Plan

**Goal:** Let Health and other consumers read common queue counts directly without parsing `skipped_by_status`.

**Architecture:** Keep `skipped_by_status` as the detailed compatibility map. Add stable aggregate fields to `HealthEvidenceAnalysisBatchResult`: `ready_to_publish`, `published`, and `blocked`. Compute them during the existing readiness scan before dry-run, summary-only, or live processing diverges.

**Implementation Tasks**

1. Add tests that assert aggregate counts for ready, complete, and blocked queues.
2. Populate the fields while scanning readiness items.
3. Update the knowledge supply contract and generated system map.
4. Run smoke checks, push to `main`, deploy, and verify the production summary-only response.

**Expected Result:** Automation can make dashboard and scheduling decisions from first-class numeric fields.
