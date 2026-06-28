---
slug: 2026-06-28-dedao-web-gui-phase3-detail-reading
status: in_progress
current_stage: S6
last-reviewed: 2026-06-28
---

# Dossier: Dedao Web GUI Phase 3 Detail Reading

## 用户原话需求

> 要支持点击进入课程，或者点击进入电子书，可以阅读，可以看到课程的详情，可以看到电子书的详情，支持这个能力。

## Scope

Ship read-only Web detail and reading flows for the already online course and ebook browsers:

- `/course/{enid}` with course detail, article list, and Markdown article reading.
- `/ebook/{enid}` with ebook detail, catalog, and bounded SVG chapter-page reading.
- Bearer-protected APIs under `/api/dedao/*`.

## Source Of Truth

Desktop source:

- `frontend/src/views/Course.vue`
- `frontend/src/views/Ebook.vue`
- `backend/course.go`: `CourseInfo`, `ArticleList`, `ArticleDetail`
- `backend/app/ebook.go`: `EbookDetail`, read-token-backed page loading

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | User approved doing option A first: click course/ebook, view detail, read content |
| G2 可行性+风险压测 | PASS | Scope is read-only; course articles are converted to Markdown server-side; ebook pages are fetched in bounded batches and rendered in sandboxed iframes; no download, shelf mutation, comments, or playback |
| G3 测试 | PASS | Red: targeted Go test failed on missing new DTOs/routes; Red: Web smoke failed on missing detail reader views; Green: `go test ./backend/app -run 'TestKBaseHTTPHandlerServesDedao(CourseDetail|CourseArticles|ArticleMarkdown|EbookDetail|EbookChapterPages)' -count=1`; `go test ./backend/app -count=1`; `node frontend-web/scripts/web-kbase-ui-smoke.mjs`; `cd frontend-web && npm run build` |
| G4 评审 | PENDING | Focused diff review and final verification still pending |
| G5 部署健康 | PENDING | Awaiting Linux build, deploy, service restart, and health check |
| G6 上线验证 | PENDING | Awaiting online API and browser route verification |

## Security Boundary

The browser receives only safe DTOs. Dedao cookies, DRM tokens, article tokens, raw jump URLs, and ebook read tokens stay server-side. Ebook SVG content is displayed in sandboxed iframe documents and loaded only by explicit chapter/page requests.
