# Dedao Web GUI Phase 2 Session Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a safe Web account/session status surface for the desktop GUI personal center route.

**Architecture:** Extend `cmd/kbase-server`'s existing Bearer-protected `/api/*` handler with `GET /api/dedao/session`. The endpoint reads server-side config metadata only and never serializes cookies. The Web app adds `AccountProfile.vue` for `/user/profile` and reuses the browser session token hydration pattern.

**Tech Stack:** Go HTTP handler/tests, Vue 3.2, TypeScript, existing `KBaseClient`.

---

### Task 1: Backend Session API

**Files:**
- Modify: `backend/app/kbase_http_test.go`
- Modify: `backend/app/kbase_http.go`
- Create: `backend/app/dedao_session.go`

Steps:

1. Add a failing HTTP test for `GET /api/dedao/session`.
2. Implement `DedaoSession` response from server config.
3. Route `/api/dedao/session` behind existing Bearer authorization.
4. Verify response excludes raw cookie fields.

### Task 2: Frontend Session Client and Profile Page

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/router.ts`
- Create: `frontend-web/src/views/AccountProfile.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`
- Modify: `frontend-web/src/style.css`

Steps:

1. Add failing smoke assertions for `AccountProfile.vue`, `getDedaoSession`, and `/api/dedao/session`.
2. Add `DedaoSession` types and client method.
3. Route `/user/profile` to `AccountProfile.vue`.
4. Render logged-in state, user metadata, and user count.

### Task 3: Verification and Deploy

Commands:

- `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoSession' -count=1`
- `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`
- `node frontend-web/scripts/web-kbase-ui-smoke.mjs`
- `cd frontend-web && npm run build`
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`
- `git diff --check`

Deploy:

- Copy binary to server and restart `dedao-kbase.service`.
- Sync `frontend-web/dist`.
- Verify `/health`, unauthenticated `/api/dedao/session` is 401, Bearer `/api/dedao/session` returns JSON, and `/user/profile` remains Basic Auth protected.
