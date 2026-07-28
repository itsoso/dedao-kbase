# HttpOnly Browser Sessions Design

## Goal

Replace browser-held KBase API tokens with revocable, server-side browser
sessions while preserving existing Bearer authentication for automated
clients.

The browser should remain signed in for 30 days after its most recent activity.
Opening a new window or restarting the browser must not require another login.
Logout, administrator revocation, expiration, and session-limit eviction must
take effect immediately.

## Confirmed Decisions

- Keep HTTP Basic Auth as the initial browser login credential.
- Store an opaque browser session in an `HttpOnly` Cookie.
- Use a 30-day sliding inactivity window with five-minute write coalescing.
- Allow at most 10 concurrent browser sessions for the single KBase user.
- Use a dedicated `KBASE_SESSION_ADMIN_TOKEN` for session administration.
- Protect Cookie-authenticated writes with strict same-site, Origin, Fetch
  Metadata, and CSRF checks.
- Migrate a valid legacy `localStorage` Bearer token once, then remove every
  historical browser token key.
- Preserve existing Bearer behavior for Health, Proof consumers, Skills, MCP,
  source agents, and other automation.

## Non-goals

- No multi-user account system, OAuth, OIDC, passkey, or role-management system.
- No change to publisher, source-agent, or consumer token permissions.
- No raw IP address, raw User-Agent, Cookie, token, or content body in session
  records or audit logs.
- No authentication requirement for the content-free static application shell.
- No silent fallback to the legacy browser token exchange after migration.

## Architecture

KBase remains the single authentication authority. A new SQLite-backed session
store is added beside the existing application state. Production sets
`KBASE_BROWSER_SESSION_DB_PATH`; local development may use the relative default
`state/browser_sessions.sqlite3`.

The browser Cookie is named `__Host-kbase_session` and contains only a
cryptographically random 256-bit token. It is configured with:

- `Secure`
- `HttpOnly`
- `SameSite=Strict`
- `Path=/`
- a 30-day maximum age

Only a SHA-256 hash of the random token is stored. The database also stores a
server-generated public record ID, creation time, last activity, sliding
expiration, optional revocation time and reason, CSRF token hash and expiration,
and privacy-bounded device metadata.

The existing API middleware accepts either:

1. a valid automation Bearer token under the existing token-separation rules;
2. a valid browser Cookie session.

The authentication method is attached to request context. CSRF checks apply
only when an unsafe request is authenticated by Cookie. Bearer clients retain
their current machine-to-machine behavior.

## Login And Migration Flow

### New Browser Login

1. The static application loads without authentication.
2. The browser calls `POST /browser/session`.
3. Nginx requests HTTP Basic Auth, removes the Basic `Authorization` header, and
   injects the existing loopback-only browser proxy secret.
4. KBase validates the proxy boundary, revokes the least-recently-active session
   if 10 are already active, and creates a new session transactionally.
5. The response sets `__Host-kbase_session` and returns session metadata. It
   never returns `KBASE_AUTH_TOKEN`.

### Legacy Token Migration

1. The new frontend detects a historical KBase token in `localStorage`.
2. It sends the token once to `POST /browser/session/migrate` using Bearer
   authentication from the exact configured public Origin.
3. KBase creates or reuses the current browser session and sets the Cookie.
4. The frontend removes all historical KBase token keys only after the Cookie
   session is confirmed.
5. Invalid or missing legacy tokens fall back to the Basic login flow.

Migration is idempotent. A valid Cookie wins over a legacy token and prevents
duplicate session creation.

## API Contract

### Browser

- `POST /browser/session`
  - Nginx Basic Auth plus private proxy-secret boundary.
  - Creates a session and sets the Cookie.
- `POST /browser/session/migrate`
  - Existing KBase Bearer plus exact-Origin validation.
  - Creates a Cookie session without returning the Bearer.
- `GET /api/browser/session`
  - Requires Cookie authentication.
  - Returns public session metadata and a short-lived CSRF token.
- `POST /api/browser/session/logout`
  - Requires Cookie authentication and CSRF.
  - Revokes the current record before clearing the Cookie.

### Administration

- `GET /api/admin/browser-sessions`
- `DELETE /api/admin/browser-sessions/{session_id}`
- `POST /api/admin/browser-sessions/revoke-all`

Administrative routes require
`Authorization: Bearer ${KBASE_SESSION_ADMIN_TOKEN}`. The admin token must be
non-empty, high entropy, and different from every API, publisher, source-agent,
proxy, and retry-signing secret. A browser session cannot call these routes.

List responses expose only record ID, normalized device label, creation time,
last activity, expiration, revocation state, and a current-session marker when
applicable.

The legacy `GET /browser/session-token` stops returning the global API token and
returns `410 Gone` with migration guidance and `Cache-Control: no-store`.

