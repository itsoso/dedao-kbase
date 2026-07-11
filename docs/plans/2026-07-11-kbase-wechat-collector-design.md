# KBase WeChat Collector Design

**Status:** Approved

**Decision:** Build a first-party, local-first WeChat public-account collector
on the existing source-agent and KBase control-plane architecture. WC Plus
becomes an optional legacy importer rather than a runtime dependency.

## Context

The repository already downloads individual public article URLs, parses WeChat
HTML into Markdown, searches public accounts with an explicitly configured MP
session, lists account articles, imports them into book knowledge, and exposes a
Web workbench. The production source-sync store also provides subscriptions,
leases, run history, idempotent article ingestion, bounded Markdown chunks, a
durable local outbox, and retry/recovery behavior.

The remaining gap is acquisition ownership. Search and history currently depend
on `WECHAT_MP_TOKEN` and `WECHAT_MP_COOKIE` in the online server, while durable
collection depends on an external WC Plus binary. The replacement must keep the
MP session on the operator's Mac and use outbound-only communication with KBase.

## Goals

- Search public accounts and enumerate the history available to an explicitly
  authorized MP session.
- Backfill an account, then synchronize only new or changed articles.
- Import a direct `mp.weixin.qq.com` article URL without an MP session.
- Archive normalized article text, cover images, and inline images privately.
- Preserve source URLs, timestamps, account identity, content hashes, and
  chunk-level citations for downstream Health and Proofroom consumers.
- Keep credentials, QR-login cookies, and local acquisition state off the
  online server.
- Surface login expiry, throttling, malformed articles, partial batches, and
  retries as durable control-plane state.

## Non-Goals

- HTTPS interception, CA installation, packet capture, or desktop WeChat
  process injection.
- Captcha bypass, proxy pools, account pools, rate-limit evasion, or unattended
  reauthentication.
- Reimplementing WC Plus licensing, analytics, prompts, payment, or proprietary
  implementation details.
- Reading-count, like, comment, or share-count collection in the first release.
- Public redistribution of downloaded copyrighted content.

## Options Considered

### Embed an existing exporter

`wechat-article-exporter` is MIT licensed and demonstrates QR login, account
search, article-list pagination, and several export formats. Embedding its
TypeScript server would add a second runtime, duplicate secret storage, and
bypass the repository's existing Go state machine. Rejected as the primary
architecture; retain it as a protocol and fixture reference, with attribution
for any copied MIT-licensed code.

### First-party Go collector agent

Generalize the existing source Agent, add a local MP-session adapter, and reuse
KBase subscriptions, leases, ingestion, outbox, and Web control plane. This
keeps one operational model and the smallest trusted surface. Selected.

### WC Plus-style interception proxy

This most closely mirrors WC Plus, but requires a trusted CA and interception of
private client traffic. It conflicts with the host-security failure observed in
production and creates an unnecessary credential boundary. Rejected.

## Architecture

```text
Local loopback enrollment UI
  -> QR login and session status
  -> macOS Keychain SecretStore

WeChat source adapter
  -> direct public article fetch
  -> session-backed account search and history discovery
  -> conservative pagination and checkpoints
  -> HTML/metadata/media normalization
  -> SQLite frontier and durable outbox

source-agent
  -> heartbeat with typed capability health
  -> outbound HTTPS lease/upload/complete

Online KBase
  -> subscriptions and source-sync runs
  -> idempotent SourceArticleEnvelope ingestion
  -> bounded chapters/chunks/citations
  -> private media assets
  -> Web control plane and downstream evidence APIs
```

## Components

### Generic source Agent

Create `cmd/source-agent` and move transport, heartbeat, lease, outbox, and run
loop behavior out of `cmd/wcplus-agent`. Adapters implement a narrow interface:

- report capability health and version
- validate a subscription snapshot
- discover source items after a cursor
- fetch and normalize one item
- return a stable next cursor

Keep `wcplus-agent` as a compatibility entry point until rollout completes.

Heartbeats gain a capability map such as `wechat_mp` and `wcplus`, each with
`healthy`, `version`, `last_error`, and `requires_action`. Existing WC Plus
fields remain readable during migration.

### Local enrollment and secrets

Bind the enrollment server to loopback only. A short-lived local CSRF secret
protects login actions. The MP token and cookies are stored through a
`SecretStore` abstraction whose macOS implementation uses Keychain. Tests use
an in-memory store. There is no plaintext-file fallback in production.

QR login is explicit and interactive. The Agent reports `login_required` when
the session expires and stops leasing WeChat runs until the operator logs in
again.

### WeChat adapters

