---
slug: 2026-06-28-dedao-web-gui-phase2-login
status: shipped
current_stage: S8
last-reviewed: 2026-06-28
---

# Dossier: Dedao Web GUI Phase 2 Login

## 用户原话需求

> 继续执行

## Slice

Continue Phase 2 auth work by replacing the Web `/user/login` placeholder with a real QR login surface backed by safe server-side HTTP endpoints.

## Scope

- Add `POST /api/dedao/auth/qrcode`.
- Add `POST /api/dedao/auth/check`.
- Reuse existing Dedao login/token/QR/check logic server-side.
- Persist successful login through existing config path.
- Return only QR payload, polling status, safe user metadata, and safe session summary.

## Non-Goals

- Do not expose Dedao cookies to the browser.
- Do not implement account switching.
- Do not implement logout in this slice.
- Do not expose membership details beyond existing `/api/dedao/session`.

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | This is the next Phase 2 auth slice after session/status shipped |
| G2 可行性 | PASS | Desktop QR login already exists in `backend/login.go` and `backend/app/login.go`; Web can expose it through Bearer-protected endpoints |
| G3 测试 | PASS | Red: `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedaoAuth' -count=1` failed on missing auth types/config field; Red: `node frontend-web/scripts/web-kbase-ui-smoke.mjs` failed on missing `AccountLogin.vue`. Green: targeted Go test, `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`, Web smoke, `cd frontend-web && npm run build`, Linux `go build`, `git diff --check`, and Chrome CDP login/profile/mobile checks |
| G4 评审 | PASS | Auth endpoints are under existing Bearer `/api/*` auth; responses contain QR/login status and safe user/session fields only; online QR check reported `has_cookie=False` and `no_store=True` |
| G5 部署健康 | PASS | Uploaded Linux binary and `frontend-web/dist`, restarted `dedao-kbase.service`, `nginx -t` successful, service active, `https://kbase.executor.life/health` returned 200 |
| G6 上线验证 | PASS | Unauthenticated `/api/dedao/auth/qrcode` and `/user/login` returned 401; Bearer QR endpoint returned 200 with QR payload, login token present, no cookie text, and no-store header; deployed assets include `account-login` |

## Plan

- Product/implementation plan: `docs/plans/2026-06-28-dedao-web-gui-phase2-login.md`

## Result

Phase 2 login slice shipped. `/user/login` now renders a Web QR login surface backed by Bearer-protected auth endpoints, while raw Dedao cookies remain server-side.
