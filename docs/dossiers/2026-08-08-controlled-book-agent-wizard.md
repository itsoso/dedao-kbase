# Controlled Book Agent Wizard Dossier

## Status

Delivered and verified in production. The pilot knowledge release and controlled
Book Agent `attention-mechanism-research-assistant` version `1.0.1` are
published.

## Requirement

Provide a human-controlled path from one authorized Dedao ebook to a usable
knowledge base and Book Agent. Preserve the shared consumer token for ordinary
browser/API access, keep the dedicated Agent Package publisher credential on the
server, and prevent quality or evaluation holds from being bypassed.

## Definition Gate (G1)

Decision: PASS.

- Scope is one existing authorized ebook and the reusable platform path.
- The first Agent is read-only and supports reading, search, grounded chat, and
  evidence inspection.
- Autonomous publication, write tools, vector fallback, and browser publisher
  credentials are excluded.

## Feasibility And Risk Gate (G2)

Decision: PASS with explicit proxy requirement.

- Existing immutable Knowledge Release, Agent Package, evaluation, runtime, and
  publication contracts are reused.
- A legacy Dedao fallback package can contain complete retrieval objects while
  lacking `content_hash`; this deterministically blocks quality publication.
- The controlled browser routes require both normal API authorization and a
  constant-time verified proxy marker matching `KBASE_BROWSER_SESSION_SECRET`.
  The proxy must clear client-supplied markers on general API routes.

## Implementation

### Content identity

- Canonical SHA-256 identity covers stable book metadata, chapters, chunks,
  claims, and citations.
- Collection order and volatile timestamps do not alter identity.
- New packages receive an identity before persistence.
- Legacy repair applies only to an empty identity and invalidates stale analysis
  and quality artifacts before saving the repaired package.

### Initial knowledge release

- The review workspace exposes legacy repair when required.
- Analysis completion refreshes the quality report automatically.
- A current passing quality report enables the first immutable Knowledge
  Release action.

### Controlled Agent

- The server derives a contract-valid lexical, citation-required, read-only
  Agent Package from one published Knowledge Release.
- It generates a ten-metric deterministic suite covering retrieval recall and
  precision, citations, faithfulness, abstention, tool selection and arguments,
  task completion, latency, and cost.
- Draft, evaluation, and publication are separate explicit browser actions.
- Publication requires a passing persisted evaluation, explicit confirmation,
  a stable idempotency key, trusted browser-session provenance, and configured
  server-side publisher authority.

## Test Gate (G3)

Decision: PASS.

Focused RED/GREEN and full-suite evidence:

- canonical content-hash tests;
- missing-hash persistence and legacy repair tests;
- initial release Web smoke checks;
- generated Agent Package contract and deterministic evaluation tests;
- controlled browser-session, evaluation, confirmation, publication, and
  credential non-disclosure HTTP tests;
- three-step wizard Web smoke checks.
- regression coverage for legacy chapter-level citations and missing Dedao
  `source_type` metadata;
- `go test ./...`, Web smoke, generated system-map drift check, privacy smoke,
  and `git diff --check` all passed locally; the Linux release source also
  passed `go test ./... -timeout=180s` before every production binary build.

## Review Gate (G4)

Decision: PASS.

- The final diff preserves server-side publisher authority and requires both
  ordinary API authorization and a constant-time verified proxy secret for the
  controlled workflow.
- The general API proxy clears client-provided browser markers; the dedicated
  controlled route overwrites the marker only after Basic Auth.
- Privacy smoke passed and no token, cookie, downloaded book content, or
  machine-specific deployment value was committed.

## Deployment Health Gate (G5)

Decision: PASS.

- The release branch was pushed as `codex/controlled-book-agent-wizard`.
- Linux built the CGO-enabled server from the exact source archive after the
  complete server-side test suite passed.
- The binary, static Web directory, and Nginx include were backed up before
  replacement. Nginx configuration validation passed before reload.
- The final service is active with `ExecMainStatus=0` and `NRestarts=0`; local
  and public health probes both return the expected service payload.
- An unauthenticated public controlled-workflow request with a forged browser
  marker returns HTTP 401.

## Online Verification Gate (G6)

Decision: PASS.

1. Repaired the pilot book's empty content identity and invalidated stale
   derived artifacts.
2. Regenerated analysis with `qwen3.7-max`: seven structured claims; all six
   quality rules passed, including content-version and citation integrity.
3. Published immutable Knowledge Release
   `release-aaa4382d565653804812170c15e37295c053c19e592df934b9aa687bc7564e31`.
