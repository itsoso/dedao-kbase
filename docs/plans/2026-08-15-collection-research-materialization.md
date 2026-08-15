# Collection Research Materialization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Convert an immutable published account collection release into an idempotent standard knowledge release that can use the unchanged v4 Research Agent compiler and runtime.

**Architecture:** Add a store-level materializer that validates every pinned collection member, namespaces cited member chunks into grounded standard-release claims, and records source-to-target provenance. Expose it through one authenticated HTTP action, then prove that the existing `research_enabled` compiler produces a v4 package while v3 packages remain unable to run Research tools.

**Tech Stack:** Go, JSON file stores with atomic writes, `net/http`, SQLite-backed Research runtime tests, existing Node smoke scripts.

---

### Task 1: Define the materialization contract and deterministic projection

**Files:**
- Create: `backend/app/knowledge_collection_materialization.go`
- Create: `backend/app/knowledge_collection_materialization_test.go`

**Step 1: Write the failing deterministic materialization test**

Create a published collection fixture containing two article packages whose
local chapter/chunk/citation IDs collide. Call:

```go
result, created, err := store.MaterializeKnowledgeCollectionRelease(release.ReleaseID)
```

Assert:

- the first call returns `created=true`;
- the target is a loadable `knowledge_release.v1`;
- projected claim and citation IDs are distinct and namespaced;
- every projected claim has at least one citation;
- source type, source account, source item key, publication time, anchor, and
  note are preserved;
- the source collection release ID/hash and target release ID/hash are stored
  in the materialization record;
- the second call returns the same IDs with `created=false`.

**Step 2: Run the test to verify it fails**

```bash
go test ./backend/app -count=1 -run '^TestMaterializeKnowledgeCollectionReleaseIsDeterministicAndNamespaced$'
```

Expected: FAIL because `MaterializeKnowledgeCollectionRelease` does not exist.

**Step 3: Implement the minimal projection**

Add these public result types:

```go
const KnowledgeCollectionMaterializationSchemaVersion = "knowledge_collection_materialization.v1"

type KnowledgeCollectionMaterialization struct {
    SchemaVersion             string `json:"schema_version"`
    SourceCollectionReleaseID string `json:"source_collection_release_id"`
    SourceContentHash         string `json:"source_content_hash"`
    TargetReleaseID           string `json:"target_release_id"`
    TargetContentHash         string `json:"target_content_hash"`
    MemberCount               int    `json:"member_count"`
    ClaimCount                int    `json:"claim_count"`
    CitationCount             int    `json:"citation_count"`
    CreatedAt                 string `json:"created_at"`
}

type KnowledgeCollectionMaterializationResult struct {
    Materialization KnowledgeCollectionMaterialization `json:"materialization"`
    Release         KnowledgeRelease                   `json:"release"`
}
```

Implement:

```go
func (s *BookKnowledgeStore) MaterializeKnowledgeCollectionRelease(
    releaseID string,
) (*KnowledgeCollectionMaterializationResult, bool, error)
```

The method must validate the source release and every pinned member, project
only allowlisted cited chunks, namespace every local ID, construct a canonical
evidence-only standard release, and persist the release plus provenance record.
Use explicit aggregate limits for claims, citations, and quoted characters.
Do not silently truncate; reject an oversized projection.

**Step 4: Run the test to verify it passes**

Run the focused test from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_collection_materialization.go backend/app/knowledge_collection_materialization_test.go
git commit -m "feat(knowledge): materialize collection releases"
```

### Task 2: Fail closed on changed, uncited, or oversized members

**Files:**
- Modify: `backend/app/knowledge_collection_materialization_test.go`
- Modify: `backend/app/knowledge_collection_materialization.go`

**Step 1: Write failing negative tests**

Add table-driven cases for collection hash mismatch, member hash or source
identity mismatch, citations outside the member allowlist, missing cited
chunks, no projectable evidence, aggregate limit overflow, and conflicting
stored provenance. Assert failure leaves no new materialization record.

**Step 2: Run the tests to verify they fail**

```bash
go test ./backend/app -count=1 -run '^TestMaterializeKnowledgeCollectionRelease(Rejects|LeavesNoPartialRecord)'
```

Expected: FAIL on the first unimplemented boundary.

**Step 3: Implement validation and recovery**

Validate everything before persistence. Keep validation errors terminal and
descriptive; propagate lock, filesystem, and atomic-write errors unchanged.
If the release is durable but provenance writing was interrupted, a retry must
finish the same provenance record. Never overwrite conflicting content.

**Step 4: Run all materialization tests**

```bash
go test ./backend/app -count=1 -run '^TestMaterializeKnowledgeCollectionRelease'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_collection_materialization.go backend/app/knowledge_collection_materialization_test.go
git commit -m "fix(knowledge): fence collection materialization"
```

### Task 3: Add the authenticated materialization endpoint

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`
- Modify after route change: `docs/_generated/system-map.json`

**Step 1: Write failing HTTP contract tests**

Cover:

```text
POST /api/knowledge/collection-releases/{release_id}/materialize
```

