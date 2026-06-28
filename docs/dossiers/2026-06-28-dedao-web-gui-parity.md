---
slug: 2026-06-28-dedao-web-gui-parity
status: shipped
current_stage: S8
last-reviewed: 2026-06-28
---

# Dossier: Dedao Web GUI Parity

## 用户原话需求

> 要根据桌面版的GUI制定Web版的规划，要复刻一个桌面GUI的实现。
> 下一步写文档，写完文档之后直接按照文档开工。

## Scope

Build an online Web version that follows the desktop Wails GUI information architecture, not only the current KBase workbench.

The desktop GUI baseline is `frontend/src/router/index.ts`:

- 首页
- 课程
- 听书书架
- 电子书架
- 知识城邦
- 书籍知识库
- 锦囊
- 设置
- 个人中心: 登录 / 个人简介 / 切换账号

## Non-Goals

- Do not expose Dedao cookies or TokenPlan secrets to the browser.
- Do not make the service public or anonymous.
- Do not implement all desktop data APIs in Phase 1.
- Do not run long downloads or media exports in synchronous HTTP requests.

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | User explicitly requested Web planning based on desktop GUI parity and then implementation |
| G2 可行性 | PASS | Desktop GUI route map and Wails bindings inspected; current `cmd/kbase-server` already serves static Web and Bearer API; Phase 1 scoped to shell/router/navigation only |
| G3 测试 | PASS | Red: `node frontend-web/scripts/web-kbase-ui-smoke.mjs` failed on missing `router.ts`; Green: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `cd frontend-web && npm run build`; Chrome/Playwright verified `/book-knowledge`, `/course`, 9 shell nav items, and no mobile horizontal overflow; `git diff --check` |
| G4 评审 | PASS | Phase 1 changed only Web static shell/router and npm dependency; no new backend routes; `/api/*` and Basic Auth boundaries unchanged |
| G5 部署健康 | PASS | Synced `frontend-web/dist` to `/var/www/kbase.executor.life`; normalized static file permissions; `https://kbase.executor.life/health` returned 200; `dedao-kbase.service` active |
| G6 上线验证 | PASS | Unauthenticated `/` and `/course` both returned 401; deployed assets contain `dedao-web-shell` CSS and `book-knowledge` route JS |

## Documents

- Product plan: `docs/plans/2026-06-28-dedao-web-gui-parity-plan.md`
- Phase 1 implementation plan: `docs/plans/2026-06-28-dedao-web-gui-phase1.md`
- Phase 2 session plan: `docs/plans/2026-06-28-dedao-web-gui-phase2-session.md`
- Phase 2 session dossier: `docs/dossiers/2026-06-28-dedao-web-gui-phase2-session.md`
- Phase 2 login plan: `docs/plans/2026-06-28-dedao-web-gui-phase2-login.md`
- Phase 2 login dossier: `docs/dossiers/2026-06-28-dedao-web-gui-phase2-login.md`
- Phase 3 ebook plan: `docs/plans/2026-06-28-dedao-web-gui-phase3-ebooks.md`
- Phase 3 ebook dossier: `docs/dossiers/2026-06-28-dedao-web-gui-phase3-ebooks.md`
- Phase 3 course plan: `docs/plans/2026-06-28-dedao-web-gui-phase3-courses.md`
- Phase 3 course dossier: `docs/dossiers/2026-06-28-dedao-web-gui-phase3-courses.md`

## Current Decision

Phase 1 shipped: `frontend-web` now has Vue Router, desktop-equivalent Web shell navigation, module landing routes, and the current KBase workbench mounted at `/book-knowledge`.

Phase 2 first slice shipped: `/user/profile` now renders a Web personal center backed by Bearer-protected `GET /api/dedao/session`, exposing only safe session metadata.

Phase 2 login slice shipped: `/user/login` now renders a Web QR login surface backed by Bearer-protected `POST /api/dedao/auth/qrcode` and `POST /api/dedao/auth/check`, with Dedao cookies kept server-side.

Phase 3 first read-only content slice shipped: `/ebook` renders a Web ebook bookshelf backed by Bearer-protected `GET /api/dedao/ebooks`, reusing the desktop `CourseList("ebook","study")` data path and returning only safe browser fields.

Phase 3 second read-only content slice shipped: `/course` renders a Web course browser backed by Bearer-protected `GET /api/dedao/courses`, reusing the desktop `CourseList("bauhinia","study")` data path and returning only safe browser fields.
