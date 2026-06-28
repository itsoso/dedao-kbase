# Dedao Web GUI Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Convert `frontend-web` into a router-based Web shell that mirrors the desktop GUI's top-level navigation while preserving the current KBase workbench.

**Architecture:** Add Vue Router to `frontend-web`, move the current KBase implementation into a dedicated view, and introduce a shared shell with desktop-equivalent module navigation. Non-KBase modules are route-level migration-status pages only in Phase 1; data APIs come later.

**Tech Stack:** Vue 3.2, TypeScript 4.6, Vite, Vue Router 4, existing plain CSS, existing `KBaseClient`.

---

### Task 1: Add Router Dependency and Smoke Coverage

**Files:**
- Modify: `frontend-web/package.json`
- Modify: `frontend-web/package-lock.json`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write the failing smoke assertions**

Assert that:

- `frontend-web/src/router.ts` exists.
- `frontend-web/src/views/KBaseWorkbench.vue` exists.
- `frontend-web/src/views/ModuleLanding.vue` exists.
- `frontend-web/src/App.vue` contains `router-view`.
- `router.ts` includes `/home`, `/course`, `/odob`, `/ebook`, `/knowledge`, `/book-knowledge`, `/compass`, `/setting`, `/user/profile`.

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`

Expected: FAIL because router files/routes do not exist yet.

**Step 3: Add dependency**

Run: `cd frontend-web && npm install vue-router@4.0.16`

Expected: `package.json` and `package-lock.json` include `vue-router`.

### Task 2: Move Current Workbench Into a Route View

**Files:**
- Create: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/src/App.vue`

**Step 1: Move current implementation**

Move the current contents of `App.vue` into `views/KBaseWorkbench.vue`.

**Step 2: Keep imports valid**

Update imports inside `KBaseWorkbench.vue`:

```ts
import { getBrowserSession, KBaseClient, ... } from '../api'
import { renderMarkdown } from '../utils/markdownRender'
```

**Step 3: Replace `App.vue` with shell**

`App.vue` should render:

```vue
<template>
  <main class="dedao-web-shell">
    <header class="shell-header">...</header>
    <nav class="shell-nav">...</nav>
    <router-view />
  </main>
</template>
```

The shell navigation must contain desktop-equivalent top-level entries.

### Task 3: Add Router and Module Landing Pages

**Files:**
- Create: `frontend-web/src/router.ts`
- Create: `frontend-web/src/views/ModuleLanding.vue`
- Modify: `frontend-web/src/main.ts`

**Step 1: Define route metadata**

Routes:

- `/` redirects to `/book-knowledge`
- `/home`
- `/course`
- `/odob`
- `/ebook`
- `/knowledge`
- `/book-knowledge`
- `/compass`
- `/setting`
- `/user/login`
- `/user/profile`
- `/user/switch`
- catch-all redirects to `/book-knowledge`

**Step 2: Mount router**

Update `main.ts`:

```ts
import router from './router'

createApp(App).use(router).mount('#app')
```

**Step 3: Module landing data**

`ModuleLanding.vue` receives route metadata and renders migration status for non-KBase modules without calling unavailable APIs.

### Task 4: Adapt Styles Without Breaking KBase

**Files:**
- Modify: `frontend-web/src/style.css`

**Step 1: Add shell styles**

Add CSS for:

- `.dedao-web-shell`
- `.shell-header`
- `.shell-nav`
- `.shell-nav a`
- `.module-landing`

**Step 2: Preserve KBase styles**

Keep existing `.kbase-web-shell`, workbench grid, markdown, jobs, and ops styles.

**Step 3: Responsive behavior**

At `max-width: 1180px`, shell nav must wrap without horizontal overflow.

### Task 5: Verify and Commit

**Files:**
- All changed Phase 1 files.

**Step 1: Run smoke**

Run: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`

Expected: PASS.

**Step 2: Run build**

Run: `cd frontend-web && npm run build`

Expected: PASS.

**Step 3: Browser check**

Run local preview:

```bash
cd frontend-web && npm run preview -- --host 127.0.0.1 --port 4173
```

Use Chrome/Playwright to verify:

- `/book-knowledge` renders KBase workbench.
- `/course` renders module landing.
- Shell nav has desktop modules.
- Mobile viewport has no horizontal overflow.

**Step 4: Diff check**

Run: `git diff --check`

Expected: no output and exit 0.

**Step 5: Commit**

```bash
git add frontend-web/package.json frontend-web/package-lock.json frontend-web/scripts/web-kbase-ui-smoke.mjs frontend-web/src/App.vue frontend-web/src/main.ts frontend-web/src/router.ts frontend-web/src/views/KBaseWorkbench.vue frontend-web/src/views/ModuleLanding.vue frontend-web/src/style.css docs/plans/2026-06-28-dedao-web-gui-parity-plan.md docs/plans/2026-06-28-dedao-web-gui-phase1.md docs/dossiers/2026-06-28-dedao-web-gui-parity.md
git commit -m "feat(web): add dedao gui shell"
```

### Task 6: Deploy Phase 1 Static Frontend

**Files:**
- Deploy artifact: `frontend-web/dist/`

**Step 1: Sync static files**

Run:

```bash
rsync -az --delete frontend-web/dist/ executor.life:/var/www/kbase.executor.life/
```

**Step 2: Online verification**

Run:

```bash
curl -fsS -o /tmp/kbase-gui-health.txt -w '%{http_code}' https://kbase.executor.life/health
curl -sS -o /tmp/kbase-gui-root-auth.txt -w '%{http_code}' https://kbase.executor.life/
ssh executor.life 'grep -R "dedao-web-shell" -n /var/www/kbase.executor.life/assets/*.css >/tmp/dedao-web-shell-css-check && grep -R "书籍知识库" -n /var/www/kbase.executor.life/assets/*.js >/tmp/dedao-web-shell-js-check && echo deployed_gui_shell_ok'
```

Expected:

- Health returns `200`.
- Root without Basic Auth returns `401`.
- Static asset grep returns `deployed_gui_shell_ok`.

**Step 3: Update Dossier**

Mark G3-G6 with exact evidence and set status to `shipped` if deployment verification passes.
