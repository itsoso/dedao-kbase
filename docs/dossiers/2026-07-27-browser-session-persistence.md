# Browser Session Persistence Delivery Dossier

## Status

Implementation, Gate 3, and Gate 4 are complete. G5-G6 are pending.

## Requirement

Opening KBase again should not require another browser login while the stored
Bearer token remains valid.

## Root Cause

Nginx applied HTTP Basic Auth to every static page request. Browser Basic Auth
credentials are process-scoped, so a new browser session was challenged before
the Web app could read its persistent Bearer token from local storage.

## Design

- Static HTML, JavaScript, and CSS may load without authentication; they contain
  no private knowledge data.
- `/browser/session-token` remains protected by HTTP Basic Auth. Nginx removes
  the Basic `Authorization` header and injects a separate high-entropy proxy
  secret before forwarding the request.
- When browser exchange is enabled, the backend requires a secret of at least
  32 visible ASCII characters, rejects reuse of an API token, compares the
  proxy header in constant time, and refuses to listen outside loopback.
- `/api/*` remains protected by the KBase Bearer token.
- The Web app requests `/browser/session-token` only when no valid stored token
  exists or an API request returns `401`.
- Token rotation and explicit browser-storage clearing still require login.

## Gate Decisions

### G1 - Admission

PASS. Repeated authentication prevents normal use of the private Web workbench.

### G2 - Feasibility And Risk

PASS with controls. The change moves authentication from the content-free
static shell to the existing token exchange endpoint. It does not expose API
data, credentials, downloaded content, or publisher/source-agent privileges.

### G3 - Tests

PASS.

- The regression test first failed because the repository had no managed Nginx
  configuration, then passed after the authentication boundary was added.
- The token smoke verifies that the first missing/invalid token is exchanged
  and stored, while a subsequent API call reuses it without calling
  `/browser/session-token` again. It also verifies a stale persisted token
  receives `401`, refreshes once, stores the rotated token, and retries.
- Backend tests reject the historical fixed header, missing or incorrect proxy
  secrets, a forwarded Basic header, short/non-visible/shared secrets, and
  public or wildcard listen addresses.
- The renderer rejects unsafe secret syntax, unresolved placeholders,
  non-loopback upstreams, and unsafe password-file paths. It atomically writes
  a mode `0600` generated location file.
- `go test ./... -count=1`, the race detector for `backend/app` and
  `cmd/kbase-server`, `go vet ./...`, and `go mod verify` passed.
- The Vue type check and production build passed with only the repository's
  existing eval and large-chunk warnings.
- Every desktop and Web static smoke passed.
- Knowledge contract, evaluation, source-agent packaging, WC Plus packaging,
  system-map drift, privacy, and diff checks passed.
- The generated system map was refreshed after the configuration field shifted
  generated route line numbers; no route was added or removed.
- A Linux preflight started the candidate Go server and a real Nginx instance
  on isolated loopback ports. It verified public static loading, Basic
  challenge and bad-password rejection, removal of forwarded Basic
  authorization, proxy-secret injection, rejection of the historical fixed
  header, Bearer API protection, and successful API access with the exchanged
  token.

The first parallel Go attempt started before `frontend/dist` existed and was
blocked by the Go embed directive. The locked npm install retry was blocked by
a transient network reset. The final frontend build reused the existing
verified dependency tree for the same lockfile, after which the complete Go
suite ran sequentially. A knowledge-evaluation retry used an isolated config
directory instead of the sandbox-denied user config path. No failed command was
treated as a passing gate. The first two proxy-smoke attempts exposed a test
fixture issue: the Nginx worker could not read a password file beneath a mode
`0700` temporary directory and returned `500`. The final fixture grants
traverse-only access to the random directory and read access only to its
ephemeral APR1 password file; the complete chain then passed.

### G4 - Review

The first independent review returned BLOCK:

- Basic credentials could be forwarded to a backend endpoint that explicitly
  rejects `Authorization`.
- The historical header value `1` was forgeable by any local process, and the
  backend did not enforce a proxy-only listen boundary.
- Regression coverage did not exercise stale-token rotation or the complete
  proxy contract.

The implementation now strips `Authorization`, uses a distinct random proxy
secret, validates token separation, requires loopback when exchange is enabled,
restricts the secret to a substitution-safe alphabet, renders the shared
location template atomically with unresolved-placeholder checks, and covers
stale-token rotation plus the real proxy chain.

Final focused re-review returned PASS with no P0-P2 findings. One non-blocking
P3 remains: the server trims the environment value while the renderer rejects
leading or trailing whitespace. This fails closed during deployment and is
covered operationally by generating the value without whitespace.

### G5 - Deployment Health

Pending.

### G6 - Online Verification

Pending.

## Rollback

Restore the previous service binary, environment file, and Nginx site
configuration, then restart the service and reload Nginx. No static asset,
knowledge artifact, API token, or browser storage migration is required.
