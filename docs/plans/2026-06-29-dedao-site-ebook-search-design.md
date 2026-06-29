# Dedao Site Ebook Search Design

## Goal

Support dedao.cn site-wide ebook search from the Web GUI while preserving the existing "my ebook bookshelf" flow.

## Product Behavior

The `/ebook` page keeps "我的书架" as the default source. A compact source switch lets the user choose "全站搜索". In site-search mode, the same search box queries dedao.cn ebook results instead of filtering purchased bookshelf items. Results use the same list layout and can be opened through the existing ebook detail/reader route when an `enid` is available.

Download and "加入书籍知识库" actions remain guarded. If a result is not owned or lacks the stable identifiers required by the job system, the UI should avoid creating a misleading job and show a clear unavailable state.

## API Shape

Add a Bearer-protected endpoint:

```text
GET /api/dedao/search/ebooks?q=<keyword>&page=<n>&page_size=<n>
```

The response reuses `DedaoEbookPage`:

```json
{
  "ebooks": [],
  "page": 1,
  "page_size": 20,
  "total": 0,
  "total_pages": 0,
  "is_more": 0
}
```

This keeps the public Web API stable and avoids mixing bookshelf semantics into `/api/dedao/ebooks`.

## Architecture

Extend `DedaoContentProvider` with `SearchEbooks(query, page, pageSize)`. The live provider delegates to a service-layer dedao.cn search helper and maps the upstream response into the safe `DedaoEbook` DTO. The HTTP handler remains responsible only for auth, pagination parsing, and JSON response writing.

The first implementation should be defensive around upstream shape changes: parse only known safe fields, ignore unknown fields, and return an explicit error when the upstream request fails instead of silently falling back to bookshelf data.

## Testing

Add backend tests for:

- unauthenticated `/api/dedao/search/ebooks` returns `401`;
- authenticated requests pass `q`, `page`, and `page_size` into the provider;
- the response does not expose cookies, tokens, read tokens, or raw upstream payload fields.

Extend the frontend smoke script to assert the new API client method and the UI source switch exist.

## Release Notes

Deploy with the existing kbase-server path: build `frontend-web`, build `cmd/kbase-server`, restart the service, and verify `/health`, protected API auth behavior, and one authenticated search request.
