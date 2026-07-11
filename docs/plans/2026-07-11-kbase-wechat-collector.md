# KBase WeChat Collector Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace WC Plus as the primary WeChat acquisition dependency with a first-party, Keychain-backed local Agent that discovers, downloads, and incrementally imports public-account articles into online KBase.

**Architecture:** Generalize the existing source Agent and control-plane health model, then add a local MP-session adapter with explicit QR enrollment. Reuse source subscriptions, leases, SQLite outbox, idempotent article ingestion, bounded chunks, citations, recovery, and Web deep links. Keep MP credentials local and treat session-backed management endpoints as an unstable, fail-closed adapter.

**Tech Stack:** Go 1.x, `net/http`, `net/http/cookiejar`, SQLite, macOS Keychain through `/usr/bin/security`, Vue-free `frontend-web` JavaScript/CSS, existing KBase HTTP server and source-sync state machine.

---

## Delivery Rules

- Work only in the dedicated `codex/wechat-collector` worktree.
- Follow RED -> GREEN -> REFACTOR for every task; never add production behavior before its failing test.
- Preserve `cmd/wcplus-agent` and legacy heartbeat fields until migration tests prove compatibility.
- Never log or persist MP cookies, tokens, QR payloads, article bodies, or local paths in committed fixtures.
- Use only sanitized fake upstream responses in tests.
- Run `bash scripts/privacy-smoke.sh` and `git diff --check` before every commit.
- Do not enable scheduling in production until the bounded manual G6 probe passes.

### Task 1: Harden Direct Article URL Fetching

**Files:**
- Modify: `backend/app/wechat_source.go`
- Modify: `backend/app/wechat_source_test.go`

**Step 1: Write the failing URL-policy tests**

Add:

```go
func TestWeChatSourceRejectsNonWeChatArticleURLs(t *testing.T)
func TestWeChatSourceRevalidatesEveryRedirect(t *testing.T)
func TestWeChatSourceRejectsPrivateResolvedAddresses(t *testing.T)
func TestWeChatSourceBoundsArticleResponseBytes(t *testing.T)
```

Inject an allowlist and resolver in tests so `httptest.Server` remains usable.
Assert default production policy accepts only HTTPS `mp.weixin.qq.com`, rejects
userinfo and nonstandard ports, rejects private/loopback resolutions, and
rejects redirects away from the allowlist.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestWeChatSource(Rejects|Revalidates|Bounds)' -count=1
```

Expected: FAIL because no URL policy or response limit exists.

**Step 3: Implement the minimal policy**

Add testable config fields:

```go
type WeChatSourceConfig struct {
    // existing fields
    ArticleHosts     []string
    ResolveHost      func(context.Context, string) ([]net.IP, error)
    MaxArticleBytes  int64
}
```

Implement `validateWeChatArticleURL`, a redirect validator, and a limited body
reader. Revalidate DNS immediately before each request and every redirect.
Default to `mp.weixin.qq.com` and a 4 MiB HTML limit.

**Step 4: Verify GREEN**

Run the focused command from Step 2, then:

```bash
go test ./backend/app -run 'TestWeChatSource' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wechat_source.go backend/app/wechat_source_test.go
git commit -m "fix(wechat): harden article fetch boundaries"
```

### Task 2: Generalize Source Capability Health

**Files:**
- Modify: `backend/app/source_sync.go`
- Modify: `backend/app/source_sync_test.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/wcplus-control-plane-smoke.mjs`

**Step 1: Write failing migration and API tests**

Add tests proving an Agent can heartbeat this shape while old WC Plus fields
remain readable:

```go
type SourceCapabilityHealth struct {
    Healthy        bool   `json:"healthy"`
    Version        string `json:"version,omitempty"`
    LastError      string `json:"last_error,omitempty"`
    RequiresAction string `json:"requires_action,omitempty"`
}
```

Expected heartbeat excerpt:

```json
{
  "capability_health": {
    "wechat_mp": {"healthy": false, "requires_action": "login"},
    "wcplus": {"healthy": false}
  }
}
```

Test migration from a database without `capability_health_json`, database
round-trip, diagnostic truncation, and `/api/source-agents` serialization.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgent.*Capability|TestKBaseHTTPHandler.*Capability' -count=1
```

