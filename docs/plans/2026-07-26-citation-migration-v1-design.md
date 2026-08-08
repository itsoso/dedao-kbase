# Citation Migration v1 Design

## Decision

The user requested the next roadmap step after Knowledge Foundation v2.
Implement citation-native generation for new packages and new analysis without
silently mutating the existing corpus.

## Considered Approaches

### Prompt-only

Tell the model to return citation IDs while continuing to expose chunk IDs.
This is small but internally contradictory and cannot guarantee valid output.

### Automatic corpus backfill

Generate missing citations whenever analysis runs and save the changed package.
This improves coverage quickly but hides a data mutation inside a model action
and can change evidence identity without operator review.

### Adapter and analysis boundary migration

Generate complete citations at normalization time, expose those citations to
structured analysis, and reject non-citation references in newly generated
analysis. Existing packages remain in compatibility mode until an explicit
migration job handles them.

This is the selected approach because it is deterministic, additive, and
honest about legacy state.

## Architecture

```text
Source HTML
  -> chapter/chunk extraction
  -> one citation per chunk
  -> citation-aware analysis context
  -> TokenPlan structured JSON
  -> citation allowlist validation
  -> quality/evidence graph
  -> immutable release
```

The existing package schema remains unchanged. A citation continues to bind a
book, chapter, and chunk. The change is behavioral:

- HTML extraction creates a citation beside each chunk.
- Analysis context renders selected chunks as `Evidence [citation:<id>]`.
- `BookKnowledgeChatSource` entries for analysis use `kind=citation`.
- `structured-v2-citations` output may reference only IDs present in
  `pkg.Citations`.

Ordinary book chat keeps its existing chapter, claim, and chunk source labels;
only the structured knowledge-production path becomes strict.

## Deterministic Identity

HTML-extracted citations use `<chunk_id>-citation`. This avoids sequence shifts
when a chapter gains more chunks and directly reflects the bound evidence
object. Source-ingest adapters already emit one citation per chunk and retain
their existing IDs for compatibility.

## Legacy Packages

When a selected legacy chunk lacks a citation, analysis context labels it
`Legacy Chunk` and states that it cannot be used in `citation_ids`. If the
model nevertheless returns that chunk ID, generation fails with a bounded
validation error and preserves the previous successful analysis payload.

This failure is intentional: it routes the package to citation migration
instead of presenting compatibility evidence as explicit evidence.

## Failure Handling

- Missing package citation: structured generation fails before quality.
- Unknown or direct object reference: structured generation fails with the
  offending field and bounded ID.
- Model or parse failure: preserve the prior answer and payload using the
  existing manifest behavior.
- Existing releases: remain readable and unchanged.

## Testing

- Extractor test with one chapter large enough to produce multiple chunks;
  assert one unique citation per chunk and full claim coverage.
- Context test proving citation labels and citation sources are emitted.
- Generation test proving citation-native output becomes ready.
- Generation test proving direct chunk references fail closed and preserve the
  previous payload.
- Existing full quality, evidence, contract, race, system-map, and privacy
  suites.

## Rollback

Revert the prompt-version, citation-aware context, output allowlist validation,
and HTML extractor behavior. No migration or artifact rewrite is required.
