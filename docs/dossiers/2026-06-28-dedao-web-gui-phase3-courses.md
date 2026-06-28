---
slug: 2026-06-28-dedao-web-gui-phase3-courses
status: shipped
current_stage: S8
last-reviewed: 2026-06-28
---

# Dossier: Dedao Web GUI Phase 3 Course Browser

## 用户原话需求

> 继续按照规划执行。

## Scope

Ship the second read-only Phase 3 content browser slice:

- `GET /api/dedao/courses`
- `frontend-web/src/views/CourseLibrary.vue`
- `/course` routed to `CourseLibrary`

## Source Of Truth

Desktop source:

- `frontend/src/views/Course.vue`
- `CourseList("bauhinia", "study", page, pageSize)`
- `CourseCategory()` for total count

## Gate Records

| Gate | Status | Evidence |
|---|---|---|
| G1 准入 | PASS | User requested continuing the approved Web GUI parity plan |
| G2 可行性+风险压测 | PASS | `/course` was still a placeholder; implementation reuses existing `CourseList("bauhinia","study")`; scoped out details, articles, playback, and downloads |
| G3 测试 | PASS | Red: `go test ./backend/app -run TestKBaseHTTPHandlerServesDedaoCourses -count=1` failed on missing course API/types; Red: `node frontend-web/scripts/web-kbase-ui-smoke.mjs` failed on missing `CourseLibrary.vue`; Green: both commands passed |
| G4 评审 | PASS | Focused diff only touched course API, course Web route, smoke tests, and docs; side-effect actions stayed out of scope |
| G5 部署健康 | PASS | Installed Linux `kbase-server`, synced `frontend-web/dist`, restarted `dedao-kbase.service`; `systemctl is-active` returned `active`; `/health` returned 200 |
| G6 上线验证 | PASS | Unauthenticated `/api/dedao/courses` returned 401; Bearer `/api/dedao/courses?page=1&page_size=5` returned 200 with 5 courses, total 233, first item had title; deployed assets contain `course-library`; unauthenticated `/course` returned 401 |

## Decision

The Web course route is read-only for now. Side-effect actions (`CourseDownload`) and richer content views (`CourseInfo`, `ArticleList`, `ArticleDetail`, playback) remain future slices.
