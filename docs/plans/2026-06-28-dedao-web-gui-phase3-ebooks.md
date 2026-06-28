---
slug: 2026-06-28-dedao-web-gui-phase3-ebooks
status: implemented
last-reviewed: 2026-06-28
---

# Dedao Web GUI Phase 3: Ebook Bookshelf

## Goal

Replace the `/ebook` placeholder with a read-only Web ebook bookshelf backed by the existing desktop data path.

## Scope

- Add Bearer-protected `GET /api/dedao/ebooks`.
- Reuse the desktop service call shape: `CourseList("ebook", "study", page, page_size)`.
- Return only safe browser fields such as `id`, `enid`, `title`, `author`, `intro`, `icon`, `price`, `progress`, and pagination metadata.
- Route `/ebook` to a real Vue page with search, pagination, book selection, and actionable empty/error states.

## Non-Goals

- No ebook detail, comments, download, shelf add/remove, or wiki sync in this slice.
- No Dedao cookies, DRM tokens, TokenPlan keys, or raw service payloads are exposed to the browser.
- No long-running ebook export runs in synchronous HTTP.

## Acceptance

- `/ebook` no longer renders `ModuleLanding`.
- Logged-in browser users can see their purchased ebook list after token hydration.
- API returns `401` without Bearer and a safe JSON page with Bearer.
- If Dedao login/session fails, Web shows the error rather than a blank page.
