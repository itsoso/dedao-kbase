# Product Map

KBase is the authoring and release control plane for collected knowledge. Local
agents collect from source systems and upload bounded packages; KBase normalizes,
analyzes, verifies, publishes immutable releases, and records downstream
delivery receipts.

Current product areas:

- **Sources:** Dedao, WeChat, and WC Plus ingestion surfaces, including a
  unified operator overview and stable worker details over independent local
  workers. The shared protocol covers heartbeat, desired state, bounded
  commands, leases, outbox truth, and constrained artifact upgrades; macOS is
  the only installed-worker platform delivered in the first release.
- **Book Knowledge:** package extraction, search, TokenPlan study prompts,
  structured analysis, quality checks, NotebookLM export, and MCP surfaces.
- **Release Governance:** explicit publish, feedback, reverification, and
  immutable release records.
- **Consumer Delivery:** private HTTP APIs for health and future consumers,
  with release feed and receipt contracts added incrementally.

For structural inventory, read `docs/_generated/system-map.json`. This page is
only a product orientation layer; it intentionally avoids copied architecture
counts so the generated map remains the only count source.
