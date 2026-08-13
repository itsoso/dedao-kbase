import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";
import { loadAppSource } from "./load-app-source.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = loadAppSource(root);
const styles = fs.readFileSync(path.join(root, "styles.css"), "utf8");
const app = { className: "", innerHTML: "", querySelector() { return null; }, querySelectorAll() { return []; } };
const context = {
  AbortController, Blob, FormData, Headers, Response, URL, URLSearchParams, clearTimeout, console, setTimeout,
  document: {
    body: { append() {} },
    createElement() { return { click() {}, remove() {} }; },
    querySelector(selector) { return selector === "#app" ? app : null; },
    querySelectorAll() { return []; },
  },
  window: {
    addEventListener() {}, clearTimeout, setTimeout,
    localStorage: { getItem() { return null; }, removeItem() {}, setItem() {} },
    location: { pathname: "/unit-test", search: "", hash: "", origin: "https://kbase.example" },
  },
};

vm.runInNewContext(`${source}\nglobalThis.__jobs = {
  jobCenterState,
  loadJobCenter,
  retryBookJob,
  sourceControlState,
  sourceControlPrefillFromLocation,
  loadSourceControlPlane,
  setApi(value) { apiFetch = value; },
};`, context, { filename: "frontend-web/app.js" });

const jobs = context.__jobs;
const deferred = () => {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
};
const subscriptionPayload = {
  subscriptions: [{
    id: "subscription-wcplus",
    source_type: "wcplus_wechat_article",
    source_account_key: "account-key",
    source_account: "测试公众号",
    operation: "sync_content",
  }, {
    id: "subscription-wechat",
    source_type: "wechat_mp_article",
    source_account_key: "wechat-key",
    source_account: "微信测试号",
    operation: "sync_articles",
  }],
};
const runPayload = {
  runs: [{
    id: "source-run-1",
    subscription_id: "subscription-wcplus",
    requested_operation: "sync_content",
    status: "succeeded",
    new_count: 2,
    updated_count: 1,
    skipped_count: 3,
    failed_count: 0,
    updated_at: "2026-08-10T00:00:00Z",
  }, {
    id: "source-run-wechat",
    subscription_id: "subscription-wechat",
    requested_operation: "sync_articles",
    status: "partial",
    new_count: 1,
    failed_count: 1,
    error: "private-path-sentinel",
    updated_at: "2026-08-10T00:01:00Z",
  }, {
    id: "source-run-discover",
    subscription_id: "subscription-wechat",
    requested_operation: "discover_articles",
    status: "leased",
  }, {
    id: "source-run-media",
    subscription_id: "subscription-wechat",
    requested_operation: "sync_media",
    status: "succeeded",
  }],
};
const oldJobs = deferred();
let jobListCalls = 0;
const initialRequests = [];
jobs.setApi(async (requestPath) => {
  initialRequests.push(requestPath);
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/source-sync/runs?limit=50") return runPayload;
  if (requestPath === "/api/wcplus/task/all") throw new Error("legacy WC Plus task endpoint must not be requested");
  if (requestPath === "/api/jobs?limit=50") {
    jobListCalls += 1;
    if (jobListCalls === 1) return oldJobs.promise;
    return { jobs: [{ id: "new-job", type: "dedao_ebook_download", status: "queued", stage: "queued", ebook_id: 2 }] };
  }
  throw new Error(`unexpected request ${requestPath}`);
});

context.window.location.pathname = "/jobs";
const staleLoad = jobs.loadJobCenter();
assert.match(app.innerHTML, /role="status" aria-live="polite"/, "job loading state should expose a live status region");
assert.match(app.innerHTML, /正在加载任务/, "job loading state should have readable Chinese copy");
await jobs.loadJobCenter();
assert.equal(jobs.jobCenterState.tasks[0].id, "new-job");
assert.ok(initialRequests.includes("/api/source-subscriptions"), "job center should load source subscriptions");
assert.ok(initialRequests.includes("/api/source-sync/runs?limit=50"), "job center should load source runs");
assert.ok(!initialRequests.includes("/api/wcplus/task/all"), "job center must not query a server-local WC Plus API");
const sourceTask = jobs.jobCenterState.tasks.find((task) => task.id === "source-run-1");
assert.equal(sourceTask?.source, "WC Plus");
assert.equal(sourceTask?.title, "测试公众号");
assert.equal(sourceTask?.operation, "同步正文");
assert.equal(sourceTask?.progress, "新增 2 · 更新 1 · 跳过 3");
assert.match(sourceTask?.sourceURL || "", /subscription_id=subscription-wcplus/);
assert.match(sourceTask?.sourceURL || "", /run_id=source-run-1/);
const wechatTask = jobs.jobCenterState.tasks.find((task) => task.id === "source-run-wechat");
assert.equal(wechatTask?.source, "微信公众号");
assert.equal(wechatTask?.operation, "同步文章");
assert.equal(wechatTask?.status, "partial");
assert.equal(wechatTask?.error, "Worker 报告异常，详细信息已隐藏；请运行诊断并人工处理。");
assert.doesNotMatch(wechatTask?.error || "", /private-path-sentinel/);
context.window.location.search = "?source_account_key=account-key&subscription_id=subscription-wcplus&run_id=source-run-1";
jobs.sourceControlPrefillFromLocation();
assert.equal(jobs.sourceControlState.selectedSubscriptionID, "subscription-wcplus");
assert.equal(jobs.sourceControlState.selectedRunID, "source-run-1");
context.window.location.search = "";
assert.equal(jobs.jobCenterState.tasks.find((task) => task.id === "source-run-discover")?.operation, "发现文章");
assert.equal(jobs.jobCenterState.tasks.find((task) => task.id === "source-run-media")?.operation, "同步媒体");
assert.match(app.innerHTML, /得到电子书与来源同步任务独立加载/);
assert.match(styles, /\.job-card__status\.is-leased/);
assert.match(styles, /\.job-card__status\.is-partial/);

