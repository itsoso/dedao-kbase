# System Map

This directory describes the current KBase architecture from generated source
inventory. Do not hand-maintain route, command, operation, or durable-object
counts in narrative docs.

The source of truth is:

- `docs/_generated/system-map.json`
- generator: `cmd/system-map`
- drift check: `bash scripts/system-map-smoke.sh`

When changing backend command surfaces, HTTP routes, source adapter operations,
or durable knowledge objects, regenerate the map:

```bash
go run ./cmd/system-map --root . --out docs/_generated/system-map.json
bash scripts/system-map-smoke.sh
```

The generated map records only structural metadata: relative file paths, line
numbers, route literals, operation names, and type names. It must not contain
downloaded content, prompts, tokens, cookies, or machine-specific absolute
paths.
