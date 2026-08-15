# WeChat Account Collection Agent Design

**Status:** Approved on 2026-08-12

## Goal

Backfill every article currently visible for one explicitly selected WeChat
public account, keep the account synchronized conservatively, and publish an
immutable account-level knowledge scope that a grounded Agent can search and
cite without flattening the articles into one synthetic book.

The first production account is selected by the operator. Account names,
article bodies, credentials, and downloaded media are runtime data and must not
be committed to the repository.

## Existing State

KBase already provides a local first-party WeChat Worker, Keychain-backed
session storage, account subscriptions, resumable discovery cursors, durable
outbox delivery, idempotent article ingestion, bounded book chunks, citations,
knowledge Releases, Agent Packages, trusted evaluation, and a Web control
plane.

Production inspection found an active, healthy Worker and an existing manual
subscription for the selected account. The account already has a substantial
set of normalized article packages. Recent incremental runs were stopped by an
upstream throttle. The current Agent compiler is designed around a small list
of individual knowledge Releases, so it is not an appropriate durable scope
for a large and evolving public-account archive.

## Options Considered

### Merge every article into one book

This is simple to explain, but every new article changes one very large
artifact. It weakens article-level identity, makes retry and update accounting
coarse, and creates an expensive rebuild boundary. Rejected.

### Bind the Agent to a live account query

This keeps synchronization cheap, but the same Agent version would silently
observe a changing knowledge set. It cannot provide an immutable evidence
snapshot or deterministic rollback. Rejected.

### Publish an immutable account collection Release

Keep every article as an independent knowledge package. Build an account
collection candidate from deterministic article identities and content hashes,
run structural quality checks, and publish a content-addressed collection
Release. An Agent version pins one collection Release and expands it only at
retrieval time. Selected.

## Architecture

```text
local WeChat Worker
  -> bounded account pagination and durable cursor
  -> idempotent article packages with chunk citations
  -> account collection candidate
  -> deterministic quality gate
  -> explicit operator publication
  -> immutable account collection Release
  -> evaluated and explicitly published collection Agent
```

Collection scope is a first-class object, not a fabricated book and not a list
hidden in prompt text.

## Collection Contracts

### Definition

A collection definition contains:

- stable `collection_id`;
- display title;
- `source_type`, initially `wechat_mp_article`;
- opaque `source_account_key` and display account name;
- enabled state and timestamps.

The unique source identity is `(source_type, source_account_key)`. The display
name is metadata and is not used for membership.

### Candidate

A candidate is rebuilt from the current knowledge catalog and article manifest.
The catalog is the source of truth for the opaque account key; the article
package is the source of truth for content and citation integrity. Each member
pins:

- `book_id`;
- article `content_hash`;
- source item identity;
- publication timestamp;
- the citation IDs available in that article package.

Membership is sorted deterministically. Rebuilding unchanged source state must
produce the same candidate hash. Invalid, empty, mismatched, or unreadable
packages are excluded with bounded reason codes rather than silently accepted.

### Quality report

The collection gate is deterministic. It verifies:

- non-empty, canonical account identity;
- at least one eligible member;
- unique book and source-item identities;
- every pinned content hash matches the current article package;
- every pinned citation resolves to the pinned member;
- every member has the expected source type and account key;
- membership and exclusion summaries are internally consistent;
- health-oriented content carries `evidence_only` usage policy.

The report is `pass`, `quarantine`, or `reject`. Model output cannot override
the gate.

### Collection Release

A collection Release is immutable and content-addressed. It embeds the
definition snapshot, member pins, quality report, usage policy, and creation
time. A newer candidate creates a new Release and never mutates the previous
one. Publication is an explicit operator action.

## Agent Package Extension

Add a backward-compatible Agent Package schema version for collection scope.
Existing single-book and evidence Agents continue to use individual Release
references unchanged. A collection Agent pins exactly one collection Release
reference containing its ID and content hash.

The collection Agent uses:

- lexical retrieval in the initial delivery;
- `wechat_mp_article` as the only allowed member source type;
- mandatory citations;
- bounded context and cost budgets;
- read-only tools;
- `evidence_only` safety policy;
- abstention for insufficient or out-of-scope evidence.