Expected: FAIL because the field and column do not exist.

**Step 3: Implement schema compatibility**

Add `CapabilityHealth map[string]SourceCapabilityHealth` to heartbeat and Agent
models. Add `capability_health_json TEXT NOT NULL DEFAULT '{}'` through the
existing additive migration helper. Normalize keys, cap values, and map legacy
WC Plus fields into `capability_health.wcplus` when the map is absent.

Update the Web renderer to prefer the map and fall back to legacy fields.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'TestSourceAgent|TestSourceSync|TestKBaseHTTPHandler' -count=1
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/source_sync.go backend/app/source_sync_test.go backend/app/kbase_http_test.go frontend-web/app.js frontend-web/scripts/wcplus-control-plane-smoke.mjs
git commit -m "feat(source): add typed capability health"
```

### Task 3: Extract a Generic Source Agent Runner

**Files:**
- Create: `backend/app/source_adapter.go`
- Create: `backend/app/source_agent_runner.go`
- Create: `backend/app/source_agent_runner_test.go`
- Modify: `backend/app/source_agent_outbox.go`
- Modify: `backend/app/source_agent_outbox_test.go`
- Modify: `backend/app/wcplus_agent.go`
- Modify: `backend/app/wcplus_agent_test.go`
- Modify: `cmd/wcplus-agent/main.go`
- Modify: `cmd/wcplus-agent/main_test.go`

**Step 1: Write the failing runner contract tests**

Define the intended seam in tests:

```go
type SourceAdapter interface {
    Name() string
    Operations() []string
    Status(context.Context) SourceCapabilityHealth
    Execute(context.Context, SourceSyncRun, SourceEnvelopeSink) (SourceAdapterResult, error)
}
```

Test heartbeat-before-lease, unhealthy no-lease, operation dispatch, enqueue
before upload, retryable upload retention, terminal item failure, cursor
completion, and lease recovery. Add a fake adapter; do not use WC Plus fixtures.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAgentRunner' -count=1
```

Expected: FAIL because the runner does not exist.

**Step 3: Implement the minimal runner**

Move transport-neutral lifecycle logic from `WCPlusAgent.RunOnce` into
`SourceAgentRunner`. Keep adapter-specific crawling in `WCPlusAgent` through a
small compatibility adapter. Rename the preferred state setting to
`SOURCE_AGENT_STATE_DIR`, while accepting `WCPLUS_AGENT_STATE_DIR` in the
legacy CLI.

**Step 4: Verify GREEN and compatibility**

```bash
go test ./backend/app -run 'Test(SourceAgentRunner|WCPlusAgent|SourceAgentOutbox)' -count=1
go test ./cmd/wcplus-agent -count=1
```

Expected: PASS with unchanged WC Plus behavior.

**Step 5: Commit**

```bash
git add backend/app/source_adapter.go backend/app/source_agent_runner.go backend/app/source_agent_runner_test.go backend/app/source_agent_outbox.go backend/app/source_agent_outbox_test.go backend/app/wcplus_agent.go backend/app/wcplus_agent_test.go cmd/wcplus-agent/main.go cmd/wcplus-agent/main_test.go
git commit -m "refactor(source): extract generic agent runner"
```

### Task 4: Add a Keychain-Backed Secret Store

**Files:**
- Create: `backend/app/source_secret_store.go`
- Create: `backend/app/source_secret_store_test.go`
- Create: `cmd/source-agent/keychain_store_darwin.go`
- Create: `cmd/source-agent/keychain_store_darwin_test.go`
- Create: `cmd/source-agent/keychain_store_other.go`

**Step 1: Write failing secret-store tests**

Specify:

