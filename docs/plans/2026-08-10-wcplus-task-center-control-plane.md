# WC Plus Task Center Control Plane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the production task center's direct WC Plus loopback query with existing source-control-plane runs.

**Architecture:** The browser loads book jobs, source sync runs, and source subscriptions independently. It joins runs to subscriptions in the browser and normalizes them into the existing task-card view; vendor-local WC Plus queues remain confined to the diagnostics UI.

**Tech Stack:** Vanilla JavaScript frontend, Node.js smoke tests, existing Go HTTP APIs.

---

### Task 1: Specify source-run task normalization

**Files:**
- Modify: `frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`
- Modify: `frontend-web/app.js`

**Step 1: Write the failing test**

Add a WC Plus subscription and source sync run fixture. Assert that loading `/jobs` requests
`/api/source-sync/runs?limit=50` and `/api/source-subscriptions`, never requests
`/api/wcplus/task/all`, and renders a normalized WC Plus task with counters.

**Step 2: Run the test to verify it fails**

Run: `node frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`

Expected: FAIL because the current code still requests `/api/wcplus/task/all`.

**Step 3: Write minimal implementation**

Add a source-run normalization branch in `normalizeJobTask`, join runs to subscriptions by
`subscription_id`, and replace the WC Plus task request in `loadJobCenter` with the two existing
control-plane endpoints.

**Step 4: Run the test to verify it passes**

Run: `node frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`

Expected: PASS.

### Task 2: Verify independent failure behavior

**Files:**
- Modify: `frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`
- Modify: `frontend-web/app.js`

**Step 1: Write the failing test**

Reject the source-run request while returning a book job. Assert that the book job remains visible
and the Chinese status identifies only the source-control-plane failure.

**Step 2: Run the test to verify it fails**

Run: `node frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`

Expected: FAIL until source errors are classified independently.

**Step 3: Write minimal implementation**

Build source-run tasks only when both runs and subscriptions are available, preserve book jobs,
and add a scoped Chinese error message for the failed source request.

**Step 4: Run the test to verify it passes**

Run: `node frontend-web/scripts/job-center-recovery-behavior-smoke.mjs`

Expected: PASS.

### Task 3: Release verification

**Files:**
- Modify only if required by failures: files already listed above

**Step 1: Run focused frontend checks**

Run:

```bash
node frontend-web/scripts/job-center-recovery-behavior-smoke.mjs
node frontend-web/scripts/source-agent-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
```

Expected: all PASS.

**Step 2: Run repository checks**

Run:

```bash
go test ./...
cd frontend && npm run build
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS with no tracked generated output.

**Step 3: Commit only scoped files**

Stage the two plan files, `frontend-web/app.js`, and the task-center smoke test explicitly, then
commit with a scoped fix subject.
