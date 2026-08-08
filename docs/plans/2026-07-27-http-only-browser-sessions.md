# HttpOnly Browser Sessions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace browser-stored KBase Bearer tokens with revocable 30-day sliding `HttpOnly` Cookie sessions while preserving every existing machine-to-machine Bearer contract.

**Architecture:** Add an opaque-token SQLite session store and teach the KBase HTTP handler to distinguish Cookie and Bearer authentication. Browser login and legacy-token migration create server-side sessions; Cookie-authenticated writes require strict Origin, Fetch Metadata, and CSRF validation. A dedicated admin Bearer token manages sessions without granting management rights to browser sessions.

**Tech Stack:** Go 1.23, `net/http`, `database/sql`, `mattn/go-sqlite3`, vanilla JavaScript, SQLite, Nginx, Node smoke tests, Playwright, systemd.

---

## Preconditions

- Work in a dedicated `codex/` worktree based on the current
  `dedao-kbase/main`.
- Read:
  - `docs/system-map/INDEX.md`
  - `docs/plans/2026-07-27-http-only-browser-sessions-design.md`
  - `docs/dossiers/2026-07-27-browser-session-persistence.md`
- Preserve the existing source-agent, publisher, audit, and downstream Bearer
  authentication branches.
- Do not push or deploy until G3 and independent G4 review pass.
- Run `bash scripts/privacy-smoke.sh` and `git diff --check` before every
  publishing step.

### Task 1: Create The Browser Session Store

**Files:**
- Create: `backend/app/browser_session.go`
- Create: `backend/app/browser_session_test.go`

**Step 1: Write the failing schema and token-storage tests**

Add tests that open a temporary database and assert:

```go
func TestBrowserSessionStoreHashesTokens(t *testing.T) {
    store := newTestBrowserSessionStore(t, fixedNow)
    created, err := store.Create(BrowserSessionCreate{
        DeviceLabel: "Desktop Browser",
        UserAgent:   "test-agent",
    })
    if err != nil {
        t.Fatal(err)
    }
    if created.Token == "" || created.CSRFToken == "" {
        t.Fatal("session credentials are empty")
    }
    assertDatabaseDoesNotContain(t, store.DBPath(), created.Token)
    assertDatabaseDoesNotContain(t, store.DBPath(), created.CSRFToken)
}
```

Also assert that the table stores a public record ID, token hash, CSRF hash,
timestamps, revocation fields, normalized device label, and User-Agent hash.

**Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./backend/app -run 'TestBrowserSessionStore' -count=1
```

Expected: FAIL because `BrowserSessionStore` does not exist.

**Step 3: Implement the minimal store and migration**

Create the core types:

```go
type BrowserSessionStoreConfig struct {
    Path            string
    Now             func() time.Time
    Random          io.Reader
    TTL             time.Duration
    RenewalInterval time.Duration
    MaxActive       int
}

type BrowserSession struct {
    ID           string    `json:"id"`
    DeviceLabel  string    `json:"device_label"`
    CreatedAt    time.Time `json:"created_at"`
    LastActiveAt time.Time `json:"last_active_at"`
    ExpiresAt    time.Time `json:"expires_at"`
    RevokedAt    time.Time `json:"revoked_at,omitempty"`
    RevokeReason string    `json:"revoke_reason,omitempty"`
}

