# Web Ebook Actions Design

## Goal

Bring the Mac app ebook actions to the online `/ebook` bookshelf: users can download a purchased ebook or add it to the book knowledge base from the Web UI.

## Recommended Approach

Use background jobs instead of synchronous HTTP requests.

- `下载` creates a `dedao_ebook_download` job with `ebook_id`, `ebook_enid`, and `download_type`.
- `加入知识库` creates a `dedao_ebook_sync_kbase` job with `ebook_id` and `ebook_enid`.
- The job runner reuses the existing desktop backend paths:
  - `EBookDownload.DownloadWithResult`
  - `SyncEbookToWiki`
- The browser shows job status and does not receive Dedao cookies, article tokens, ebook read tokens, or raw download internals.

## UI

Each ebook row gets compact action buttons:

- `加入知识库`
- `下载`
- a format selector for `HTML / PDF / EPUB`

The right detail panel shows recent job status for the selected ebook so users can see whether the action is queued, running, succeeded, or failed.

## Non-Goals

- No batch downloads.
- No shelf add/remove.
- No direct file streaming to the browser.
- No synchronous long-running `/api/*` request.
