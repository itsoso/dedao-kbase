---
slug: 2026-06-28-dedao-web-gui-parity
status: definition
current_stage: S3
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
| G3 测试 | pending | Phase 1 must pass Web smoke, frontend build, browser nav check, and `git diff --check` |
| G4 评审 | pending | Phase 1 must preserve Basic Auth/Bearer boundaries and avoid new unauthenticated data APIs |
| G5 部署健康 | pending | Deploy static `frontend-web/dist`, verify `/health` and service active |
| G6 上线验证 | pending | Verify online page includes desktop-parity nav and root remains Basic Auth protected |

## Documents

- Product plan: `docs/plans/2026-06-28-dedao-web-gui-parity-plan.md`
- Phase 1 implementation plan: `docs/plans/2026-06-28-dedao-web-gui-phase1.md`

## Current Decision

Start with Phase 1: upgrade `frontend-web` into a Web shell with Vue Router and desktop-equivalent top-level navigation, keeping the current KBase workbench as `/book-knowledge`.
