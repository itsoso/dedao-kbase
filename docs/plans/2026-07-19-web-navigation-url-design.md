# Web Navigation And URL Design

**Status:** Approved for implementation

## Goal

Make the Web product predictable: every source, content item, knowledge package,
analysis surface, job, and downstream delivery view must have one canonical URL
and one primary navigation home.

## Problem

The current Web UI grew by feature area. Dedao pages, book knowledge, WC Plus,
TokenPlan analysis, jobs, releases, and health delivery are useful but scattered.
This caused repeated failures where a page rendered, but links did not resolve to
the intended product surface. Recent course fixes showed the root issue: source
detail routes, reading routes, and desktop parity routes shared similar paths
without a route contract.

## Product Navigation

Use seven top-level workspaces:

- **Sources:** Dedao, WeChat, WC Plus, and future source adapters.
- **Ingestion:** download, sync, import, and source-agent runs.
- **Knowledge:** packages, search, claims, chunks, citations, and NotebookLM.
- **Analysis:** TokenPlan workbench over current source document or package.
- **Review:** quality gates, candidates, reverification, and publish decisions.
- **Delivery:** health/proofroom feeds, receipts, gaps, impact, and lineage.
- **Settings:** tokens, local agent, credentials, storage, and model registry.

Legacy navigation entries may remain as aliases during migration, but they must
resolve into this workspace model.

## Canonical URL Contract

All URLs should be REST-like and stable after refresh:

- `/sources/dedao/home`
- `/sources/dedao/courses`
- `/sources/dedao/courses/{courseId}`
- `/sources/dedao/courses/{courseId}/articles/{articleId}`
- `/sources/dedao/courses/detail/{enid}`
- `/sources/dedao/ebooks`
- `/sources/dedao/ebooks/{bookId}`
- `/sources/dedao/audio`
- `/sources/wechat/accounts`
- `/sources/wechat/articles/{sourceId}`
- `/knowledge/packages`
- `/knowledge/packages/{packageId}`
- `/analysis/source/{sourceId}`
- `/analysis/package/{packageId}`
- `/jobs/{jobId}`
- `/delivery/health/releases`
- `/delivery/health/releases/{releaseId}`

Existing URLs such as `/course`, `/ebook`, `/book-knowledge`, and
`/wcplus-source` should keep working as compatibility aliases.

## Routing Rules

Source pages own browsing and acquisition state. Knowledge pages own transformed
packages and citations. Analysis pages can open from any source or package but
must not mutate source identity. Jobs are global and link back to the source or
package they operate on. Delivery pages never read raw source content directly;
they operate on published releases and receipts.

## Error Handling

Every canonical page needs three visible states: loading, loaded, and actionable
failure. Route errors must say whether the identifier is missing, unsupported, or
not found. Compatibility aliases should redirect or render the canonical surface
without silent fallback to unrelated pages.

## Testing

Add route contract smoke tests that assert:

- Source list cards use canonical links.
- Legacy aliases still render the intended canonical surface.
- Detail and reading links do not share ambiguous route parsing.
- Refreshing a deep URL loads the same content state.
- Static resource versions change when routing changes.

## Rollout

Implement in two phases. First add route helpers, canonical links, aliases, and
smoke tests without changing backend contracts. Then migrate navigation labels
and page groups to the seven-workspace model.
