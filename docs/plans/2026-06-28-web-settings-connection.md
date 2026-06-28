# Web Settings Connection Relocation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move the repeated Web `Base URL` and `Token` connection controls out of business pages and into `/setting`.

**Architecture:** Add a real Web settings view backed by the existing `dedao-kbase-web-settings` localStorage record. Business pages keep using the same stored connection values and browser-session token hydration, but their top toolbars only show page identity, refresh action, and connection status.

**Tech Stack:** Vue 3 single-file components, Vue Router, existing `KBaseClient`, browser localStorage, existing smoke script.

---

### Task 1: Smoke Test

**Files:**
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Steps:**
1. Assert `router.ts` routes `/setting` to a real `WebSettings` component.
2. Assert `WebSettings.vue` contains `settingsBaseUrl`, `settingsToken`, and the shared storage key.
3. Assert course, ebook, course-detail, and ebook-detail pages no longer contain their old Base URL/Token input names.
4. Run `node frontend-web/scripts/web-kbase-ui-smoke.mjs` and confirm RED.

### Task 2: Settings View

**Files:**
- Create: `frontend-web/src/views/WebSettings.vue`
- Modify: `frontend-web/src/router.ts`

**Steps:**
1. Import `WebSettings` in the router.
2. Route `/setting` to `WebSettings`.
3. Build a compact settings form for Base URL and Token.
4. Load/save `dedao-kbase-web-settings`.
5. Add a button to hydrate from `/browser/session-token`.

### Task 3: Business Page Toolbars

**Files:**
- Modify: `frontend-web/src/views/CourseLibrary.vue`
- Modify: `frontend-web/src/views/EbookLibrary.vue`
- Modify: `frontend-web/src/views/CourseDetailReader.vue`
- Modify: `frontend-web/src/views/EbookDetailReader.vue`
- Optionally modify: `frontend-web/src/views/KBaseWorkbench.vue`

**Steps:**
1. Remove inline Base URL and Token labels from the top toolbar.
2. Keep localStorage-based connection loading in script.
3. Keep refresh/connect action and status pill.
4. Adjust grid columns so the toolbar is compact.

### Task 4: Verify

**Commands:**
- `node frontend-web/scripts/web-kbase-ui-smoke.mjs`
- `cd frontend-web && npm run build`
- `git diff --check`

Expected: all commands exit 0.