```go
type SourceSecretStore interface {
    Load(context.Context, string) ([]byte, error)
    Save(context.Context, string, []byte) error
    Delete(context.Context, string) error
}
```

Use a fake command runner to assert `/usr/bin/security` receives arguments
without placing secret bytes in logs or error text. Test update, not-found,
delete, invalid key, and non-macOS fail-closed behavior.

**Step 2: Verify RED**

```bash
go test ./backend/app ./cmd/source-agent -run 'Test.*Secret|TestKeychain' -count=1
```

Expected: FAIL because the package and store do not exist.

**Step 3: Implement Keychain storage**

Use service `life.executor.kbase.source-agent` and the Agent ID plus logical key
as the Keychain account. Pass secret data through command stdin where supported;
never interpolate it into an error. Keep an in-memory implementation for unit
tests only.

**Step 4: Verify GREEN**

```bash
go test ./backend/app ./cmd/source-agent -run 'Test.*Secret|TestKeychain' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/source_secret_store.go backend/app/source_secret_store_test.go cmd/source-agent/keychain_store_darwin.go cmd/source-agent/keychain_store_darwin_test.go cmd/source-agent/keychain_store_other.go
git commit -m "feat(source): store local sessions in keychain"
```

### Task 5: Implement Explicit MP QR Enrollment

**Files:**
- Create: `backend/app/wechat_mp_session.go`
- Create: `backend/app/wechat_mp_session_test.go`
- Create: `cmd/source-agent/enrollment.go`
- Create: `cmd/source-agent/enrollment_test.go`

**Step 1: Write failing protocol tests**

Use one fake MP server to cover:

- start-login session creation
- QR image retrieval and UUID cookie retention
- pending, scanned, expired, and confirmed polling states
- login completion, redirect token parsing, and `Set-Cookie` capture
- upstream nonzero error and malformed redirect rejection
- saving only after successful confirmation

Assert no response or error contains the token or cookie.

**Step 2: Verify RED**

```bash
go test ./backend/app ./cmd/source-agent -run 'TestWeChatMPLogin|TestEnrollment' -count=1
```

Expected: FAIL because the MP session client and enrollment server are absent.

**Step 3: Implement the session client**

Use `net/http/cookiejar` and explicit request structs. Persist a minimal JSON
session containing token, cookies, account display metadata, and observed
expiry through `SourceSecretStore`. Do not copy upstream debug logging.

Bind enrollment to `127.0.0.1`; require a random startup CSRF value for mutating
requests and allow only the loopback Origin. Expose:

```text
POST /api/local/wechat/login/start
GET  /api/local/wechat/login/qr
GET  /api/local/wechat/login/status
POST /api/local/wechat/logout
```

**Step 4: Verify GREEN**

```bash
go test ./backend/app ./cmd/source-agent -run 'TestWeChatMPLogin|TestEnrollment' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wechat_mp_session.go backend/app/wechat_mp_session_test.go cmd/source-agent/enrollment.go cmd/source-agent/enrollment_test.go
git commit -m "feat(wechat): add local qr enrollment"
```

### Task 6: Add Session-Backed Account Discovery

**Files:**
- Modify: `backend/app/wechat_source.go`
- Modify: `backend/app/wechat_source_test.go`
- Create: `backend/app/wechat_discovery.go`
- Create: `backend/app/wechat_discovery_test.go`

**Step 1: Write failing discovery tests**

Cover account search, `appmsgpublish` history pagination, title filtering,
multiple items per publication, deterministic article keys, bounded page size,
empty final page, nonzero upstream error, login expiry, throttling, and
verification challenge responses.

Add a cursor fixture:

