# WeChat Source Adapter Design

## Goal

Build a first-party WeChat public account source adapter that can turn user-authorized WeChat articles into private book-knowledge packages. The adapter should replace the need for wcplus in the core workflow while keeping sensitive login data out of downstream systems.

WC Plus remains useful when the user already runs its local downloader. The repository should treat it as an optional local source backend, not as a hard dependency.

## Scope

The first version supports two paths:

- Single article URL import: fetch a `mp.weixin.qq.com/s/...` article, extract metadata and Markdown, then save it as a kbase package.
- Official-account metadata sync: with explicit `WECHAT_MP_TOKEN` and `WECHAT_MP_COOKIE`, search accounts and list historical articles through the WeChat public-platform endpoints.
- WC Plus local API import: read already-synced official accounts, article lists, article Markdown, search/report results, export operations, and tasks from the local WC Plus API, then import single articles or the latest N articles into book knowledge.
- WC Plus skill workflow: verify the local API environment, batch-search public-account nicknames, create `gzh_article_link` tasks from exact matches, and optionally start the download queue.

Reading counts, likes, comments, and `appmsg_token`-based APIs are intentionally out of scope. They are higher risk and not needed to build useful knowledge packs.

## Architecture

Add a small backend service in `backend/app`:

- `WeChatSourceService` owns HTTP fetching, article parsing, official-account search, and article-list pagination.
- `WeChatSourceConfig` takes token/cookie/base URLs from explicit configuration or environment variables.
- `WeChatArticleToPackage` converts a downloaded article into the existing `BookKnowledgePackage` shape.
- `cmd/kbase-server` exposes the adapter through authenticated `/api/wechat/*` routes.
- `frontend-web` provides the online workbench pages for `/wechat-source` and `/book-knowledge`.
- `WCPlusSourceService` talks to an explicit local WC Plus base URL from `WCPLUS_BASE_URL`, falls back to the documented `WCPLUSPRO_BASE_URL`, and defaults to the documented localhost API only inside server runtime configuration.

The service is deliberately source-adapter shaped. `dedao-gui` remains the workbench and knowledge-pack builder; health and proofroom consume exported evidence packages only.

## Data Flow

```text
article URL
  -> fetch HTML
  -> parse title/account/publish time/content/images
  -> Markdown text
  -> BookKnowledgePackage
  -> private kbase store
  -> downstream authority/evidence export
```

For account sync:

```text
WECHAT_MP_TOKEN + WECHAT_MP_COOKIE
  -> searchbiz(query)
  -> fakeid
  -> appmsg list_ex(fakeid, begin, count)
  -> article URL list
```

For WC Plus:

```text
WCPLUS_BASE_URL or default local WC Plus API
  -> service and /api/gzh/list environment check
  -> /api/gzh/list
  -> /api/report/gzh_articles
  -> /api/article/content
  -> /api/search/search and /api/article/search_title
  -> /api/search_gzh/search exact nickname matching
  -> /api/batch_task/create_task with crawlerType=gzh_article_link
  -> /api/task/control command=run
  -> /api/article/export_text, /api/gzh/export_csv, /api/article/all_articles/export_xlsx
  -> /api/batch_task/* and /api/task/control
  -> BookKnowledgePackage
  -> private kbase store
```

## Security

No token, cookie, or downloaded raw article is written to docs or logs. The server reads credentials only from explicit env vars. Missing credentials must fail visibly with a clear error instead of falling back to local files.

WC Plus integration stores only imported book-knowledge packages. It does not persist WC Plus configuration, browser cookies, or private local app paths.

## Acceptance

- Single article HTML can be converted to Markdown with metadata and image URLs.
- Search/list requests can run against a fake WeChat API server in tests.
- `POST /api/wechat/import` writes a private book-knowledge package.
- All new API routes remain behind the existing Bearer token middleware.
- `/wechat-source` can preview/import a direct article URL, search public accounts, list recent articles, and import a listed article.
- `/book-knowledge` can show imported books, run kbase search, and link each book to the reader page.
- `/wechat-source` can also operate WC Plus local accounts, article lists, single article import, latest-N account import, task listing, task creation, and task control through authenticated `/api/wcplus/*` routes.
- `/wechat-source` exposes WC Plus status, account/title/full-text search, TXT/CSV export triggers, all-library XLSX download, queue start, and batch-task cleanup without embedding the local WC Plus URL in frontend code.
- `/wechat-source` exposes WC Plus environment checks and batch nickname import for the documented skill flow, including exact-match failures and copyable success/failure text.
