# Health Authority Pack v1.1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Harden `health_authority_pack_v1` so Aheng can dry-run Dedao health knowledge with normalized source refs, review metadata, and deterministic risk reasons.

**Architecture:** Keep the contract name stable and add fields only. Dedao-kbase owns pack generation and web visibility; health-llm-driven owns dry-run validation and still writes nothing.

**Tech Stack:** Go `backend/app`, Vue 3/TypeScript `frontend-web`, Python dataclass importer and pytest in `health-llm-driven`.

---

### Task 1: Dedao Pack Metadata

**Files:**
- Modify: `backend/app/book_health_authority.go`
- Modify: `backend/app/book_health_authority_test.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing tests**

Add tests that expect a pack record to expose `source_refs`, `review_status`, `risk_reason`, and `entity_candidates`, while keeping flat fields:

```go
func TestBuildHealthAuthorityPackAddsReviewMetadata(t *testing.T) {
    store := NewBookKnowledgeStore(t.TempDir())
    saveHealthAuthorityPackFixture(t, store)
    pack, err := store.BuildHealthAuthorityPack(0)
    if err != nil { t.Fatal(err) }
    record := findHealthAuthorityPackRecord(t, pack, "dedao:verify-book:verify-claim-medication")
    if record.SourceRefs.SourceHash != record.SourceHash { t.Fatalf("missing normalized source refs") }
    if record.ReviewStatus != "blocked" { t.Fatalf("ReviewStatus = %q", record.ReviewStatus) }
    if record.RiskReason == "" { t.Fatal("RiskReason is empty") }
    if len(record.EntityCandidates) == 0 { t.Fatal("EntityCandidates is empty") }
}
```

Update HTTP export tests to require `"source_refs"`, `"review_status"`, `"risk_reason"`, and `"entity_candidates"`.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestBuildHealthAuthorityPackAddsReviewMetadata|TestKBaseHTTPHandler.*HealthAuthorityPack' -count=1
```

Expected: fail because those fields do not exist yet.

**Step 3: Implement minimal code**

Add `HealthAuthoritySourceRefs` and populate it from existing package data. Derive:

- medication/high-risk records: `review_status=blocked`, `risk_reason=medical_action_boundary`;
- health-sensitive non-action records: `review_status=education_only`, `risk_reason=health_sensitive_education_only`;
- all others: `review_status=needs_review`, `risk_reason=dedao_educational_source`;
- simple entity candidates from title/summary terms.

**Step 4: Verify GREEN**

Run the same focused Go command. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/book_health_authority.go backend/app/book_health_authority_test.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): enrich health authority pack metadata"
```

### Task 2: Web Pack Quality Visibility

**Files:**
- Modify: `frontend-web/src/api.ts`
- Modify: `frontend-web/src/views/KBaseWorkbench.vue`
- Modify: `frontend-web/scripts/web-kbase-ui-smoke.mjs`

**Step 1: Write failing smoke checks**

Assert web types and panel source mention `blocked_count`, `reviewable_count`, `risk_reason_counts`, and `source_refs`.

**Step 2: Verify RED**

Run:

```bash
node frontend-web/scripts/web-kbase-ui-smoke.mjs
```

Expected: fail until UI/types are updated.

**Step 3: Implement minimal code**

Add optional TypeScript fields and show compact summary counters in the existing Health Authority Pack panel. Do not add navigation.

**Step 4: Verify GREEN**

Run the smoke script again. Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/src/api.ts frontend-web/src/views/KBaseWorkbench.vue frontend-web/scripts/web-kbase-ui-smoke.mjs
git commit -m "feat(kbase): show health authority pack quality"
```

### Task 3: Health Importer Compatibility

**Files in `/Users/liqiuhua/.config/superpowers/worktrees/health-llm-driven/codex-health-authority-pack-import`:**
- Modify: `backend/app/services/system_kb/dedao_authority_import.py`
- Modify: `backend/tests/services/test_dedao_authority_import.py`
- Modify: `docs/plans/2026-06-30-dedao-authority-import.md`

**Step 1: Write failing tests**

Add pytest cases for nested `source_refs`, preserving `entity_candidates`, preserving `risk_reason`, and blocking a record whose `review_status` is `blocked`.

**Step 2: Verify RED**

Run:

```bash
DATABASE_URL=sqlite:///:memory: TZ=Asia/Shanghai /Users/liqiuhua/work/personal/health-llm-driven/backend/.venv/bin/python -m pytest backend/tests/services/test_dedao_authority_import.py -q --no-cov
```

Expected: fail because the importer currently reads only flat source fields and does not expose new metadata.

**Step 3: Implement minimal code**

Normalize source refs with fallback to flat fields. Add `review_status`, `risk_reason`, and `entity_candidates` to review candidates. Treat `review_status=blocked` as blocked.

**Step 4: Verify GREEN**

Run the same pytest command. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/services/system_kb/dedao_authority_import.py backend/tests/services/test_dedao_authority_import.py docs/plans/2026-06-30-dedao-authority-import.md
git commit -m "feat(kb): accept enriched dedao authority refs"
```

### Task 4: Full Verification and Deploy

**Dedao commands:**

```bash
go test ./backend/app -run 'TestBuildHealthAuthorityPack|TestKBaseHTTPHandler' -count=1
node frontend-web/scripts/web-kbase-ui-smoke.mjs
npm --prefix frontend-web run build
go test ./...
git diff --check
```

**Health commands:**

```bash
DATABASE_URL=sqlite:///:memory: TZ=Asia/Shanghai /Users/liqiuhua/work/personal/health-llm-driven/backend/.venv/bin/python -m pytest backend/tests/services/test_dedao_authority_import.py -q --no-cov
DATABASE_URL=sqlite:///:memory: TZ=Asia/Shanghai /Users/liqiuhua/work/personal/health-llm-driven/backend/.venv/bin/python scripts/check_doc_drift.py
git diff --check
```

If all green, deploy dedao-kbase using the existing kbase-server build and static sync flow, then verify `/health`, unauthorized `401`, Bearer refresh, JSONL export, and deployed JS markers.
