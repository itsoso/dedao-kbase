# Agent Compiler v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Compile deterministic dual, evidence, and study Agent Package candidates from strict Release Assembly state.

**Architecture:** Add a pure profile-driven compiler in `backend/app` and keep compilation read-only. The compiler emits finalized package candidates plus bounded diagnostics; existing trusted evaluation and publication functions remain the only persistence path. A thin publisher-authenticated HTTP route and Book Agents preview panel expose the capability.

**Tech Stack:** Go, `net/http`, JSON Schema, existing KBase Release/Assembly/Agent Package contracts, vanilla Web frontend, Node static smoke tests.

---

### Task 1: Define Compilation Contracts

**Files:**
- Create: `backend/app/agent_compiler.go`
- Create: `backend/app/agent_compiler_test.go`
- Create: `contracts/agent-compilation-request-v1.schema.json`
- Create: `contracts/agent-compilation-v1.schema.json`
- Modify: `backend/app/knowledge_contract_test.go`

**Step 1: Write failing contract tests**

Add tests that require:

```go
const (
    AgentCompilationRequestSchemaVersion = "agent-compilation-request.v1"
    AgentCompilationSchemaVersion = "agent-compilation.v1"
    AgentCompilerVersion = "deterministic-agent-compiler.v1"
)
```

Test valid `dual`, `evidence`, and `study` requests; reject unknown modes,
blank primary Release IDs, invalid versions, duplicate support IDs, more than
16 support IDs, and unknown JSON fields in the HTTP task later.

Test response invariants: at most two ordered candidates, unique candidate
kinds, ready candidates require a finalized package, blocked candidates require
bounded issue codes, and overall status must agree with candidate statuses.

**Step 2: Run tests to verify RED**

Run:

```bash
go test ./backend/app -run 'AgentCompilation|AgentCompilerContract' -count=1
```

Expected: FAIL because compilation contracts do not exist.

**Step 3: Implement contract types and pure validation**

Define:

```go
type AgentCompilationRequest struct {
    SchemaVersion       string   `json:"schema_version"`
    Mode                string   `json:"mode"`
    PrimaryReleaseID    string   `json:"primary_release_id"`
    SupportingReleaseIDs []string `json:"supporting_release_ids,omitempty"`
    Version             string   `json:"version"`
}

type AgentCompilation struct {
    SchemaVersion   string                      `json:"schema_version"`
    CompilerVersion string                      `json:"compiler_version"`
    CompilationID   string                      `json:"compilation_id"`
    Mode            string                      `json:"mode"`
    AssemblyID      string                      `json:"assembly_id"`
    ReleaseIDs      []string                    `json:"release_ids"`
    Status          string                      `json:"status"`
    Candidates      []AgentCompilationCandidate `json:"candidates"`
}
```

Use stable constants for modes, statuses, candidate kinds, issue codes, and
next actions. Bound diagnostic strings to 256 Unicode code points. Add strict
JSON Schema files and schema-presence/limit tests.

**Step 4: Run tests to verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_compiler.go backend/app/agent_compiler_test.go backend/app/knowledge_contract_test.go contracts/agent-compilation-request-v1.schema.json contracts/agent-compilation-v1.schema.json
git commit -m "feat(kbase): define agent compiler contract"
```

### Task 2: Implement A - Dual Compilation

**Files:**
- Modify: `backend/app/agent_compiler.go`
- Modify: `backend/app/agent_compiler_test.go`

**Step 1: Write failing dual-mode tests**

Create two valid immutable Releases and assert:

- dual mode returns candidates in `study`, `evidence` order;
- study is ready;
- evidence is ready with explicit independent support;
- one Assembly snapshot identity is shared by both candidates;
- recompilation yields identical compilation ID and package hashes;
- input request and loaded Releases are not mutated;
- when support is absent, overall status is `partial`, study is ready, and
  evidence is blocked with `supporting_release_required`.

**Step 2: Run tests to verify RED**

```bash
go test ./backend/app -run 'TestCompileAgentPackagesDual' -count=1
```

Expected: FAIL because the compiler does not build candidates.

**Step 3: Implement shared compiler orchestration**

Add:

```go
func CompileAgentPackages(
    store *BookKnowledgeStore,
    request AgentCompilationRequest,
) (*AgentCompilation, error)
```

Build the unfiltered Assembly once, require the primary Release to be a member,
load and compatibility-adapt selected immutable Releases, invoke fixed study
and evidence profile builders, validate every ready package with
`ValidateAgentPackage`, then derive the compilation ID from compiler version,
Assembly ID, normalized request, candidate status, issues, and package hashes.

No store write is allowed.

**Step 4: Run tests to verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_compiler.go backend/app/agent_compiler_test.go
git commit -m "feat(kbase): compile dual agent candidates"
```

