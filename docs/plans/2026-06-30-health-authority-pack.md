# Health Authority Pack Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Health Authority Pack export from dedao-kbase and a dry-run import path for health-llm-driven.

**Architecture:** Extend the existing dedao-kbase health project collection with a derived `health_authority_pack_v1` contract. Keep dedao-kbase responsible for source governance and health-llm-driven responsible for final System KB admission. Ship dry-run import before any production write path.

**Tech Stack:** Go `net/http`, JSON/JSONL file store, Vue 3/Vite smoke checks, Python/FastAPI health backend tests.

---

### Task 1: Authority Pack Domain Model

**Files:**
- Create: `backend/app/book_health_authority.go`
- Test: `backend/app/book_health_authority_test.go`

**Step 1: Write the failing tests**

Add tests for:

```go
func TestBuildHealthAuthorityPackDowngradesHighRiskClaims(t *testing.T)
func TestBuildHealthAuthorityPackKeepsStableSourceRefs(t *testing.T)
func TestBuildHealthAuthorityPackBlocksMedicationActionClaims(t *testing.T)
```

Expected assertions:

- `ConsumerContract == "health_authority_pack_v1"`
- `ClaimID == "dedao:verify-book:verify-claim-medication"`
- medication/dose claims include blocked uses `diagnosis`, `treatment`, `dosage`, `medication_change`, `emergency_guidance`
- Dedao-only claims never return `action_support_candidate`

**Step 2: Run tests to verify red**

Run:

```bash
go test ./backend/app -run 'TestBuildHealthAuthorityPack' -count=1
```

Expected: fail because authority pack types and builder do not exist.

**Step 3: Implement minimal model and builder**

Create:

```go
const HealthAuthorityPackContractV1 = "health_authority_pack_v1"

type HealthAuthorityPack struct {
  ConsumerContract string                      `json:"consumer_contract"`
  ProjectID        string                      `json:"project_id"`
  TargetSystem     string                      `json:"target_system"`
  GeneratedAt      string                      `json:"generated_at"`
  ItemCount        int                         `json:"item_count"`
  Items            []HealthAuthorityPackRecord `json:"items"`
}
```

Add `BuildHealthAuthorityPack(limit int) (*HealthAuthorityPack, error)` on `BookKnowledgeStore`. Build from `RefreshProjectCollection(BookKnowledgeProjectHealth, limit)` or the verification report, preserving source hashes and citations.

**Step 4: Verify green**

Run:

```bash
go test ./backend/app -run 'TestBuildHealthAuthorityPack' -count=1
```

Expected: pass.

**Step 5: Commit**

```bash
git add backend/app/book_health_authority.go backend/app/book_health_authority_test.go
git commit -m "feat(kbase): add health authority pack model"
```

### Task 2: Authority Pack HTTP API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Test: `backend/app/kbase_http_test.go`

**Step 1: Write the failing tests**

Add:

```go
func TestKBaseHTTPHandlerServesHealthAuthorityPack(t *testing.T)
func TestKBaseHTTPHandlerExportsHealthAuthorityPackJSONL(t *testing.T)
```

Assert:

- unauthenticated `/api/projects/health/authority-pack` returns `401`
- `POST /api/projects/health/authority-pack/refresh?limit=10` returns pack JSON
- `GET /api/projects/health/authority-pack/export?format=jsonl` returns `application/x-ndjson`
- `proofroom/authority-pack` returns `404`