```go
type WeChatDiscoveryCursor struct {
    Begin          int    `json:"begin"`
    LastArticleKey string `json:"last_article_key,omitempty"`
    LastTimestamp  int64  `json:"last_timestamp,omitempty"`
}
```

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestWeChatDiscovery' -count=1
```

Expected: FAIL because session-backed discovery is absent.

**Step 3: Implement discovery**

Refactor static token/cookie use behind a `WeChatMPSessionProvider`. Keep env
credentials only as an explicit development compatibility provider. Normalize
the upstream response into existing `WeChatOfficialAccount` and
`WeChatOfficialArticle` models plus stable keys.

Return typed errors for login required, throttle, verification, and malformed
contracts so the Agent can choose retryable versus operator action.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'Test(WeChatDiscovery|WeChatSourceSearchAndList)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wechat_source.go backend/app/wechat_source_test.go backend/app/wechat_discovery.go backend/app/wechat_discovery_test.go
git commit -m "feat(wechat): discover account article history"
```

### Task 7: Add Private Source Asset Storage

**Files:**
- Create: `backend/app/source_asset.go`
- Create: `backend/app/source_asset_test.go`
- Modify: `backend/app/source_agent_client.go`
- Modify: `backend/app/source_agent_client_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing asset tests**

Test hash-addressed storage, duplicate upload, MIME sniffing, 8 MiB item limit,
invalid hash, unsupported type, path traversal, atomic write, private file
permissions, authenticated upload, authenticated read, and cross-run lease
ownership.

Use an intended model:

```go
type SourceAssetEnvelope struct {
    SourceItemKey string `json:"source_item_key"`
    SourceURL     string `json:"source_url"`
    SHA256        string `json:"sha256"`
    ContentType   string `json:"content_type"`
    Data          []byte `json:"-"`
}
```

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestSourceAsset|TestKBaseHTTPHandler.*Asset|TestSourceAgentClient.*Asset' -count=1
```

Expected: FAIL because asset storage and routes do not exist.

**Step 3: Implement storage and routes**

Store assets under the configured private KBase root by SHA-256. Add bounded
binary routes:

```text
POST /api/source-agent/runs/{run}/assets
GET  /api/source-assets/{sha256}
```

The upload route uses the dedicated Agent token and active lease. The read route
uses normal KBase authentication. Return a stable asset reference without
making files public.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'TestSourceAsset|TestKBaseHTTPHandler.*Asset|TestSourceAgentClient.*Asset' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/source_asset.go backend/app/source_asset_test.go backend/app/source_agent_client.go backend/app/source_agent_client_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(source): archive private article assets"
```

### Task 8: Preserve Article Structure and Download Media

**Files:**
- Modify: `backend/app/wechat_source.go`
- Modify: `backend/app/wechat_source_test.go`
- Create: `backend/app/wechat_media.go`
- Create: `backend/app/wechat_media_test.go`

**Step 1: Write failing normalization tests**

Test DOM-order preservation across headings, paragraphs, nested lists, quotes,
links, and interleaved images. Test `data-src`, `src`, lazy-load aliases,
duplicate images, invalid schemes, redirects, host policy, MIME sniffing, byte
limits, and partial media failure.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestWeChat(SourcePreserves|Media)' -count=1
```

Expected: FAIL because current Markdown appends all images after all text and
does not archive media.

**Step 3: Implement ordered rendering and media manifests**

Replace selector-by-selector concatenation with a bounded DOM walker. Return a
media manifest alongside article content. Download media through the same
redirect/DNS policy, compute SHA-256, and deduplicate before upload. Keep the
original image URL in metadata and Markdown until the authenticated reader
resolves the private asset reference.

**Step 4: Verify GREEN**

```bash
go test ./backend/app -run 'TestWeChat' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wechat_source.go backend/app/wechat_source_test.go backend/app/wechat_media.go backend/app/wechat_media_test.go
git commit -m "feat(wechat): preserve article media and structure"
```

### Task 9: Execute WeChat Backfills and Incremental Syncs

**Files:**
- Create: `backend/app/wechat_agent.go`
- Create: `backend/app/wechat_agent_test.go`
- Create: `cmd/source-agent/main.go`
- Create: `cmd/source-agent/main_test.go`
- Modify: `backend/app/source_ingest.go`
- Modify: `backend/app/source_ingest_test.go`

**Step 1: Write the failing end-to-end Agent tests**

