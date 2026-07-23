# Health Evidence Review Workspace v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a read-only Health evidence review queue to the Knowledge Operations Console.

**Architecture:** Extend the existing KBase operations aggregation model with a derived `health_review_queue` array, computed from pipeline, quality, release, and Health readiness metadata. Render the queue in the existing `/operations` frontend-web route with label-only operator guidance and no downstream Health mutation.

**Tech Stack:** Go backend and tests, existing private KBase HTTP API, frontend-web vanilla JavaScript/CSS smoke tests, lifecycle dossier.

---

### Task 1: Lifecycle documents and Gate scaffold

**Files:**
- Create: `docs/plans/2026-07-22-health-evidence-review-workspace-design.md`
- Create: `docs/plans/2026-07-22-health-evidence-review-workspace.md`
- Create: `docs/dossiers/2026-07-22-health-evidence-review-workspace.md`

**Steps:**

1. Write the approved v2 design with scope, non-goals, queue contract, safety boundaries, and tests.
2. Write this implementation plan.
3. Create the dossier with S0-S3, G1, and G2.
4. Run `bash scripts/privacy-smoke.sh && git diff --check`.
5. Commit only the lifecycle docs if checks pass.

### Task 2: Backend queue contract

**Files:**
- Modify: `backend/app/knowledge_operations.go`
- Modify: `backend/app/knowledge_operations_test.go`

**Steps:**

1. Write failing test `TestKnowledgeOperationsBuildsHealthReviewQueue`.
2. Run `go test ./backend/app -run TestKnowledgeOperationsBuildsHealthReviewQueue -count=1`; expect RED because `health_review_queue` does not exist.
3. Add `HealthReviewQueue []KnowledgeOperationsHealthReviewItem` to `KnowledgeOperationsConsole`.
4. Add queue item fields for book ID, title, release ID, status, priority, priority label, next operator action, consumer review requirement, serving allowed, claim count, citation count, risk counts, and reasons.
5. Build the queue from existing operations items.
6. Sort by priority descending, then title/book ID.
7. Run the focused test; expect GREEN.

### Task 3: Backend privacy and safety regression

**Files:**
- Modify: `backend/app/knowledge_operations_test.go`

**Steps:**

1. Extend serialization coverage to assert the queue contains no claim statements and no source bodies.
2. Assert every queue item has `serving_allowed=false`.
3. Assert queue action labels do not include `publish` or `health_serving_promote`.
4. Run `go test ./backend/app -run 'KnowledgeOperations.*Health' -count=1`; expect GREEN.

### Task 4: Frontend queue workspace

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/knowledge-operations-console-smoke.mjs`

**Steps:**

1. Add failing smoke expectations for `health_review_queue`, a queue panel marker, queue action markers, and absence of unsafe action buttons.
2. Run `node frontend-web/scripts/knowledge-operations-console-smoke.mjs`; expect RED.
3. Render a Health review queue panel above the operations item table.
4. Display priority label, next operator action, readiness status, reasons, claim/citation counts, and risk distribution.
5. Add compact responsive CSS.
6. Run `node --check frontend-web/app.js && node frontend-web/scripts/knowledge-operations-console-smoke.mjs`; expect GREEN.

### Task 5: System-map, dossier, and G3/G4

**Files:**
- Modify if structural inventory changed: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-07-22-health-evidence-review-workspace.md`

**Steps:**

1. Regenerate the system map because Go API types changed:
   `go run ./cmd/system-map --root . --out docs/_generated/system-map.json`.
2. Run `bash scripts/system-map-smoke.sh`.
3. Run focused backend tests:
   `go test ./backend/app -run 'KnowledgeOperations' -count=1`.
4. Run frontend checks:
   `node --check frontend-web/app.js && node frontend-web/scripts/knowledge-operations-console-smoke.mjs`.
5. Run `bash scripts/privacy-smoke.sh && git diff --check`.
6. Update the dossier with exact command results.
7. Decide G3 and G4. Do not commit if either Gate fails.

### Task 6: Commit and deployment decision

**Files:**
- Modify: `docs/dossiers/2026-07-22-health-evidence-review-workspace.md`

**Steps:**

1. If G3/G4 pass, stage only v2 feature files.
2. Commit intentionally.
3. Push the feature branch if network approval is available.
4. Do not deploy unless G3/G4 pass and the deployment branch is a clean main.
5. If deployment proceeds, update the dossier with G5/G6 online verification.
