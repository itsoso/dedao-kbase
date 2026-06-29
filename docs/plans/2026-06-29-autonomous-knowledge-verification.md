# Autonomous Knowledge Verification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a read-only verification gateway that gives health and Proofroom machine-readable risk tiers, verification scores, and allowed uses for book claims.

**Architecture:** Extend the existing project knowledge layer in `backend/app` with deterministic verification reports. Expose a Bearer-protected `/api/projects/{project}/verification-report` route and surface the summary in the Web KBase project panel. Keep all outputs draft/assistive and avoid downstream writes.

**Tech Stack:** Go, JSON book knowledge store, private kbase HTTP API, Vue 3/Vite TypeScript frontend.

---

### Task 1: Backend Verification Report

**Files:**
- Test: `backend/app/kbase_http_test.go`
- Create: `backend/app/book_project_verification.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Write the failing HTTP test**

Add a test that saves a package with cited, uncited, and health-sensitive claims, then requests:

```text
GET /api/projects/health/verification-report?limit=10
GET /api/projects/proofroom/verification-report?limit=10
```

Expected before implementation: 404 or missing `verification_report` semantics.

**Step 2: Run the focused test and confirm RED**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesProjectVerificationReport' -count=1
```

Expected: FAIL because the route is not implemented.

**Step 3: Implement deterministic verification**

Create verification types, score rules, risk tiers, decisions, provenance hashes, and project-specific allowed/blocked uses. Health-sensitive claims must not become `auto_usable`.

**Step 4: Wire the route**

Add `verification-report` handling under `/api/projects/{project}/...`, with bounded `limit`.

**Step 5: Run the focused test and confirm GREEN**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesProjectVerificationReport' -count=1
```

Expected: PASS.

### Task 2: Web Project Verification Panel

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/src/style.css`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write smoke assertions first**

Assert that the API client and workbench reference `verification-report`, `verification_score`, `risk_tier`, and project verification UI.

**Step 2: Run smoke and confirm RED**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: FAIL before frontend implementation.

**Step 3: Add API types and client method**

Add TypeScript interfaces for verification reports and a `getProjectVerificationReport(projectID, limit)` method.

**Step 4: Add compact verification UI**

Load the report with the existing project hub calls. Show tier counts, policy, and top verified items with score, tier, decision, and failure reasons.

**Step 5: Run smoke and build**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
```

Expected: both pass.

### Task 3: Verification, Deploy, Commit

**Files:**
- All files changed in Tasks 1-2.

**Step 1: Run full checks**

```bash
go test ./...
git diff --check
```

**Step 2: Build deployable artifacts**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
```

**Step 3: Deploy**

Install the rebuilt server, sync `frontend-web/dist/`, restart `dedao-kbase.service`, and verify `/health`, `/api/projects/health/verification-report`, `/api/projects/proofroom/verification-report`, unauthorized 401 behavior, and deployed bundle markers.

**Step 4: Commit and push**

Stage only task files, commit with:

```bash
git commit -m "feat(kbase): add autonomous verification report"
```
