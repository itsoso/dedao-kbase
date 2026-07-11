# KBase WeChat Collector Delivery Dossier

**Status:** G1-G5 PASS; G6 BLOCKED pending explicit local enrollment and an
operator-selected public account.

## Scope and safety boundaries

The delivered slice provides a first-party local source Agent, Keychain-backed
MP session storage, loopback-only QR enrollment, account history discovery,
bounded article and media acquisition, durable outbox replay, idempotent KBase
ingestion, private asset storage, typed capability health, and a collector-first
Web control plane. WC Plus remains available only as a compatibility path.

The implementation excludes interception, CA installation, process injection,
captcha bypass, proxy or account pools, and rate-limit evasion. Scheduling is
not enabled by this delivery record.

## Gate decisions

- **G1 Admission — PASS:** approved design and implementation plan define the
  credential boundary, non-goals, and acceptance criteria.
- **G2 Feasibility and risk — PASS:** MP credentials remain local; remote MP
  traffic requires HTTPS except for loopback tests; direct articles and media
  enforce host, DNS, redirect, MIME, and byte policies; local mutation routes
  enforce loopback Origin and CSRF.
- **G3 Test — PASS:** `go test ./... -count=1`, `go vet ./...`, race tests for
  backend, server, and source Agent, all collector/WC Plus/book Web smokes,
  JavaScript syntax, packaging smoke, privacy smoke, and diff checks passed
  with fail-fast shell execution.
- **G4 Review — PASS:** reviewed auth separation, Keychain stdin handling,
  diagnostic redaction, lease ownership, asset path safety, cursor behavior,
  outbox ordering, idempotency, and WC Plus compatibility. Review findings for
  the enrollment command, run loop, and insecure remote MP base URLs were
  fixed with regression tests.
- **G5 Deployment health — PASS:** source commit:
  `7622651`; source archive SHA-256:
  `6e6683303bfd5c5ec9284f7086bddf484c26a3cd87d99a30873095251d34ebd5`;
  native arm64 Agent SHA-256:
  `04da10458fa84208d8d84220de92ffcbf1d31629214e0aebe888963daa8ac6fd`;
  Linux x86-64 server SHA-256:
  `f42fac1cfd0be16def88e93b55d43c0623d8d77633f9959790d049fcfc8c37bd`.
  Production service and public health are active; deployed static asset
  hashes match the checked source. Unauthenticated admin, source-agent, and
  browser-session probes return `401`; authenticated admin and dedicated Agent
  probes pass. Rollback backups are `20260711222021` and `20260711224222`.
  The native Agent runs as `source-agent-mac-1`, stores its transport token in
  Keychain, reports all three WeChat capabilities, and serves the loopback
  enrollment page. The legacy WC Plus LaunchAgent and its plaintext token plist
  were removed while its state and logs were preserved.
- **G6 Online validation — BLOCKED:** requires operator interaction and account
  selection. A real QR can be generated and loaded, but no MP session has been
  confirmed yet. Scheduling remains disabled.

## Bounded G6 probe

After the operator explicitly enrolls locally and selects an account:

1. verify healthy `wechat_mp` capability without exposing session material;
2. search the selected account;
3. create a manual subscription with `max_items=1`;
4. run `discover_articles`, then `sync_articles`;
5. verify one authenticated knowledge detail and chunk citation;
6. replay and verify one skip with no duplicate document or asset;
7. restart the Agent and verify cursor and outbox recovery;
8. keep scheduling disabled unless every check passes.

Any login, throttling, verification, media, authentication, or ingestion
failure keeps G6 blocked and returns the work to the owning task.

## Rollback

Disable the subscription, unload the source Agent LaunchAgent, restore the
previous server/static release, and retain the local outbox and imported data.
The uninstall command preserves state unless `--purge-state` is explicitly
provided. WC Plus compatibility fields and entrypoint remain available.
