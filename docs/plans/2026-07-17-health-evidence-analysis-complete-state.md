# Health Evidence Analysis Complete State Plan

**Goal:** Prevent automation from treating an already prepared Health evidence queue as a blocker.

**Architecture:** Keep the existing batch analysis endpoint and response fields. Refine queue-state derivation so `needs_analysis` still reports `ready`, true blocker statuses report `blocked`, an empty scan reports `empty`, and queues containing only `published` or `ready_to_publish` report `complete`.

**Implementation Tasks**

1. Add tests for a complete queue and a blocked queue.
2. Extract a small queue-decision helper in `backend/app/health_evidence.go`.
3. Update the knowledge supply contract vocabulary.
4. Run the standard smoke ladder, push, deploy, and verify the production summary-only response.

**Expected Result:** Health and downstream automation can safely distinguish “run more analysis,” “review blockers,” and “nothing to do.”
