# Health Evidence Analysis Has-More Flag Plan

**Goal:** Let schedulers decide whether to continue after the next Health evidence analysis batch without recomputing queue math.

**Architecture:** Keep the existing batch endpoint and all current fields. Add `has_more_after_next_batch` to `HealthEvidenceAnalysisBatchResult`, derived from `remaining_after_next_batch > 0`. The field is available in live, dry-run, and summary-only responses.

**Implementation Tasks**

1. Add tests for limited, full, summary-only, and complete queues.
2. Populate `has_more_after_next_batch` during summary calculation.
3. Update the knowledge supply contract and generated system map.
4. Run the smoke ladder, push to `main`, deploy, and verify production includes the field.

**Expected Result:** External automation can loop on a boolean field and stop cleanly.