Use fake MP, article, media, and KBase servers. Queue each operation and assert:

- `discover_articles` advances a monotonic cursor without upload
- `sync_articles` backfills a bounded page and uploads new articles
- replay produces skips
- changed content produces updates
- `sync_media` retries only missing assets
- login expiry reports `requires_action=login` and preserves the cursor
- throttle retains outbox data and fails retryably
- one malformed article yields partial, not false success
- restart replays pending article and asset work exactly once

**Step 2: Verify RED**

```bash
go test ./backend/app ./cmd/source-agent -run 'TestWeChatAgent|TestSourceAgentCLI' -count=1
```

Expected: FAIL because the adapter and CLI do not exist.

**Step 3: Implement the adapter and CLI**

Implement `WeChatSourceAdapter` on `SourceAdapter`. Use source type
`wechat_mp_article`, stable source item keys, content-derived idempotency keys,
and subscription options capped server-side:

```json
{
  "page_size": 10,
  "max_items": 100,
  "include_media": true,
  "title_query": ""
}
```

Add `doctor`, `once`, and `run` commands plus `enroll`. The CLI reads Agent
transport settings from environment but loads MP credentials only from
Keychain.

**Step 4: Verify GREEN**

```bash
go test ./backend/app ./cmd/source-agent -run 'TestWeChatAgent|TestSourceAgentCLI|TestSourceIngest' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/wechat_agent.go backend/app/wechat_agent_test.go cmd/source-agent/main.go cmd/source-agent/main_test.go backend/app/source_ingest.go backend/app/source_ingest_test.go
git commit -m "feat(wechat): add first-party collection agent"
```

### Task 10: Build the WeChat Collector Control Plane

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/scripts/wechat-source-ui-smoke.mjs`
- Modify: `frontend-web/scripts/wcplus-control-plane-smoke.mjs`
- Create: `frontend-web/scripts/wechat-collector-control-plane-smoke.mjs`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing HTTP and UI smoke checks**

Require:

- first viewport shows Agent/login state and account/subscription actions
- two-column layout with a wide article/run workspace
- source type `wechat_mp_article`
- operations `discover_articles`, `sync_articles`, `sync_media`
- run filters, retry, cancel, imported knowledge links, and REST deep links
- WC Plus controls only inside a collapsed legacy section
- no token, cookie, QR payload, or localhost WC Plus URL in browser code

Add authenticated account discovery proxy routes only if they dispatch through
the local Agent control plane; the online server must never call MP directly.

**Step 2: Verify RED**

```bash
node frontend-web/scripts/wechat-collector-control-plane-smoke.mjs
go test ./backend/app -run 'TestKBaseHTTPHandler.*WeChatCollector' -count=1
```

Expected: FAIL because the collector UI and routes do not exist.

**Step 3: Implement the two-column workbench**

Reuse the current source-control API and REST routing. Replace the WC Plus-first
visual hierarchy with source health, login action guidance, account search,
subscriptions, article results, and run detail. Keep navigation dense and
work-focused; avoid nested cards and marketing copy.

**Step 4: Verify GREEN**

```bash
node frontend-web/scripts/wechat-source-ui-smoke.mjs
node frontend-web/scripts/wechat-collector-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node --check frontend-web/app.js
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html frontend-web/scripts/wechat-source-ui-smoke.mjs frontend-web/scripts/wcplus-control-plane-smoke.mjs frontend-web/scripts/wechat-collector-control-plane-smoke.mjs backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(web): add wechat collector control plane"
```

### Task 11: Package the First-Party macOS Agent

**Files:**
- Create: `scripts/build-source-agent-macos.sh`
- Create: `scripts/install-source-agent-macos.sh`
- Create: `scripts/uninstall-source-agent-macos.sh`
- Create: `scripts/source-agent-packaging-smoke.sh`
- Modify: `README.md`
- Modify: `.gitignore`

**Step 1: Write failing packaging smoke checks**

Require a native arm64 binary, optional codesigning through
`CODESIGN_IDENTITY`, a `0600` LaunchAgent plist, `0700` state directory,
Keychain-only MP secrets, loopback enrollment address, separate Agent token,
and uninstall that preserves outbox and imported data by default.

**Step 2: Verify RED**

```bash
bash scripts/source-agent-packaging-smoke.sh
```

Expected: FAIL because the scripts do not exist.

**Step 3: Implement build/install/uninstall**

Build from local source so no downloaded third-party executable is required.
Write only non-secret settings to the plist. Refuse a shared admin/Agent token.
Do not start until Keychain and remote-auth preflight pass. Support explicit
`--purge-state` only on uninstall.

**Step 4: Verify GREEN**

```bash
bash -n scripts/build-source-agent-macos.sh
bash -n scripts/install-source-agent-macos.sh
bash -n scripts/uninstall-source-agent-macos.sh
bash scripts/source-agent-packaging-smoke.sh
```

Expected: PASS.

**Step 5: Commit**

```bash
git add scripts/build-source-agent-macos.sh scripts/install-source-agent-macos.sh scripts/uninstall-source-agent-macos.sh scripts/source-agent-packaging-smoke.sh README.md .gitignore
git commit -m "build(wechat): package first-party source agent"
```

### Task 12: Full Verification, Dossier, Deployment, and G6 Probe

**Files:**
- Create: `docs/dossiers/2026-07-11-kbase-wechat-collector.md`
- Modify: `docs/plans/2026-07-11-kbase-wechat-collector-design.md`
- Modify: `README.md`

**Step 1: Run the full release gate**

Run without piping through plain `tail`:

```bash
go test ./... -count=1
go vet ./...
go test -race ./backend/app ./cmd/kbase-server ./cmd/source-agent -count=1
node frontend-web/scripts/wechat-source-ui-smoke.mjs
node frontend-web/scripts/wechat-collector-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node --check frontend-web/app.js
bash scripts/source-agent-packaging-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS.

