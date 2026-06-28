---
slug: 2026-06-28-online-web-jobs-foundation
status: in_progress
current_stage: S5
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
| G3 测试 | 待跑 | 后端 HTTP tests、Web smoke、frontend build、linux build |
| G4 评审 | 待跑 | 新增 Bearer-protected job mutation;需确认不泄露 secret/cookie |
| G5 部署健康 | 待跑 | deploy 后 `/health` 和 job smoke |
| G6 上线验证 | 待跑 | production create/list/get job |

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
