# Knowledge Package Detail-First Implementation Plan

> **For Codex:** Use the repository execution and verification workflow to implement this plan task by task.

**Goal:** Make knowledge package detail routes show the selected package immediately instead of below the global review and pipeline dashboards.

**Architecture:** Derive index/detail mode from the existing `/knowledge/packages/:bookID` route. Render global operations only on the index route and render a dedicated package workspace on detail routes, reusing existing package data, review, analysis, and event handlers.

**Tech Stack:** Vanilla JavaScript, CSS, Node smoke scripts, Playwright.

---

### Task 1: Add route behavior regression coverage

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

1. Add assertions for index/detail route gating and detail navigation controls.
2. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs`.
3. Confirm the new assertions fail before implementation.

### Task 2: Split index and package detail rendering

**Files:**
- Modify: `frontend-web/app.js`

1. Add a helper that identifies package detail routes.
2. Render review cockpit and pipeline only for the index.
3. Render the package workspace first on detail routes.
4. Add global, previous, and next navigation controls.
5. Keep existing search, review, baseline analysis, and TokenPlan behavior.

### Task 3: Build the responsive workspace layout

**Files:**
- Modify: `frontend-web/styles.css`

1. Add compact detail toolbar and stable two-column workspace dimensions.
2. Make the package list sticky and independently scrollable on desktop.
3. Collapse to a single-column layout on mobile.
4. Ensure controls and long titles do not overlap.

### Task 4: Verify and ship

**Files:**
- Test: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Test: all `frontend-web/scripts/*smoke*.mjs`

1. Run the targeted smoke test.
2. Run all frontend smoke scripts.
3. Run `go test ./...`, `bash scripts/privacy-smoke.sh`, and `git diff --check`.
4. Use Playwright at desktop and mobile viewports to verify route and click behavior.
5. Commit, push to `dedao-kbase/main`, deploy, and validate the production URL.