### Task 3: Implement B - Evidence Compilation

**Files:**
- Modify: `backend/app/agent_compiler.go`
- Modify: `backend/app/agent_compiler_test.go`

**Step 1: Write failing evidence-mode tests**

Cover:

- explicit independent support compiles `agent-package.v2`;
- support outside Assembly is blocked;
- primary repeated as support is rejected;
- support with ineligible or same publication identity is blocked;
- automatic support is selected only from a shared assertion or conflict
  cluster with an independently eligible publication identity;
- unrelated Releases are never auto-selected;
- all claim citation allowlists are unique, sorted, non-empty, and resolve to
  the pinned Release;
- package evidence roles contain exactly one primary and at least one support.

**Step 2: Run tests to verify RED**

```bash
go test ./backend/app -run 'TestCompileAgentPackagesEvidence' -count=1
```

Expected: FAIL on evidence selection and profile assertions.

**Step 3: Implement evidence selection and fixed profile**

Use Assembly claim refs as the only source of publication identity and automatic
relationship evidence. Explicit support may be unrelated but must be in the
Assembly and independently eligible with a different identity from the primary.

Build a fixed v2 package:

- lexical retrieval with citations and eight context chunks;
- model capability `reasoning`, fallback `qwen3.7-max`;
- evidence-audit prompt/output profile;
- all read-only Book MCP tools;
- evidence-only safety and abstention;
- existing evidence metrics with fixed thresholds;
- bounded evidence policy and evidence UI capabilities.

**Step 4: Run tests to verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_compiler.go backend/app/agent_compiler_test.go
git commit -m "feat(kbase): compile evidence agent candidates"
```

### Task 4: Implement C - Study Compilation

**Files:**
- Modify: `backend/app/agent_compiler.go`
- Modify: `backend/app/agent_compiler_test.go`

**Step 1: Write failing study-mode tests**

Cover:

- one latest Assembly Release compiles `agent-package.v1`;
- package ID is opaque, stable, and ends in `-study`;
- only citations referenced by analysis claims are pinned;
- source type and citation allowlists are deterministic;
- model fallback is `qwen3.7-max`;
- all tools are read-only;
- missing analysis citations block compilation;
- superseded or non-Assembly primary Releases are blocked.

**Step 2: Run tests to verify RED**

```bash
go test ./backend/app -run 'TestCompileAgentPackagesStudy' -count=1
```

Expected: FAIL on study profile assertions.

**Step 3: Complete the study profile**

Use fixed grounded-answer prompts, standard usage, explicit abstention, and
reader/search/chat/evidence/quiz UI capabilities. Derive the opaque package ID
from the primary book ID and omit timestamps.

**Step 4: Run tests to verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/agent_compiler.go backend/app/agent_compiler_test.go
git commit -m "feat(kbase): compile study agent candidates"
```

### Task 5: Expose Publisher-Authenticated Compile API

**Files:**
- Modify: `backend/app/kbase_http.go`
- Modify: `backend/app/kbase_http_test.go`

**Step 1: Write failing HTTP tests**

Require:

- `POST /api/agent-packages/compile`;
- publisher token succeeds;
- normal API, consumer, missing, and wrong tokens return `401`;
- methods other than POST return `405`;
- body above 64 KiB, unknown fields, trailing JSON, and invalid requests return
  `400`;
- blocked compilation returns `200`;
- internal store failures return generic `agent compilation unavailable`
  without paths or raw errors.

**Step 2: Run tests to verify RED**

```bash
go test ./backend/app -run 'TestKBaseHTTPHandlerAgentCompilation' -count=1
```

Expected: FAIL with route not found.

**Step 3: Implement the thin handler**

