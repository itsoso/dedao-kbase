import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");

for (const marker of [
  "wcplusState",
  "renderWCPlusPage",
  "bootstrapWCPlusSource",
  "loadWCPlusAccounts",
  "loadWCPlusArticles",
  "pageWCPlusAccounts",
  "pageWCPlusArticles",
  "checkWCPlusStatus",
  "checkWCPlusEnvironment",
  "searchWCPlus",
  "pageWCPlusSearch",
  "batchImportWCPlusNicknames",
  "copyWCPlusBatchText",
  "copyWCPlusDiagnostics",
  "isWCPlusBootstrapped",
  "batchImportToKBase",
  "batchWaitForCompletion",
  "batchImportLimit",
  "importedPackages",
  "searchOffset",
  "searchNum",
  "loadWCPlusRawFile",
  "wcplusArticleURL",
  "importRawWCPlusArticle",
  "importWCPlusArticle",
  "importWCPlusAccount",
  "rememberWCPlusImportedBooks",
  "renderWCPlusImportedBooks",
  "loadWCPlusTasks",
  "createWCPlusTask",
  "createWCPlusTaskForAccount",
  "createWCPlusBatchTask",
  "controlWCPlusTask",
  "runWCPlusQueue",
  "exportWCPlusAllArticlesXLSX",
]) {
  assert.ok(js.includes(marker), `app.js should include ${marker}`);
}

for (const endpoint of [
  "/api/wcplus/gzh/list",
  "/api/wcplus/gzh/articles",
  "/api/wcplus/article/content",
  "/api/wcplus/import/article",
  "/api/wcplus/import/raw",
  "/api/wcplus/import/account",
  "/api/wcplus/status",
  "/api/wcplus/env/check",
  "/api/wcplus/gzh/search",
  "/api/wcplus/search-gzh",
  "/api/wcplus/article/all",
  "/api/wcplus/article/search-title",
  "/api/wcplus/search",
  "/api/wcplus/report/reading-data",
  "/api/wcplus/report/statistic-data",
  "/api/wcplus/article/gzh",
  "/api/wcplus/like-articles",
  "/api/wcplus/request/gzh",
  "/api/wcplus/export/text",
  "/api/wcplus/export/gzh-csv",
  "/api/wcplus/export/all-articles-xlsx",
  "/api/wcplus/task/all",
  "/api/wcplus/task/new",
  "/api/wcplus/task/control",
  "/api/wcplus/batch-task/create",
  "/api/wcplus/batch-task/delete",
  "/api/wcplus/batch-import/gzh",
]) {
  assert.ok(js.includes(endpoint), `WC Plus UI should call ${endpoint}`);
}

for (const label of [
  "WC Plus 本地服务",
  "检查状态",
  "环境检查",
  "搜索 WC Plus",
  "批量导入公众号昵称",
  "同步后导入书籍知识库",
  "等待任务完成后入库",
  "入库篇数",
  "手动导入知识库",
  "正文 Markdown / 纯文本",
  "同步公众号",
  "批量任务",
  "批量导入",
  "启动队列",
  "启动时自动检查环境",
  "文章缺少 nickname/id 或 URL",
  "导出全库 XLSX",
  "环境诊断",
  "服务地址",
  "复制诊断",
  "批量结果",
  "最近入库",
  "下载任务",
  "任务类型",
  "每页",
  "导入篇数",
  "最近导出",
  "抓取篇数",
  "辅助查询",
  "阅读数据",
  "统计数据",
  "公众号详情",
  "收藏文章",
  "请求公众号",
]) {
  assert.ok(js.includes(label), `WC Plus UI should render ${label}`);
}

