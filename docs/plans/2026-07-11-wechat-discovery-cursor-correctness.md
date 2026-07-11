# WeChat Discovery Cursor Correctness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make WeChat discovery and bounded synchronization resume without repeated pages, skipped articles, or cursor advancement past failures.

**Architecture:** Discovery reports upstream publication progress independently from filtered article results. The adapter owns per-item processing progress and persists a two-level cursor only after successful local acceptance, while retaining read compatibility with the legacy `begin` field.

**Tech Stack:** Go 1.21, `encoding/json`, existing WeChat discovery adapter, source Agent runner, SQLite outbox, Go unit tests.

---

### Task 1: Separate Upstream Page Progress From Filtered Results

**Files:**
- Modify: `backend/app/wechat_discovery.go`
- Modify: `backend/app/wechat_discovery_test.go`

**Step 1: Write failing tests**

Add tests proving that one upstream publication containing two articles reports
`PublicationCount=1`, and that a title filter producing zero results still
reports the same upstream publication count and next begin position.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestWeChatDiscovery(ReportsPublicationProgress|AdvancesFilteredEmptyPage)' -count=1
```

Expected: FAIL because discovery currently derives `next.Begin` from the
filtered article count and returns no page metadata.

**Step 3: Implement minimal page metadata**

Introduce:

```go
type WeChatDiscoveryPage struct {
    Articles         []WeChatDiscoveredArticle
    UpstreamBegin    int
    PublicationCount int
}
```

Return page metadata from discovery. Count upstream publications before local
title filtering. Do not advance a durable item cursor in discovery.

**Step 4: Verify GREEN**

Run the focused command, then:

```bash
go test ./backend/app -run 'TestWeChatDiscovery' -count=1
```

**Step 5: Commit**

```bash
git add backend/app/wechat_discovery.go backend/app/wechat_discovery_test.go
git commit -m "fix(wechat): separate discovery page progress"
```

### Task 2: Add Compatible Two-Level Cursor Decoding

**Files:**
- Modify: `backend/app/wechat_agent.go`
- Modify: `backend/app/wechat_agent_test.go`

**Step 1: Write failing tests**

Add tests for:

- legacy `{"begin":10}` decoding to `UpstreamBegin=10`;
- new cursor round-trip;
- invalid non-empty JSON returning a visible error.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestWeChatAgentCursor' -count=1
```

Expected: FAIL because invalid JSON is silently ignored and no two-level cursor
exists.

**Step 3: Implement minimal codec**

Add a private versioned codec. Read legacy `begin`; write only
`upstream_begin`, `publication_item_index`, `last_article_key`, and
`last_timestamp`. Reject negative offsets and malformed JSON.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'TestWeChatAgentCursor' -count=1
bash scripts/privacy-smoke.sh
git diff --check
git add backend/app/wechat_agent.go backend/app/wechat_agent_test.go
git commit -m "fix(wechat): add resumable discovery cursor"
```

### Task 3: Resume Mid-Page At `max_items`

**Files:**
- Modify: `backend/app/wechat_agent.go`
- Modify: `backend/app/wechat_agent_test.go`

**Step 1: Write failing adapter tests**

Use a deterministic fake discovery page containing three articles in one
publication. With `max_items=1`, assert the first run processes only article 1
and returns `PublicationItemIndex=1`; the next run processes article 2 rather
than refetching article 1 or skipping the publication.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestWeChatAgentResumesMidPublication' -count=1
```

**Step 3: Implement processing checkpoint**

Start at `PublicationItemIndex`. Increment it only after `sink.Enqueue`
succeeds. When all articles on the page are consumed, advance
`UpstreamBegin += PublicationCount` and reset the item index.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'TestWeChatAgentResumesMidPublication' -count=1
bash scripts/privacy-smoke.sh
git diff --check
git add backend/app/wechat_agent.go backend/app/wechat_agent_test.go
git commit -m "fix(wechat): resume bounded sync within page"
```

### Task 4: Preserve Cursor Before Failed Item

**Files:**
- Modify: `backend/app/wechat_agent.go`
- Modify: `backend/app/wechat_agent_test.go`

**Step 1: Write failing tests**

Test both article-download failure and outbox-enqueue failure. Assert the
returned/persistable checkpoint remains immediately before the failed article.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestWeChatAgentDoesNotAdvancePastFailure' -count=1
```

**Step 3: Implement failure-safe result**

Return a typed adapter execution error carrying the last safe cursor so the
runner can preserve progress without completing the failed run. Do not advance
the subscription cursor through the normal completion route.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'TestWeChatAgentDoesNotAdvancePastFailure' -count=1
bash scripts/privacy-smoke.sh
git diff --check
git add backend/app/wechat_agent.go backend/app/wechat_agent_test.go backend/app/source_agent_runner.go backend/app/source_agent_runner_test.go
git commit -m "fix(source): preserve safe cursor on item failure"
```

### Task 5: Full Cursor Verification

**Files:**
- Modify: `docs/plans/2026-07-11-wechat-discovery-cursor-correctness-design.md`

**Step 1: Run gates**

```bash
go test ./backend/app -run 'Test(WeChatDiscovery|WeChatAgent|SourceAgentRunner)' -count=1
go test ./backend/app -count=1
bash scripts/privacy-smoke.sh
git diff --check
```

**Step 2: Review invariants**

Confirm filtering does not control upstream progress, `max_items` cannot skip
items, failures cannot advance past an item, new cursors contain no content or
credentials, and legacy cursors remain readable.

**Step 3: Record completion and commit**

Update the design status to implemented with the exact verification commands.

```bash
git add docs/plans/2026-07-11-wechat-discovery-cursor-correctness-design.md
git commit -m "docs(wechat): verify discovery cursor correctness"
```
