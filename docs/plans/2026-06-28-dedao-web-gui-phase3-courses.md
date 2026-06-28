---
slug: 2026-06-28-dedao-web-gui-phase3-courses
status: implemented
last-reviewed: 2026-06-28
---

# Dedao Web GUI Phase 3: Course Browser

## Goal

Replace the `/course` placeholder with a read-only Web course browser backed by the existing desktop course list data path.

## Scope

- Add Bearer-protected `GET /api/dedao/courses`.
- Reuse the desktop service call shape: `CourseList("bauhinia", "study", page, page_size)`.
- Return only safe browser fields such as `id`, `class_id`, `enid`, `title`, `intro`, `author`, `icon`, `price`, `progress`, `publish_num`, `course_num`, and pagination metadata.
- Route `/course` to a real Vue page with search, pagination, course selection, and actionable empty/error states.

## Non-Goals

- No course detail, chapter list, article detail, playback, or download in this slice.
- No Dedao cookies, DRM tokens, TokenPlan keys, raw URLs, or raw service payloads are exposed to the browser.
- No long-running course export runs in synchronous HTTP.

## Acceptance

- `/course` no longer renders `ModuleLanding`.
- Logged-in browser users can see purchased courses after token hydration.
- API returns `401` without Bearer and a safe JSON page with Bearer.
- If Dedao login/session fails, Web shows the error rather than a blank page.
