# Book as Agent Platform Design

## Goal

Turn every subscribed book into a stable source page that can progress into a
versioned knowledge package, a callable agent, and an optional standalone web
application. A source page must work before the book is downloaded.

## Architecture

The product uses four explicit layers:

1. **Source book**: `/sources/dedao/ebooks/{source_enid}` is the canonical page
   for title, author, introduction, progress, source identity, and ingestion
   actions. It never assumes a local package exists.
2. **Knowledge package**: `/knowledge/packages/{package_id}` contains normalized
   chapters, chunks, claims, citations, quality state, and release history.
3. **Book agent**: `/agents/books/{package_id}` binds one released package to a
   model policy, prompt set, retrieval scope, and evaluation results.
4. **Book app**: `/apps/books/{package_id}` is the reader-facing software surface
   built on the same agent contract. It can later add workflows specific to the
   book without copying ingestion or retrieval code.

The source identity and package identity are deliberately different. The
source page links forward only when a package or agent exists; otherwise it
offers download and package creation as jobs with visible status.

## Data Flow

`Dedao source -> download job -> normalized package -> quality review -> release
-> book agent -> downstream application/API`

Every transition is idempotent and preserves provenance. The package manifest
records the source identifier, content hash, conversion version, and release
status. Agent responses cite package chunks rather than raw downloaded files.

## Error Handling

Source metadata remains visible when downloading, conversion, or package lookup
fails. Actions report explicit job errors and can be retried. Missing packages
are normal product state, not reader errors.

## Initial Scope

This change repairs the canonical source-book route, adds a useful source detail
page, and exposes the next lifecycle actions. Full agent runtime and generated
book applications remain separate incremental deliveries behind the stable
route and package contracts.

## Verification

Smoke tests assert that source routes are not parsed as local reader IDs, that
the detail route loads by source identity, and that the page exposes package and
agent lifecycle actions. Browser verification covers a subscribed but not yet
downloaded book.