**Step 2: Run tests to verify red**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler.*HealthAuthorityPack' -count=1
```

Expected: fail with `404`.

**Step 3: Implement routes**

Extend `handleProjectSubroute` for:

- `authority-pack`
- `authority-pack/refresh`
- `authority-pack/export`

Only accept `projectID == BookKnowledgeProjectHealth`.

**Step 4: Verify green**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler.*HealthAuthorityPack' -count=1
go test ./backend/app -run 'TestKBaseHTTPHandler' -count=1
```

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): expose health authority pack api"
```

### Task 3: Web KBase Visibility

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write smoke assertions**

Assert source contains:

- `HealthAuthorityPack`
- `refreshHealthAuthorityPack`
- `health-authority-pack`
- `health_authority_pack_v1`

**Step 2: Verify red**

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: fail on missing strings.

**Step 3: Implement compact panel**

Add API methods for refresh/get/export. In the KBase project/ops area, add a compact Health Authority Pack panel showing `item_count`, latest generation time, and buttons for refresh/export. Keep wording explicit: review pack, not medical advice.

**Step 4: Verify green**

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
```

**Step 5: Commit**

```bash
git add frontend-web/src/api.ts frontend-web/src/views/KBaseWorkbench.vue frontend-web/scripts/web-kbase-ui-smoke.mjs
git commit -m "feat(kbase): show health authority pack"
```

### Task 4: health-llm-driven Dry-Run Importer

**Files:**
- Create in health repo: `backend/app/services/system_kb/dedao_authority_import.py`
- Test in health repo: `backend/tests/services/test_dedao_authority_import.py`
- Optional docs: `docs/plans/2026-06-30-dedao-authority-import.md`

**Step 1: Write failing tests**

Test cases:

```python
def test_dry_run_rejects_unknown_contract()
def test_dry_run_blocks_medication_action_claim()
def test_dry_run_reports_review_candidates_without_writing()
```

Use an in-memory JSONL fixture with `health_authority_pack_v1` records.

**Step 2: Verify red**

Run from health repo:

```bash
PYTHONPATH=backend pytest backend/tests/services/test_dedao_authority_import.py -q
```

Expected: fail because importer module does not exist.

**Step 3: Implement dry-run importer**

Implement:

```python
def dry_run_import_dedao_authority_pack(lines: Iterable[str]) -> DedaoAuthorityImportReport:
    ...
```

Report buckets:

- `accepted_for_review`
- `blocked`
- `duplicates`
- `invalid`
- `missing_source_refs`

Do not write DB rows in this task.

**Step 4: Verify green**

```bash
PYTHONPATH=backend pytest backend/tests/services/test_dedao_authority_import.py -q
```

**Step 5: Commit in health repo**

```bash
git add backend/app/services/system_kb/dedao_authority_import.py backend/tests/services/test_dedao_authority_import.py docs/plans/2026-06-30-dedao-authority-import.md
git commit -m "feat(kb): add dedao authority pack dry run"
```

### Task 5: End-to-End Verification And Deployment

**Files:**
- Modify: `README.md`
- Modify: `docs/system-map/product-map.md`
- Optional dossier: `docs/dossiers/2026-06-30-health-authority-pack.md`

**Step 1: Run dedao-kbase verification**

```bash
go test ./backend/app -run 'TestBuildHealthAuthorityPack|TestKBaseHTTPHandler' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
go test ./...
git diff --check
```

If `scripts/privacy-smoke.sh` exists in the active checkout, run:

```bash
bash scripts/privacy-smoke.sh
```

**Step 2: Build and deploy dedao-kbase**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/dedao-kbase-web/kbase-server-linux-amd64 ./cmd/kbase-server
```

Install the binary to the existing server path, sync `frontend-web/dist/`, restart `dedao-kbase.service`, and verify:

```bash
curl -fsS https://kbase.executor.life/health
```

Also verify Bearer-protected authority pack endpoints from the host without printing tokens.

**Step 3: Run health verification**

From health repo:

```bash
PYTHONPATH=backend pytest backend/tests/services/test_dedao_authority_import.py -q
```

Then run the narrow System KB or knowledge tests that already cover reviewed-first behavior.

**Step 4: Commit docs**

```bash
git add README.md docs/system-map/product-map.md docs/dossiers/2026-06-30-health-authority-pack.md
git commit -m "docs(kbase): document health authority pack"
```