for (const selector of [
  "data-wcplus-account-page",
  "data-wcplus-article-page",
  "data-wcplus-search-page",
  "name=\"searchNum\"",
  "name=\"taskCrawlerType\"",
  "name=\"taskArticleListType\"",
  "name=\"articleListAmount\"",
  "name=\"importLimit\"",
  "name=\"exportRecentNum\"",
  "name=\"batchArticleListType\"",
  "name=\"batchArticleListAmount\"",
  "name=\"importToKBase\"",
  "name=\"waitForCompletion\"",
  "name=\"batchImportLimit\"",
  "id=\"wcplus-raw-import-form\"",
  "name=\"rawContent\"",
  "name=\"rawFile\"",
  "data-wcplus-copy-diagnostics",
  "data-wcplus-copy-batch=\"success\"",
  "data-wcplus-copy-batch=\"failed\"",
  "data-wcplus-utility=\"reading\"",
  "data-wcplus-utility=\"statistics\"",
  "data-wcplus-utility=\"owner\"",
  "data-wcplus-utility=\"likes\"",
  "data-wcplus-utility=\"request\"",
  "data-wcplus-sync-result-account",
  "book_id=",
]) {
  assert.ok(js.includes(selector), `WC Plus UI should include ${selector}`);
}

for (const className of [
  ".wcplus-source",
  ".wcplus-source__toolbar",
  ".wcplus-source__articles",
  ".wcplus-source__tasks",
  ".wcplus-source__search-results",
  ".wcplus-source__badge",
  ".wcplus-source__env",
  ".wcplus-source__utility",
  ".wcplus-source__utility-result",
  ".wcplus-source__batch-form",
  ".wcplus-source__batch-result",
  ".wcplus-source__manual-form",
  ".wcplus-source__imported-books",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(js.includes("success_text"), "WC Plus UI should render batch import success_text");
assert.ok(js.includes("failed_text"), "WC Plus UI should render batch import failed_text");
assert.ok(js.includes("import_to_kbase"), "WC Plus batch import should support direct knowledge import");
assert.ok(js.includes("wait_for_completion"), "WC Plus batch import should support waiting for synced tasks");
assert.ok(js.includes("import_limit"), "WC Plus batch import should send knowledge import limits");
assert.ok(js.includes("poll_attempts"), "WC Plus batch import should send bounded task polling");
assert.ok(js.includes("offset: String(wcplusState.searchOffset)"), "WC Plus search should send a pageable offset");
assert.ok(js.includes("num: String(wcplusState.searchNum)"), "WC Plus search should send a configurable page size");
assert.ok(js.includes("imported_count"), "WC Plus UI should render imported knowledge count");
assert.ok(js.includes("imported_books"), "WC Plus UI should render batch imported books");
assert.ok(js.includes("books"), "WC Plus UI should remember account imported books");
assert.ok(js.includes("envCheck"), "WC Plus UI should keep environment check details");
assert.ok(js.includes("utilityResult"), "WC Plus UI should render auxiliary API results");
assert.ok(js.includes("runWCPlusUtility"), "WC Plus UI should call auxiliary WC Plus APIs");
assert.ok(js.includes("base_url"), "WC Plus UI should show server-side WC Plus base_url diagnostics");
assert.ok(js.includes("FileReader"), "WC Plus UI should support raw TXT/Markdown file import fallback");
assert.ok(js.includes("new URLSearchParams({ url: articleURL })"), "WC Plus UI should preview URL-only article results");
assert.ok(js.includes("JSON.stringify(id ? { nickname, id } : { url: articleURL })"), "WC Plus UI should import URL-only article results");
assert.ok(js.includes("const articleURL = wcplusArticleURL(article);"), "WC Plus article list should read article URL aliases");
assert.ok(js.includes('data-wcplus-preview="${index}" ${id || articleURL ? "" : "disabled"}'), "WC Plus article list should preview URL-only articles");
assert.ok(js.includes('data-wcplus-import="${index}" ${id || articleURL ? "" : "disabled"}'), "WC Plus article list should import URL-only articles");
assert.ok(js.includes("appmsgid"), "WC Plus UI should normalize alternate article id fields");
assert.ok(js.includes("\"link\""), "WC Plus UI should normalize alternate article URL fields");
assert.doesNotMatch(js, /WCPLUS_BASE_URL\s*=/, "web UI must not embed WC Plus base URL configuration");
assert.doesNotMatch(js, /127\.0\.0\.1:5001/, "web UI should not hardcode the local WC Plus URL");

console.log("wcplus source UI smoke passed");
