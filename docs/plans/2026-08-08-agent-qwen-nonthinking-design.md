# Agent Qwen Non-Thinking Runtime Design

## Context

Production grounded conversation for the immutable Agent Package
`attention-mechanism-research-assistant@1.0.1` intermittently reaches its
30-second model deadline. The public timeout response is already safe, but the
Agent cannot answer reliably.

Production evidence isolates the delay to model generation:

- the server reaches the TokenPlan endpoint and receives an unauthenticated
  response in about one tenth of a second;
- an authenticated minimal `qwen3.7-max` request completes in 0.61 seconds when
  `enable_thinking` is explicitly false and in 1.14 seconds with the provider
  default;
- Agent traces include one five-evidence completion in 6.5 seconds and several
  five-evidence failures at the exact 30-second package deadline;
- ordinary book analysis already applies the repository's Qwen 3.7
  non-thinking policy, while Agent Package chat does not.

The evidence indicates an intermittent generation-tail problem, not DNS, TCP,
TLS, authentication, retrieval, or page-state failure.

## Decision

After selecting the Package model, Agent Package chat will call the existing
`applyStructuredQwenThinkingPolicy` helper. For the current Qwen 3.7 hybrid
model this sets `BookTokenPlanConfig.EnableThinking` to false, causing the
existing TokenPlan client to send `"enable_thinking": false`.

This is deliberately a runtime correction rather than a Package mutation:

- Package version 1.0.1, its content hash, evaluation, model identity, release
  pins, and publication state remain immutable;
- the selected model remains `qwen3.7-max`;
- the 30-second timeout and cost ceiling remain unchanged;
- non-Qwen models keep their existing configuration;
- retrieval, citations, abstention, traces, and HTTP behavior remain unchanged.

## Alternatives Rejected

- Publishing a new Package with a 60-second timeout preserves provider-default
  thinking but worsens user latency and creates a new immutable version for a
  runtime integration inconsistency.
- Retrying the model call can duplicate billing and exceed the Package's maximum
  cost. A timeout also leaves no safe time for a second attempt inside the
  current deadline.
- Switching to an undeclared fallback model would violate Package model policy.
- Streaming is a larger protocol and parsing change without evidence that it is
  necessary after non-thinking mode is applied.

## Error Handling

The existing HTTP 504 mapping and stable retry message remain in place for real
provider outages. This fix reduces avoidable long-tail generation; it does not
pretend that an unavailable provider succeeded.

## Verification

Acceptance requires:

- a RED/GREEN runtime test proving Qwen 3.7 Agent chat passes an explicit false
  `EnableThinking` value to the model client;
- a companion test proving a non-Qwen Package remains unset;
- existing Agent retrieval, citations, abstention, timeout, and HTTP tests;
- complete Go, Web, frontend build, system-map, privacy, and whitespace gates;
- a clean Linux build from the exact commit;
- production grounded conversation completing with citations before the
  Package deadline, followed by health, restart-count, and journal checks.

## Out Of Scope

- changing or republishing Agent Package 1.0.1;
- automatic retries, model fallback, streaming, or timeout increases;
- changing TokenPlan credentials, endpoint, proxy, or Nginx configuration;
- suppressing future genuine provider failures.
