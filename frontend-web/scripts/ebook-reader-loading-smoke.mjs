import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

assert.match(html, /data-reader-loading/, "reader loading shell should render before JavaScript runs");
assert.match(html, /reader-loading__book" role="status"/, "loading book should be announced as status");
assert.match(html, /reader-loading__sheet/, "loading state should use a full page skeleton");
assert.match(html, /reader-loading__rail/, "desktop loading state should include a chapter rail skeleton");

assert.match(css, /\.reader-loading__stage/, "loading layout should be styled");
assert.match(css, /@keyframes reader-shimmer/, "loading skeleton should use a shimmer animation");
assert.match(css, /prefers-reduced-motion: reduce/, "loading animation should respect reduced motion");
assert.doesNotMatch(css, /border:\s*1px\s+dashed/, "loading page should not use the old dashed placeholder");

assert.match(js, /apiFetch\(`\/api\/books\/\$\{encodeURIComponent\(bookID\)\}`/, "reader should load the selected book through the API client");
assert.match(js, /renderError/, "reader should surface load failures");

console.log("ebook reader loading smoke passed");
