import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = fs.readFileSync(path.join(root, "app.js"), "utf8");
const storage = new Map([["kbase.token", "fixture-token"]]);
const app = {
  className: "",
  innerHTML: "",
  querySelector() { return null; },
  querySelectorAll() { return []; },
};
const jobBodies = [];
let fetchMode = "jobs";
let releaseSlowJob;

const jsonResponse = (payload, status = 200) => new Response(JSON.stringify(payload), {
  status,
  headers: { "content-type": "application/json" },
});

const context = {
  AbortController,
  Blob,
  FormData,
  Headers,
  Response,
  URL,
  URLSearchParams,
  clearTimeout,
  console,
  setTimeout,
  document: {
    body: { append() {} },
    createElement() { return { click() {}, remove() {} }; },
    querySelector(selector) { return selector === "#app" ? app : null; },
    querySelectorAll() { return []; },
  },
  window: {
    addEventListener() {},
    clearTimeout,
    setTimeout,
    localStorage: {
      getItem(key) { return storage.get(key) || null; },
      removeItem(key) { storage.delete(key); },
      setItem(key, value) { storage.set(key, String(value)); },
    },
    location: { pathname: "/unit-test", search: "", hash: "", origin: "https://kbase.example" },
  },
};

context.fetch = async (url, options = {}) => {
  const requestURL = new URL(String(url), context.window.location.origin);
  const requestPath = `${requestURL.pathname}${requestURL.search}`;
  if (requestPath === "/api/browser/session") {
    return jsonResponse({
      session: { id: "session-fixture" },
      csrf_token: "csrf-fixture",
      csrf_expires_at: "2099-01-01T00:00:00Z",
      client_id: "client_fixture_0123456789",
      epoch: 1,
    });
  }
  if (requestPath === "/api/jobs" && options.method === "POST") {
    assert.equal(new Headers(options.headers).get("X-KBase-CSRF"), "csrf-fixture");
    const body = JSON.parse(String(options.body || "{}"));
    jobBodies.push(body);
    const response = jsonResponse({ job: { id: `job-${jobBodies.length}`, status: "queued", ebook_enid: body.ebook_enid } }, 202);
    if (fetchMode === "slow-job") {
      return new Promise((resolve) => { releaseSlowJob = () => resolve(response); });
    }
    return response;
  }
  if (requestPath === "/api/dedao/home?page_size=4") {
    return jsonResponse({ courses: { list: [] }, ebooks: { list: [] }, odob: { list: [] } });
  }
  if (requestPath === "/api/dedao/session") {
    return jsonResponse({ logged_in: true, active_user: { uid_hazy: "safe-user", name: "测试用户" }, user_count: 1 });
  }
  if (requestPath === "/api/jobs?limit=50") {
    return jsonResponse({ jobs: [{ id: "persisted-job", type: "dedao_ebook_download", status: "running", ebook_enid: "restored-enid" }] });
  }
  if (requestPath === "/api/jobs/persisted-job") {
    return jsonResponse({ job: { id: "persisted-job", type: "dedao_ebook_download", status: "succeeded", ebook_enid: "restored-enid" } });
  }
  if (requestPath.startsWith("/api/dedao/search/ebooks")) {
    return jsonResponse({ error: "temporary upstream failure" }, 503);
  }
  throw new Error(`unexpected fetch ${requestURL.href}`);
};

vm.runInNewContext(`${source}\nglobalThis.__dedao = {
  createDedaoEbookJob,
  dedaoEbookAcquisitionState,
  dedaoLibraryState,
  dedaoLoginState,
  loadDedaoHome,
  loadDedaoEbookJobs,
  renderDedaoEbookAcquisition,
  renderDedaoLogin,
  searchDedaoEbooks,
};`, context, { filename: "frontend-web/app.js" });

const dedao = context.__dedao;
dedao.dedaoLibraryState.pages.ebook.items = [{ id: 42, enid: "format-enid", title: "格式测试书", is_buy: true }];
dedao.renderDedaoEbookAcquisition();
assert.match(app.innerHTML, /下载格式/);
assert.match(app.innerHTML, /<option value="1">HTML<\/option>/);
assert.match(app.innerHTML, /<option value="2">PDF<\/option>/);
assert.match(app.innerHTML, /<option value="3">EPUB<\/option>/);

await dedao.createDedaoEbookJob({ id: 42, enid: "pdf-enid", type: "dedao_ebook_download", downloadType: 2 });
await dedao.createDedaoEbookJob({ id: 43, enid: "sync-enid", type: "dedao_ebook_sync_kbase", downloadType: 3 });
assert.equal(jobBodies[0].download_type, 2, "download should preserve the selected PDF format");
assert.equal(jobBodies[1].download_type, 1, "knowledge sync should always use HTML");

fetchMode = "slow-job";
const firstSubmit = dedao.createDedaoEbookJob({ id: 44, enid: "duplicate-enid", type: "dedao_ebook_download", downloadType: 3 });
await Promise.resolve();
const duplicateSubmit = dedao.createDedaoEbookJob({ id: 44, enid: "duplicate-enid", type: "dedao_ebook_download", downloadType: 3 });
assert.equal(jobBodies.filter((body) => body.ebook_enid === "duplicate-enid").length, 1, "duplicate clicks should create one request");
releaseSlowJob();
await Promise.all([firstSubmit, duplicateSubmit]);

fetchMode = "search-failure";
const retained = [{ id: 77, enid: "retained-enid", title: "保留结果" }];
dedao.dedaoEbookAcquisitionState.source = "site";
dedao.dedaoEbookAcquisitionState.query = "测试";
dedao.dedaoEbookAcquisitionState.siteItems = retained;
await dedao.searchDedaoEbooks();
assert.equal(dedao.dedaoEbookAcquisitionState.siteItems, retained, "failed search should retain current results");

dedao.dedaoEbookAcquisitionState.jobs = {};
context.window.location.pathname = "/sources/dedao/ebooks";
await dedao.loadDedaoEbookJobs();
assert.equal(dedao.dedaoEbookAcquisitionState.jobs["restored-enid"].status, "succeeded", "restored active jobs should resume polling to a terminal state");

dedao.dedaoLoginState.session = { logged_in: true, active_user: { name: "旧账号" } };
dedao.dedaoLoginState.phase = "idle";
dedao.renderDedaoLogin();
assert.match(app.innerHTML, /重新扫码登录/, "stored sessions should always offer a repair path");

await dedao.loadDedaoHome();
assert.equal(dedao.dedaoLoginState.session.active_user.name, "测试用户");
assert.match(app.innerHTML, /测试用户 · 浏览电子书/, "home should render the hydrated account state");

console.log("dedao ebook acquisition behavior smoke passed");
