# Book Knowledge Workbench Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the first vertical slice of a local book knowledge workbench in `dedao-gui`: package generation, GUI display, and read-only MCP access.

**Architecture:** The ebook downloader remains the Dedao-facing source acquisition layer. A new backend package manager stores normalized book knowledge packages on disk, and both the GUI and MCP server read the same files. Health and quant integrations are export adapters layered on top of the neutral package format.

**Tech Stack:** Go, Wails v2, Vue 3, Element Plus, JSON/JSONL, local stdio MCP-compatible command surface.

---

### Task 1: Package Store

**Files:**
- Create: `backend/app/book_knowledge.go`
- Create: `backend/app/book_knowledge_test.go`

**Step 1: Write the failing test**

Test that `BookKnowledgeStore` resolves:

- global manifest at `<root>/manifest.json`
- per-book manifest at `<root>/books/<book_id>/manifest.json`
- JSONL files at `<root>/books/<book_id>/{chapters,chunks,claims,citations}.jsonl`

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeStorePaths' -count=1
```

Expected: FAIL because the store does not exist.

**Step 2: Implement minimal store paths and manifest structs**

Add structs for `BookKnowledgeManifest`, `BookKnowledgeBook`, `BookKnowledgeChapter`, `BookKnowledgeChunk`, `BookKnowledgeClaim`, and `BookKnowledgeCitation`.

**Step 3: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeStorePaths' -count=1
```

Expected: PASS.

### Task 2: JSONL Read/Write

**Files:**
- Modify: `backend/app/book_knowledge.go`
- Modify: `backend/app/book_knowledge_test.go`

**Step 1: Write failing tests**

Test that writing and reading a package round-trips:

- book manifest
- two chapters
- two chunks
- one claim
- one citation

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgePackageRoundTrip' -count=1
```

Expected: FAIL because read/write is missing.

**Step 2: Implement JSON and JSONL helpers**

Use `encoding/json` with one JSON object per line. Return line-numbered errors for malformed JSONL.

**Step 3: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgePackageRoundTrip' -count=1
```

Expected: PASS.

### Task 3: Deterministic HTML Extractor

**Files:**
- Create: `backend/app/book_extract.go`
- Create: `backend/app/book_extract_test.go`
- Modify: `backend/app/ebook_wiki.go`

**Step 1: Write failing tests**

Given a small HTML string with two headings and paragraphs, extraction should create:

- two chapters
- chunks attached to chapters
- draft claims derived from chapter summaries
- citations pointing to chunk IDs

Run:

```bash
go test ./backend/app -run 'TestExtractBookKnowledgeFromHTML' -count=1
```

Expected: FAIL because extractor is missing.

**Step 2: Implement fallback extraction**

Use `golang.org/x/net/html` or conservative regex/tokenization if the existing dependency surface is simpler. Strip scripts/styles, preserve heading text, split text into bounded chunks, and create conservative draft claims.

**Step 3: Run test to verify it passes**

Run:

```bash
go test ./backend/app -run 'TestExtractBookKnowledgeFromHTML' -count=1
```

Expected: PASS.

### Task 4: Integrate With Ebook Action

**Files:**
- Modify: `backend/app/ebook_wiki.go`
- Modify: `backend/download.go`
- Modify: `frontend/wailsjs/go/backend/App.d.ts`
- Modify: `frontend/wailsjs/go/backend/App.js`

**Step 1: Write failing backend test**

Test that `BuildBookKnowledgeFromHTML` writes a package and returns package metadata without calling Dedao.

Run:

```bash
go test ./backend/app -run 'TestBuildBookKnowledgeFromHTML' -count=1
```

Expected: FAIL.

**Step 2: Implement package generation after HTML download**

After existing HTML download, build or refresh the knowledge package. Keep the old `llms-wikis` command path optional; fallback extractor must work without it.

**Step 3: Run focused tests**

Run:

```bash
go test ./backend/app -count=1
```

Expected: PASS.

### Task 5: GUI Knowledge View

**Files:**
- Create: `frontend/src/views/BookKnowledge.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/Menu.vue` or the existing menu data source if needed
- Modify: `frontend/wailsjs/go/backend/App.d.ts`
- Modify: `frontend/wailsjs/go/backend/App.js`
- Modify: `backend/download.go`

**Step 1: Expose read APIs**

Add Wails methods:

- `BookKnowledgeListBooks()`
- `BookKnowledgeGetBook(bookID string)`
- `BookKnowledgeSearch(query string, bookID string, limit int)`

**Step 2: Build the view**

Show book list, chapters, claims, and selected claim detail. Keep it dense and functional; no landing page.

**Step 3: Verify frontend**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

### Task 6: Read-Only MCP Server

**Files:**
- Create: `backend/app/book_mcp.go`
- Create: `backend/app/book_mcp_test.go`
- Optionally create: `cmd/book-mcp/main.go` if a separate binary is cleaner

**Step 1: Write failing handler tests**

Test JSON-RPC-like tool calls for:

- `book.list_books`
- `book.search`
- `book.get_claim`
- `book.get_chapter`
- `book.get_context`

Run:

```bash
go test ./backend/app -run 'TestBookMCP' -count=1
```

Expected: FAIL.

**Step 2: Implement read-only MCP handlers**

Keep handlers independent of Wails runtime. They should only read the package store.

**Step 3: Expose launch/config from GUI**

Add Wails methods to get the MCP command/config and optionally start/stop a local stdio process if practical.

### Task 7: Export Adapter Skeletons

**Files:**
- Create: `backend/app/book_export.go`
- Create: `backend/app/book_export_test.go`

**Step 1: Write failing tests**

Test that health export writes draft System KB V2-compatible JSONL and quant export writes draft rule cards.

Run:

```bash
go test ./backend/app -run 'TestBookKnowledgeExport' -count=1
```

Expected: FAIL.

**Step 2: Implement adapter skeletons**

Do not import into downstream repos yet. Write local export artifacts under each book's `exports/` directory.

### Task 8: Verification

Run:

```bash
go test ./backend/app -count=1
go test ./backend ./backend/app ./backend/services -count=1
cd frontend && npm run build
```

Expected:

- backend/app passes;
- focused backend packages pass;
- frontend build passes.

Known pre-existing caveat:

```bash
go test ./backend/... -count=1
```

may still fail at `backend/utils/TestPrintToPdf` with chromedp websocket timeout. Record it if unchanged.
