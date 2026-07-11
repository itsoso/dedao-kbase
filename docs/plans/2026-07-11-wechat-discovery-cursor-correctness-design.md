# WeChat Discovery Cursor Correctness Design

**Status:** Implemented and verified on 2026-07-11

## Problem

The current discovery cursor advances `Begin` by the number of articles that
survive local filtering. That value is not the upstream pagination position:
one upstream publication can contain multiple articles, a title filter can
produce zero articles, and a bounded run can stop in the middle of a
publication. The current behavior can repeat a page forever, skip unprocessed
articles, or advance past a failed item.

## Decision

Use a two-level cursor that separates upstream page position from local item
progress:

```go
type weChatAgentCursor struct {
    UpstreamBegin        int    `json:"upstream_begin"`
    PublicationItemIndex int    `json:"publication_item_index,omitempty"`
    LastArticleKey       string `json:"last_article_key,omitempty"`
    LastTimestamp        int64  `json:"last_timestamp,omitempty"`
}
```

Discovery returns an ordered page plus enough checkpoint metadata to identify
the next upstream page. It does not infer upstream progress from the number of
filtered articles. The adapter processes the page from
`PublicationItemIndex`, stops at `max_items`, and advances the durable cursor
only after each item is successfully accepted into the local outbox. When the
page is fully consumed, it advances `UpstreamBegin` by the upstream publication
count and resets the item index.

## Compatibility

Decode the legacy `begin` cursor field as `UpstreamBegin`. New cursors are
written only in the new shape. Invalid non-empty cursor JSON is a visible run
failure rather than an implicit restart from the beginning.

## Failure behavior

- A page with zero title matches still advances past the upstream publications.
- A download or outbox failure preserves the cursor immediately before the
  failed item. The runner uploads already accepted outbox items before
  reporting failure, and the failure endpoint atomically stores the safe
  cursor without updating `last_success_at`.
- A `max_items` boundary preserves the page position and resumes at the next
  unprocessed item.
- An empty upstream page leaves a stable terminal cursor.
- Cursor state never includes account names, titles, credentials, or response
  bodies.

## Verification

Deterministic coverage now includes filtered-empty pages, multi-item
publications, three-run `max_items` continuation, download and enqueue failures,
runner upload-before-fail ordering, failure cursor persistence, legacy cursor
decode, and invalid cursor rejection.

Verified on 2026-07-11 with:

```bash
go test ./backend/app -run 'Test(WeChatDiscovery|WeChatAgent|SourceAgentRunner)' -count=1
go test ./backend/app -count=1
bash scripts/privacy-smoke.sh
git diff --check
```
