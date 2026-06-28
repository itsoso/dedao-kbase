---
slug: 2026-06-28-dedao-web-gui-phase3-detail-reading-design
status: approved
last-reviewed: 2026-06-28
---

# Dedao Web GUI Phase 3 Detail And Reading Design

## Goal

Allow users to click a course or ebook in the Web GUI, inspect its detail page, and open readable content without exposing Dedao cookies, read tokens, DRM tokens, or download workflows to the browser.

## Chosen Approach

Use a server-side, read-only HTTP facade over existing desktop app/service functions.

- Course detail uses `CourseInfoByEnid(enid)` plus `ArticleList(enid, "", count, max_id)`.
- Course article reading uses `ArticleDetailByEnid(1, article_enid)` and `ContentsToMarkdown`.
- Ebook detail uses `EbookDetail(enid)` for metadata and catalog.
- Ebook reading uses `EbookReadToken(enid)` and `EbookPages(chapter_id, token, index, count, offset)` server-side, returning decrypted SVG pages for one bounded request.

## Routes And API Shape

- `GET /api/dedao/courses/{enid}`: safe course metadata, lecturer summary, stats, highlights, and initial articles.
- `GET /api/dedao/courses/{enid}/articles?count=30&max_id=0`: safe article list for pagination.
- `GET /api/dedao/articles/{enid}?type=course`: Markdown article body and safe metadata.
- `GET /api/dedao/ebooks/{enid}`: safe ebook metadata and catalog.
- `GET /api/dedao/ebooks/{enid}/chapters/{chapter_id}/pages?index=0&count=8&offset=0`: decrypted SVG pages for one chapter window.

## Web UX

`/course` and `/ebook` remain list pages, but row click opens a detail/reader workspace instead of only selecting the row. Detail pages are dense learning views: metadata at top, article/catalog list at left, reader pane at right. Markdown article output reuses the existing Web markdown renderer. Ebook SVG pages render inside a sandboxed iframe `srcdoc` so scripts cannot execute in the app context.

## Non-Goals

- No downloads, export jobs, shelf add/remove, comments, audio/video playback, or full-book batch reading in this slice.
- No raw Dedao response forwarding.
- No browser-visible Dedao cookies, read tokens, article tokens, DRM tokens, or TokenPlan credentials.

## Error Handling

Every new API stays Bearer-protected and returns explicit JSON errors. The Web UI shows loading/error states and keeps the list page usable if a detail or read request fails.

## Verification

- Backend handler tests with fake content provider for course detail, article Markdown, ebook detail, and ebook chapter pages.
- Frontend smoke checks for new API client methods, route/view files, and key rendering hooks.
- Build and browser DOM checks for `/course` and `/ebook` detail/reader flows.
