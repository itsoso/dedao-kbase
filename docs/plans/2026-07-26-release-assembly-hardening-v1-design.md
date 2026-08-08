# Release Assembly Hardening v1 Design

## Context

Release Assembly is the deterministic boundary between immutable Knowledge
Releases and a future Agent Compiler. The compiler must not trust counts,
status, publication independence, or conflict edges supplied by a malformed
projection.

## Decision

Harden the existing v1 projection instead of introducing a v2 schema.
Assembly is generated on demand and has no persisted v1 artifact migration
cost. Tightening invalid-state rejection is therefore safer than adding a
second contract before the first compiler exists.

## Bounds

The builder and validator share these limits:

| Field | Limit |
| --- | ---: |
| Claims per cluster | 128 |
| Statement or normalized assertion | 4,096 Unicode code points |
| Citation IDs per claim | 128 |
| Potential conflicts per cluster | 256 |

Bounds fail the complete projection. The API never silently truncates evidence
inside a cluster because a consumer could mistake a partial cluster for a
complete evidence set.

## Relationship Validation

The validator derives rather than trusts:

- `cluster_id` from `normalized_assertion`;
- each claim's normalized assertion and polarity from its statement;
- publication and independent-publication counts from claim identities;
- potential conflicts from positive and negative claims;
- cluster status from conflicts and independent publications.

It additionally verifies:

- release IDs are unique and every visible claim points to one;
- visible claim keys and citation IDs are unique where required;
- identity basis and independence eligibility agree;
- category totals equal `summary.cluster_count`;
- returned and matched counts agree with `clusters` and `has_more`;
- visible category and claim counts never exceed full-summary counts.

Validation uses a copied claim slice and does not mutate caller data.

## Failure Model

Builder input exceeding a bound returns a deterministic internal error and the
HTTP layer keeps the generic `knowledge assembly unavailable` response.
Malformed contract payloads return precise validator errors in tests and
internal tooling. No raw source body, path, prompt, or account value is added.

## Rollout

Add focused RED tests for each bound and forged relationship, then implement
the smallest shared validator helpers. Update the JSON Schema limits and
generated system map. Run full Go, race, contract, consumer, system-map, and
privacy gates before deployment.

