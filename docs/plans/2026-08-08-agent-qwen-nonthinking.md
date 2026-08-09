# Agent Qwen Non-Thinking Runtime Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Qwen 3.7 Agent Package conversation avoid provider-default thinking latency while preserving the immutable Package contract.

**Architecture:** Agent Package chat will reuse the existing model-specific configuration policy already used by book analysis. The runtime applies that policy after resolving the Package model and before calculating the cost budget; the TokenPlan client and HTTP contract remain unchanged.

**Tech Stack:** Go runtime and HTTP services, Go tests, vanilla JavaScript/Web smoke checks, systemd direct deployment.

---

### Task 1: Apply The Existing Qwen Runtime Policy

**Files:**
- Modify: `backend/app/agent_runtime_test.go`
- Modify: `backend/app/agent_runtime.go`

**Step 1: Write the failing Qwen test**

Add `TestAgentPackageRuntimeDisablesThinkingForQwenHybridModel`. Use the existing
published runtime fixture and fake model client, complete a cited response, and
require:

```go
if client.cfg.EnableThinking == nil || *client.cfg.EnableThinking {
    t.Fatalf("enable_thinking = %v, want explicit false", client.cfg.EnableThinking)
}
```

Add `TestAgentPackageRuntimeLeavesThinkingUnsetForNonQwenModel` with the Package
fallback changed to `MiniMax-M2.5`; require `client.cfg.EnableThinking == nil`.

**Step 2: Run the focused tests and verify RED**

```bash
go test ./backend/app -run 'TestAgentPackageRuntime(DisablesThinkingForQwenHybridModel|LeavesThinkingUnsetForNonQwenModel)' -count=1
```

Expected: the Qwen test fails because `EnableThinking` is nil; the non-Qwen
boundary passes.

**Step 3: Implement the minimal runtime change**

Immediately after `cfg.Model = normalizedModel` in
`chatFinalizedAgentPackageWithClient`, add:

```go
applyStructuredQwenThinkingPolicy(&cfg)
```

Do not change the Package, timeout, cost calculation, TokenPlan client, or HTTP
error mapping.

**Step 4: Run focused tests and verify GREEN**

Run the Step 2 command again, then:

```bash
go test -race ./backend/app -run 'TestAgentPackageRuntime(DisablesThinkingForQwenHybridModel|LeavesThinkingUnsetForNonQwenModel|ChatUsesPinnedEvidencePolicyAndCitations)' -count=1
```

Expected: all tests pass and the race detector reports no failure.

**Step 5: Commit**

Stage only the two Task 1 files and commit:

```text
fix(agent): disable Qwen thinking in package chat
```

### Task 2: Full Quality Gates

**Files:**
- Modify only files required by a verified task-related failure
- Modify: `docs/dossiers/2026-08-08-controlled-book-agent-wizard.md`

**Step 1: Run Go and Web gates**

```bash
go vet ./...
go test ./... -timeout=300s -count=1
node --check frontend-web/app.js
for smoke in frontend-web/scripts/*smoke*.mjs; do node "$smoke"; done
```

Expected: all commands exit zero.

**Step 2: Run desktop frontend and repository gates**

```bash
cd frontend && npm run build
node frontend/scripts/markdown-render-smoke.mjs
node frontend/scripts/book-knowledge-ui-smoke.mjs
bash scripts/system-map-smoke.sh
bash scripts/privacy-smoke.sh
git diff --check
git status --short
```

Expected: build and all checks pass; status contains only task-owned files.

**Step 3: Record local evidence**

Append the diagnosed generation-tail evidence, selected design, RED/GREEN tests,
and full local gate results to the controlled Book Agent dossier.

**Step 4: Commit the dossier**

Stage only the dossier and commit:

```text
docs(kbase): record Qwen runtime latency gates
```

### Task 3: Publish, Deploy, And Verify Production

**Files:**
- Modify only the dossier for verified deployment evidence

**Step 1: Review and publish commits**

Confirm the isolated worktree is clean, the branch contains only the approved
design, implementation, generated metadata if required, and dossier evidence.
Push the feature branch and fast-forward canonical `main`.

**Step 2: Build the exact code commit on Linux**

Create and checksum a Git archive. On the production host, use build-scoped npm
and Go caches, then run frontend build/smokes, module verification, `go vet`,
the complete Go suite, and a CGO build carrying the exact code revision.

**Step 3: Deploy with rollback protection**

Back up the current server binary and static Web directory in one batch. Replace
both atomically, restart the service, and restore both automatically unless
loopback health reports the exact code revision. Do not modify Nginx.

**Step 4: Run fresh online acceptance**

Using the authenticated production page:

- ask `注意力机制的演化` once;
- verify the form becomes busy and its button disables;
- require completion before 30 seconds with a non-empty answer and resolved
  citation;
- verify an unrelated question still abstains without a model call;
- verify browser application logs contain no new error;
- confirm public health, active service, zero restarts, and no warning-or-higher
  journal entries.

If the relevant chat still times out, stop the release loop and return to root
cause analysis; do not add retry, change model, or increase timeout implicitly.

**Step 5: Record deployment evidence**

Append exact revision, archive/binary hashes, backup batch, Linux gates, chat
latency/outcome, citations, health, and logs to the dossier. Commit and push the
dossier separately so production health continues to identify the deployed code
commit.
