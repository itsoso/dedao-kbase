import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const js = fs.readFileSync(path.join(root, "app.js"), "utf8");
const css = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const html = fs.readFileSync(path.join(root, "index.html"), "utf8");

for (const marker of [
  'dedaoLogin: "/sources/dedao/login"',
  "/api/dedao/session",
  "/api/dedao/auth/qrcode",
  "/api/dedao/auth/check",
  "扫码登录得到",
  "重新扫码登录",
  "renderDedaoLogin",
  "loadDedaoSession",
  "createDedaoLoginQRCode",
  "startDedaoLoginPolling",
  "stopDedaoLoginPolling",
  "routePathname === ROUTES.dedaoLogin",
  'window.addEventListener("beforeunload", stopDedaoLoginPolling)',
  "/api/dedao/search/ebooks",
  "/bookshelf",
  "/api/jobs",
  "dedao_ebook_download",
  "dedao_ebook_sync_kbase",
  "我的书架",
  "全站搜索",
  "仅下载",
  "下载格式",
  "data-dedao-download-type",
  "PDF",
  "EPUB",
  "下载并入知识库",
  "renderDedaoEbookAcquisition",
  "searchDedaoEbooks",
  "addDedaoEbookToBookshelf",
  "createDedaoEbookJob",
  "pollBookKnowledgeJob",
  "loadDedaoEbookJobs",
  "bookJobStageLabel",
  "bookJobFailureMessage",
  "canRetryBookJob",
  "retryBookJob",
  "/api/jobs/${encodeURIComponent(jobID)}/retry",
  "data-book-job-retry",
  "已有重试任务正在排队或运行",
  'dedaoEbookAcquisitionState.message = "重试任务已进入队列。"',
  "任务已安全停止，可以从这里重新执行",
  "原任务",
  "排队等待",
  "正在下载",
  "正在生成知识库",
  "等待人工恢复",
  "已中断",
  "normalizeDedaoEbook",
  "jobActive",
  "任务进行中",
  "Promise.allSettled",
  "loadDedaoHomeSession",
]) {
  assert.ok(js.includes(marker), `app.js should include login marker ${marker}`);
}

for (const className of [
  ".dedao-login",
  ".dedao-login__panel",
  ".dedao-login__qr-frame",
  ".dedao-login__status",
  ".dedao-ebook-acquisition",
  ".dedao-ebook-acquisition__tabs",
  ".dedao-ebook-acquisition__search",
  ".dedao-ebook-card__actions",
  ".dedao-ebook-card__format",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(css.includes("@media (max-width: 760px)"), "login UI should have a responsive layout");
assert.ok(css.includes("@media (prefers-reduced-motion: reduce)"), "login UI should respect reduced motion");
assert.ok(html.includes("20260809-book-job-recovery"), "book job recovery release should use a fresh browser cache version");
assert.ok(css.includes(".book-job-recovery"), "book job recovery should have a deliberate recovery layout");
assert.ok(css.includes(".book-job-recovery__history"), "book job recovery should style retry lineage");
assert.doesNotMatch(js, />\s*job execution failed\s*</i, "raw worker failures must not be product copy");

const loginStatusSource = js.match(/function dedaoLoginStatusCopy\(\) \{([\s\S]*?)\n\}/)?.[1] || "";
const loginSuccessIndex = loginStatusSource.indexOf('dedaoLoginState.phase === "success"');
const loggedInSessionIndex = loginStatusSource.indexOf("dedaoLoginState.session?.logged_in");
assert.ok(loginSuccessIndex >= 0, "login status copy should handle terminal success explicitly");
assert.ok(
  loginSuccessIndex < loggedInSessionIndex,
  "terminal login success copy should take precedence over the generic session copy",
);

console.log("dedao ebook acquisition smoke passed");