Trusted evaluation must cover membership tampering, stale member hashes,
cross-account leakage, unresolved citations, unsupported tools, and runtime
answer citations before publication.

## Retrieval and Citations

At runtime the server loads the pinned collection Release, verifies its content
hash, then searches only the pinned member packages. It ranks matching chunks
globally and returns at most the Agent Package context limit. Each evidence
item records the collection Release, member book, chunk, and citation IDs.

Citation resolution loads the pinned member package and refuses citations that
are absent from that member's allowlist. Runtime traces record the Agent
Package hash, collection Release hash, selected member content hashes,
retrieval ranks, model route, and final citations. Article text is not copied
into operational logs.

## Backfill and Incremental Synchronization

The existing source subscription remains the acquisition owner. Backfill uses
bounded pages, durable cursors, and idempotent uploads until the upstream
history boundary is reached. Completion requires a stable terminal cursor and
successive empty pages; a configured item maximum alone is not proof of
completeness.

Throttle responses enter a visible cooldown with a bounded next-attempt time.
Authentication expiry, verification challenges, and permission failures remain
operator-blocked and are never bypassed. Successful later runs resume from the
saved cursor. New and changed articles create a new collection candidate, but
do not automatically replace the published collection Release or Agent.

After the initial backfill, the selected subscription runs once per day. The
operator publishes a candidate collection Release and then an Agent version
through explicit controls. The previous published Agent remains runnable until
the new version passes evaluation and is published.

## Web Experience

Add an account collection workspace linked from the WeChat source page and
Agent management:

- acquisition health, last cursor, cooldown, and last successful run;
- discovered, imported, skipped, updated, failed, and excluded counts;
- candidate versus published membership diff;
- failed article list with retry links;
- collection quality rules and explicit publish button;
- Agent draft, trusted evaluation, publish, and rollback status;
- direct links to member knowledge packages and the published Agent.

All operator-facing text is Chinese. Dense tables are used for article and
failure inventories; summary cards are reserved for actionable state.

## Failure and Recovery

- Upstream throttle: stop immediately, preserve the cursor, expose cooldown,
  and resume after the permitted time.
- Login expiry or verification: mark `requires_action`; do not lease more
  account runs until the operator restores the session.
- Individual article failure: record the item failure, continue the bounded
  batch, and leave the collection candidate incomplete until retried or
  explicitly excluded with a reason.
- Changed article: update its deterministic package; the published collection
  remains immutable until a new candidate passes and is published.
- Worker or server restart: preserve cursor, run, outbox, collection candidate,
  and published Release state.
- Agent evaluation failure: keep the draft visible and the previous published
  Agent active.

## Security and Privacy

- Credentials remain in the local Keychain and never enter KBase.
- Collection identity uses opaque source keys; URLs and article metadata are
  stored only in private runtime knowledge storage.
- No article body, cookie, token, QR payload, or downloaded media is committed
  to Git or written to application logs.
- All online collection and Agent mutation routes use existing authenticated
  admin boundaries.
- Collection publication and Agent publication remain human-controlled.
- Medical material is evidence only and cannot be promoted to personalized
  diagnosis or treatment advice.

## Acceptance Criteria

- Backfill reaches the history boundary visible to the current authorized
  session and records the observed total plus all exclusions.
- Replaying the same history produces skips and no duplicate packages.
- A collection candidate contains every eligible article package for the
  account and no package from another account.
- Tampered member hashes, account identities, or citations fail the quality or
  runtime gate.
- An immutable collection Release can be published and later superseded
  without changing the old Release.
- A collection Agent can search across more than the current small individual
  Release limit, answer with resolvable original-article citations, and abstain
  outside its scope.
- A production run answers at least three cross-article questions and records
  successful traces without leaking source text into logs.
- Daily incremental synchronization can form a candidate automatically, while
  collection and Agent publication still require explicit operator action.

## Rollout and Rollback

Deploy contracts and read paths first, then candidate building, publication,
Agent execution, and Web controls. Run one bounded production candidate before
the full account backfill. Expand only after idempotency, restart recovery,
throttle cooldown, and citation resolution pass.

Rollback disables scheduled synchronization and collection mutation routes,
restores the previous server/static artifact, and leaves article packages,
collection Releases, and Agent history intact. The last published Agent remains
the known-good runtime target.
