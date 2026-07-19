# Job Center Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a global `/jobs` workspace that aggregates existing task status and exposes actionable errors.

**Architecture:** Start as a frontend aggregator over existing private APIs, beginning with WC Plus tasks. Keep local task panels in place, but add global navigation and source links so later Dedao and knowledge jobs can join the same model.

**Tech Stack:** Vanilla `frontend-web/app.js`, `frontend-web/styles.css`, existing `/api/wcplus/task/all`, smoke tests in `frontend-web/scripts/book-knowledge-web-smoke.mjs`.

---

### Task 1: Add Job Center Smoke Markers

**Files:**
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Steps:**
1. Assert `ROUTES.jobs`, `renderJobCenter`, `loadJobCenter`, `normalizeJobTask`, `jobCenterState`, and `/jobs` exist.
2. Run `node frontend-web/scripts/book-knowledge-web-smoke.mjs` and confirm it fails.

### Task 2: Implement Job Center State And Route

**Files:**
- Modify: `frontend-web/app.js`

**Steps:**
1. Add `ROUTES.jobs = "/jobs"`.
2. Add `jobCenterState`.
3. Add `normalizeJobTask`, `jobStatusLabel`, `jobStatusClass`, `renderJobCenter`, and `loadJobCenter`.
4. Add a nav link to `/jobs`.
5. Route `/jobs` in `boot()`.
6. Run `node --check frontend-web/app.js` and the Web smoke test.

### Task 3: Style Job Center

**Files:**
- Modify: `frontend-web/styles.css`

**Steps:**
1. Add `.job-center`, `.job-center__toolbar`, `.job-center__grid`, `.job-card`, and status classes.
2. Keep layout compact and consistent with existing KBase controls.
3. Run the Web smoke test.

### Task 4: Browser Verify

**Files:**
- No source edits unless verification fails.

**Steps:**
1. Mock `/api/wcplus/task/all` with running and failed tasks.
2. Open `/jobs`.
3. Verify rows show source, status, progress, error, and source workspace link.
4. Verify refresh reloads tasks.

### Task 5: Full Verify And Deploy

**Steps:**
1. Run Go tests, frontend build, Web smoke, WC Plus smoke, knowledge contract smoke, health smoke, privacy smoke, and `git diff --check`.
2. Commit changed files.
3. Push and deploy with the existing dedao-kbase workflow.
