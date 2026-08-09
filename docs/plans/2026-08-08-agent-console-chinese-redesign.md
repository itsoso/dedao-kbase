# Agent Console Chinese Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the v1 Book Agent route into a Chinese-first professional control console with a responsive workspace and status rail while preserving runtime behavior.

**Architecture:** Add a route-specific renderer for the v1 Agent console and reuse the existing search, chat, reader, evidence, state, and event binding contracts. Keep Package, Book App, and v2 evidence-audit rendering on their current paths. Add isolated CSS under an `agent-console` namespace and a source-level smoke test that defines the required content and responsive structure.

**Tech Stack:** Vanilla JavaScript template rendering, CSS Grid, existing Node.js smoke tests, Go HTTP service deployment.

---

### Task 1: Define the console contract with a failing smoke test

**Files:**
- Create: `frontend-web/scripts/agent-console-ui-smoke.mjs`

**Step 1: Write the failing test**

Assert that `app.js` contains a dedicated v1 Agent console renderer, Chinese route and status labels, a Chinese metric mapping, unchanged search/chat form IDs, and a collapsed technical identity section. Assert that `styles.css` contains namespaced desktop two-column, sticky rail, mobile one-column, safe wrapping, and reduced-motion rules.

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/agent-console-ui-smoke.mjs`

Expected: FAIL because the dedicated renderer and `agent-console` styles do not exist.

**Step 3: Commit the test**

Stage only `frontend-web/scripts/agent-console-ui-smoke.mjs` and commit with `test(web): define Chinese Agent console contract`.

### Task 2: Implement the Chinese control console

**Files:**
- Modify: `frontend-web/app.js:4761`
- Modify: `frontend-web/styles.css:4949`

**Step 1: Add minimal rendering helpers**

Add helpers that derive the Chinese display name from the pinned Release, translate known evaluation metric keys, render the reused reader/search tools, and render the v1 Agent console. Do not alter API calls or form IDs.

**Step 2: Route only v1 Agent pages into the new renderer**

In `renderBookAgentPlatform`, use the new console only when `route.view === "agent"` and the package is not `agent-package.v2`. Preserve the existing package/app Hero and the v2 evidence-audit branch.

**Step 3: Add isolated responsive styles**

Add `.agent-console` styles for the industrial dark masthead, status summary, two-column body, sticky rail, compact metrics, collapsed technical identity, and 760px one-column behavior. Ensure long values wrap and all grid children have `min-width: 0`.

**Step 4: Run test to verify it passes**

Run: `node frontend-web/scripts/agent-console-ui-smoke.mjs`

Expected: PASS.

**Step 5: Run related regression checks**

Run:

```bash
node frontend-web/scripts/evidence-audit-agent-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node --check frontend-web/app.js
```

Expected: all PASS with no syntax error.

**Step 6: Commit the implementation**

Stage only the renderer, stylesheet, and smoke test changes and commit with `feat(web): redesign Agent console in Chinese`.

### Task 3: Verify the release candidate

**Files:**
- Create: `docs/dossiers/2026-08-08-agent-console-chinese-redesign.md`

**Step 1: Run full relevant gates**

Run the new smoke, all `frontend-web/scripts/*smoke.mjs`, `go test ./... -timeout=300s`, `bash scripts/system-map-smoke.sh`, `bash scripts/privacy-smoke.sh`, `git diff --check`, and `git status --short`.

Expected: all gates PASS and only task files are changed.

**Step 2: Review the diff**

Confirm every production change traces to the approved redesign, no authentication/runtime contract changed, and no private data or downloaded content appears.

**Step 3: Record G1-G4 evidence**

Create the dossier with requirement, scope, tests, review decision, deployment plan, rollback plan, and pending online checks.

**Step 4: Commit the dossier**

Stage only the dossier and commit with `docs(web): record Agent console release gates`.

### Task 4: Deploy and verify production

**Files:**
- Modify after verification: `docs/dossiers/2026-08-08-agent-console-chinese-redesign.md`

**Step 1: Build and deploy the exact clean commit**

Push the feature branch, archive the exact commit, run Linux tests before building, back up the current binary and static frontend, deploy atomically, restart the service, and retain the rollback path.

**Step 2: Verify deployment health**

Check the public health endpoint reports the expected revision, the service is active with zero restarts, and recent logs contain no new fatal/error entries.

**Step 3: Verify desktop and mobile UI**

At desktop width, confirm the two-column console, Chinese title, visible status rail, collapsed technical identity, search, chat, and no horizontal overflow. At mobile width, confirm single-column ordering, safe long-value wrapping, readable tools, and no horizontal overflow.

**Step 4: Verify runtime behavior**

Run a relevant grounded search and an unrelated query. Confirm the relevant query returns cited evidence and the unrelated query returns a clear empty/abstention result without changing the page route.

**Step 5: Record G5-G6 and commit**

Update the dossier with exact revision, backup/rollback identity, health and browser evidence. Re-run privacy and whitespace checks, then commit only the dossier update.