**Step 2: Review security and migration boundaries**

Review the final diff for SSRF, redirect revalidation, secret leakage, Keychain
argument handling, CSRF/Origin enforcement, auth separation, payload limits,
lease ownership, cursor monotonicity, idempotency, asset path safety, and WC Plus
backward compatibility. Fix findings with failing regression tests first.

**Step 3: Write the dossier**

Record G1-G5 evidence, artifact hashes, rollback, privacy checks, and the exact
bounded production probe. Do not include account names, article titles, QR
data, cookies, tokens, raw content, or machine-specific paths.

**Step 4: Deploy with rollback artifacts**

Deploy the server/static assets from a clean `dedao-kbase/main`, then install the
locally built Agent. Verify public health, authenticated control plane,
dedicated Agent auth, heartbeat capability state, and no scheduled work.

**Step 5: Execute bounded G6 validation**

After explicit local QR enrollment:

1. search one operator-selected account
2. create a manual subscription with `max_items=1`
3. run `discover_articles`
4. run `sync_articles`
5. verify one REST knowledge detail and its citations
6. replay and verify `skipped=1`
7. restart the Agent and verify outbox/cursor recovery
8. leave scheduling disabled unless every check passes

Any login, throttle, verification, media, auth, or ingestion failure keeps G6
blocked and returns to the appropriate upstream task.

**Step 6: Commit the verified record**

```bash
git add docs/dossiers/2026-07-11-kbase-wechat-collector.md docs/plans/2026-07-11-kbase-wechat-collector-design.md README.md
git commit -m "docs(wechat): record collector rollout"
```

## Execution Order

Tasks are intentionally sequential. Tasks 1-3 establish shared safety and
runtime seams; Tasks 4-6 establish local authorization and discovery; Tasks
7-9 complete durable ingestion; Tasks 10-11 provide product and operations;
Task 12 is the release gate. Do not parallelize tasks that modify
`source_sync.go`, `source_agent_client.go`, `kbase_http.go`, or
`frontend-web/app.js`.

