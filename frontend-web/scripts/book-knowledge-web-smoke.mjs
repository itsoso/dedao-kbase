import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

for (const marker of [
  "renderBookKnowledge",
  "knowledgeState",
  "ensureBrowserSessionToken",
  "refreshBrowserSessionToken",
  "loadBookKnowledge",
  "searchBookKnowledge",
  'window.location.pathname.startsWith("/book-knowledge")',
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

for (const endpoint of [
  "/browser/session-token",
  "/api/books",
  "/api/search?",
  "/api/book-chat",
  "/analysis",
]) {
  assert.ok(js.includes(endpoint), `book knowledge web UI should call ${endpoint}`);
}

for (const authMarker of [
  "localStorage.setItem",
  'credentials: "same-origin"',
  "response.status === 401",
  "isSafeBearerToken",
  "clearStoredToken",
  "setAuthorizationHeader",
  "skip invalid kbase token",
]) {
  assert.ok(js.includes(authMarker), `book knowledge web UI should include auth marker ${authMarker}`);
}

for (const unwrap of [
  "payload.books",
  "payload.results",
]) {
  assert.ok(js.includes(unwrap), `book knowledge web UI should unwrap ${unwrap}`);
}

for (const className of [
  ".knowledge-web",
  ".knowledge-web__layout",
  ".knowledge-web__sidebar",
  ".knowledge-web__main",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes("暂无知识库条目，可先从微信来源导入。"), "empty state should point users to source import");
assert.ok(js.includes('href="/ebook/${encodeURIComponent(currentBook.book_id)}"'), "book details should link to the reader");
assert.ok(js.includes("knowledge-web__analysis"), "single article knowledge pages should expose an LLM analysis workspace");
assert.ok(js.includes("分析当前文章"), "single article knowledge pages should include an article analysis action");
assert.ok(js.includes("Qwen-3.7-Max"), "book knowledge analysis should default to Qwen-3.7-Max");
for (const marker of [
  "analysisManifest",
  "loadKnowledgeAnalysisManifest",
  "generateKnowledgeAnalysisManifest",
  "知识基线分析",
  "生成基线分析",
  "重新生成",
  "pending",
  "ready",
  "failed",
]) {
  assert.ok(js.includes(marker), `book knowledge web UI should include durable analysis marker ${marker}`);
}
assert.ok(css.includes(".knowledge-web__manifest"), "styles.css should style the durable analysis manifest");

console.log("book knowledge web smoke passed");