4. Production acceptance exposed and then fixed two legacy compatibility gaps:
   chapter-level analysis references now resolve to release citations, and
   missing source metadata is inferred only when durable Dedao ebook identity
   is present.
5. The first immutable Agent version exposed an unavailable legacy model and
   was retained for audit. Version `1.0.1` pins the production-verified
   `qwen3.7-max`, passed all ten deterministic metrics, and was explicitly
   published.
6. Runtime search returned one matching claim with two citations. A grounded
   chat completed with one evidence item, two resolved citations, a non-empty
   answer, and a persisted trace ID. An unrelated/insufficient query abstained
   without calling the model.
7. No response exposed publisher authority; the final service remained active
   with zero restarts and no new fatal/error log entries.

No Gate may advance with a failed quality, evaluation, privacy, deployment, or
online acceptance check.

## Post-deployment hardening follow-up (2026-08-08)

### Scope and implementation

- The Dedao ebook detail page now derives its Agent lifecycle from published
  Knowledge Releases and published Agent Packages. It distinguishes loading,
  available, ready-to-create, blocked, and lookup-failure states instead of
  presenting every matched book as pending.
- The desktop frontend moved to Vue 3.5, Vue Router 5, Pinia 4, Element Plus
  2.14, Vite 8, and their coordinated stable plugins. TypeScript remains on
  5.9 because TypeScript 7 no longer exposes `lib/tsc`, which vue-tsc 3 still
  requires.
- The build-only Node.js engine range is `^22.18.0 || >=24.11.0`, inherited
  from Vue Router 5's Babel 8 dependency. The deployed Go service and static
  Web assets do not require Node.js at runtime.
- Route and template icons use a bounded Element Plus registry. The globally
  loaded but unused Volcengine player SDK and stylesheet were removed from the
  HTML entry; their source files remain available for a future page-local
  integration.

### Test Gate (G3)

Decision: PASS locally.

- A clean npm install, vue-tsc check, Vite production build, all desktop
  frontend smoke checks, JavaScript syntax check, and all production Web smoke
  checks exited successfully.
- Both production-only and full npm audits reported zero vulnerabilities.
- The built JavaScript entry is 981,505 bytes, 82.2 percent below the
  5,499,050-byte baseline and below the 2,000,000-byte gate.
- `wails build --clean`, `go vet ./...`, and `go test ./... -timeout=300s`
  exited successfully. The slowest package, `backend/app`, completed in
  123.591 seconds.
- The generated system-map drift check, privacy smoke, and whitespace check
  passed after discarding Wails 2.9.1 binding regeneration noise. The tracked
  frontend package checksum was refreshed to match the upgraded manifest.

### Review and deployment notes

- G4 independent review initially rejected the branch for a stale ebook-route
  response race and unsafe title fallback in source-to-knowledge matching.
  Navigation now invalidates pending ebook loads, every delayed state write and
  render is route guarded, strong source identifiers cannot fall back to title,
  and ambiguous or internally conflicting identities fail closed. Regression
  smoke checks cover route exit/switch, same-title books, conflicting IDs, and
  duplicate-title fallback. Re-review found no remaining Critical or Important
  issue and approved the branch for merge.
- The repository-owned changes do not alter proxy or Nginx behavior.
- The existing duplicate-listener warning originates outside this repository:
  `health.executor.life.conf` is enabled from both `conf.d` and
  `sites-enabled`, while `langbridge-proxy.conf` also declares the same IP
  listener. Those files are explicitly out of scope and were not modified.
- G4 decision: PASS.

### Deployment health gate (G5)

Decision: PASS.

- Canonical `main` and the release branch both contain deployed code revision
  `4bf6a6f7c151633a123feea143465fb2110356ee`.
- The exact Git archive hash was
  `7c900954e0d8d5330f88e4507d5df1063b0c25bc8b13554cfbd1ae2c6c3e4ea4`.
  Linux repeated the frontend build and bundle gate, `go vet ./...`, and
  `go test ./... -timeout=300s`; the backend package completed in 70.027
  seconds.
- The final CGO server hash is
  `b6ecf55718854ebd574db32f1d6d23261ab2d07897cef2556d5b268b369ddbe6`.
  The rollback batch is `direct-4bf6a6f-20260808T153532Z`.
- A candidate with an incorrect embedded full revision reported
  `development`; the health gate rejected it and restored the previous binary
  before the corrected candidate was built. The final service reports the
  exact revision above, remains active/running with `ExecMainStatus=0` and
  `NRestarts=0`, and emitted no warning-or-higher log entry after deployment.
- Static Web assets were not replaced during the runtime-only correction, and
  no proxy or Nginx file was changed.

