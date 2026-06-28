# Dedao Web GUI Phase 2 Login Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a safe Web QR login flow for the desktop GUI personal center route.

**Architecture:** Extend `cmd/kbase-server`'s Bearer-protected `/api/*` handler with QR login and polling endpoints. The production auth provider reuses existing Dedao service/login helpers, while tests inject a fake provider so no real Dedao network call is needed. The Web app adds a dedicated `/user/login` page that requests a QR code and polls login status without ever receiving cookies.

**Tech Stack:** Go HTTP handler/tests, Vue 3.2, TypeScript, existing `KBaseClient`.

---

### Task 1: Backend QR Login API

**Files:**
- Modify: `backend/app/kbase_http_test.go`
- Modify: `backend/app/kbase_http.go`
- Create: `backend/app/dedao_auth.go`

Steps:

1. Add failing HTTP tests for `POST /api/dedao/auth/qrcode` and `POST /api/dedao/auth/check`.
2. Add a `DedaoAuthProvider` interface and default implementation backed by existing Dedao login helpers.
3. Route both endpoints behind existing Bearer authorization.
4. Return only safe login status and user/session metadata.
5. Verify responses exclude raw cookie fields.

### Task 2: Frontend Login Client and Page

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/router.ts`
- Create: `frontend-web/src/views/AccountLogin.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`
- Modify: `frontend-web/src/style.css`

Steps:

1. Add failing smoke assertions for `AccountLogin.vue`, auth client methods, and `/api/dedao/auth/*`.
2. Add QR/login check types and `KBaseClient` methods.
3. Route `/user/login` to `AccountLogin.vue`.
4. Render QR code, polling state, expired state, and success link to `/user/profile`.

### Task 3: Verification and Deploy

Commands:

- `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoAuth' -count=1`
- `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`
- `node frontend-web/scripts/web-kbase-ui-smoke.mjs`
- `cd frontend-web && npm run build`
- `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`
- `git diff --check`

Deploy:

- Copy binary to server and restart `dedao-kbase.service`.
- Sync `frontend-web/dist`.
- Verify `/health`, unauthenticated auth endpoints return 401, Bearer QR endpoint returns JSON or upstream error without leaking cookies, and `/user/login` remains Basic Auth protected.
