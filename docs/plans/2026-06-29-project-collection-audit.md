# Project Collection Audit Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist machine-verified project collections and expose an async audit queue for health and Proofroom consumers.

**Architecture:** Reuse the deterministic verification report as the source of truth. A refresh endpoint materializes a project collection to `projects/<project>/collection.json`; read endpoints return the last persisted collection and its audit queue. This remains a draft/assistive layer and does not write into downstream production systems.

**Tech Stack:** Go, local JSON files, private kbase HTTP API.

---

### Task 1: Collection And Audit Backend

**Files:**
- Test: `backend/app/kbase_http_test.go`
- Create: `backend/app/book_project_collection.go`
- Modify: `backend/app/kbase_http.go`

**Step 1: Write the failing HTTP test**

Add `TestKBaseHTTPHandlerPersistsProjectCollectionAndAuditQueue`. It should:
- save `sampleBookKnowledgePackageForVerification()`
- `POST /api/projects/health/collection/refresh?limit=10`
- `GET /api/projects/health/collection`
- `GET /api/projects/health/audit-queue?limit=10`
- assert collection metadata, verified items, audit items, and `pending_async_audit`

**Step 2: Run focused test and confirm RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerPersistsProjectCollectionAndAuditQueue' -count=1
```

Expected: FAIL because the collection routes do not exist.

**Step 3: Implement collection persistence**

Create `BookKnowledgeProjectCollection`, `BookKnowledgeProjectCollectionItem`, and `BookKnowledgeProjectAuditItem`. Build collections from `BuildProjectVerificationReport`, write them to `projects/<project>/collection.json`, and load them back.

**Step 4: Wire HTTP routes**

Support:
- `POST /api/projects/{project}/collection/refresh`
- `GET /api/projects/{project}/collection`
- `GET /api/projects/{project}/audit-queue`

**Step 5: Run focused test and confirm GREEN**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerPersistsProjectCollectionAndAuditQueue' -count=1
```

Expected: PASS.

### Task 2: Verification And Deploy

**Files:**
- All files changed in Task 1.

**Step 1: Run checks**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerServesProject|TestKBaseHTTPHandlerPersistsProject' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
go test ./...
git diff --check
```

**Step 2: Build deployable server**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
```

**Step 3: Deploy and verify**

Install the server, sync existing frontend assets if rebuilt, restart `dedao-kbase.service`, and verify `/health`, `POST /api/projects/health/collection/refresh`, `GET /api/projects/health/collection`, `GET /api/projects/health/audit-queue`, and unauthorized 401 behavior.
