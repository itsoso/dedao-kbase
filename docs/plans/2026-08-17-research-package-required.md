# Research Package Required Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent invalid research creation requests when Agent Package or version is missing and replace raw server codes with actionable Chinese guidance.

**Architecture:** Keep the backend fail-closed contract unchanged. Add client-side required-field semantics and validation in the static research workspace, then map the two research creation error codes at the UI boundary. Preserve URL-based package/version prefill.

**Tech Stack:** Vanilla JavaScript, static HTML rendering, Node.js smoke tests.

---

### Task 1: Add failing research workspace contract checks

**Files:**
- Modify: `frontend-web/scripts/research-workspace-smoke.mjs`
- Test: `frontend-web/scripts/research-workspace-smoke.mjs`

**Step 1: Write the failing test**

Add assertions that the research workspace source:

- marks `package_id` and `package_version` inputs as required;
- exposes an Agent management selection link;
- validates missing Package/version before calling `/api/research/runs`;
- contains Chinese mappings for `invalid_research_request` and `research_package_not_eligible`.

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/research-workspace-smoke.mjs`

Expected: FAIL because the current inputs still use `placeholder="可选"` and there is no client-side validation/mapping.

### Task 2: Implement the minimal frontend correction

**Files:**
- Modify: `frontend-web/app.js`
- Test: `frontend-web/scripts/research-workspace-smoke.mjs`

**Step 1: Add a focused error presentation helper**

Add a small helper that maps only:

- `invalid_research_request` → ask for a complete Package, version, question, mode and sources;
- `research_package_not_eligible` → explain that the Package does not support the selected mode/source and link the user back to Agent management.

Unknown errors must keep the existing message.

**Step 2: Correct form semantics**

Mark both Package fields `required`, replace the optional placeholders with `必填`, and render a link to `/sources/agents` or the existing Agent management route.

**Step 3: Validate before the request**

After reading `FormData`, if either Package field is empty:

- set the Chinese validation message;
- clear the create loading state;
- render the workspace;
- return before `apiFetch`.

**Step 4: Map server errors at the create boundary**

In the create catch path, pass the server error message through the focused research creation mapping helper.

**Step 5: Run the focused test**

Run: `node frontend-web/scripts/research-workspace-smoke.mjs`

Expected: PASS.

### Task 3: Verify the research workspace and repository gates

**Files:**
- Verify: `frontend-web/app.js`
- Verify: `frontend-web/scripts/research-workspace-smoke.mjs`
- Verify: `docs/plans/2026-08-17-research-package-required-design.md`
- Verify: `docs/plans/2026-08-17-research-package-required.md`

**Step 1: Run adjacent frontend smoke tests**

Run:

```bash
node frontend-web/scripts/research-workspace-smoke.mjs
node frontend-web/scripts/browser-cookie-session-smoke.mjs
node frontend-web/scripts/agent-console-ui-smoke.mjs
```

Expected: all PASS.

**Step 2: Run repository policy checks**

Run:

```bash
bash scripts/privacy-smoke.sh
bash scripts/system-map-smoke.sh
git diff --check
```

Expected: all PASS.

**Step 3: Review the final diff**

Run: `git diff -- frontend-web/app.js frontend-web/scripts/research-workspace-smoke.mjs docs/plans/2026-08-17-research-package-required.md`

Expected: only the required validation, Chinese guidance, selection link and tests are changed.

**Step 4: Commit**

Stage only the implementation plan, frontend source and focused smoke test, then commit with:

```bash
git commit -m "fix(research): require package before creating run"
```