context.window.location.pathname = "/wcplus-source";
jobs.sourceControlState.selectedSubscriptionID = "subscription-wcplus";
jobs.sourceControlState.selectedRunID = "source-run-wechat";
let collectionListRequested = false;
jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/source-agents") return { agents: [] };
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/source-sync/runs?limit=200") return runPayload;
  if (requestPath === "/api/knowledge/collections") {
    collectionListRequested = true;
    throw new Error("collections unavailable");
  }
  if (requestPath === "/api/source-sync/runs/source-run-wechat") return { run: runPayload.runs[1], items: [] };
  throw new Error(`unexpected request ${requestPath}`);
});
await jobs.loadSourceControlPlane({ silent: true, renderResult: false });
assert.equal(collectionListRequested, true, "source control should request account collections independently");
assert.equal(jobs.sourceControlState.selectedSubscriptionID, "subscription-wcplus");
assert.equal(jobs.sourceControlState.selectedRunID, "", "a deep-linked run from another subscription must be cleared");
assert.match(jobs.sourceControlState.message, /集合列表暂不可用/, "collection failure should remain visible without hiding source runs");
context.window.location.pathname = "/jobs";
oldJobs.resolve({ jobs: [{ id: "old-job", type: "dedao_ebook_download", status: "failed", stage: "failed", ebook_id: 1 }] });
await staleLoad;
assert.equal(jobs.jobCenterState.tasks[0].id, "new-job", "an older refresh must not overwrite the latest task list");

const leavingJobs = deferred();
jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/source-sync/runs?limit=50") return runPayload;
  return leavingJobs.promise;
});
context.window.location.pathname = "/jobs";
const routeLoad = jobs.loadJobCenter();
context.window.location.pathname = "/sources/agents";
app.innerHTML = "agent-page-sentinel";
leavingJobs.resolve({ jobs: [{ id: "route-old", type: "dedao_ebook_download", status: "failed", ebook_id: 3 }] });
await routeLoad;
assert.equal(app.innerHTML, "agent-page-sentinel", "a completed jobs request must not repaint after navigation");
assert.notEqual(jobs.jobCenterState.tasks[0]?.id, "route-old", "a completed jobs request must not mutate state after navigation");

const overlapOld = deferred();
let overlapLists = 0;
jobs.setApi(async (requestPath, options = {}) => {
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/source-sync/runs?limit=50") return runPayload;
  if (requestPath === "/api/jobs?limit=50") {
    overlapLists += 1;
    return overlapLists === 1 ? overlapOld.promise : { jobs: [{ id: "retry-job", type: "dedao_ebook_download", status: "queued", stage: "queued", ebook_id: 4, retry_of: "failed-job" }] };
  }
  if (requestPath === "/api/jobs/failed-job/retry" && options.method === "POST") {
    return { job: { id: "retry-job", type: "dedao_ebook_download", status: "queued", stage: "queued", ebook_id: 4, retry_of: "failed-job" } };
  }
  throw new Error(`unexpected request ${requestPath}`);
});
context.window.location.pathname = "/jobs";
const overlappingRefresh = jobs.loadJobCenter();
await jobs.retryBookJob("failed-job");
overlapOld.resolve({ jobs: [{ id: "stale-before-retry", type: "dedao_ebook_download", status: "failed", ebook_id: 4 }] });
await overlappingRefresh;
assert.equal(jobs.jobCenterState.tasks[0].id, "retry-job", "refresh started before retry must not overwrite the retry refresh");

jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/source-sync/runs?limit=50") throw new Error("control plane unavailable");
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/jobs?limit=50") {
    return { jobs: [{ id: "book-survives", type: "dedao_ebook_download", status: "succeeded", stage: "completed", ebook_id: 5 }] };
  }
  throw new Error(`unexpected request ${requestPath}`);
});
await jobs.loadJobCenter();
assert.equal(jobs.jobCenterState.tasks[0]?.id, "book-survives", "source failure must not hide book jobs");
assert.match(jobs.jobCenterState.message, /来源同步任务加载失败：control plane unavailable/);
assert.doesNotMatch(jobs.jobCenterState.message, /WC Plus local API request failed/);

jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/source-sync/runs?limit=50") return runPayload;
  if (requestPath === "/api/source-subscriptions") return subscriptionPayload;
  if (requestPath === "/api/jobs?limit=50") throw new Error("book jobs unavailable");
  throw new Error(`unexpected request ${requestPath}`);
});
await jobs.loadJobCenter();
assert.ok(jobs.jobCenterState.tasks.some((task) => task.id === "source-run-1"), "book failure must not hide source runs");
assert.match(jobs.jobCenterState.message, /KBase 任务加载失败：book jobs unavailable/);

jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/source-sync/runs?limit=50") return runPayload;
  if (requestPath === "/api/source-subscriptions") throw new Error("subscriptions unavailable");
  if (requestPath === "/api/jobs?limit=50") {
    return { jobs: [{ id: "book-without-subscriptions", type: "dedao_ebook_download", status: "succeeded", stage: "completed", ebook_id: 6 }] };
  }
  throw new Error(`unexpected request ${requestPath}`);
});
await jobs.loadJobCenter();
assert.equal(jobs.jobCenterState.tasks[0]?.id, "book-without-subscriptions");
assert.match(jobs.jobCenterState.message, /来源订阅加载失败：subscriptions unavailable/);

console.log("job center recovery behavior smoke passed");