type BrowserSessionCredentials struct {
    Session   BrowserSession
    Token     string
    CSRFToken string
}
```

Use a 32-byte `crypto/rand` token encoded with raw URL-safe base64. Store only
`sha256.Sum256` hashes. Configure SQLite with one writer connection,
`busy_timeout=5000`, foreign keys, and a mode `0750` parent directory.

Create a migration equivalent to:

```sql
CREATE TABLE IF NOT EXISTS browser_sessions (
    id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    csrf_hash BLOB NOT NULL,
    csrf_expires_at TEXT NOT NULL,
    device_label TEXT NOT NULL,
    user_agent_hash BLOB NOT NULL,
    created_at TEXT NOT NULL,
    last_active_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT NOT NULL DEFAULT '',
    revoke_reason TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_browser_sessions_active
    ON browser_sessions(revoked_at, expires_at, last_active_at);
```

Do not store the raw User-Agent or network address.

**Step 4: Run the focused test and verify GREEN**

Run:

```bash
go test ./backend/app -run 'TestBrowserSessionStore' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/browser_session.go backend/app/browser_session_test.go
git commit -m "feat(kbase): add browser session store"
```

### Task 2: Implement Sliding Expiry, Limits, And Revocation

**Files:**
- Modify: `backend/app/browser_session.go`
- Modify: `backend/app/browser_session_test.go`

**Step 1: Write failing lifecycle tests**

Add tests for:

- expiry exactly 30 days after creation;
- activity advances logical expiry by 30 days;
- repeated activity inside five minutes does not write a second renewal;
- activity after five minutes renews both record and Cookie deadline;
- the 11th session revokes the least-recently-active active record;
- concurrent creation never leaves more than 10 active sessions;
- self logout, individual admin revocation, and revoke-all are idempotent;
- expired and revoked tokens cannot authenticate;
- listing returns metadata but no hashes;
- cleanup removes only records past the retention boundary.

Use an injected clock and deterministic reader. Do not use sleeps.

**Step 2: Run the tests and verify RED**

Run:

```bash
go test ./backend/app -run 'TestBrowserSession(Store|Lifecycle|Limit|Revoke|Cleanup)' -count=1
```

Expected: FAIL on missing lifecycle methods.

**Step 3: Implement transactional lifecycle methods**

Add:

```go
func (s *BrowserSessionStore) AuthenticateAndRenew(token string) (BrowserSessionAuth, error)
func (s *BrowserSessionStore) RotateCSRF(id string) (string, time.Time, error)
func (s *BrowserSessionStore) ValidateCSRF(id, token string) error
func (s *BrowserSessionStore) Revoke(id, reason string) error
func (s *BrowserSessionStore) RevokeByToken(token, reason string) error
func (s *BrowserSessionStore) RevokeAll(reason string) (int64, error)
func (s *BrowserSessionStore) List() ([]BrowserSession, error)
func (s *BrowserSessionStore) Cleanup(retainAfter time.Duration) (int64, error)
```

Use `BEGIN IMMEDIATE` semantics for active-count eviction and creation. Return
typed errors for missing, expired, revoked, conflict, and unavailable states so
HTTP code mapping does not depend on string matching.

**Step 4: Run focused tests and the race detector**

Run:

```bash
go test ./backend/app -run 'TestBrowserSession(Store|Lifecycle|Limit|Revoke|Cleanup)' -count=1
go test -race ./backend/app -run 'TestBrowserSession(Store|Lifecycle|Limit|Revoke|Cleanup)' -count=1
```

Expected: PASS with no race.

**Step 5: Commit**

```bash
git add backend/app/browser_session.go backend/app/browser_session_test.go
git commit -m "feat(kbase): enforce browser session lifecycle"
```

### Task 3: Add Server Configuration And Secret Separation

**Files:**
- Modify: `cmd/kbase-server/main.go`
- Modify: `cmd/kbase-server/main_test.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Write failing configuration tests**

Cover:

- `KBASE_BROWSER_SESSION_DB_PATH`;
- exact `KBASE_PUBLIC_ORIGIN`;
- `KBASE_SESSION_ADMIN_TOKEN`;
- defaults of 30 days, five minutes, and 10 sessions;
- rejection of empty production Origin;
- rejection of non-HTTPS Origin except loopback development;
- rejection when the admin token equals any API, publisher, source-agent,
  browser-proxy, or retry-signing secret;
- startup failure when Cookie sessions are enabled without a usable session
  database.

**Step 2: Verify RED**

Run:

```bash
go test ./cmd/kbase-server -run 'Test.*BrowserSession|Test.*TokenSeparation|Test.*PublicOrigin' -count=1
```

Expected: FAIL on missing configuration.

**Step 3: Implement configuration and store wiring**

Add helpers equivalent to:

```go
func defaultBrowserSessionDBPath() string
func defaultPublicOrigin() string
func defaultSessionAdminToken() string
func validateSessionConfiguration(cfg sessionServerConfig, reserved ...string) error
```

Construct `BrowserSessionStore` before the HTTP handler, pass it through
`KBaseHTTPConfig`, and close it during shutdown. Add the store, admin token,
public Origin, TTL, renewal interval, and maximum count to the handler config.

Fail startup rather than silently disabling Cookie authentication when any
required value is invalid.

**Step 4: Verify GREEN**

Run:

```bash
go test ./cmd/kbase-server -run 'Test.*BrowserSession|Test.*TokenSeparation|Test.*PublicOrigin' -count=1
go test ./backend/app -run 'TestKBaseHTTPHandler.*Session' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/kbase-server/main.go cmd/kbase-server/main_test.go backend/app/kbase_http.go
git commit -m "feat(kbase): configure server-side browser sessions"
```

### Task 4: Add Cookie Login, Migration, And Token Endpoint Retirement

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing HTTP contract tests**

Assert:

- only `POST /browser/session` is accepted;
- login rejects forwarded `Authorization`;
- login requires the constant-time proxy-secret boundary;
- the response has no API token;
- `Set-Cookie` contains
  `__Host-kbase_session`, `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`;
- migration requires a valid existing Bearer and exact Origin;
- migration is idempotent when a valid Cookie already exists;
- `GET /browser/session-token` returns `410`, `Cache-Control: no-store`, and no
  token;
- invalid credentials never reveal whether a session record exists.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerBrowser(Session|Migration|LegacyToken)' -count=1
```

Expected: FAIL because the new routes do not exist.

**Step 3: Implement Cookie helpers and handlers**

Add constants and helpers:

```go
const browserSessionCookieName = "__Host-kbase_session"

func setBrowserSessionCookie(w http.ResponseWriter, token string, expires time.Time)
func clearBrowserSessionCookie(w http.ResponseWriter)
func (h *kbaseHTTPHandler) handleBrowserSessionLogin(w http.ResponseWriter, r *http.Request)
func (h *kbaseHTTPHandler) handleBrowserSessionMigration(w http.ResponseWriter, r *http.Request)
func (h *kbaseHTTPHandler) handleLegacyBrowserToken(w http.ResponseWriter, r *http.Request)
```

Normalize the device label from bounded User-Agent categories without
persisting the original header. Return public session metadata only.

**Step 4: Verify GREEN**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerBrowser(Session|Migration|LegacyToken)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): issue secure browser session cookies"
```

### Task 5: Add Dual Authentication And CSRF Enforcement

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing authentication tests**

Add coverage for:

- a Cookie session can read normal `/api/*` routes;
- existing KBase Bearer clients still work unchanged;
- source-agent and publisher routes remain dedicated-Bearer-only;
- audit routes preserve their stable error envelopes with Cookie auth;
- `GET /api/browser/session` returns metadata plus a short-lived CSRF token;
- unsafe Cookie requests reject missing/wrong Origin, cross-site Fetch Metadata,
  and missing/wrong CSRF with `403`;
- unsafe Bearer requests do not require CSRF;
- logout revokes before clearing the Cookie;
- expired/revoked sessions return `401` and clear the Cookie;
- session-store failures return `503`, not `401`;
- concurrent logout returns stable idempotent behavior.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler(CookieAuth|CSRF|BrowserSessionStatus|BrowserLogout|BearerCompatibility)' -count=1
```

Expected: FAIL on missing dual-auth context and CSRF checks.

**Step 3: Implement request authentication context**

Introduce an internal result:

```go
type kbaseRequestAuth struct {
    Method    string
    SessionID string
    Renewed   bool
    ExpiresAt time.Time
}
```

Refactor only the general KBase and audit authorization branches to accept
Cookie sessions. Keep source-agent, publisher, and session-admin branches on
their dedicated Bearer tokens.

Before dispatching an unsafe general API request authenticated by Cookie:

1. compare `Origin` to `KBASE_PUBLIC_ORIGIN`;
2. require `Sec-Fetch-Site: same-origin`;
3. validate `X-KBase-CSRF`;
4. reject before any handler can mutate state.

Reissue the Cookie only when `AuthenticateAndRenew` reports a coalesced renewal.

**Step 4: Verify GREEN and broad HTTP compatibility**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler(CookieAuth|CSRF|BrowserSessionStatus|BrowserLogout|BearerCompatibility)' -count=1
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): authenticate browser cookies with csrf"
```

### Task 6: Add Session Administration API And CLI

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Create: `cmd/kbase-session-admin/main.go`
- Create: `cmd/kbase-session-admin/main_test.go`

**Step 1: Write failing API and CLI tests**

Assert:

- browser Cookie and ordinary KBase Bearer cannot call admin routes;
- only `KBASE_SESSION_ADMIN_TOKEN` can list, revoke one, or revoke all;
- responses never include token or hash fields;
- revocation takes effect on the next API request;
- CLI commands are `list`, `revoke <id>`, and `revoke-all --confirm`;
- CLI reads the token from environment or an explicit protected file, never a
  positional argument;
- `revoke-all` refuses to run without `--confirm`.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerSessionAdmin' -count=1
go test ./cmd/kbase-session-admin -count=1
```

Expected: FAIL because the routes and command do not exist.

**Step 3: Implement the admin routes and command**

Route admin paths before the general `/api/*` authorization branch. Use
constant-time dedicated-token comparison. The CLI should call the HTTPS API,
print bounded metadata, and emit a non-zero exit code for every non-2xx
response.

Do not add browser UI for all-session administration in this release.

**Step 4: Verify GREEN**

Run:

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerSessionAdmin' -count=1
go test ./cmd/kbase-session-admin -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go \
  cmd/kbase-session-admin/main.go cmd/kbase-session-admin/main_test.go
git commit -m "feat(kbase): add browser session administration"
```

### Task 7: Migrate The Web Client To Cookie Authentication

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/scripts/kbase-token-header-smoke.mjs`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`
- Modify: `frontend-web/scripts/markdown-render-smoke.mjs`
- Create: `frontend-web/scripts/browser-cookie-session-smoke.mjs`

**Step 1: Write the failing browser-session smoke**

The smoke must verify:

- a valid old token is sent only to `/browser/session/migrate`;
- successful migration clears every key in `tokenKeys`;
- ordinary API calls use `credentials: "same-origin"` and no Authorization
  header;
- unsafe Cookie requests fetch and send `X-KBase-CSRF`;
- `401` performs one login/session refresh and one retry;
- `403` and `503` are surfaced without opening a login loop;
- logout broadcasts state and clears in-memory CSRF;
- private image and download requests use Cookie credentials.

**Step 2: Verify RED**

Run:

```bash
node frontend-web/scripts/browser-cookie-session-smoke.mjs
```

Expected: FAIL because the Web client still stores and sends Bearer tokens.

**Step 3: Implement the minimal Cookie client**

Replace token refresh with:

```js
const browserSessionState = {
  ready: false,
  csrfToken: "",
  session: null,
  loginPromise: null,
};

async function ensureBrowserSession() { /* migrate once or POST login */ }
async function loadBrowserSession() { /* GET metadata and CSRF */ }
async function logoutBrowserSession() { /* CSRF POST and broadcast */ }
```

`apiFetch` and `apiDownload` always use `credentials: "same-origin"`. For unsafe
methods they require an in-memory CSRF token. The only remaining use of
`Authorization: Bearer` in browser code is the one-time migration request.

Use `BroadcastChannel("kbase-browser-session")` when available and a guarded
storage event fallback otherwise.

**Step 4: Run all Web smokes**

Run:

```bash
node frontend-web/scripts/browser-cookie-session-smoke.mjs
set -euo pipefail
for smoke in frontend-web/scripts/*.mjs; do node "$smoke"; done
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/scripts
git commit -m "feat(kbase): migrate web auth to secure cookies"
```

### Task 8: Add Current-Session Settings UI

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/index.html`
- Create: `frontend-web/scripts/browser-session-settings-smoke.mjs`

**Step 1: Write the failing UI smoke**

Assert the page has:

- canonical route `/settings/session`;
- a compact “会话” navigation entry;
- current device, last activity, and expiry fields;
- an explicit logout button;
- loading, revoked, unauthorized, forbidden, and unavailable states;
- no all-device administrator controls.

**Step 2: Verify RED**

Run:

```bash
node frontend-web/scripts/browser-session-settings-smoke.mjs
```

Expected: FAIL because the route does not exist.

**Step 3: Implement the route and restrained UI**

Add `sessionSettings` to `ROUTES`, route dispatch, and `renderShell`. Render an
unframed settings band with one current-session panel and a standard logout
button. Keep typography consistent with the workbench and avoid a nested-card
layout.

Update the static asset query version in `frontend-web/index.html`.

**Step 4: Verify GREEN and responsive layout**

Run:

```bash
node frontend-web/scripts/browser-session-settings-smoke.mjs
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Then use Playwright at desktop and mobile widths. Verify no overlap, clipped
labels, or blank state.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/index.html \
  frontend-web/scripts/browser-session-settings-smoke.mjs
git commit -m "feat(kbase): add browser session settings"
```

### Task 9: Update Nginx And The Real Proxy Smoke

**Files:**
- Modify: `deploy/nginx/kbase.locations.conf.template`
- Modify: `deploy/nginx/browser-session-proxy-smoke.sh`
- Modify: `deploy/nginx/render-kbase-config.sh`
- Modify: `README.md`

**Step 1: Update the smoke first and verify RED**

Change the real Nginx smoke to assert:

1. `/browser/session` requires valid Basic Auth.
2. Basic Authorization never reaches the backend.
3. login creates a secure session Cookie and returns no API token.
4. Cookie access to `/api/books` succeeds.
5. a CSRF-protected logout succeeds and invalidates the Cookie.
6. migration creates a Cookie using the existing Bearer.
7. machine Bearer access still succeeds.
8. `/browser/session-token` returns `410`.
9. fixed/forged proxy headers remain rejected.

Run against a candidate server:

```bash
KBASE_SERVER_BIN=/tmp/kbase-server \
  bash deploy/nginx/browser-session-proxy-smoke.sh
```

Expected: FAIL against the old Nginx route contract.

**Step 2: Update the location template**

Replace the Basic-protected token location with:

```nginx
location = /browser/session {
    auth_basic "dedao-kbase";
    auth_basic_user_file __KBASE_BASIC_AUTH_FILE__;
    proxy_pass http://__KBASE_BACKEND_ADDR__;
    proxy_set_header Authorization "";
    proxy_set_header X-KBase-Browser-Session "__KBASE_BROWSER_SESSION_SECRET__";
}
```

Proxy migration and the retired endpoint without Basic Auth; backend
authentication and Origin checks remain authoritative there. Preserve all
existing forwarding, timeout, buffering, and static rules.

**Step 3: Verify renderer and proxy GREEN**

Run:

```bash
bash deploy/nginx/browser-session-proxy-smoke.sh
```

Expected: PASS with real Nginx.

**Step 4: Document configuration**

Document:

- `KBASE_BROWSER_SESSION_DB_PATH`
- `KBASE_PUBLIC_ORIGIN`
- `KBASE_SESSION_ADMIN_TOKEN`
- Cookie/Bearer split
- one-time migration
- admin CLI examples without literal secrets
- rollback behavior

**Step 5: Commit**

```bash
git add deploy/nginx README.md
git commit -m "feat(kbase): proxy secure browser sessions"
```

### Task 10: Refresh Architecture Artifacts And Complete G3/G4

**Files:**
- Modify: `docs/_generated/system-map.json`
- Create: `docs/dossiers/2026-07-27-http-only-browser-sessions.md`
- Modify as required: `README.md`

**Step 1: Regenerate the system map**

Run:

```bash
GOCACHE=/tmp/kbase-session-go-cache \
  go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

Expected: PASS with generated route/type changes only.

**Step 2: Run the full verification ladder**

Run without `| tail`:

```bash
go test ./... -count=1 -timeout=240s
go test -race ./backend/app ./cmd/kbase-server ./cmd/kbase-session-admin
go vet ./...
go mod verify

(cd frontend && npm run build)

set -euo pipefail
for smoke in frontend/scripts/*.mjs frontend-web/scripts/*.mjs; do
  node "$smoke"
done

bash scripts/knowledge-contract-smoke.sh
bash scripts/knowledge-eval-smoke.sh
bash scripts/health-evidence-smoke.sh
bash scripts/proof-consumer-contract-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/wcplus-agent-packaging-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: every command exits `0`. If sandbox restrictions block `httptest`,
DNS, or Keychain, rerun the identical Go command outside the sandbox and record
both outcomes.

**Step 3: Perform independent G4 review**

Request focused review of:

- Cookie attributes and token non-disclosure;
- CSRF and Origin enforcement before mutation;
- Bearer compatibility and dedicated-token separation;
- SQLite concurrency and fail-closed behavior;
- admin revocation authority;
- Nginx Basic-header stripping;
- migration cleanup and no login loop;
- privacy and rollback.

Any P0-P2 finding returns the task to implementation and requires a fresh
review.

**Step 4: Record the dossier**

Document G1-G4 commands, results, review findings, remediation, exact commit,
known non-blockers, and rollback plan. Do not mark G5/G6 passed before
deployment.

**Step 5: Commit**

```bash
git add docs/_generated/system-map.json \
  docs/dossiers/2026-07-27-http-only-browser-sessions.md README.md
git commit -m "docs(kbase): record secure session verification"
```

### Task 11: Push, Deploy, And Complete G5/G6

**Files:**
- Modify after verification: `docs/dossiers/2026-07-27-http-only-browser-sessions.md`

**Step 1: Confirm integration authorization and clean main**

Fetch the canonical remote, verify the reviewed commit is a direct descendant,
and obtain explicit authorization for the exact repository, commit, `main`
integration, and production target.

Do not push an additional documentation or remediation commit under
authorization that names only an earlier commit.

**Step 2: Build the exact release archive**

Use `git archive` from the reviewed commit, record SHA-256, upload it, and
rebuild Vue plus the Linux CGO server from that archive.

**Step 3: Run Linux production preflight**

On an isolated source directory:

- verify archive SHA-256;
- run every Web smoke;
- build Vue;
- run `go test ./... -count=1 -timeout=240s`;
- build the candidate server;
- run the real Nginx proxy-chain smoke;
- record the candidate binary SHA-256.

**Step 4: Deploy in one rollback transaction**

Back up:

- service binary;
- `frontend-web`;
- environment file;
- Nginx site and rendered location file.

Generate new high-entropy admin and session values without printing them.
Preserve mode `0600` on secret-bearing files. Replace the exact candidate
binary, static UI, environment, and rendered Nginx config. Restart the service,
check loopback health, validate Nginx, and reload it. Restore all backups on any
failure.

**Step 5: Verify G5**

Assert:

- installed binary hash equals the preflight hash;
- service is active with `ExecMainStatus=0` and `NRestarts=0`;
- local and public health return the expected JSON;
- Nginx configuration passes;
- recent logs have no panic, fatal, error, or failed line.

**Step 6: Verify G6 without exposing credentials**

Use root-only temporary curl config files or Playwright:

- static shell loads without Basic challenge;
- Basic login creates the required Cookie and returns no API token;
- browser restart remains authenticated;
- legacy token migrates and disappears from browser storage;
- Cookie read succeeds;
- missing and invalid CSRF writes fail;
- valid CSRF write succeeds;
- logout immediately invalidates the session;
- admin revocation immediately invalidates a second session;
- the 11th session evicts the oldest;
- downstream Bearer probes still succeed;
- `/browser/session-token` returns `410`;
- no credential appears in process arguments, logs, response bodies, or HTML.

**Step 7: Update and commit rollout evidence**

Record exact hashes, snapshot paths, G5/G6 probes, failed fixture attempts, and
the rollback command. Run privacy and diff checks, commit only the dossier, and
request separate push authorization if the original authorization did not
cover the new evidence commit.

## Execution Notes

- Use `superpowers:test-driven-development` for Tasks 1-9.
- Use `superpowers:requesting-code-review` before G4.
- Use `superpowers:verification-before-completion` before every completion
  claim, commit group, push, and deployment.
- Preserve the dedicated worktree until G6 and rollout evidence are complete.
- Stop immediately at any failed Gate; do not deploy a partial Cookie/CSRF
  contract.
