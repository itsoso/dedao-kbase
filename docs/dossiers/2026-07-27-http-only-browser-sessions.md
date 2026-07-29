# HttpOnly Browser Sessions Delivery Dossier

## Status

G1-G4 passed. G5-G6 have not started.

No commit in this dossier has been pushed, merged to `main`, or deployed.

## Requirement

Keep a browser signed in for 30 days with sliding renewal, while removing the
KBase API Bearer token from browser-readable storage. Activity extends the
session; explicit logout and administrator revocation invalidate it
immediately. Machine clients must retain the existing Bearer contract.

## Delivered Design

- The browser receives an opaque `__Host-kbase_session` Cookie with
  `Secure`, `HttpOnly`, `SameSite=Strict`, and `Path=/`.
- Session records, client-family epochs, and bounded structured security audit
  events are stored in a persistent SQLite database. Tokens are stored as
  hashes, not plaintext.
- Renewal, revocation, client-family fencing, expiry, and the per-client
  session limit are committed transactionally.
- Cookie reads and machine Bearer reads share the API without changing
  downstream Health, Proofroom, source-agent, publisher, or audit contracts.
- Cookie-authenticated mutations require the configured Origin, same-origin
  Fetch Metadata, and a short-lived one-time CSRF token.
- Browser login is the only Nginx location protected by Basic Auth. Nginx
  strips Basic credentials and injects a separate loopback-only proxy secret.
- One-time legacy migration accepts the old API Bearer only at the exact
  migration route. The retired token-returning route always returns `410`.
- A separate administrator token lists and revokes browser sessions. Browser
  sessions and the main API token cannot administer sessions.
- The server cleans expired and revoked sessions plus audit events immediately
  at startup and hourly thereafter, with a bounded 30-day default retention.
- The Web client coordinates login, migration, logout, and revocation across
  tabs, fences stale asynchronous work by client generation, and exposes a
  session settings page without rendering secrets.

## Gate Decisions

### G1 - Admission

PASS. Repeated Basic login harmed normal use, while persisting the root API
Bearer in browser storage gave any same-origin script unnecessary long-lived
authority. A server-side browser credential was required.

### G2 - Feasibility And Risk

PASS with controls.

- Static assets remain public but contain no private data or credentials.
- Browser and machine authentication are separate paths with explicit token
  separation checks.
- Browser session configuration fails closed when storage, public Origin,
  loopback binding, or dedicated secrets are invalid.
- Cookie writes require CSRF controls before renewal or mutation commits.
- Deployment must replace the server, Web assets, environment, and rendered
  Nginx locations as one rollback transaction.

### G3 - Verification

PASS on 2026-07-28.

Commands that passed:

```bash
go test ./... -count=1 -timeout=300s
go test -race ./backend/app ./cmd/kbase-server ./cmd/kbase-session-admin \
  -count=1 -timeout=360s
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
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

The current candidate server also passed
`deploy/nginx/browser-session-proxy-smoke.sh` with a real Nginx process. The
smoke covered Basic challenge and rejection, credential stripping, secure
Cookie creation, Cookie API access, missing and invalid CSRF rejection, valid
logout, immediate invalidation, legacy migration, machine Bearer compatibility,
the retired route, and forged internal headers.

The Vue build emitted the repository's existing `eval` and large-chunk
warnings. Race builds emitted the existing macOS linker `LC_DYSYMTAB` warning.
Neither command failed.

#### Failed Attempts And Remediation

- The first sandboxed Go run could not bind `httptest` loopback ports, resolve
  the external media fixture, or access Keychain. It was not counted as a pass.
- The same unrestricted run exposed a real pre-existing source-agent defect:
  `/usr/bin/security ... -w` waits for a terminal and ignored ordinary stdin,
  so first-time master-key creation timed out. Commit `9766f48` uses the macOS
  `expect` executable to provide a private pseudo-terminal, keeps the key out of
  process arguments and output, and enforces a ten-second timeout. The focused
  Keychain test, source-agent package, and full Go suite then passed.
- The first Nginx quality review blocked because fallback routes preserved
  unnecessary `Authorization` and renderer validation accepted commented
  directives. Commit `f65f665` narrowed Bearer forwarding to `/api/*` and the
  exact migration route, validates one active directive per security boundary,
  and added negative CSRF and Cookie assertions. Fresh specification and
  security reviews passed.
- The full frontend smoke then found an old assertion that still expected Basic
  Auth on the retired token route. Commit `2913bc1` updated the test to the
  Cookie login, migration, and `410` route contract. The entire smoke set then
  passed from the beginning.
- The first final backend review blocked G4 because `Cleanup` was never
  scheduled and the required session security audit events were absent.
  Commit `29dc62e` starts cleanup with the server, runs it immediately and
  hourly, bounds retention configuration, and waits for shutdown. Commit
  `6e6b496` persists login, migration, renewal, logout, administrator
  revocation, session-limit eviction, and rejected-authentication events
  without credentials, IP addresses, or full User-Agent values. Audit rows
  share the retention cleanup transaction.
- Rejected-authentication coverage was expanded only after adding abuse
  controls in `aec83f6`: identical session/reason failures coalesce for one
  hour, invalid Cookies, forged proxy credentials, and unexpected browser
  Authorization headers are recorded without preserving the submitted
  credential.
- A second security review blocked the initial single-cap design because
  rejected traffic could evict lifecycle evidence and rejection insertion plus
  pruning used separate commits. Commit `f67d4b8` gives lifecycle and rejected
  events independent bounded quotas and moves rejected-event lookup,
  coalescing, insertion, and pruning into one immediate transaction. A
  deliberate SQLite DELETE-trigger failure verifies that pruning failure rolls
  back the insert.
- The first smoke rerun after the audit patch correctly failed because HTTP
  line movement made the generated system map stale. Commit `c0f364a`
  regenerated the code-derived inventory. The complete smoke group then
  passed from the beginning.

### G4 - Independent Review

PASS on 2026-07-28 after two blocked review/remediation cycles.

- The first complete review blocked the missing production cleanup scheduler
  and audit chain. Commits `29dc62e` and `6e6b496` remediated both findings.
- The second complete security review blocked the shared audit quota and
  non-atomic rejected-event retention. Commit `f67d4b8` isolated the quotas and
  made rejected-event retention atomic.
- Two fresh reviewers approved `f67d4b8`. They found no actionable P0-P2 in
  the complete `b940c33..f67d4b8` range or the focused remediation range.
  Cookie, CSRF, Bearer, Nginx, SQLite migration, scheduler lifecycle, audit
  privacy, transaction rollback, cross-tab behavior, downstream compatibility,
  and HTTP status semantics were covered.

### G5 - Deployment Health

PENDING explicit release authorization.

### G6 - Online Verification

PENDING successful G5.

## Reviewed Commits

The implementation currently ends at `f67d4b8`. The main milestones are:

- `1ba7f6a` through `395c2f9`: persistent opaque session store and hardening;
- `3df61ff` through `a571ab1`: lifecycle, renewal, eviction, and fencing;
- `f65df52` through `2937d81`: server configuration and resource cleanup;
- `508078a` through `6e9ace7`: Cookie issuance, dual authentication, and CSRF;
- `dd7f021` and `114391e`: administrator API and CLI;
- `516557a` through `2b0c1e4`: Web migration, generation fencing, and settings;
- `285e0a2` and `f65f665`: Nginx proxy contract and security hardening;
- `9766f48`: deterministic non-interactive Keychain storage;
- `2913bc1`: final browser/Nginx contract smoke alignment;
- `29dc62e`: production session and audit retention scheduler;
- `6e6b496`: persistent structured browser-session audit chain;
- `c0f364a`: regenerated code-derived system map;
- `aec83f6`: bounded and coalesced rejected-authentication audits;
- `f67d4b8`: isolated audit quotas and atomic rejected-event retention.

## Rollback

Before deployment, snapshot the current server binary, Web directory,
environment file, site configuration, and rendered location file. Restore all
five together if any rollout or verification step fails, then restart KBase,
validate Nginx, and reload it.

The new browser-session SQLite database may remain on disk because the old
server does not read it. A browser that already removed its legacy token will
need to complete Basic login after rollback. Knowledge artifacts and machine
Bearer credentials are not migrated or modified by this feature.
