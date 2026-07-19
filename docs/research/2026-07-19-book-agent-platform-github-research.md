# Book Agent Platform GitHub Research

**Reviewed:** 2026-07-19

## Research Question

Can one book become an installable agent product by combining a model, a
versioned knowledge base, and tools? The answer is yes, provided the product is
generated from a shared runtime rather than implemented as a code fork. A book
is a useful knowledge boundary, but it is not automatically current, complete,
unbiased, authoritative, or safe for high-risk decisions.

## Projects Reviewed

| Project | Useful capability | Adoption decision |
|---|---|---|
| [RAGFlow](https://github.com/infiniflow/ragflow) | Document understanding, explainable chunking, grounded citations | Reuse its ingestion and citation patterns; do not replace KBase |
| [LlamaIndex](https://github.com/run-llama/llama_index) | Connectors, structured indexing, retrieval, document agents | Evaluate selected parsing and retrieval components |
| [GraphRAG](https://github.com/microsoft/graphrag) | Entity and relationship extraction across unstructured sources | Apply selectively to cross-source concepts and contradictions because indexing is expensive |
| [LangGraph](https://github.com/langchain-ai/langgraph) | Durable, stateful workflows with explicit interruption points | Preferred runtime candidate for multi-step agents |
| [Haystack](https://github.com/deepset-ai/haystack) | Transparent retrieval, routing, memory, tools, REST and MCP serving | Runtime alternative if component-level control proves more important than graph persistence |
| [Model Context Protocol](https://github.com/modelcontextprotocol/modelcontextprotocol) | Standard tools, resources and prompts | Use as the external tool boundary, with an independent policy gate |
| [DeepEval](https://github.com/confident-ai/deepeval) | RAG faithfulness, retrieval and agent tool-use metrics | Use for release-blocking regression tests |
| [Ragas](https://github.com/vibrantlabsai/ragas) | Dataset-driven RAG evaluation | Use for retrieval experiments and golden-set scoring |
| [Phoenix](https://github.com/Arize-ai/phoenix) | Traces, versioned datasets, experiments and prompt comparison | Use for runtime observability and replay |
| [Dify](https://github.com/langgenius/dify) | Fast visual RAG and agent prototypes | Optional prototyping console only; its modified license requires separate review for multi-tenant or rebranded use |

## Findings

No reviewed repository is a complete system of record for authorized source
collection, immutable evidence releases, agent execution, domain review, and
consumer feedback. Replacing KBase with a single framework would discard
working provenance and release controls while adding platform coupling.

The recurring production pattern is separation of concerns:

1. Compile heterogeneous sources into traceable context.
2. Publish immutable, versioned knowledge artifacts.
3. Execute stateful agent workflows against pinned artifacts.
4. Isolate tools behind typed and auditable contracts.
5. evaluate retrieval, answer grounding, task completion, and tool arguments.
6. Feed observed gaps and conflicts back into the knowledge pipeline.

## Product Implications

The deployable unit should be an **Agent Package**, not a book-specific source
tree. One package can represent one book, a book plus selected articles, or a
reviewed domain collection. Product shells should resolve the package manifest
at runtime and share authentication, retrieval, telemetry, and UI components.

Paid or account-scoped sources must retain access and usage policy. Downstream
systems should receive bounded transformed evidence and resolvable provenance,
not unrestricted source bodies. High-risk consumers must add domain review and
must abstain when evidence is stale, conflicting, or insufficient.