Assert authenticated creation returns HTTP 201 with `created=true`, replay
returns HTTP 200 with `created=false`, and the response exposes only release
IDs, hashes, counts, usage policy, and creation metadata. Assert missing auth,
unknown JSON keys, oversized bodies, invalid IDs, unsupported methods, missing
releases, and immutable conflicts return bounded errors.

**Step 2: Run the tests to verify they fail**

```bash
go test ./backend/app -count=1 -run '^TestKBaseHTTPKnowledgeCollectionMaterialization'
```

Expected: FAIL with the route missing.

**Step 3: Implement the route**

Extend the collection-release path parser to distinguish detail GET from the
`materialize` POST action. Decode exactly one empty JSON object with unknown
fields rejected and no trailing payload. Call the store method and select 201
or 200 from the `created` value. Map immutable conflicts to 409, unknown
releases to 404, malformed requests to 400, and unexpected store failures to a
generic 500 without leaking local paths or article text.

**Step 4: Regenerate and verify the route inventory**

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
go test ./backend/app -count=1 -run '^TestKBaseHTTPKnowledgeCollectionMaterialization'
bash scripts/system-map-smoke.sh
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go docs/_generated/system-map.json
git commit -m "feat(kbase): expose collection materialization"
```

### Task 4: Prove unchanged v4 compilation and Research retrieval

**Files:**
- Modify: `backend/app/agent_compiler_test.go`
- Modify: `backend/app/research_tools_test.go`
- Modify: `backend/app/agent_runtime_test.go`

**Step 1: Write the compiler integration test**

Materialize a collection, then compile a study-mode request with the returned
standard `release_id` and `research_enabled=true`. Assert the result is a valid
v4 package with only the materialized standard release pinned, explicit
Research policy, deep-research UI capability, and all required read-only tool
rules.

**Step 2: Write Research search/fetch tests**

Evaluate and publish the v4 fixture with the existing trusted Research suite,
then execute `research/search_knowledge` and
`research/fetch_knowledge_evidence`. Assert the fetched evidence resolves to a
materialized claim and citation. Also assert the original v3 collection
package remains policy-denied for run creation and `Advance`.

**Step 3: Run the tests to verify they fail where integration is incomplete**

```bash
go test ./backend/app -count=1 -run 'TestAgentCompilerAcceptsMaterializedCollectionForResearch|TestResearchToolsFetchMaterializedCollectionEvidence|TestResearchRuntimeStillRejectsV3Collection'
```

Expected: FAIL until fixtures and any missing compatibility code are complete.

**Step 4: Add only necessary compatibility code**

The preferred outcome is no production compiler or Research runtime change.
If a standard release invariant is missing, fix the materializer instead of
adding collection-specific branches to the compiler or Research tools.

**Step 5: Run focused integration and race tests**

```bash
go test ./backend/app -count=1 -run 'TestAgentCompilerAcceptsMaterializedCollectionForResearch|TestResearchToolsFetchMaterializedCollectionEvidence|TestResearchRuntimeStillRejectsV3Collection'
go test -race ./backend/app -count=1 -run 'TestMaterializeKnowledgeCollectionRelease|TestResearchToolsFetchMaterializedCollectionEvidence'
```

Expected: PASS.

**Step 6: Commit**

```bash
git add backend/app/agent_compiler_test.go backend/app/research_tools_test.go backend/app/agent_runtime_test.go
git commit -m "test(research): cover materialized collection packages"
```

### Task 5: Complete release gates and online acceptance

**Files:**
- Modify: `scripts/research-agent-smoke.sh`
- Modify: `docs/dossiers/2026-08-13-research-agent-platform.md`

**Step 1: Extend the process smoke**

Add a synthetic published collection, materialize it through the real HTTP
route, compile a v4 package from the returned release, and verify a Research
quick path can search and fetch its grounded evidence. Keep fixture content
synthetic and assert no private sentinel reaches process logs.

**Step 2: Run focused and complete local gates**

```bash
bash scripts/research-agent-smoke.sh
go test ./...
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Expected: all PASS with untruncated command exit status.

**Step 3: Review and commit the release candidate**

Review only the task diff, then stage explicit files and commit. Push the
feature branch and fast-forward `main` only if the complete gate remains green.

**Step 4: Deploy through the existing direct cutover**

Build the exact clean `main` revision as the non-root service user, verify
separate binary hashes and build revisions, create revision-scoped KBase and
evolution Worker backups, execute the existing rollback-enabled cutovers, and
update the macOS Chatlog Worker to the same revision. Do not add request
signing.

**Step 5: Run authorized real-data acceptance**

Materialize the selected published account collection, compile and publish a
new v4 package version, then start a deep Research Run requesting knowledge and
Chatlog. Verify the requested historical range, bounded Worker jobs, evidence
counts by source, actual searched/cited scope, model audit, substantive verified
conclusions, and citation re-fetch status.

**Step 6: Update the dossier and final checks**

Record only the exact revision, backup paths, run IDs, hashes, counts, timings,
outcomes, prior G6 feedback, and final gate decision. Do not copy private
article or Chatlog content.

```bash
bash scripts/privacy-smoke.sh
bash scripts/system-map-smoke.sh
git diff --check
git status --short
```

Expected: all PASS and G6 changes to PASS only after the real cross-source run
and citation re-fetch succeed.
