# Knowledge Foundation v2 Implementation Plan

> Implement task by task with test-driven development. Do not advance a failed
> Gate. Commit only the files listed for the current task.

**Goal:** Make claim evidence integrity deterministic, observable, and
unskippable during Knowledge Release publication.

**Architecture:** Add a pure evidence graph validator and canonical publication
identity, integrate it into quality and publication, then expose a read-only
readiness projection through the authenticated KBase API.

**Tech stack:** Go, existing JSON artifact stores, `net/http`, generated system
map, Go and Node smoke tests.

---

### Task 1: Document The Contract And Delivery Gates

**Files:**
- Create: `docs/prd/2026-07-25-knowledge-foundation-v2.md`
- Create: `docs/plans/2026-07-25-knowledge-foundation-v2-design.md`
- Create: `docs/plans/2026-07-25-knowledge-foundation-v2.md`
- Create: `docs/dossiers/2026-07-25-knowledge-foundation-v2.md`

**Steps:**

1. Record the approved scope, compatibility policy, safety boundary, and Gate
   decisions.
2. Run `bash scripts/privacy-smoke.sh` and `git diff --check`.
3. Commit with `docs(kbase): design verifiable knowledge foundation`.

### Task 2: Add The Evidence Graph Contract

**Files:**
- Create: `backend/app/knowledge_evidence.go`
- Create: `backend/app/knowledge_evidence_test.go`

**Step 1: Write failing tests**

Cover:

- complete explicit citation chains;
- compatible direct chunk references;
- unresolved references;
- duplicate conflicting IDs;
- cross-book chapter, chunk, claim, and citation edges;
- citation/chunk chapter mismatch;
- zero-safe coverage calculations;
- canonical publication identities and conservative independence eligibility;
- absence of source bodies and local paths in the serialized report.

**Step 2: Run focused tests**

```bash
go test ./backend/app -run 'TestEvaluateKnowledgeEvidence|TestCanonicalPublicationIdentity' -count=1
```

Expected: FAIL because the evidence contract does not exist.

**Step 3: Implement the pure validator**

Add deterministic indexes, issue codes, resolution kinds, coverage metrics, and
source identity normalization. Do not write files or call external services.

**Step 4: Run focused tests**

```bash
go test ./backend/app -run 'TestEvaluateKnowledgeEvidence|TestCanonicalPublicationIdentity' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_evidence.go backend/app/knowledge_evidence_test.go
git commit -m "feat(kbase): validate claim evidence graph"
```

### Task 3: Integrate Quality And Publication Gates

**Files:**
- Modify: `backend/app/book_quality.go`
- Modify: `backend/app/book_quality_test.go`
- Modify: `backend/app/knowledge_release.go`
- Modify: `backend/app/knowledge_release_test.go`

**Step 1: Write failing tests**

Prove:

- unresolved analysis evidence quarantines quality;
- cross-book or structurally corrupt evidence rejects quality;
- direct chunk compatibility still passes when fully resolvable;
- publication recomputes evidence validation;
- a forged passing quality report cannot publish unresolved evidence;
- existing immutable release reads remain unchanged.

**Step 2: Run focused tests**

```bash
go test ./backend/app -run 'TestEvaluateBookAnalysisQuality|TestKnowledgeReleaseRejectsInvalidEvidence' -count=1
```

Expected: FAIL on the new evidence rules and publish re-check.

**Step 3: Implement minimal integration**

Map evidence blockers to stable quality rules. Recompute the evidence report in
`PublishKnowledgeRelease` after hash checks and before creating a Release.

**Step 4: Run quality and release tests**

```bash
go test ./backend/app -run 'TestEvaluateBookAnalysisQuality|TestKnowledgeRelease' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/book_quality.go backend/app/book_quality_test.go backend/app/knowledge_release.go backend/app/knowledge_release_test.go
git commit -m "feat(kbase): enforce evidence release gates"
```

### Task 4: Add The Readiness Projection

**Files:**
- Create: `backend/app/knowledge_readiness.go`
- Create: `backend/app/knowledge_readiness_test.go`

**Step 1: Write failing tests**

Cover:

- aggregate counts and zero-safe ratios;
- missing analysis, missing quality, ready, published, and blocked states;
- stable ordering and `book_id` filter;
- bounded issue lists;
- current Release identity;
- no source body, prompt, answer, token, cookie, or absolute path in JSON.

**Step 2: Run focused tests**

```bash
go test ./backend/app -run 'TestBuildKnowledgeReadiness' -count=1
```

Expected: FAIL because the projection does not exist.

**Step 3: Implement the projection**

Compose the existing pipeline projection with the evidence validator and Release
manifest. Keep the function read-only.

**Step 4: Run focused tests**

```bash
go test ./backend/app -run 'TestBuildKnowledgeReadiness' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_readiness.go backend/app/knowledge_readiness_test.go
git commit -m "feat(kbase): expose knowledge readiness projection"
```

### Task 5: Expose The Authenticated Readiness API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Create: `contracts/knowledge-readiness-v1.schema.json`
- Modify: `docs/contracts/knowledge-supply-v1.md`

**Step 1: Write failing HTTP and contract tests**

Cover:

- bearer authentication;
- `GET` only;
- `limit` bounds;
- optional exact `book_id`;
- schema validation;
- privacy-safe response payload.

**Step 2: Run focused tests**

```bash
go test ./backend/app -run 'TestKBaseHTTPKnowledgeReadiness|TestKnowledgeReadinessContract' -count=1
```

Expected: FAIL because the route and schema do not exist.

**Step 3: Implement route and schema**

Add:

```text
GET /api/knowledge/readiness
```

Reuse existing JSON/error helpers and bearer authentication.

**Step 4: Run focused tests**

```bash
go test ./backend/app -run 'TestKBaseHTTPKnowledgeReadiness|TestKnowledgeReadinessContract' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go contracts/knowledge-readiness-v1.schema.json docs/contracts/knowledge-supply-v1.md
git commit -m "feat(api): add knowledge readiness contract"
```

### Task 6: Regenerate Architecture Metadata And Verify

**Files:**
- Modify: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-07-25-knowledge-foundation-v2.md`

**Steps:**

1. Regenerate structural metadata:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

2. Run the complete backend suite:

```bash
env DEDAO_GO_CONFIG_DIR=/private/tmp/dedao-test-config \
  GOCACHE=/private/tmp/dedao-go-build \
  go test ./... -count=1
go test -race ./backend/app ./cmd/kbase-server -count=1
go vet ./...
```

3. Run contract and repository checks:

```bash
bash scripts/knowledge-contract-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

4. Request independent review. Fix blockers and repeat the affected tests.
5. Record G3 and G4 results in the Dossier.
6. Commit with `docs(kbase): record knowledge foundation verification`.

Deployment and production verification remain G5/G6 checkpoints and require an
explicit clean-main release decision.