## Session Lifecycle

An authenticated API request counts as activity; static asset requests do not.
The logical expiration is always 30 days after the most recent activity.
Database and `Set-Cookie` updates are coalesced into five-minute windows to avoid
write amplification while retaining a five-minute maximum timing granularity.

Session creation and oldest-session eviction occur in one transaction. Logout
and administrator revocation set `revoked_at` and `revoke_reason`; records are
not physically deleted during the request. Expired and revoked sessions return
`401` and clear the Cookie.

Periodic cleanup removes old expired and revoked records after an operational
retention window. Cleanup never changes the immediate authentication decision.

The frontend uses `BroadcastChannel` to propagate login and logout state among
tabs. Server validation remains authoritative; cross-tab signaling is only a
user-experience optimization.

## CSRF And Request Security

Every Cookie-authenticated `POST`, `PUT`, `PATCH`, and `DELETE` must satisfy all
of the following:

- the request Origin exactly matches `KBASE_PUBLIC_ORIGIN`;
- Fetch Metadata identifies a same-origin request;
- `X-KBase-CSRF` matches the active session's short-lived CSRF token;
- the session is active and unexpired.

CSRF tokens are random, returned only through the authenticated session
endpoint, cached in browser memory, stored as hashes, and rotated on a bounded
schedule. Missing or invalid CSRF returns `403`. Missing, expired, or revoked
session state returns `401`. Session-store failures return `503`; they are never
reported as ordinary logout and never fail open.

Nginx continues stripping browser Basic credentials before proxying. The
backend continues requiring a loopback listener whenever browser login exchange
is enabled.

## Error And UX Behavior

- `401`: the session is missing, expired, or revoked. Clear the Cookie and show
  the login state without navigating to a blank page.
- `403`: the session is valid but Origin, Fetch Metadata, or CSRF failed. Keep
  the session and show a retryable security error.
- `409`: a concurrent lifecycle operation conflicts with current session state.
- `503`: the session store is unavailable. Preserve the Cookie and show a
  service-unavailable state rather than claiming the user logged out.

The Settings page shows the current device, recent activity, expiration, and an
explicit logout command. Device-wide listing and revocation remain in the
admin API and CLI for the first release.

## Audit And Privacy

Structured audit events cover login, migration, renewal, logout, administrator
revocation, session-limit eviction, and rejected authentication. Events may
contain the public session record ID, normalized device class, reason code, and
timestamp. They must not contain credentials, raw network addresses, full
User-Agent values, source content, prompts, or knowledge data.

## Testing Strategy

Backend TDD covers:

- token hashing and constant-time comparison;
- creation, renewal, expiration, logout, and revocation;
- five-minute renewal coalescing;
- transactional 10-session eviction under concurrency;
- CSRF issuance, rotation, and rejection;
- token-separation and loopback configuration validation;
- `401`, `403`, `409`, and `503` semantics;
- backward-compatible Bearer automation.

Frontend regression coverage verifies:

- successful legacy-token migration and immediate local cleanup;
- no API token in response bodies, browser storage, or rendered HTML;
- browser restart with an active Cookie;
- login and logout synchronization across tabs;
- explicit handling of revoked and unavailable states.

The real Nginx smoke exercises Basic login, Basic-header removal, Cookie
creation, Cookie-authenticated reads, CSRF-protected writes, Bearer automation,
invalid proxy headers, and the retired token endpoint. Playwright verifies the
complete browser flow before deployment.

## Rollout And Rollback

1. Add the session store and dual Cookie/Bearer middleware while retaining all
   automation contracts.
2. Configure the session database path, public Origin, proxy secret, and
   dedicated admin token with secret-separation validation.
3. Deploy the backend, Nginx routes, and migration-capable frontend in one
   rollback transaction.
4. Verify Cookie flags, migration cleanup, restart persistence, CSRF, logout,
   administrator revocation, and downstream Bearer calls.
5. Observe authentication metrics before removing temporary frontend migration
   code.

Rollback restores the previous binary, frontend, environment file, and Nginx
configuration. The new SQLite session database can remain because the old
version does not read it. Browsers that already removed their legacy token may
need one Basic login after rollback; no knowledge artifact or automation token
is migrated.

## Delivery Gates

- **G1:** repeated browser login is reproduced and the security requirement is
  accepted.
- **G2:** token separation, migration, CSRF, revocation, and rollback designs are
  approved.
- **G3:** backend, frontend, Nginx, browser, privacy, system-map, and downstream
  contract checks pass.
- **G4:** independent security review returns no blocking finding.
- **G5:** exact clean-main artifacts deploy with healthy service and rollback
  snapshots.
- **G6:** production browser restart, revocation, CSRF, and Bearer consumer
  probes pass without exposing credentials.
