import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
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
  setApi(value) { apiFetch = value; },
};`, context, { filename: "frontend-web/app.js" });

const jobs = context.__jobs;
const deferred = () => {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
};
const wcPayload = { tasks: [] };
const oldJobs = deferred();
let jobListCalls = 0;
jobs.setApi(async (requestPath) => {
  if (requestPath === "/api/wcplus/task/all") return wcPayload;
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
oldJobs.resolve({ jobs: [{ id: "old-job", type: "dedao_ebook_download", status: "failed", stage: "failed", ebook_id: 1 }] });
await staleLoad;
assert.equal(jobs.jobCenterState.tasks[0].id, "new-job", "an older refresh must not overwrite the latest task list");

const leavingJobs = deferred();
jobs.setApi(async (requestPath) => requestPath === "/api/wcplus/task/all" ? wcPayload : leavingJobs.promise);
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
  if (requestPath === "/api/wcplus/task/all") return wcPayload;
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

console.log("job center recovery behavior smoke passed");
