---
slug: 2026-06-28-web-kbase-ui
status: online
current_stage: S8
last-reviewed: 2026-06-28
---

# Dossier: Web KBase UI

## 用户原话需求(逐字)
> 帮我开发一下web版本对应的UI页面

## 四问(feature-plan)
- **Q1 用户价值**:个人使用者可在浏览器里访问私有 kbase,浏览和检索本地书籍知识库,不必启动 Wails 桌面端。
- **Q2 边界(What NOT)**:首版只读;不做 TokenPlan 对话、聊天历史、NotebookLM 导出、写操作或公开匿名访问。
- **Q3 最简实现**:新增独立 `frontend-web/` Vite/Vue app;复用 `cmd/kbase-server` 的现有 HTTP API;给 kbase server 增加静态资源托管。
- **Q4 风险**:涉及 Bearer token 鉴权和私有知识库暴露面;必须保持 `/api/*` 鉴权,错误不可静默吞掉。不涉及新 Wails 绑定。

## ASCII 数据流

```text
浏览器用户
  -> frontend-web Vue app
  -> HTTP fetch with Authorization: Bearer <token>
  -> cmd/kbase-server
  -> backend/app.KBaseHTTPHandler
  -> BookKnowledgeStore / system_kb_export.json
```

## 阶段产出物(链接)
| 阶段 | 产出 | 链接 |
|---|---|---|
| S1 Discovery | 现状图 | 本文件 Discovery 节 |
| S2 PRD | 轻量 PRD | `docs/plans/2026-06-28-web-kbase-ui-design.md` |
| S3 规划 | 实现计划 | `docs/plans/2026-06-28-web-kbase-ui.md` |
| S5 实现 | 分支 | `codex/web-kbase-ui-kbase` |
| S6 构建 | 前端构建与后端 kbase 窄测 | `npm run build`; `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1` |
| S7 验证 | 待浏览器实测 | 用户输入 token 后访问 `http://127.0.0.1:8719/` |

## Gate 裁决记录
| Gate | 裁决 | 依据 / 日期 |
|---|---|---|
| G1 准入 | PASS | 映射到私有 kbase 浏览器 UI;最小端到端切片为连接 token -> 列书 -> 搜索 -> 看详情;2026-06-28 |
| G2 可行性 | PASS | 用户确认采用独立 Web 版 MVP;复用现有 HTTP API,首版只读;2026-06-28 |
| G3 测试 | PARTIAL PASS | `node frontend-web/scripts/web-kbase-ui-smoke.mjs` PASS; `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1` PASS;全量 `go test ./...` 仍红在无关 `backend/utils.TestPrintToPdf` Chrome/chromedp 60s 超时;2026-06-28 |
| G4 评审 | PASS | `TestKBaseHTTPHandlerServesWebAssets` 覆盖静态资源、SPA fallback、`/api/books` 无 token 仍 401;2026-06-28 |
| G5 构建健康 | PASS | `cd frontend-web && npm run build` PASS; `git diff --check` PASS;2026-06-28 |
| G6 验证 | PASS | 已发布到 `https://kbase.executor.life/`;浏览器入口受 Basic Auth 保护,页面资源已替换为 `frontend-web/dist`;2026-06-28 |

## 待拍板决策(STOP 问人)
- 已拍板:选择独立 Web 版书籍知识库页面,首版只读,使用现有 kbase HTTP API。

## Discovery 现状图(带 file:line)
- `backend/app/kbase_http.go:14` 定义 `KBaseHTTPConfig`,当前只有 store、token 和 System KB export path。
- `backend/app/kbase_http.go:38` 的 `ServeHTTP` 当前只处理 `/health` 和 `/api/*`,非 API 路径直接 404。
- `backend/app/kbase_http.go:74` 统一校验 `/api/*` Bearer token;这是静态托管改动必须保护的不变量。
- `cmd/kbase-server/main.go:14` 创建 kbase HTTP server,当前没有 web asset 目录配置。
- `cmd/kbase-server/main.go:21` 把 `KBaseHTTPConfig` 传入 handler,适合新增 `StaticDir`。
- `frontend/src/views/BookKnowledge.vue` 是 Wails 桌面工作台,依赖 Wails bindings,不适合直接作为浏览器 web UI。

## 沉淀(S8)
- README 已更新 Web UI 构建、启动和 token 使用说明。
- 代码结构新增 `frontend-web/` 和 kbase static serving;已新增最小 system-map 入口,后续如要加入计数需走生成文件。
- 完成分支流程当前不能进入 merge/PR 选项:全量 `go test ./...` 被既有 `backend/utils.TestPrintToPdf` chromedp 环境问题阻塞。
- 线上 Nginx 已切换为:浏览器页面和 assets 走 Basic Auth 静态托管,`/browser/session-token` 仅在 Basic Auth 后反代并注入受信代理头,`/health`、`/.well-known/dedao-kbase-skills.json`、`/api/*` 反代到 kbase-server。
- 页面加载后如果 localStorage 没有 token,会通过 `/browser/session-token` 自动填充 `KBASE_AUTH_TOKEN`。
