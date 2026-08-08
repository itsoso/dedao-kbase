# Agent Package Reliability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the published Agent Package page responsive and reliable, retrieve natural Chinese queries consistently, and return safe actionable timeout feedback.

**Architecture:** A shared backend retrieval helper adds a bounded Han-bigram fallback to existing lexical search, while the HTTP boundary owns timeout sanitization. The static Web UI adds route-scoped action sequencing and local status state, and CSS converts the metric strip to a responsive grid without changing published Agent data.

**Tech Stack:** Go HTTP/runtime services and tests, vanilla JavaScript, CSS, Node smoke tests, systemd deployment.

---

### Task 1: Natural Chinese Grounded Search

**Files:**
- Modify: `backend/app/agent_runtime_test.go`
- Modify: `backend/app/agent_runtime.go`

**Step 1: Write the failing runtime test**

Add a lexical package fixture containing Chinese evidence and assert that the
natural phrase `注意力机制的演化` returns grounded evidence through
`SearchAgentPackage`.

**Step 2: Run the focused test and verify RED**

```bash
go test ./backend/app -run TestAgentPackageRuntimeSearchRetrievesChineseNaturalLanguageQuery -count=1
```

Expected: FAIL because public search does not use the chat-only Han fallback.

**Step 3: Share the bounded fallback**

Rename/generalize the existing fallback helper and call it from both public
search and chat. Preserve the lexical-only, empty-result-only, multi-term match
guards.

**Step 4: Run focused runtime tests and verify GREEN**

```bash
go test ./backend/app -run 'TestAgentPackageRuntime(SearchRetrievesChineseNaturalLanguageQuery|Chat)' -count=1
```

Expected: PASS.

### Task 2: Sanitized Model Timeout

**Files:**
- Modify: `backend/app/agent_runtime_test.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Write the failing HTTP test**

Use a fake Agent chat client that returns a wrapped
`context.DeadlineExceeded`. Require HTTP 504, the stable retry message, and no
upstream URL or raw deadline detail.

**Step 2: Run the focused test and verify RED**

```bash
go test ./backend/app -run TestKBaseHTTPHandlerSanitizesAgentChatTimeout -count=1
```

Expected: FAIL with HTTP 500 and the raw provider error.

**Step 3: Add the HTTP boundary mapping**

Handle `errors.Is(err, context.DeadlineExceeded)` before the generic internal
error case and return `504 agent model timed out; please retry`.

**Step 4: Run focused HTTP tests and verify GREEN**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler(RunsVersionedAgentPackage|SanitizesAgentChatTimeout)' -count=1
```

Expected: PASS.

### Task 3: Reliable Responsive Agent UI

**Files:**
- Modify: `frontend-web/index.html`
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/book-knowledge-web-smoke.mjs`

**Step 1: Add failing source-contract assertions**

Require an inline favicon, responsive metric grid, local search/chat status,
disabled busy controls, a monotonic action sequence, and route validation before
rendering action results.

**Step 2: Run the Web smoke and verify RED**

```bash
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: FAIL because the current page has none of those contracts.

**Step 3: Implement the minimal UI state and layout**

Add explicit search/chat status fields and one active action marker. Guard each
request by sequence and package/version route, invalidate pending actions on
Agent navigation, disable both submit buttons while busy, and render local
loading/error/zero-result states. Convert the metric strip to an auto-fitting
grid with wrapping and add a data-URL SVG favicon.

**Step 4: Run syntax and focused smoke checks**

```bash
node --check frontend-web/app.js
node frontend-web/scripts/book-knowledge-web-smoke.mjs
```

Expected: PASS.

### Task 4: Full Verification And Production Release

**Files:**
- Modify only files required by a verified failure
- Modify: `docs/dossiers/2026-08-08-controlled-book-agent-wizard.md`

**Step 1: Run local quality gates**

```bash
go test ./backend/app -count=1
go test ./... -timeout=300s
go vet ./...
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
cd frontend && npm run build
node frontend/scripts/markdown-render-smoke.mjs
node frontend/scripts/book-knowledge-ui-smoke.mjs
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all commands exit zero. Run commands from the repository root except
the explicitly scoped frontend build.

**Step 2: Record acceptance evidence**

Update the controlled Book Agent dossier with the defect, fix commit, complete
test commands, production revision, and G3-G6 decisions.

**Step 3: Commit and publish**

Stage only task-owned files, commit the implementation, push the feature branch,
fast-forward the clean release source, and deploy the exact commit with binary
and static Web backups. Restore both if health validation fails.

**Step 4: Verify production**

At 1163, 760, and 401 pixel widths verify zero horizontal overflow. Verify the
natural Chinese query returns evidence, an unrelated query gives an explicit
zero-result state, exact search remains correct, pending controls disable, and
late responses cannot replace another route. Confirm `/health` reports the
deployed revision, the service restart count remains zero, and application logs
contain no new errors. Treat browser-extension warnings as external evidence,
not an application acceptance failure.
