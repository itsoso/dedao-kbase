# Verified Evidence Pack v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a shared `verified_evidence_pack_v1` contract so dedao-kbase can export versioned, diffable evidence packs for health, Proofroom, and future consumers.

**Architecture:** Add a base evidence-pack builder over persisted project collections. Domain-specific exports such as Health Authority Pack become projections of the base pack. The Web Project Hub shows pack status and diff metadata, while downstream systems continue to run their own gates.

**Tech Stack:** Go, local JSON/JSONL project collection files, private kbase HTTP API, Vue 3/Vite frontend-web smoke tests.

---

### Task 1: Base Evidence Pack Contract

**Files:**
- Create: `backend/app/book_evidence_pack.go`
- Test: `backend/app/book_evidence_pack_test.go`
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write the failing pack builder test**

Add `TestBuildVerifiedEvidencePackFromProjectCollection`. It should create a sample project collection and require:
- `consumer_contract == "verified_evidence_pack_v1"`
- deterministic `pack_id`
- non-empty `source_fingerprint`
- stable `evidence_id`
- normalized `source_refs`
- `quality_summary`
- no raw content, cookies, tokens, or local paths

**Step 2: Run RED**

```bash
go test ./backend/app -run 'TestBuildVerifiedEvidencePackFromProjectCollection' -count=1
```

Expected: FAIL because the builder does not exist.

**Step 3: Implement the minimal builder**

Create:
- `VerifiedEvidencePack`
- `VerifiedEvidencePackRecord`
- `VerifiedEvidenceSourceRefs`
- `VerifiedEvidencePackQualitySummary`
- `BuildVerifiedEvidencePack(projectID string, limit int)`

Reuse `RefreshProjectCollection` as the source. Build `pack_id` from `project_id`, `collection_id`, and `source_fingerprint`.

**Step 4: Run GREEN**

```bash
go test ./backend/app -run 'TestBuildVerifiedEvidencePackFromProjectCollection' -count=1
```

Expected: PASS.

**Step 5: Add HTTP routes**

Add Bearer-protected routes:
- `GET /api/projects/{project}/evidence-pack`
- `GET /api/projects/{project}/evidence-pack/export?format=jsonl`

Add focused HTTP tests for auth, JSON response, JSONL content type, unknown project, and redaction.

### Task 2: Evidence Pack Diff

**Files:**
- Modify: `backend/app/book_evidence_pack.go`
- Test: `backend/app/book_evidence_pack_test.go`
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing diff tests**

Add tests for:
- added record
- removed record
- changed record when source hash or normalized claim changes
- unchanged record
- blocked record count

**Step 2: Implement diff API**

Add:

```text
GET /api/projects/{project}/evidence-pack/diff?previous_pack_id=...
```

The first implementation can compare the latest pack with a persisted previous pack artifact if present. If absent, return a clear `previous_pack_not_found` error instead of silently comparing against empty data.

**Step 3: Verify**

```bash
go test ./backend/app -run 'Test.*EvidencePack.*Diff|TestKBaseHTTPHandler.*EvidencePack' -count=1
```

### Task 3: Health Authority Projection Reuse

**Files:**
- Modify: `backend/app/book_health_authority.go`
- Test: `backend/app/book_health_authority_test.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write compatibility tests**

Require `health_authority_pack_v1` to keep existing fields while internally preserving:
- base `source_refs`
- base `evidence_id`
- base `source_fingerprint`
- strict health `allowed_uses` and `blocked_uses`

**Step 2: Refactor projection**

Use the base `VerifiedEvidencePack` as input for `BuildHealthAuthorityPack`. Keep the public contract stable and preserve existing downstream health importer compatibility.

**Step 3: Verify**

```bash
go test ./backend/app -run 'Test.*HealthAuthority|TestKBaseHTTPHandler.*AuthorityPack' -count=1
```

### Task 4: Proofroom Argument Pack Draft

**Files:**
- Create: `backend/app/book_proofroom_pack.go`
- Test: `backend/app/book_proofroom_pack_test.go`
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write failing tests**

Require `proofroom_argument_pack_v1` to include:
- evidence records with citations
- argument role candidates: `claim`, `support`, `counterpoint`, `question`
- contradiction candidates when risk flags or low confidence are present
- review status for unsupported records

**Step 2: Implement projection**

Project from `VerifiedEvidencePack` without calling LLMs. Unsupported records stay in review status and must not be auto-promoted.

**Step 3: Verify**

```bash
go test ./backend/app -run 'Test.*Proofroom.*Pack|TestKBaseHTTPHandler.*Proofroom' -count=1
```

### Task 5: Web Project Hub Surface

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/src/style.css`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Extend smoke tests**

Require the frontend to expose:
- evidence pack API client methods
- latest pack summary
- source unchanged indicator
- export and diff actions
- top risk reasons and recommended actions

**Step 2: Add typed API methods**

Add:
- `getProjectEvidencePack(projectID)`
- `exportProjectEvidencePack(projectID)`
- `getProjectEvidencePackDiff(projectID, previousPackID)`

**Step 3: Render in Project Hub**

Show pack metadata in the existing project panel without adding a new top-level navigation item.

**Step 4: Verify**

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
```

### Task 6: Release Verification

**Files:**
- All changed files.

**Step 1: Run repository checks**

```bash
go test ./backend/app -run 'Test.*EvidencePack|Test.*HealthAuthority|Test.*Proofroom|TestKBaseHTTPHandler.*Evidence' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
go test ./...
bash scripts/privacy-smoke.sh
git diff --check
```

**Step 2: Build deployable server**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
```

**Step 3: Deploy and verify**

Deploy through the existing `dedao-kbase.service` path. Verify:
- `/health`
- unauthenticated evidence-pack endpoints return `401`
- authenticated evidence-pack JSON and JSONL responses
- Health Authority Pack still returns `health_authority_pack_v1`
- Web bundle contains the Project Hub pack UI

**Step 4: Commit**

Stage only files changed for this feature. Do not add generated dist artifacts.
