# Source-to-Knowledge Closed Loop Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the KBase producer-side closed loop from structured article analysis through deterministic verification, immutable release serving, and consumer feedback.

**Architecture:** Extend the existing per-book analysis manifest with a typed payload while preserving its Markdown rendering. Add deterministic quality and content-addressed release stores under `BookKnowledgeStore`, expose authenticated REST resources, and persist non-sensitive consumer feedback separately from source content.

**Tech Stack:** Go, JSON/JSONL atomic files, existing `BookKnowledgeStore`, TokenPlan OpenAI-compatible chat, `net/http`, vanilla frontend smoke tests.

---

### Task 1: Parse and Persist Structured Analysis

**Files:**
- Modify: `backend/app/book_analysis.go`
- Modify: `backend/app/book_analysis_test.go`

**Step 1: Write failing tests**

Add tests proving that a model response containing fenced JSON is parsed into
typed summary, claims, risks, and actions; each claim retains citation IDs,
scope, confidence, and risk; malformed JSON fails visibly without erasing the
previous successful manifest.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestGenerateBookAnalysisManifest(ParsesStructuredPayload|RejectsMalformedPayload)' -count=1
```

Expected: FAIL because the manifest stores only Markdown text.

**Step 3: Implement minimal schema and parser**

Add typed `BookAnalysisPayload`, `BookAnalysisClaim`, `BookAnalysisRisk`, and
`BookAnalysisAction`. Require one JSON object and retain a generated Markdown
rendering in `Answer` for backward-compatible UI display.

**Step 4: Verify GREEN**

Run the focused command and existing `TestGenerateBookAnalysisManifest` tests.

**Step 5: Commit**

```bash
git add backend/app/book_analysis.go backend/app/book_analysis_test.go
git commit -m "feat(kbase): structure source analysis payloads"
```

### Task 2: Add Deterministic Quality Reports

**Files:**
- Create: `backend/app/book_quality.go`
- Create: `backend/app/book_quality_test.go`
- Modify: `backend/app/book_analysis.go`

**Step 1: Write failing tests**

Cover valid citations, missing citations, unknown citation IDs, invalid
confidence/risk, content-hash mismatch, and high-risk evidence-only policy.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestEvaluateBookAnalysisQuality' -count=1
```

**Step 3: Implement the quality gate**

Persist `quality_report.json` atomically after successful structured analysis.
The decision is `pass`, `quarantine`, or `reject`; no model call is allowed in
this evaluator.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'Test(EvaluateBookAnalysisQuality|GenerateBookAnalysisManifest)' -count=1
git add backend/app/book_quality.go backend/app/book_quality_test.go backend/app/book_analysis.go
git commit -m "feat(kbase): verify source analysis quality"
```

### Task 3: Store Immutable Knowledge Releases

**Files:**
- Create: `backend/app/knowledge_release.go`
- Create: `backend/app/knowledge_release_test.go`

**Step 1: Write failing tests**

Prove release IDs are content-addressed and stable, failed reports cannot be
published, repeated publication is idempotent, and a newer content hash creates
a new release without modifying the old file.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKnowledgeRelease' -count=1
```

**Step 3: Implement release storage**

Store releases under `releases/{release_id}.json` and maintain an atomic
`releases/manifest.json`. Include `usage_policy=evidence_only` when any claim is
high risk.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'TestKnowledgeRelease' -count=1
git add backend/app/knowledge_release.go backend/app/knowledge_release_test.go
git commit -m "feat(kbase): publish immutable knowledge releases"
```

### Task 4: Expose Quality and Release REST Resources

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing HTTP tests**

Cover quality GET, publish POST, cursor-based release listing, release detail,
authentication, method handling, and attempts to publish a non-passing report.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandler(KnowledgeQuality|KnowledgeRelease)' -count=1
```

**Step 3: Implement routes and verify GREEN**

Add the REST routes before generic book routing. Keep drafts private and return
only immutable published releases through consumer reads.

**Step 4: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): expose knowledge release API"
```

### Task 5: Persist Consumer Feedback

**Files:**
- Create: `backend/app/knowledge_feedback.go`
- Create: `backend/app/knowledge_feedback_test.go`
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing tests**

Accept only `used`, `rejected`, `stale`, `conflict`, and `zero_hit`; validate
referenced claim IDs; reject personal-record-shaped fields; make feedback
idempotent by consumer event ID.

**Step 2: Verify RED**

```bash
go test ./backend/app -run 'Test(KnowledgeFeedback|KBaseHTTPHandlerKnowledgeFeedback)' -count=1
```

**Step 3: Implement JSONL persistence and POST route**

Store bounded, non-sensitive feedback under the release store. Return the
accepted feedback ID and aggregate status counts.

**Step 4: Verify GREEN and commit**

```bash
go test ./backend/app -run 'Test(KnowledgeFeedback|KBaseHTTPHandlerKnowledgeFeedback)' -count=1
git add backend/app/knowledge_feedback.go backend/app/knowledge_feedback_test.go backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(kbase): close consumer feedback loop"
```

### Task 6: Full Gate and Dossier Update

**Files:**
- Modify: `docs/dossiers/2026-07-12-source-to-knowledge-loop.md`

**Step 1: Run all gates**

```bash
go test ./...
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/wechat-collector-control-plane-smoke.mjs
node frontend-web/scripts/wcplus-control-plane-smoke.mjs
bash scripts/privacy-smoke.sh
git diff --check
```

**Step 2: Review security boundary**

Confirm release APIs never expose draft failures, raw credentials, or personal
health records; high-risk claims are evidence-only.

**Step 3: Record gate evidence and commit**

```bash
git add docs/dossiers/2026-07-12-source-to-knowledge-loop.md
git commit -m "docs(kbase): verify source knowledge loop"
```

