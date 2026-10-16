# Health identity sign-in

The private knowledge workspace can use one explicitly linked Health account
instead of prompting for its independent browser password. This does not log in
to the external content provider or import any Health data. Default: disabled.

## Configuration

Enable with `KBASE_HEALTH_LOGIN_ENABLED=true`. Set the canonical HTTPS issuer in
`KBASE_HEALTH_LOGIN_ISSUER`, the verified owner ID in
`KBASE_HEALTH_LOGIN_SUBJECT`, and a dedicated random secret of at least 32 bytes
in `KBASE_HEALTH_LOGIN_SECRET`. The existing `KBASE_PUBLIC_ORIGIN` and browser
session storage/configuration remain required. Never reuse an API token, browser
proxy secret or Health JWT signing key as this secret.

The Health service must use the same issuer, workspace origin, owner ID and
shared proof secret. Its frontend must be built and served with the exact
workspace origin permitted in `/connect/kbase`'s `form-action` CSP. No wildcard
CSP or shared parent-domain cookie is needed. Install the Nginx server config and
locations template together; the former defines the start endpoint's limit zone.

## Protocol

- Public GET `/browser/health/config`: enabled status only.
- Same-origin POST `/browser/health/start`, with existing browser client ID:
  five-minute random state and independent Secure/HttpOnly binding cookie;
  returns the fixed issuer login URL. The current family epoch is captured.
- Health uses its normal first-party Web login and requires the displayed owner
  account to confirm. It signs an identity-only 60-second proof and submits it
  by form POST, never through a query parameter or browser storage.
- POST `/browser/health/callback`: exact issuer Origin, signature, purpose,
  audience, subject, time and matching pending browser binding are all required.
  The pending request is consumed under a mutex before normal session creation.
  `Store.Create` enforces the captured epoch against logout/revocation races.

Proof format: base64url(JSON) + `.` + base64url(HMAC-SHA256(secret,
`kbase-login-v1.` + encoded payload)). Fields: `v=1`,
`purpose=kbase-browser-login`, `iss`, `aud`, `sub`, `state`, `iat`, `exp`.

The single configured subject owns the existing single workspace. This must not
be broadened to all Health accounts without adding actual tenant isolation.
Existing KBase sessions retain their current lifecycle; Health sign-out/disable
is not global KBase revocation. Revoke KBase sessions on owner removal, owner
mapping changes or a security incident. This release does not claim global SSO
logout.

## Verification boundary

Local tests cover legitimate login, owner and issuer mismatch, expiry, tampering,
missing/wrong browser binding, replay under concurrency, and a real logout fence.
Existing browser-session tests and UI smoke checks remain in use. The Health
side validates cookie-only authentication, explicit expected subject, origin and
proxy restrictions, audited ticket issuance, frontend and CSP behavior.

Deployment remains separately authorized. Before activation validate Nginx,
both exact revisions and the actual owner browser round trip. A server restart
invalidates pending logins; users restart login. Never copy live credentials or
paid source content into fixtures, logs or the repository.

Local verification receipt: `go test ./...`, the focused login race test, existing
browser cookie/session settings/persistence smokes, Vue build, privacy smoke and
System Map check passed. Independent read-only safety review returned local GO.
The Health login document uses strict-origin (not no-referrer): a synthetic real
browser POST verified the expected Origin and an origin-only Referer. This is not
an actual production owner-login receipt. Nginx runtime validation and real HTTPS
owner login remain deployment gates.
