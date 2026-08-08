# Release Assembly v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a deterministic cross-Release claim assembly with conservative conflict candidates and independent-publication scoring.

**Architecture:** Select the latest immutable Release for each book, normalize and cluster its structured claims, derive a content-addressed assembly identity, then expose the privacy-safe projection through the authenticated KBase API. Model-assisted adjudication remains in Evidence Audit.

**Tech Stack:** Go 1.23, JSON artifact store, `net/http`, generated system map, Go tests, Node smoke scripts.

---

### Task 1: Define Delivery Contracts

**Files:**
- Create: `docs/prd/2026-07-26-release-assembly-v1.md`
- Create: `docs/plans/2026-07-26-release-assembly-v1-design.md`
- Create: `docs/plans/2026-07-26-release-assembly-v1.md`
- Create: `docs/dossiers/2026-07-26-release-assembly-v1.md`

**Steps:**

1. Record scope, alternatives, safety boundaries, schema, and Gate decisions.
2. Run `bash scripts/privacy-smoke.sh` and `git diff --check`.
3. Commit with `docs(kbase): design release assembly`.

### Task 2: Build The Deterministic Assembly

**Files:**
- Create: `backend/app/knowledge_assembly.go`
- Create: `backend/app/knowledge_assembly_test.go`
- Modify: `backend/app/knowledge_evidence.go`
- Modify: `backend/app/knowledge_evidence_test.go`

**Step 1: Write failing tests**

Cover latest-per-book selection, deterministic assembly IDs, claim
normalization, explicit polarity, same-publication deduplication,
corroboration, potential conflicts, and privacy-safe serialization.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestBuildKnowledgeReleaseAssembly|TestNormalizeKnowledgeAssemblyClaim|TestCanonicalPublicationIdentityIgnoresTransport' -count=1
```

Expected: FAIL because the assembly contract and transport-independent identity
do not exist.

**Step 3: Implement the minimal pure projection**

Create deterministic selection, normalization, clustering, scoring, sorting,
hashing, validation, query filtering, and result bounding. Do not call a model
or write an artifact.

**Step 4: Verify GREEN**

Run the focused command again. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_assembly.go backend/app/knowledge_assembly_test.go backend/app/knowledge_evidence.go backend/app/knowledge_evidence_test.go
git commit -m "feat(kbase): assemble release claims"
```

### Task 3: Expose The Authenticated API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Create: `contracts/knowledge-release-assembly-v1.schema.json`
- Modify: `docs/contracts/knowledge-supply-v1.md`

**Step 1: Write failing HTTP and contract tests**

Cover bearer auth, `GET` only, limit bounds, bounded query, deterministic
schema validation, result limiting, and privacy-safe output.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPKnowledgeAssembly|TestKnowledgeReleaseAssemblyContract' -count=1
```

Expected: FAIL because the route and JSON schema do not exist.

**Step 3: Implement route and schema**

Add `GET /api/knowledge/assembly`, reuse existing auth/error helpers, and return
the deterministic projection.

**Step 4: Verify GREEN**

Run the focused command again. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go contracts/knowledge-release-assembly-v1.schema.json docs/contracts/knowledge-supply-v1.md
git commit -m "feat(api): expose release assembly"
```

### Task 4: Verify, Review, Deploy

**Files:**
- Modify: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-07-26-release-assembly-v1.md`

**Steps:**

1. Regenerate the system map and run its drift check.
2. Run focused and full Go tests.
3. Run race detection, vet, module verification, frontend builds, contracts,
   privacy, and all static smoke scripts.
4. Request independent review and fix all P1/P2 findings.
5. Record G3/G4 evidence and commit.
6. Fast-forward clean canonical `main`.
7. Run isolated server preflight, deploy with scoped rollback, and verify
   public health plus an authenticated metadata-only assembly request.
