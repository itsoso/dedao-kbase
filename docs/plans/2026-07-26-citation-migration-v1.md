# Citation Migration v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make new extraction and structured analysis citation-native while preserving legacy read compatibility.

**Architecture:** Extend deterministic HTML normalization to create a citation for every chunk. Add a citation-aware structured-analysis context and validate all generated evidence references against the package citation allowlist before persisting a ready manifest.

**Tech Stack:** Go 1.23, JSON manifests, existing TokenPlan client, table-driven Go tests.

---

### Task 1: Complete HTML Citation Coverage

**Files:**
- Modify: `backend/app/book_extract_test.go`
- Modify: `backend/app/book_extract.go`

1. Add a failing test that creates multiple chunks in one chapter and expects
   one unique citation per chunk.
2. Run `go test ./backend/app -run 'TestExtractBookKnowledge' -count=1` and
   verify the new assertion fails.
3. Create each citation when its chunk is created; collect all chapter
   citation IDs on the draft chapter claim.
4. Run the focused extractor tests and commit.

### Task 2: Build Citation-Aware Analysis Context

**Files:**
- Modify: `backend/app/book_analysis_test.go`
- Modify: `backend/app/book_chat.go`
- Modify: `backend/app/book_analysis.go`

1. Add a failing generation test that expects
   `Evidence [citation:<id>]`, a citation source entry, and prompt version
   `structured-v2-citations`.
2. Run the test and verify RED.
3. Add a dedicated analysis context wrapper without changing normal chat
   behavior.
4. Update the structured-analysis prompt to require explicit citation IDs.
5. Run the focused test and commit.

### Task 3: Fail Closed On Non-Citation Output

**Files:**
- Modify: `backend/app/book_analysis_test.go`
- Modify: `backend/app/book_analysis.go`

1. Add failing tests for chunk IDs and unknown IDs in claims, risks, and
   actions.
2. Verify failures occur because no allowlist validation exists.
3. Validate generated references against `pkg.Citations` before marking the
   manifest ready.
4. Preserve the prior successful answer and payload on validation failure.
5. Run `go test ./backend/app -run 'BookAnalysis' -count=1` and commit.

### Task 4: Verify Foundation Compatibility

**Files:**
- Modify: `docs/_generated/system-map.json` only if generated structure changes
- Modify: `docs/dossiers/2026-07-26-citation-migration-v1.md`

1. Run focused extractor, analysis, quality, evidence, readiness, and
   publication tests.
2. Run `go test ./... -count=1`.
3. Run `go test -race ./backend/app ./cmd/kbase-server -count=1`.
4. Run `go vet ./...`, `go mod verify`, contract smoke, system-map smoke,
   privacy smoke, and `git diff --check`.
5. Request independent review and return to implementation on any blocker.
6. Record G3/G4 evidence before merge or deployment.
