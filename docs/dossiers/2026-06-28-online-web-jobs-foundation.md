---
slug: 2026-06-28-online-web-jobs-foundation
status: shipped
current_stage: S6
last-reviewed: 2026-06-28
---

# Dossier: Online Web Jobs Foundation

## 用户原话需求
> 规划执行

## Feature Slice

执行 `docs/plans/2026-06-28-dedao-online-web-product-plan.md` 的第一批可上线切片：为在线 Web 版建立 Job API、持久化任务记录和 Web 任务中心，并先接入已有的书籍导出能力。

## Scope

- 新增 Bearer-protected `/api/jobs` 和 `/api/jobs/{id}`。
- 支持任务类型：
  - `notebooklm_export`
  - `book_export` with `target=health_system_kb_v2`
  - `book_export` with `target=quant_rule_cards`
- Web 右侧增加 `Jobs` 入口，可对当前书籍创建导出任务、刷新任务列表、查看状态和结果。

## Non-Goals

- 不迁移 Dedao 登录。
- 不做电子书下载/抽取 Worker。
- 不暴露服务器文件下载公开链接。
- 不开放匿名调用。

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | 用户要求执行在线 Web 产品规划;第一批切片选择在线化基础 Job 系统 |
| G2 可行性 | PASS | 复用现有 `BookKnowledgeStore`、NotebookLM export 和 book export;不触碰 Dedao cookie 和下载长任务 |
| G3 测试 | PASS | `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`; `CGO_ENABLED=0 go test ./backend/app -run 'TestKBaseHTTPHandlerServesJobs' -count=1`; `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `cd frontend-web && npm run build`; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`; `git diff --check` |
| G4 评审 | PASS | Job mutation remains under Bearer-protected `/api/*`; browser receives only existing session token via Basic Auth-gated `/browser/session-token`; TokenPlan and service secrets stay server-side |
| G5 部署健康 | PASS | Deployed `/tmp/dedao-kbase-web/kbase-server-linux-amd64` to `/opt/dedao-kbase/bin/kbase-server`; synced `frontend-web/dist`; `nginx -t`; `systemctl restart dedao-kbase.service`; service active; `https://kbase.executor.life/health` returned 200 |
| G6 上线验证 | PASS | Browser root without Basic Auth returned 401; unauthenticated `/api/jobs?limit=1` returned 401; Bearer production job check created `notebooklm_export` for book `67929` and returned `status:succeeded`, list status `succeeded` |

## Data Flow

```text
Browser Jobs panel
  -> POST /api/jobs
  -> BookKnowledgeStore jobs.json
  -> async job runner
  -> existing NotebookLM/export functions
  -> GET /api/jobs/{id}
```

## Notes

- Job store 使用 JSON 文件而不是 SQLite，保持 `CGO_ENABLED=0` Linux binary 可部署。
- 第一版任务执行在 kbase-server 进程内 goroutine 中，后续可替换为独立 Worker。
