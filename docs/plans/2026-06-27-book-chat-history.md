# Book Chat History Implementation Plan

**Goal:** Persist every completed book analysis/chat response and show per-book history in the `书籍知识库` chat tab.

**Design:** Keep the existing book knowledge JSON/JSONL package format unchanged. Add a small SQLite database under the book knowledge root for chat history only. The backend writes a history row after TokenPlan returns successfully, then returns the persisted `history_id` to the UI. The frontend lists recent history for the selected book and can restore any previous answer into the current answer panel.

**Tasks:**

1. Add backend tests for persisted chat history.
2. Add a SQLite-backed history store with migration, insert, and list methods.
3. Persist from `BookKnowledgeChatWithClient` after a successful LLM response.
4. Expose `BookKnowledgeChatHistory` through Wails.
5. Add a chat history sidebar in `BookKnowledge.vue`.
6. Verify with Go tests, UI smoke, frontend build, and Wails build.
