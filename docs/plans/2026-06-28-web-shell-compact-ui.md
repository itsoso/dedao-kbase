# Web Shell Compact UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Compact the browser Web GUI shell and placeholder module pages so less vertical space is wasted above the working area.

**Architecture:** Keep the existing Vue Router shell in `frontend-web/src/App.vue`. Add explicit compact layout hooks, update shared CSS in `frontend-web/src/style.css`, and reshape `frontend-web/src/views/ModuleLanding.vue` without changing route metadata or API clients.

**Tech Stack:** Vue 3, Vue Router, Vite, existing `frontend-web/scripts/web-kbase-ui-smoke.mjs`.

---

### Task 1: Smoke RED

**Files:**
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1:** Add assertions for `compact-shell-header`, `compact-shell-nav`, `module-landing-compact`, and `module-summary-row`.

**Step 2:** Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: fail on missing compact hooks.

### Task 2: Compact Shell And Placeholder Pages

**Files:**
- Modify: `frontend-web/src/App.vue`
- Modify: `frontend-web/src/style.css`
- Modify: `frontend-web/src/views/ModuleLanding.vue`

**Step 1:** Add compact hook classes to the shell header/nav.

**Step 2:** Reduce header/nav padding, card height, and top margins in shared CSS.

**Step 3:** Reshape `ModuleLanding` into a compact summary row plus methods strip.

**Step 4:** Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
cd frontend-web && npm run build
```

Expected: pass.

### Task 3: Browser Check, Deploy, Commit

**Files:**
- Modify: `docs/plans/2026-06-28-web-shell-compact-ui-design.md`
- Modify: `docs/plans/2026-06-28-web-shell-compact-ui.md`

**Step 1:** Use local `cmd/kbase-server` with `KBASE_WEB_DIR=frontend-web/dist` and browser automation to verify `/home`, `/course`, and `/book-knowledge`.

**Step 2:** Deploy `frontend-web/dist` to `kbase.executor.life`.

**Step 3:** Verify `/health` and deployed assets.

**Step 4:** Commit only task-related files.
