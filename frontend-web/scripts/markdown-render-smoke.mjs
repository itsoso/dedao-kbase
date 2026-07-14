import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const start = js.indexOf("function renderInlineMarkdown");
const end = js.indexOf("\nfunction resetKnowledgeAnalysis", start);

assert.ok(start >= 0 && end > start, "app.js should expose the Markdown renderer helpers");

const context = {
  escapeHTML(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#039;");
  },
  escapeAttribute(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll('"', "&quot;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;");
  },
};
vm.createContext(context);
vm.runInContext(js.slice(start, end), context);

const rendered = context.renderSimpleMarkdown(`## 结论

**书中观点**：这是重点，包含 \`gp120\`。

---

1. 第一项
2. 第二项

[来源](https://example.com/report)

<script>alert(1)</script>`);

assert.match(rendered, /<h2>结论<\/h2>/);
assert.match(rendered, /<strong>书中观点<\/strong>/);
assert.match(rendered, /<code>gp120<\/code>/);
assert.match(rendered, /<hr>/);
assert.match(rendered, /<ol><li>第一项<\/li><li>第二项<\/li><\/ol>/);
assert.match(rendered, /<a href="https:\/\/example\.com\/report"[^>]*>来源<\/a>/);
assert.ok(!rendered.includes("<script>"), "Markdown renderer must not emit raw scripts");
assert.match(rendered, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);

console.log("markdown render smoke passed");
