# Release Assembly Hardening v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Release Assembly payloads bounded and prove their cross-field
relationships before Agent Compiler consumption.

**Architecture:** Keep `knowledge_release_assembly.v1` and its read-only API.
Add shared hard limits to the builder and validator, then recompute cluster
identity, counts, status, and conflict edges from copied claims instead of
trusting serialized fields.

**Tech Stack:** Go, JSON Schema, existing JSON artifact store, `net/http`.

---

### Task 1: Enforce Cluster Payload Bounds

**Files:**
- Modify: `backend/app/knowledge_assembly_test.go`
- Modify: `backend/app/knowledge_assembly.go`

**Step 1: Write failing tests**

Add table-driven builder and validator cases for:

- 129 claims in one cluster;
- a 4,097-rune statement;
- 129 citation IDs on one claim;
- more than 256 generated conflict edges.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestBuildKnowledgeReleaseAssemblyRejectsOversizedCluster|TestValidateKnowledgeReleaseAssemblyRejectsOversizedCluster' -count=1
```

Expected: FAIL because the limits do not exist.

**Step 3: Implement minimal bounds**

Add shared constants and check statements/citations before claim insertion,
claim count before append, and conflict count before append. Return bounded,
deterministic errors.

**Step 4: Verify GREEN**

Run the focused command again. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_assembly.go backend/app/knowledge_assembly_test.go
git commit -m "fix(kbase): bound assembly clusters"
```

### Task 2: Enforce Derived Relationships

**Files:**
- Modify: `backend/app/knowledge_assembly_test.go`
- Modify: `backend/app/knowledge_assembly.go`

**Step 1: Write failing tests**

Start from a valid complete Assembly and forge one field at a time:

- unknown claim release ID;
- mismatched cluster ID or normalized assertion;
- duplicate visible claim key or citation ID;
- forged publication counts or eligibility;
- forged status or conflict edge;
- inconsistent summary category totals;
- inconsistent returned, matched, and `has_more` values.

Assert that validation does not mutate the supplied Assembly.

**Step 2: Verify RED**

Run:

```bash
go test ./backend/app -run 'TestValidateKnowledgeReleaseAssemblyRelationships|TestValidateKnowledgeReleaseAssemblyDoesNotMutateInput' -count=1
```

Expected: FAIL because the current validator trusts derived values.

**Step 3: Implement minimal derivation**

Validate release membership and uniqueness. Copy the claim slice, clear derived
fields, call the deterministic finalizer, and compare cluster ID, assertion,
counts, status, and conflicts. Check summary/category/pagination relationships.

**Step 4: Verify GREEN**

Run the focused command again. Expected: PASS.

**Step 5: Commit**

```bash
git add backend/app/knowledge_assembly.go backend/app/knowledge_assembly_test.go
git commit -m "fix(kbase): verify assembly relationships"
```

### Task 3: Align Contract And Architecture Metadata

**Files:**
- Modify: `contracts/knowledge-release-assembly-v1.schema.json`
- Modify: `backend/app/knowledge_contract_test.go`
- Modify: `docs/contracts/knowledge-supply-v1.md`
- Modify: `docs/_generated/system-map.json`

**Step 1: Write a failing contract test**

Assert the schema carries `maxItems` for claims, citation IDs, and conflicts,
plus `maxLength` for statement and normalized assertion.

**Step 2: Verify RED**

```bash
go test ./backend/app -run TestKnowledgeReleaseAssemblySchemaCarriesHardLimits -count=1
```

Expected: FAIL because those schema limits are absent.

**Step 3: Update schema and docs**

Use the same numeric constants documented in the PRD. Regenerate the system
map:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
```

**Step 4: Verify GREEN**

Run the focused test and:

```bash
bash scripts/system-map-smoke.sh
```

Expected: PASS.

**Step 5: Commit**

```bash
git add contracts/knowledge-release-assembly-v1.schema.json backend/app/knowledge_contract_test.go docs/contracts/knowledge-supply-v1.md docs/_generated/system-map.json
git commit -m "docs(kbase): harden assembly contract"
```

### Task 4: Run Delivery Gates And Roll Out

**Files:**
- Modify: `docs/dossiers/2026-07-26-release-assembly-hardening-v1.md`

**Step 1: Run Gate 3**

Run full Go, race, vet, module, Vue build, contract, evaluation, Proof
consumer, system-map, Source Agent packaging, privacy, and whitespace checks.

**Step 2: Run Gate 4**

Review backward compatibility, denial-of-service bounds, relationship
completeness, input immutability, and privacy. Remediate P1/P2 findings before
continuing.

**Step 3: Update the dossier**

Record exact revisions, results, accepted residual risks, and rollback.

**Step 4: Merge and deploy**

Fast-forward canonical `main`, build the exact archive on Linux, deploy with a
binary/static rollback snapshot, and verify service health.

**Step 5: Verify Gate 6**

Through public HTTPS, prove authenticated Assembly still returns the production
snapshot, unauthenticated access returns 401, and the payload passes bounds,
relationship, and privacy checks.

