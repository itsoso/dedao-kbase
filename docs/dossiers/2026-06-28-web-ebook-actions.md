---
slug: 2026-06-28-web-ebook-actions
status: shipped
current_stage: S6
last-reviewed: 2026-06-28
---

# Dossier: Web Ebook Actions

## 用户原话需求
> https://kbase.executor.life/ebook 电子书架页面 增加Mac App上的按钮 可以加入书籍知识库 可以下载

## Feature Slice

在在线 Web 版 `/ebook` 书架复刻桌面 Mac App 的电子书动作按钮：按电子书创建后台下载任务，或创建“加入书籍知识库”任务。长任务通过现有 `/api/jobs` 执行，浏览器不接收 Dedao Cookie、阅读 token 或服务器文件路径权限。

## Scope

- `/ebook` 行内增加 HTML/PDF/EPUB 下载格式选择、“加入书籍知识库”和“下载”按钮。
- 右侧当前书上下文显示该书最近 job 状态。
- `BookKnowledgeJob` 新增 `dedao_ebook_download` 和 `dedao_ebook_sync_kbase` 类型。
- 服务端复用 `EBookDownload.DownloadWithResult` 和 ebook wiki sync 管线，并将同步结果写入当前 `BookKnowledgeStore`。

## Non-Goals

- 不开放匿名下载。
- 不把 Dedao Cookie、阅读 token 或下载直链下发到浏览器。
- 不做公开文件下载 URL。
- 不迁移课程、听书、锦囊等其它下载按钮。

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | 用户要求 `/ebook` 增加 Mac App 上的下载和加入知识库按钮，并确认采用 job 化方案 A |
| G2 可行性 | PASS | 现有 `EBookDownload`、`SyncEbookToWiki`、`BookKnowledgeStore` 和 `/api/jobs` 可复用；新增 sync store 注入避免线上写入桌面默认目录 |
| G3 测试 | PASS | `go test ./backend/app -run 'TestBookKnowledgeJob.*DedaoEbook' -count=1`; `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `cd frontend-web && npm run build`; `go test ./... -count=1`; `git diff --check` |
| G4 评审 | PASS | Job API 仍在 Bearer-protected `/api/*` 下；下载和同步只在服务端运行；新增字段只包含 `ebook_id`、`ebook_enid`、`download_type` |
| G5 部署健康 | PASS | Built `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` server and `frontend-web/dist`; installed to `/opt/dedao-kbase/bin/kbase-server` and `/var/www/kbase.executor.life`; `nginx -t`; `systemctl restart dedao-kbase.service`; service active |
| G6 上线验证 | PASS | `https://kbase.executor.life/health` returned 200; unauthenticated `/api/jobs?limit=1` returned 401; Bearer `/api/jobs?limit=1` returned 200; invalid `dedao_ebook_download` payload returned `download_type must be 1, 2, or 3`; deployed JS contains `dedao_ebook_download` |

## Data Flow

```text
Browser /ebook row action
  -> POST /api/jobs
  -> BookKnowledgeStore jobs.json
  -> async kbase-server job runner
  -> EBookDownload.DownloadWithResult or SyncEbookToWikiStore
  -> GET /api/jobs
```

## Notes

- `backend/utils/TestPrintToPdf` is now opt-in via `DEDAO_RUN_CHROMEDP_INTEGRATION=1` because it depends on live Chrome, external Dedao pages, and PDF generation. Default `go test ./...` now stays deterministic for CI-style gates.
