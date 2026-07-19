import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

for (const marker of [
  "renderWeChatSource",
  "wechat-source",
  "/wechat-import",
  "wechatState",
  "apiFetch",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

for (const endpoint of [
  "/api/wechat/article",
  "/api/wechat/import",
  "/api/wechat/search",
  "/api/wechat/articles",
]) {
  assert.ok(js.includes(endpoint), `wechat source UI should call ${endpoint}`);
}

for (const unwrap of [
  "payload.article",
  "payload.accounts",
  "payload.articles",
]) {
  assert.ok(js.includes(unwrap), `wechat source UI should unwrap ${unwrap}`);
}

for (const label of [
  "微信公众号来源",
  "导入知识库",
  "搜索公众号",
  "最近文章",
]) {
  assert.ok(js.includes(label), `wechat source UI should render ${label}`);
}

for (const className of [
  ".wechat-source",
  ".wechat-source__layout",
  ".wechat-source__article",
  ".wechat-source__preview",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.doesNotMatch(js, /WECHAT_MP_(TOKEN|COOKIE)\s*=/, "web UI must not embed WeChat credentials");
const wechatSourceStart = css.indexOf(".wechat-source {");
const wechatSourceEnd = css.indexOf(".source-control", wechatSourceStart);
const wechatSourceCSS = css.slice(wechatSourceStart, wechatSourceEnd > wechatSourceStart ? wechatSourceEnd : undefined);
assert.doesNotMatch(wechatSourceCSS, /border:\s*1px\s+dashed/, "new source page should not use dashed placeholders");

console.log("wechat source UI smoke passed");
