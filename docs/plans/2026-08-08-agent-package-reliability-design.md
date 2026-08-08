# Agent Package Reliability Design

## Context

Production testing of the immutable Agent Package
`attention-mechanism-research-assistant@1.0.1` exposed four repository-owned
problems:

1. the desktop-width page can scroll hundreds of pixels horizontally because
   the ten-item metric strip is forced into one flex row;
2. a natural Chinese query such as `注意力机制的演化` returns no evidence even
   though the package contains closely matching Chinese claims;
3. an upstream model timeout is returned as a raw internal URL and Go error;
4. search and chat can be submitted repeatedly, and a late response can update
   a different Agent route.

The page also requests a missing `/favicon.ico`. The MetaMask,
`ObjectMultiplex`, and listener warnings shown by the browser are injected by a
browser extension and do not occur in the application console, so this change
does not add application workarounds for them.

## Decisions

- Keep Agent Package version 1.0.1 immutable. The repair applies to the shared
  runtime and Web shell, not to published package content.
- Keep the existing lexical retrieval contract. When an initial lexical search
  for an unspaced Han query returns no result, retry with derived Han bigrams
  and retain only evidence matching multiple terms.
- Share that natural-language fallback between grounded search and grounded
  conversation so the two interfaces retrieve consistently.
- Preserve the package's 30-second model timeout. Convert deadline expiry into
  a stable retryable HTTP 504 response without exposing provider endpoints.
- Permit only one active search or chat action. Sequence each action and bind
  its completion to the package/version route that started it.
- Show loading, zero-result, answer, and error feedback beside the relevant
  search or conversation control instead of only in the page-level banner.
- Replace the rigid metric flex row with a responsive grid and add an inline SVG
  favicon. No Nginx configuration changes are required.

## Retrieval Flow

The shared retrieval helper first executes the package's configured strategy
unchanged. If that produces evidence, it returns immediately. Only an empty
lexical result containing Han text enters the fallback. The fallback derives
deduplicated Han bigrams, searches the same pinned release, and filters results
to evidence matching at least two derived terms. Exact identifiers, English
queries, vector strategies, release pinning, and evidence limits remain
unchanged.

## UI State And Concurrency

Search and conversation receive separate local status fields plus one active
action marker. Starting an action increments a monotonic sequence and captures
the current package/version route. Buttons are disabled while the action is in
flight. A response may update the page only when both its sequence and captured
route still match. Loading another Agent route or leaving the Agent surface
invalidates pending actions.

The search panel distinguishes its initial prompt from an executed query with
zero results. The conversation panel distinguishes a pending response, a
grounded answer or abstention, and a request error. Existing global page-load
messages remain reserved for failures that prevent the Agent page itself from
loading.

## Error Handling

`context.DeadlineExceeded` from Agent chat maps to HTTP 504 with the public
message `agent model timed out; please retry`. Other validation, not-found, and
unexpected errors retain their existing status mapping. The server still logs
operational context through its normal path; the HTTP body does not disclose
the upstream model URL.

## Verification

The change is accepted only when:

- a Go runtime test proves natural Chinese grounded search returns evidence;
- an HTTP test proves model timeout returns 504 and no internal endpoint or raw
  deadline text;
- the Web smoke checks the responsive grid, inline favicon, local action
  statuses, disabled controls, and stale-response guard;
- all existing Go, frontend Web, desktop frontend, system-map, privacy, and
  whitespace gates pass;
- production browser checks at 1163, 760, and 401 pixels show no horizontal
  overflow, Chinese natural search returns evidence, unrelated search returns
  a clear zero-result state, and navigation cannot be overwritten by a stale
  action.

## Out Of Scope

- republishing or modifying Agent Package 1.0.1;
- increasing the model timeout or hiding genuine provider availability issues;
- suppressing errors emitted by browser extensions;
- changing authentication, Nginx, or the controlled publication protocol.
