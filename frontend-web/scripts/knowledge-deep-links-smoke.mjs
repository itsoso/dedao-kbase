import assert from "node:assert/strict";

await import("../knowledge-deep-links.js");

const {
  knowledgeBookForRoute,
  knowledgeBookPath,
  knowledgeChapterPath,
  knowledgeResourceFromPath,
  knowledgeResourceTargetSelector,
  knowledgeResultFromPackage,
  knowledgeResultPath,
  knowledgeResultRouteID,
  knowledgeSearchIsCurrent,
  knowledgeVisibleChapters,
  shouldHandleKnowledgeClick,
} = globalThis.KnowledgeDeepLinks;

assert.equal(knowledgeBookPath("book / 1"), "/knowledge/packages/book%20%2F%201");
assert.equal(
  knowledgeChapterPath("book-1", "chapter/2"),
  "/knowledge/packages/book-1/chapters/chapter%2F2",
);
assert.equal(
  knowledgeResultPath("book-1", "claim", "claim/3"),
  "/knowledge/packages/book-1/results/claim/claim%2F3",
);

assert.deepEqual(
  knowledgeResourceFromPath("/knowledge/packages/book%20%2F%201/chapters/chapter%2F2"),
  { type: "chapter", bookID: "book / 1", resourceID: "chapter/2", kind: "chapter" },
);
assert.deepEqual(
  knowledgeResourceFromPath("/knowledge/packages/book-1/results/claim/claim%2F3"),
  { type: "result", bookID: "book-1", resourceID: "claim/3", kind: "claim" },
);
assert.equal(knowledgeResourceFromPath("/knowledge/packages/book-1/unknown/value"), null);
assert.equal(
  knowledgeBookForRoute([{ book_id: "book-1" }], { bookID: "missing" }),
  null,
  "an invalid route must not silently select the first book",
);

assert.equal(knowledgeResultRouteID({ chunk_id: "chunk-1" }), "chunk-1");
assert.equal(knowledgeResultRouteID({ claim_id: "claim-1" }), "claim-1");
assert.equal(knowledgeResultRouteID({ citation_id: "citation-1" }), "citation-1");

const pkg = {
  chapters: [{ chapter_id: "chapter-1", title: "第一章" }],
  chunks: [{ chunk_id: "chunk-1", chapter_id: "chapter-1", text: "用于恢复的正文" }],
  claims: [{ claim_id: "claim-1", title: "结论", summary: "用于恢复的摘要" }],
  citations: [{ citation_id: "citation-1", note: "用于恢复的引用" }],
};
assert.deepEqual(knowledgeResultFromPackage(pkg, { kind: "chunk", resourceID: "chunk-1" }), {
  kind: "chunk",
  id: "chunk-1",
  chunk_id: "chunk-1",
  title: "第一章",
  snippet: "用于恢复的正文",
});
assert.deepEqual(knowledgeResultFromPackage(pkg, { kind: "claim", resourceID: "claim-1" }), {
  kind: "claim",
  id: "claim-1",
  claim_id: "claim-1",
  title: "结论",
  snippet: "用于恢复的摘要",
});
assert.equal(knowledgeResultFromPackage(pkg, { kind: "chunk", resourceID: "missing" }), null);

assert.equal(shouldHandleKnowledgeClick({ button: 0 }), true);
assert.equal(shouldHandleKnowledgeClick({ button: 0, metaKey: true }), false);
assert.equal(shouldHandleKnowledgeClick({ button: 0, ctrlKey: true }), false);
assert.equal(shouldHandleKnowledgeClick({ button: 1 }), false);
assert.equal(knowledgeResourceTargetSelector({ type: "chapter" }), "[data-chapter-index].active");
assert.equal(knowledgeResourceTargetSelector({ type: "result" }), "[data-result-index].active");
assert.equal(knowledgeResourceTargetSelector({ type: "book" }), "");
assert.equal(knowledgeSearchIsCurrent(4, 4, "book-1", "book-1"), true);
assert.equal(knowledgeSearchIsCurrent(3, 4, "book-1", "book-1"), false);
assert.equal(knowledgeSearchIsCurrent(4, 4, "book-1", "book-2"), false);

const manyChapters = Array.from({ length: 20 }, (_, index) => ({
  chapter_id: `chapter-${index + 1}`,
  title: `第 ${index + 1} 章`,
}));
const visibleChapters = knowledgeVisibleChapters(
  manyChapters,
  { type: "chapter", resourceID: "chapter-17" },
  16,
);
assert.equal(visibleChapters.length, 17);
assert.equal(visibleChapters.at(-1)?.chapter_id, "chapter-17");

console.log("knowledge deep links smoke passed");
