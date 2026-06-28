---
slug: 2026-06-28-dedao-web-gui-phase3-ebooks
status: shipped
current_stage: S8
last-reviewed: 2026-06-28
---

# Dossier: Dedao Web GUI Phase 3 Ebook Bookshelf

## 用户原话需求

> 登录成功 但是无法看到内容 内容是空的

## Diagnosis

The screenshot shows `/ebook` rendering the Web GUI parity placeholder (`ModuleLanding`) with `status: planned`. Login is working, but the ebook bookshelf route had not been migrated from the desktop Wails GUI.

Desktop source of truth:

- `frontend/src/views/Ebook.vue`
- `CourseList("ebook", "study", page, pageSize)`
- `CourseCategory()` for total count

## Scope

Ship the first read-only Phase 3 content browser slice:

- `GET /api/dedao/ebooks`
- `frontend-web/src/views/EbookLibrary.vue`
- `/ebook` routed to `EbookLibrary`

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | User reported successful login but blank `/ebook` content |
| G2 可行性+风险压测 | PASS | Root cause traced to placeholder route; reused existing `CourseList("ebook","study")`; scoped out downloads and shelf mutations |
| G3 测试 | PASS | Red: `go test ./backend/app -run TestKBaseHTTPHandlerServesDedaoEbooks -count=1` failed on missing API/types; Red: `node frontend-web/scripts/web-kbase-ui-smoke.mjs` failed on missing `EbookLibrary.vue`; Green: both commands passed; `go test ./backend/app -count=1`; `npm run build` in `frontend-web` |
| G4 评审 | PASS | Focused diff only touched ebook API, ebook Web route, smoke tests, and docs; side-effect actions stayed out of scope |
| G5 部署健康 | PASS | Installed Linux `kbase-server`, synced `frontend-web/dist`, restarted `dedao-kbase.service`; `systemctl is-active` returned `active`; `/health` returned 200 |
| G6 上线验证 | PASS | Unauthenticated `/api/dedao/ebooks` returned 401; Bearer `/api/dedao/ebooks?page=1&page_size=5` returned 200 with 5 ebooks, total 653, first item had title; deployed assets contain `ebook-library`; unauthenticated `/ebook` returned 401 |

## Decision

The Web ebook route is read-only for now. Actions with side effects (`EbookDownload`, `EbookDownloadAndSyncWiki`, shelf remove/add) remain future job-backed slices.
