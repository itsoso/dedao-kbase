# Book Knowledge Workbench Design

## Goal

Turn `dedao-gui` from a Dedao downloader into a local book knowledge workbench. The first release builds a reusable book knowledge package, displays it in the GUI, and exposes it through a local MCP server so other LLMs can query the book.

## Scope

First release:

- Download one selected ebook through the existing authenticated `dedao-gui` flow.
- Create or refresh a local `book_knowledge` package for that book.
- Show imported books, chapters, extracted claims, chunks, and citations inside `dedao-gui`.
- Provide local MCP tools backed by the same package.

Later releases:

- Open/read the downloaded source book in `dedao-gui`.
- Export a reviewed health package into `health-llm-driven` System KB V2.
- Export quant rule cards and paper-only experiment queues for `macd-analysis-claude`.

## Key Decision

Use a domain-neutral intermediate package instead of coupling `dedao-gui` directly to health or quant systems.

```
book_knowledge/
  manifest.json
  books/<book_id>/
    manifest.json
    chapters.jsonl
    chunks.jsonl
    claims.jsonl
    entities.jsonl
    citations.jsonl
    exports/
      health_system_kb_v2/
      quant_rule_cards/
```

This keeps the base workflow useful for any book. Domain-specific adapters can consume the same package later.

## Data Model

`book manifest` contains:

- `book_id`
- `dedao_id`
- `enid`
- `title`
- `author`
- `source_html`
- `created_at`
- `updated_at`
- `status`

`chapters.jsonl` contains chapter IDs, titles, order, and source references.

`chunks.jsonl` contains searchable text chunks with source chapter and offset.

`claims.jsonl` contains extracted knowledge statements:

- `claim_id`
- `book_id`
- `title`
- `summary`
- `body`
- `chapter_id`
- `evidence_level`
- `confidence`
- `review_status`
- `citations`

`citations.jsonl` maps generated facts back to source chapter/chunk locations. It should not contain large verbatim source spans by default.

## Extraction Strategy

First release uses a deterministic fallback extractor:

1. Parse downloaded HTML.
2. Detect headings as chapters.
3. Split text into chunks.
4. Create conservative draft claims from chapter summaries.

If a local `llms-wikis` command exists, `dedao-gui` can call it as an enhanced extractor. If it does not exist, the deterministic extractor still creates a usable searchable package.

## GUI

Add a `书籍知识库` view:

- left: imported book list
- center: chapter list and claim list
- right: selected claim/chunk detail with citations
- actions: refresh package, start MCP server, copy MCP config, export target

The existing ebook list keeps `下载并入 Wiki`; this action should also refresh the knowledge package.

## MCP

Expose a local stdio MCP server command first. HTTP/SSE can be added later.

Tools:

- `book.list_books`
- `book.search`
- `book.get_chapter`
- `book.get_claim`
- `book.get_context`
- `book.export`

The MCP server is read-only in the first release. It cannot mutate packages, re-run extraction, or write downstream repos.

## Health Export

Health export should produce System KB V2-compatible JSONL files:

- `entities.jsonl`
- `claims.jsonl`
- `pages.jsonl`
- `relations.jsonl`

Default `review_status` must be `draft` or `needs_review`. A later explicit review/import action can call `health-llm-driven` importer or admin endpoints.

Health-specific constraints:

- do not sync personal/private material;
- do not expose long paid-course/book text;
- attach claim boundaries for diagnosis, prescriptions, treatment, and emergency care;
- keep review/reindex in `health-llm-driven`, not hidden inside `dedao-gui`.

## Quant Export

Quant export should produce paper-only artifacts:

- `rule_cards.jsonl`
- `strategy_skeletons/`
- `paper_queue.json`
- `risk_notes.md`

Default status is `draft`. Generated rules must not be wired to live trading. They enter `macd-analysis-claude` through a paper-only queue and must pass that repo's readiness gates.

## Reader

The reader is explicitly lower priority. It should open the already-downloaded HTML/PDF/EPUB and preserve source navigation for citations. It should not block the knowledge package or MCP work.

## Error Handling

- Missing source file: fail visibly.
- Missing `llms-wikis`: use deterministic extractor and record `extractor=fallback`.
- Malformed package: show validation errors in GUI and MCP.
- Export adapter failure: do not mark package imported.
- MCP startup failure: return command and output to the GUI.

## Tests

Start with backend tests for package pathing, JSONL read/write, deterministic extraction, and MCP tool handlers. Then add Vue build verification. Avoid tests that require Dedao network access.
