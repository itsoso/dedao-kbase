# Dedao KBase Skills Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose dedao-kbase as installable agent skills while keeping private invocation protected by Bearer token.

**Architecture:** `cmd/kbase-server` serves public discovery documents and per-skill manifests under `/.well-known` and `/api/skills/*`. Only `POST /api/skills/{name}/invoke` requires `Authorization: Bearer <token>` and calls the same `BookKnowledgeStore` and System KB export helpers as existing `/api/*` routes.

**Tech Stack:** Go `net/http`, `httptest`, JSON/OpenAPI descriptors, Markdown skill documents, Nginx routing.

---

### Task 1: Public Discovery And Descriptor Tests

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Add a failing test for `/.well-known/dedao-kbase-skills.json` without a token.
2. Assert `/api/skills`, per-skill `manifest.json`, `openapi.json`, and `SKILL.md` are public.
3. Assert `POST /api/skills/dedao.book.search/invoke` without Bearer returns 401.
4. Run `go test ./backend/app -run 'TestKBaseHTTPHandlerServesSkillDiscoveryWithoutBearer' -count=1` and confirm it fails before implementation.

### Task 2: Skill Invocation Tests

**Files:**
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Add a failing test for authenticated invocations.
2. Cover `dedao.book.search`, `dedao.book.get_context`, and `dedao.system_kb.manifest`.
3. Assert unknown skill manifests return 404.
4. Run the focused test and confirm it fails before implementation.

### Task 3: KBase Skill Routes

**Files:**
- Modify: `backend/app/kbase_http.go`
- Create: `backend/app/kbase_skills.go`

**Steps:**
1. Route `/.well-known/dedao-kbase-skills.json` before static serving.
2. Route `/api/skills/*` before the existing `/api/*` GET-only auth gate.
3. Serve public discovery, manifest, OpenAPI, and `SKILL.md`.
4. Require Bearer auth only for `invoke`.
5. Implement read-only invocations for book search, book context, System KB manifest, and System KB export.

### Task 4: Docs And Deployment Notes

**Files:**
- Modify: `README.md`
- Modify: `docs/system-map/product-map.md`
- Create: `docs/dossiers/2026-06-28-dedao-kbase-skills.md`

**Steps:**
1. Document public discovery URLs and Bearer-protected invocation examples.
2. Record the boundary: discovery public, invocation private, downstream systems must review draft claims before promoting them.
3. Update system-map narrative without hand-written generated counts.

### Task 5: Verification And Online Deployment

**Steps:**
1. Run `go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1`.
2. Run `npm run build` in `frontend-web`.
3. Run `GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server`.
4. Deploy the binary to `/opt/dedao-kbase/bin/kbase-server`.
5. Deploy `frontend-web/dist` to `/var/www/kbase.executor.life`.
6. Configure Nginx so `/health`, `/.well-known/dedao-kbase-skills.json`, and `/api/*` proxy to kbase-server, while browser pages serve the Web Workbench behind Basic Auth.
7. Restart `dedao-kbase.service`, reload Nginx, and verify public discovery, protected invocation, health, and Web Workbench access behavior.