The direct URL adapter accepts only HTTPS `mp.weixin.qq.com` article URLs after
DNS and redirect validation. It rejects credentials in URLs, loopback/private
targets, non-WeChat redirects, oversized responses, and unsupported content.

The MP-session adapter performs account search and article-list pagination from
the local machine. These session-backed management endpoints are treated as an
unstable integration: responses are contract-checked, fixtures are sanitized,
and nonzero upstream errors stop the run visibly. Requests use conservative
bounded concurrency and stop on throttling or verification challenges.

### Content and media

Article identity uses a canonical WeChat article key when available, otherwise
a hash of the canonical source URL. The Agent preserves semantic order while
converting headings, paragraphs, lists, quotes, links, and images to Markdown.

Media is downloaded locally with host allowlists, MIME sniffing, byte limits,
and content hashes. A separate authenticated upload route stores deduplicated
assets under the private knowledge package. Failed media makes the run partial;
it does not discard valid article text.

### Online control plane

Reuse `source_subscriptions`, `source_sync_runs`, `source_sync_items`, source
documents, receipts, and scheduler behavior. Add source type
`wechat_mp_article` and operations:

- `discover_articles`
- `sync_articles`
- `sync_media`

The Web UI presents a two-column workbench. The left side contains Agent/login
status, account search, subscriptions, and filters. The main side contains
article results, run progress, errors, imported knowledge links, and retry or
cancel actions. WC Plus diagnostics move behind a legacy section.

## Data Flow

1. The operator opens the local enrollment URL and scans the MP login QR code.
2. The Agent stores the resulting session in Keychain and heartbeats healthy.
3. The operator searches an account and creates a KBase subscription.
4. KBase queues a bounded discovery or synchronization run.
5. The Agent leases it, resumes from the subscription cursor, and paginates.
6. Each normalized article is placed in the local outbox before upload.
7. KBase hashes and ingests it as new, updated, or skipped knowledge.
8. Media uploads complete independently; failures remain retryable.
9. The Agent completes the run with a monotonic cursor and durable counters.

## Failure Handling

- Missing or expired session: mark `requires_action=login`, lease no WeChat run.
- Throttle or verification response: stop the page loop, preserve cursor, and
  return a retryable failure with the upstream code.
- Agent or network interruption: retain articles and media in the SQLite
  outbox; replay idempotently after restart.
- One malformed article: record an item failure and continue within the bounded
  batch; finish partial.
- Redirect or SSRF violation: reject before any fetch and record a permanent
  item failure.
- Changed article: update the existing deterministic knowledge document.
- Unchanged article: record skipped without rewriting package files.

## Security and Privacy

- Keep MP credentials in Keychain and never send them to KBase.
- Never log cookies, tokens, QR payloads, full response bodies, or article text.
- Keep all local HTTP listeners on loopback with explicit Origin and CSRF
  validation.
- Allowlist upstream hosts and revalidate every redirect and resolved address.
- Apply response, upload, media, batch, and concurrency limits.
- Preserve private-by-default storage and Bearer authentication on all online
  control-plane routes.
- Run privacy smoke checks before every commit and release.

## Delivery Phases

1. **Foundation:** harden direct URL validation; generalize Agent health and
   adapter interfaces without changing WC Plus behavior.
2. **Local login:** add Keychain-backed QR enrollment and session diagnostics.
3. **Discovery:** add account search, history pagination, local frontier, and
   manual subscription creation.
4. **Ingestion:** add backfill, incremental cursors, media archiving, and
   idempotent upload through the existing outbox.
5. **Product UI:** replace the WC Plus-first page with the two-column WeChat
   Collector workbench and durable deep links.
6. **Rollout:** install the signed first-party Agent, validate one account with
   a bounded run, verify replay and recovery, then enable scheduling.

## Acceptance Criteria

- A clean Mac installation can enroll locally without writing a credential to
  repository files, logs, LaunchAgent plist, or KBase.
- A user can search one public account, paginate available history, and create a
  subscription.
- A bounded backfill imports article text and media, and every knowledge chunk
  links to the original article.
- Repeating a run produces skips; changed content produces updates; neither
  creates duplicate books or assets.
- Restarting either process preserves cursor, outbox, accepted items, and run
  truth.
- Expired login, throttling, partial media, and malformed content are visible
  and actionable in the Web UI.
- Health and Proofroom can consume the imported knowledge through existing
  authenticated KBase evidence surfaces without receiving MP credentials.
- No WC Plus binary, certificate, license, or local API is required for the
  first-party workflow.

## Open-Source Reference

- `wechat-article-exporter` (MIT): QR login flow, account search, article-list
  request shapes, and sanitized fixtures.
- `wechat_articles_spider` (Apache-2.0): historical compatibility reference
  only; interception and anti-rate-limit techniques are explicitly excluded.

