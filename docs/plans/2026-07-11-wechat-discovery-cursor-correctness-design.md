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

Use a versioned cursor that separates upstream page position, local item
progress, and the committed newest-article frontier:

```go
type weChatAgentCursor struct {
    UpstreamBegin        int    `json:"upstream_begin"`
    PublicationItemIndex int    `json:"publication_item_index"`
    LastArticleKey       string `json:"last_article_key,omitempty"`
    LastTimestamp        int64  `json:"last_timestamp,omitempty"`
    FrontierArticleKey   string `json:"frontier_article_key,omitempty"`
    FrontierTimestamp    int64  `json:"frontier_timestamp,omitempty"`
    PendingFrontierKey   string `json:"pending_frontier_key,omitempty"`
    PendingFrontierTime  int64  `json:"pending_frontier_timestamp,omitempty"`
}
```

Discovery returns an ordered page plus enough checkpoint metadata to identify
the next upstream page. It does not infer upstream progress from the number of
filtered articles. The adapter processes the page from
`PublicationItemIndex`, stops at `max_items`, and advances the durable cursor
only after each item is successfully accepted into the local outbox. When the
page is fully consumed, it advances `UpstreamBegin` by the upstream publication
count and resets the item index. Initial backfill remembers the newest matching
article as a pending frontier, commits it only after reaching the terminal empty
page, and resets the next cycle to page zero. Later cycles stop when they reach
the committed frontier, so only newer matching articles are processed.

## Compatibility

Decode the legacy `begin` cursor field and version 1 cursor as
`UpstreamBegin`-compatible state. New cursors are written as version 2. Invalid
non-empty cursor JSON is a visible run failure rather than an implicit restart
from the beginning.

## Failure behavior

- A page with zero title matches still advances past the upstream publications.
- A retryable download or outbox failure preserves the cursor immediately
  before the failed item. The runner uploads already accepted outbox items
  before reporting failure, and the failure endpoint atomically stores the
  safe cursor without updating `last_success_at`.
- A permanently invalid article or partial media archive records an item
  failure, retains valid article text when possible, advances to the next item,
  and completes the run as partial.
- A `max_items` boundary preserves the page position and resumes at the next
  unprocessed item.
- An empty upstream page leaves a stable terminal cursor.
- Cursor state never includes account names, titles, credentials, or response
  bodies.

## Verification

Deterministic coverage now includes filtered-empty pages, filtered frontier
selection, multi-item publications, three-run `max_items` continuation, new-only
cycles, download and enqueue failures, partial item failures, runner
upload-before-fail ordering, failure cursor persistence, version 1 and legacy
cursor decode, invalid cursor rejection, and positive batch-size enforcement.

Verified on 2026-07-11 with:

```bash
go test ./backend/app -run 'Test(WeChatDiscovery|WeChatAgent|SourceAgentRunner)' -count=1
go test ./backend/app -count=1
bash scripts/privacy-smoke.sh
git diff --check
```
