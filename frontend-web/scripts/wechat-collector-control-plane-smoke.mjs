import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),"..");
const js=fs.readFileSync(path.join(root,"app.js"),"utf8");const css=fs.readFileSync(path.join(root,"styles.css"),"utf8");
for(const marker of ["WeChat Collector","wechat_mp_article","discover_articles","sync_articles","sync_media","capability_health","requires_action","登录","公众号搜索","运行历史","data-source-run-retry","data-source-run-cancel","sourceKnowledgeURL","wcplus-legacy-diagnostics"]){assert.ok(js.includes(marker),`missing collector marker ${marker}`)}
assert.ok(css.includes(".source-control__layout")&&css.includes("grid-template-columns"),"collector should use a two-column layout");
assert.ok(js.includes("<details")&&js.includes("wcplus-legacy-diagnostics"),"WC Plus should remain collapsed legacy diagnostics");
assert.doesNotMatch(js,/WECHAT_MP_(TOKEN|COOKIE)|loginqrcode|127\.0\.0\.1:5001/,"browser code must not contain local secrets or WC Plus endpoint");
console.log("wechat collector control plane smoke passed");
