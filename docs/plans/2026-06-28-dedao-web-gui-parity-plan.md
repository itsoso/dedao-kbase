# Dedao Web GUI Parity Plan

## Goal

Replicate the desktop Wails GUI as a private online Web application while preserving the existing security boundary: Dedao cookies, TokenPlan credentials, file paths, and long-running download/export work stay server-side.

## Current Baseline

The desktop GUI is organized by `frontend/src/router/index.ts` and rendered through `frontend/src/App.vue` with a top menu and `router-view`.

Desktop primary surfaces:

- 首页: home discovery, banners, category modules, user card.
- 课程: course list, course detail, chapter list, article detail, audio/video, downloads.
- 听书书架: listening-book list, detail, manuscript, playback, downloads.
- 电子书架: ebook list, detail, comments, shelf operations, download, download-and-sync-wiki.
- 知识城邦: topic and note browsing.
- 书籍知识库: extracted book packages, search, claims/chunks, NotebookLM, MCP, TokenPlan chat.
- 锦囊: compass list and download entry.
- 设置: local paths, theme, external tool paths.
- 个人中心: login, profile, membership status, account switch.

Current online Web only covers the KBase branch under `frontend-web`, backed by `cmd/kbase-server` and Bearer-protected `/api/*`.

## Target Product Shape

The Web version should become **Dedao Private Web GUI**:

- Same top-level information architecture as desktop.
- Browser-native routing and responsive layout.
- Server-side session/cookie management.
- REST APIs replacing Wails bindings.
- Job system replacing synchronous downloads and local filesystem actions.
- KBase remains the learning center, not the entire application.

## Architecture

```text
Browser Web GUI
  -> Vue Router shell
  -> HTTP client
  -> cmd/kbase-server / future dedao-web server routes
  -> existing backend/services and backend/app modules
  -> job store for downloads/exports
  -> server-side config, cookies, and artifacts
```

Implementation rule: do not reuse generated `frontend/wailsjs` bindings in Web. Every desktop Wails method that Web needs must be exposed through a deliberate HTTP route or Job route.

## API Migration Map

| Desktop Wails method group | Web API shape | Priority |
|---|---|---|
| `GetHomeInitialState`, `SunflowerLabelList`, `SunflowerLabelContent`, `SunflowerResourceList` | `GET /api/dedao/home/*` | P2 |
| `GetQrcode`, `CheckLogin`, `Logout`, `UserInfo` | `GET/POST /api/dedao/auth/*`, `GET /api/dedao/user` | P1 |
| `CourseList`, `CourseInfo`, `ArticleList`, `ArticleDetail` | `GET /api/dedao/courses`, `/courses/{id}`, `/articles/{id}` | P2 |
| `AudioDetail`, `GetVolcPlayAuthToken`, `GetVolcPlayInfo` | `GET /api/dedao/media/*` with server-side proxy rules | P3 |
| `EbookInfo`, `EbookCommentList`, `EbookShelfAdd/Remove` | `GET/POST /api/dedao/ebooks/*` | P2 |
| `CourseDownload`, `OdobDownload`, `EbookDownload`, `EbookDownloadAndSyncWiki` | `POST /api/jobs` with typed job payloads | P3 |
| `TopicAll`, `TopicNoteDetail`, `TopicNotesList` | `GET /api/dedao/knowledge/*` | P3 |
| `OpenDirectoryDialog`, `SetDir` | Admin config status and controlled server config, not browser file dialogs | P4 |
| `BookKnowledge*` | Existing `/api/books`, `/api/search`, `/api/jobs`, `/api/system-kb`, `/api/skills` | P0/P1 |

## Phase Plan

### Phase 1: Web Shell and Desktop Navigation

Upgrade `frontend-web` from a single KBase page to a router-based Web shell. Add desktop-equivalent navigation and placeholder module pages with honest migration state. Keep current KBase workbench mounted as `/book-knowledge`.

Acceptance:

- `/book-knowledge` renders the current learning workbench.
- Desktop primary modules have routable Web entries.
- Unknown routes redirect to `/book-knowledge`.
- No new data APIs or authentication behavior are introduced.

### Phase 2: Auth and User Center

Expose login QR, login polling, logout, user profile, and membership status through server-side HTTP routes.

Acceptance:

- Browser can show login state and user profile without accessing raw cookies.
- Failed/expired login is visible and actionable.
- Existing Basic Auth/Bearer boundary remains intact.

### Phase 3: Read-Only Content Browser

Migrate home, course, odob, ebook, compass, and article browsing. Use dense operational tables and detail panels rather than marketing pages.

Acceptance:

- User can browse purchased/available content online.
- Article/manuscript views render Markdown/HTML safely.
- No downloads run synchronously.

### Phase 4: Job-Based Downloads and Exports

Extend `/api/jobs` for course, odob, ebook, ebook-wiki-sync, PDF, Markdown, MP3, HTML, and EPUB jobs.

Acceptance:

- Every long action has status, logs, errors, retry, and artifact metadata.
- Jobs fail loudly; no silent fallback.

### Phase 5: Media and Artifact Management

Add media playback/proxy and artifact browser with retention controls.

Acceptance:

- Playback works only through authenticated private routes.
- Artifacts can be inspected and downloaded according to server retention policy.

### Phase 6: Admin and Ops

Expose server config state, login state, job queues, disk usage, System KB version, and dependency checks.

Acceptance:

- Operator can diagnose service readiness from Web.
- Browser cannot arbitrarily mutate filesystem paths.

## Security and Compliance

- Browser never receives Dedao cookies.
- Browser never receives TokenPlan API keys.
- Public unauthenticated routes remain limited to `/health` and public skills discovery.
- `/api/*` mutation routes require Bearer token.
- Basic Auth continues to protect the browser page on the online host.
- Downloaded/extracted content remains personal-use only.

## Gate Binding

- G3 local tests: `node frontend-web/scripts/web-kbase-ui-smoke.mjs`, `cd frontend-web && npm run build`, `git diff --check`.
- G3 browser check: local preview with Chrome/Playwright for nav rendering and mobile overflow.
- G4 review: verify no new unauthenticated data APIs and no token/cookie exposure.
- G5 deploy: sync `frontend-web/dist` and verify `https://kbase.executor.life/health`.
- G6 online: verify Basic Auth remains 401 without credentials and deployed assets contain the expected shell/nav.