### Fresh online acceptance gate (G6)

Decision: PASS after one failed acceptance loop and correction.

- The pilot ebook page resolves Knowledge Package `128942` and published Agent
  Package `attention-mechanism-research-assistant` version `1.0.1`; its package,
  Agent, and `/sources/agents` routes load without browser console errors.
- Grounded search for `Transformer 分水岭` returns the pinned `claim-6` and its
  citation identity.
- Initial acceptance exposed that lexical retrieval treated an unspaced
  Chinese natural-language question as one term. Search succeeded, but chat
  abstained before the model call. The runtime now uses a bounded Han-bigram
  fallback only when the original lexical chat search is empty; at least two
  evidence-statement terms must match, and citation and abstention checks remain
  unchanged.
- The previously failing question now answers that Transformer is the dividing
  line, with citation `128942-citation-ffa7c6d2697326e0`. The unrelated question
  about the author's favorite color still returns `insufficient_evidence`
  without entering the model path.

## Agent Package reliability follow-up (2026-08-08)

### Scope and implementation

- Grounded search and grounded conversation now share the same bounded Chinese
  natural-language lexical fallback. The published Package and immutable
  version `1.0.1` were not changed.
- Model deadline expiry maps to HTTP 504 with the stable public message
  `agent model timed out; please retry`; the provider endpoint and raw Go error
  no longer enter the HTTP response.
- Search and conversation use one route-scoped action sequence. Submit controls
  disable during a request, local live regions distinguish progress, zero
  results, completion, abstention, and errors, and a late response cannot render
  after the user changes Agent view, Package, or version.
- The ten evaluation metrics use an auto-fitting grid instead of one rigid flex
  row. The Web shell now carries an inline SVG favicon and versioned static asset
  URLs. Proxy, Nginx, authentication, and publication policy were unchanged.
- MetaMask, `ObjectMultiplex`, and listener warnings from the original screenshot
  were not present in the application browser log and have no source match in
  the repository. They remain classified as injected browser-extension output.

### Test and review gates (G3-G4)

Decision: PASS.

- RED tests reproduced all three repository defects: natural Chinese search
  returned no evidence, model timeout returned raw HTTP 500 details, and the Web
  shell lacked the new responsive/action contracts.
- Focused runtime and HTTP tests, their race-enabled variants, JavaScript syntax,
  all production Web smoke checks, all desktop frontend smoke checks, Vite build,
  `go vet ./...`, and `go test ./... -timeout=300s -count=1` passed locally.
- One unrelated 40ms evidence-audit lease test failed while Go tests and vet ran
  concurrently; ten isolated repetitions passed and the complete suite passed
  when rerun without competing compiler load. No unrelated timing code changed.
- The generated system map was refreshed for line-number drift. Its drift check,
  privacy smoke, and `git diff --check` passed.
- Review confirmed the fallback remains lexical-only and empty-result-only,
  retains the multi-term filter, preserves requested result limits, and does not
  weaken release pinning, citations, abstention, or evaluation policy.

### Deployment health gate (G5)

Decision: PASS.

- Code revision `ee20b3c14b45128f658f699fa6c4e7a8e714e079` was pushed to the
  release branch and canonical `main`. Its Git archive SHA-256 is
  `a8def9eceb1a1e3ed602aa496ceb569389f2f7b88cef4e584324b8ee2fce1082`.
- The first server build attempt stopped before deployment because the service
  account's default npm cache contained root-owned files. A build-scoped cache
  avoided changing shared ownership. A later `proxy.golang.org` timeout and the
  service account's unwritable default Go cache were likewise resolved with
  build-scoped Go caches and a checksum-verified module proxy. No failed attempt
  changed the running service.
- Linux repeated dependency installation (zero vulnerabilities), frontend build,
  all frontend and Web smoke checks, module verification, vet, and the complete
  Go suite. The backend package completed in 70.153 seconds.
- The deployed CGO binary SHA-256 is
  `758bdfabbef247c88fae7880d67afff5388efa4f6dbfb78c69ae17b9c6778b97`.
  Binary and static Web rollback assets are stored in batch
  `direct-ee20b3c-20260808T162423Z`.
- Loopback and public health report the exact code revision. The service is
  active with `ExecMainStatus=0`, `NRestarts=0`, and no warning-or-higher journal
  entry since deployment. No Nginx file was changed.

### Fresh online acceptance gate (G6)

Decision: PASS.

- The authenticated production Package page loaded version `1.0.1`, its fixed
  release, all ten evaluation metrics, tools, and evidence ledger.