Add the compile path to the dedicated publisher-auth branch. Decode with
`http.MaxBytesReader`, `DisallowUnknownFields`, and a trailing-token check.
Call the pure compiler and return the contract without persistence.

**Step 4: Run tests to verify GREEN**

Run the focused command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/kbase_http.go backend/app/kbase_http_test.go
git commit -m "feat(api): expose agent compiler"
```

### Task 6: Add Book Agents Compiler Preview

**Files:**
- Modify: `frontend-web/app.js`
- Modify: `frontend-web/styles.css`
- Modify: `frontend-web/scripts/evidence-audit-agent-smoke.mjs`

**Step 1: Write failing static UI smoke assertions**

Require mode controls for dual/evidence/study, primary and supporting Release
selectors, version input, compile command, candidate status/issues, and trusted
evaluation next-action copy. Assert there is no direct publish shortcut.

**Step 2: Run smoke to verify RED**

```bash
node frontend-web/scripts/evidence-audit-agent-smoke.mjs
```

Expected: FAIL because the compiler panel is absent.

**Step 3: Implement compact compiler panel**

Reuse the current Book Agents workspace and API helper. Keep the package list
and selected package stable. Render ready and blocked candidates near the
compile controls; do not send package content to storage or local persistence.
Use existing segmented controls and restrained workspace styling.

**Step 4: Run smoke to verify GREEN**

Run the command from Step 2. Expected: PASS.

**Step 5: Commit**

```bash
git add frontend-web/app.js frontend-web/styles.css frontend-web/scripts/evidence-audit-agent-smoke.mjs
git commit -m "feat(web): preview compiled agent candidates"
```

### Task 7: Complete Gates, Merge, And Deploy

**Files:**
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Modify: `README.md`
- Regenerate: `docs/_generated/system-map.json`
- Modify: `docs/dossiers/2026-07-26-agent-compiler-v1.md`

**Step 1: Update contracts and generated architecture**

Document compiler modes, read-only semantics, authentication, blocked status,
and trusted evaluation boundary. Regenerate:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
```

**Step 2: Run Gate 3**

Run:

```bash
go test ./... -count=1
go test -race ./backend/app ./cmd/kbase-server -count=1
go vet ./...
go mod verify
cd frontend && npm run build
bash scripts/knowledge-contract-smoke.sh
bash scripts/knowledge-eval-smoke.sh
bash scripts/proof-consumer-contract-smoke.sh
bash scripts/source-agent-packaging-smoke.sh
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
```

Run every existing desktop and Web Node smoke. Expected: all pass; only existing
frontend dependency warnings are accepted.

**Step 3: Run Gate 4**

Review for:

- deterministic output and map-order independence;
- no store mutation;
- no publisher/evaluation bypass;
- automatic support never infers semantic relevance;
- request, candidate, issue, and string bounds;
- generic HTTP internals and recursive privacy safety.

Fix every P1/P2 finding with RED/GREEN tests.

**Step 4: Commit gate evidence**

```bash
git add README.md docs/contracts/knowledge-supply-v1.md docs/_generated/system-map.json docs/dossiers/2026-07-26-agent-compiler-v1.md
git commit -m "docs(kbase): record agent compiler gates"
```

**Step 5: Fast-forward canonical main and Linux preflight**

Fetch canonical `main`, require the feature branch to remain a direct
fast-forward, push it, archive the exact revision, verify its SHA256, and repeat
Gate 3 on Linux. Run a read-only compiler probe against production Releases
before replacing the service.

**Step 6: Deploy with automatic rollback**

Snapshot the current binary and `frontend-web`, deploy only the exact preflight
binary and static source, restart, and automatically restore the snapshot if
health fails.

**Step 7: Complete Gate 5/6**

Verify:

- service active, zero restarts, successful local/public health;
- compile endpoint rejects unauthenticated and normal API credentials;
- authenticated study compilation is ready for a production Release;
- dual compilation is ready or honestly partial based on support evidence;
- repeated compilation returns identical IDs and hashes;
- no package, evaluation, or Release store changed;
- recursive output privacy and bounds pass;
- logs contain no panic, fatal, or failed request.

Update the dossier with exact code revision, archive/binary hashes, rollback
snapshot, and online contract results. Commit and push the docs-only rollout
record.
