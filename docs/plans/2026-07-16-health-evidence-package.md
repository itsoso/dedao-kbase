# Health Evidence Package Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Provide the Health consumer with a stable evidence-only package and search API over published KBase releases.

**Architecture:** Keep KBase as the source and release control plane. Add a Health-specific mapper over immutable `KnowledgeRelease` records, expose detail and search endpoints under `/api/consumers/health/*`, and validate the new contract with fixtures and smoke tests.

**Tech Stack:** Go 1.23, `net/http`, filesystem-backed knowledge releases, existing KBase HTTP auth, JSON fixtures, shell/Node smoke scripts.

---

## Task 1: Add Health Evidence Contract

**Files:**
- Create: `backend/app/health_evidence.go`
- Create: `backend/app/health_evidence_test.go`
- Create: `contracts/health-evidence-v1.schema.json`
- Create: `contracts/fixtures/health-evidence-package.json`

**Steps:**
1. Write failing tests for mapping an `evidence_only` release into `health_evidence.v1`.
2. Verify the test fails because the mapper does not exist.
3. Implement Health evidence DTOs, tag extraction, citation mapping, safety flags, and contract validation.
4. Verify `go test ./backend/app -run HealthEvidence -count=1` passes.
5. Commit `feat(kbase): add health evidence package contract`.

## Task 2: Expose Health Evidence API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/health_kb_feed_test.go`
- Modify: `backend/app/kbase_http_test.go`

**Steps:**
1. Write failing HTTP tests for `GET /api/consumers/health/evidence/{release_id}` and `GET /api/consumers/health/search`.
2. Verify failures are `404`.
3. Add route handling, auth reuse, method validation, bad query handling, and JSON responses.
4. Verify `go test ./backend/app -run 'HealthKnowledge|HealthEvidence|KBaseHTTP' -count=1` passes.
5. Commit `feat(kbase): expose health evidence api`.

## Task 3: Add Health Consumer Smoke

**Files:**
- Create: `scripts/health-evidence-smoke.sh`
- Modify: `docs/plans/2026-07-16-health-evidence-package-design.md`

**Steps:**
1. Write a smoke script that runs the Health evidence tests and validates fixtures.
2. Run the smoke and confirm failure if fixture validation is missing.
3. Implement validation command using existing Go tests and JSON contract validation.
4. Verify `bash scripts/health-evidence-smoke.sh` passes.
5. Commit `test(kbase): add health evidence smoke`.

## Task 4: Full Verification And Rollout

**Commands:**

```bash
go test ./...
cd frontend && npm run build
node frontend-web/scripts/book-knowledge-web-smoke.mjs
node frontend-web/scripts/wcplus-source-ui-smoke.mjs
bash scripts/knowledge-eval-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/health-evidence-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Deploy only from a clean pushed branch. After deployment, verify:

```text
/health
/api/consumers/health/releases
/api/consumers/health/evidence/{release_id}
/api/consumers/health/search?q=...
```