- `注意力机制的演化` returned five fixed-scope evidence items; the existing
  exact query `Transformer 分水岭` still returned only `claim-6` with citation
  `128942-citation-ffa7c6d2697326e0`.
- `这本书的作者喜欢什么颜色？` returned an explicit zero-result state with no
  evidence card.
- The production browser's 1280-pixel viewport reported zero document overflow,
  and the ten metrics wrapped into two rows. Source smoke enforces the
  auto-fitting grid plus the 760-pixel single-column breakpoint.
- During a real relevant chat request, the form reported busy, its submit button
  was disabled, and its local status announced evidence-based generation. The
  upstream call timed out; the final page showed only the stable retry message,
  restored the button, and rendered no raw provider URL.
- Application browser logs were empty, including no favicon 404. Extension-only
  MetaMask and multiplex warnings were not reproduced.

## Qwen Agent latency follow-up (2026-08-08)

### Root cause and implementation

- Repeated production traces for the relevant grounded-chat path ended at the
  Package's 30-second model deadline, while retrieval completed and a previous
  five-evidence request completed in 6.52 seconds.
- Production-to-provider probes ruled out DNS, TCP, TLS, authorization, and
  ordinary provider availability: an authenticated minimal `qwen3.7-max`
  request with thinking disabled completed in about 0.61 seconds.
- The ordinary book-analysis path already applies the shared Qwen 3.7
  structured-output policy, but Agent Package chat only selected the model and
  left `enable_thinking` unset. The provider therefore used its default
  thinking behavior, whose latency tail intermittently exceeded the Package
  deadline.
- Agent Package chat now reuses that existing policy after model selection.
  `qwen3.7-max` receives explicit `enable_thinking=false`; non-Qwen models keep
  the field unset. Package `1.0.1`, its 30-second deadline, model policy,
  retrieval scope, citations, cost cap, and publication record are unchanged.
- Retry, timeout extension, undeclared model fallback, proxy changes, and
  Package republication remain out of scope.

### Local test and review gates (G3-G4)

Decision: PASS.

- A RED test first demonstrated that the Qwen Agent path did not set the
  thinking flag. The production fix is one policy call, covered by a
  `qwen3.7-max` assertion and a non-Qwen boundary assertion.
- Focused tests and their race-enabled variants passed.
- The Node 22.23 toolchain completed the desktop frontend production build,
  bundle-size gate, all desktop smoke checks, and all production Web smoke
  checks. The built entry remained 981,505 bytes.
- Every repository shell smoke check, including managed Worker transaction,
  rollback, install, upgrade, and uninstall scenarios, completed with an
  explicit zero exit status. `go vet ./...` passed.
- `go test ./... -timeout=300s -count=1` passed; `backend/app` completed in
  99.128 seconds. Privacy, generated system-map drift, and whitespace checks
  passed.

### First deployment and failed online acceptance

- Revision `5f5bec5357194046cd24ad131b3ae9444c819b1a` was built from an
  archive with SHA-256
  `c5a0375a9b8e2d0738ed230e6f7d79667f5fad8406299b93e445e6c15490489a`.
  Linux repeated dependency audit, frontend build and smoke checks, module
  verification, vet, and the complete Go suite; `backend/app` completed in
  72.037 seconds.
- The first candidate binary SHA-256 was
  `7acd10b8d48870685da9cb535c982dcb3043e1ff32358d60018818b191b64be0`.
  It deployed with rollback batch `direct-5f5bec5-20260809T022409Z`, exact
  loopback revision, active service state, and zero restarts.
- G6 then failed safely. `注意力机制的演化` no longer timed out and restored the
  form after 12.332 seconds, but the runtime returned `citation_required`
  instead of a grounded answer. The release loop returned to diagnosis rather
  than weakening citation policy.
- A privacy-bounded provider probe showed that Qwen grouped multiple retrieved
  IDs as `[citation:id1, citation:id2]`, repeating the `citation:` prefix inside
  the same brackets. The parser previously treated the whole comma-separated
  payload as one unknown ID.

### Grouped citation correction

- The runtime now splits a grouped citation marker on commas, removes an
  optional repeated `citation:` prefix, and validates every resulting ID
  against retrieved evidence before accepting any of them.
- A production-shaped grouped-citation test failed before the change and passed
  afterward. A mixed valid/unknown group still causes complete abstention, and
  focused race-enabled coverage passed.
- The second complete local gate run passed: supported Node frontend build,
  every frontend/Web and repository shell smoke, `go vet ./...`, privacy and
  whitespace checks, and `go test ./... -timeout=300s -count=1` with
  `backend/app` completing in 99.600 seconds.
