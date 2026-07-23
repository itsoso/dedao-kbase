# Knowledge Package Workspace Implementation Plan

> **For Codex:** Execute this plan task by task with test-driven development and
> verify each gate before deployment.

**Goal:** Convert a knowledge package detail page into a navigable lifecycle
workspace with a collapsible directory and truthful Agent supply status.

**Architecture:** Extend the existing route-aware vanilla JavaScript renderer.
Derive lifecycle stages from the current package, analysis, review, release, and
Agent Package payloads. Keep all actions within existing API contracts and
render a published Agent link only when a package pins the current release.

**Tech Stack:** Vanilla JavaScript, CSS, Go HTTP API contracts, Node smoke tests,
Playwright.

---

### Task 1: Add lifecycle and navigation regression coverage

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Add failing assertions for the lifecycle rail, anchored workspace sections,
   directory collapse control, and Agent supply matching helper.
2. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs`.
3. Confirm failure is caused by the missing workspace behavior.

### Task 2: Model real package lifecycle state

**Files:**
- Modify: `frontend-web/app.js`

1. Add Agent Package collection state and reset behavior.
2. Load `/api/agent-packages?limit=200` on package detail routes.
3. Match a published package by the currently selected immutable release.
4. Derive content, analysis, quality/release, and Agent stage labels without
   optimistic fallbacks.
5. Run the targeted smoke test and confirm the model assertions pass.

### Task 3: Build the workspace shell

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`

1. Render lifecycle rail and metadata below the package title.
2. Add sticky anchors for overview, evidence, analysis, and Agent.
3. Give existing review, chapter/search, analysis manifest, TokenPlan, and
   Agent supply sections stable IDs.
4. Add directory collapse/restore behavior and expanded main width.
5. Keep mobile detail-first ordering and place the directory below the main
   workspace.

### Task 4: Connect contextual actions

**Files:**
- Modify: `frontend-web/app.js`

1. Make lifecycle steps navigate to their corresponding sections.
2. Open review details from the quality step.
3. Link an available Agent step to its exact versioned route.
4. Show an explicit prerequisite message when Agent supply is blocked.
5. Preserve package route and browser history behavior.

### Task 5: Verify and release

**Files:**
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Create: `docs/dossiers/2026-07-23-knowledge-package-workspace.md`

1. Run all frontend smoke scripts and JavaScript syntax checks.
2. Build the frontend and run all Go tests and vet.
3. Run privacy, generated system-map drift, and diff checks.
4. Run desktop and mobile browser tests with both Agent-ready and blocked
   package fixtures.
5. Commit, push `dedao-kbase/main`, deploy with rollback protection, and verify
   health and cache markers.
