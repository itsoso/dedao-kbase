# Web KBase UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a browser-accessible, read-only book knowledge workbench served by `cmd/kbase-server`.

**Architecture:** Add an independent `frontend-web/` Vue/Vite app that talks to the existing `/api/*` kbase HTTP endpoints with a Bearer token. Extend `backend/app/kbase_http.go` and `cmd/kbase-server/main.go` so the same private server can optionally serve the built web app while keeping `/api/*` authenticated and `/health` unauthenticated.

**Tech Stack:** Go `net/http` + `httptest`, Vue 3, Vite, TypeScript, plain CSS, Node smoke scripts.

---

### Task 1: Web UI Smoke Test

**Files:**
- Create: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write the failing smoke test**

Create a Node script that reads `frontend-web/src/App.vue` and `frontend-web/src/api.ts`, then asserts:

- `kbase-web-shell`, `connection-bar`, `book-rail`, `search-panel`, `detail-panel`, `system-kb-panel` exist in the page.
- The page exposes base URL and token inputs.
- The page calls `listBooks`, `getBook`, `searchKnowledge`, `getSystemKBManifest`, and `getSystemKBExport`.
- `api.ts` sets an `Authorization` header with `Bearer ${token}`.
- `api.ts` throws an error containing HTTP status text/body for failed requests.

**Step 2: Run test to verify it fails**

Run: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`

Expected: FAIL because `frontend-web/src/App.vue` and `frontend-web/src/api.ts` do not exist yet.

**Step 3: Commit**

Do not commit yet; keep this red test with Task 2 implementation.

### Task 2: Frontend Web App

**Files:**
- Create: `frontend-web/package.json`
- Create: `frontend-web/index.html`
- Create: `frontend-web/tsconfig.json`
- Create: `frontend-web/vite.config.ts`
- Create: `frontend-web/src/api.ts`
- Create: `frontend-web/src/main.ts`
- Create: `frontend-web/src/App.vue`
- Create: `frontend-web/src/style.css`
- Test: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Implement the minimal app shell**

Use Vue 3 with a single `App.vue`. Keep the app read-only and browser-native:

- Persist `baseUrl` and `token` in `localStorage`.
- Show connection fields and a refresh button.
- Load books from `/api/books`.
- Load selected book from `/api/books/{book_id}`.
- Search via `/api/search?q=...&book_id=...`.
- Load System KB manifest/export from `/api/system-kb/*`.

**Step 2: Implement the API client**

Create `frontend-web/src/api.ts` with a `KBaseClient` class:

- Normalize `baseUrl` without a trailing slash.
- Attach `Authorization: Bearer ${token}` for every API call.
- Parse JSON responses.
- Throw `Error("HTTP <status>: <body>")` on non-2xx responses.

**Step 3: Run smoke to verify it passes**

Run: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`

Expected: PASS with `web kbase UI smoke passed`.

**Step 4: Build the web app**

Run:

```bash
cd frontend-web && npm install && npm run build
```

Expected: TypeScript and Vite build exit 0.

**Step 5: Commit**

```bash
git add frontend-web
git commit -m "feat(kbase): add web knowledge UI"
```

### Task 3: Serve Web Assets From KBase Server

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `cmd/kbase-server/main.go`

**Step 1: Write failing Go tests**

Add `TestKBaseHTTPHandlerServesWebAssets` to `backend/app/kbase_http_test.go`:

- Create a temp web dir with `index.html` and `assets/app.js`.
- Build `NewKBaseHTTPHandler(KBaseHTTPConfig{StaticDir: webDir, AuthToken: "secret-token", Store: store})`.
- Assert `GET /` returns the index HTML without Authorization.
- Assert `GET /assets/app.js` returns the asset.
- Assert unknown browser routes such as `/books/42` fall back to index HTML.
- Assert `/api/books` still requires Bearer auth.

**Step 2: Run test to verify it fails**

Run: `go test ./backend/app -run TestKBaseHTTPHandlerServesWebAssets -count=1`

Expected: FAIL because `KBaseHTTPConfig` has no `StaticDir` behavior yet.

**Step 3: Implement static serving**

Add `StaticDir string` to `KBaseHTTPConfig` and `kbaseHTTPHandler`. In `ServeHTTP`:

- Keep `/health` first.
- Keep `/api/*` authenticated and GET-only.
- For non-API requests, serve from `StaticDir` if configured.
- Use SPA fallback to `index.html` when a requested static path does not exist.
- Return 404 when `StaticDir` is empty or invalid.

**Step 4: Wire server flag/env**

In `cmd/kbase-server/main.go`, add:

- `--web-dir`, defaulting to `KBASE_WEB_DIR`.
- If unset, default to `frontend-web/dist` when that directory exists under the current working directory.
- Pass the value into `KBaseHTTPConfig.StaticDir`.

**Step 5: Run focused Go tests**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go cmd/kbase-server/main.go
git commit -m "feat(kbase): serve web UI assets"
```

### Task 4: README Web UI Usage

**Files:**
- Modify: `README.md`

**Step 1: Document local web UI workflow**

Add a short section under `kbase HTTP 服务`:

```bash
cd frontend-web
npm install
npm run build

cd ..
KBASE_AUTH_TOKEN="replace-with-long-secret" \
KBASE_WEB_DIR="$PWD/frontend-web/dist" \
go run ./cmd/kbase-server --addr 127.0.0.1:8719
```

Mention that the browser opens `http://127.0.0.1:8719/` and the same token is entered in the web connection bar.

**Step 2: Run markdown diff check**

Run: `git diff --check -- README.md`

Expected: PASS.

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add web kbase UI usage"
```

### Task 5: Final Verification

**Files:**
- All files touched above.

**Step 1: Run frontend smoke**

Run: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`

Expected: PASS.

**Step 2: Run frontend build**

Run: `cd frontend-web && npm run build`

Expected: PASS.

**Step 3: Run focused backend tests**

Run: `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`

Expected: PASS.

**Step 4: Run repository diff check**

Run: `git diff --check`

Expected: PASS.
