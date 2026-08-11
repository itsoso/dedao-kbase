import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const fixturePath = fileURLToPath(import.meta.url);
const root = path.resolve(path.dirname(fixturePath), "..");
const host = "127.0.0.1";
const port = Number(process.env.EVOLUTION_FIXTURE_PORT || 8899);
const delayMS = Math.max(0, Number(process.env.EVOLUTION_FIXTURE_DELAY_MS || 0));
const now = "2026-08-11T12:00:00Z";
export const fixtureBrowserClientID = "fixture-browser-client";

export function isValidBrowserClientID(value) {
  return typeof value === "string" && value.length >= 16 && value.length <= 128 && /^[A-Za-z0-9_-]+$/.test(value);
}

const runs = [
  { run_id: "run-open-a", package_id: "attention-agent", run_type: "agent_policy", status: "awaiting_approval", risk_level: "critical", priority_score: 96, updated_at: now, trigger_signal_count: 4 },
  { run_id: "run-blocked", package_id: "knowledge-agent", run_type: "knowledge_release", status: "blocked", risk_level: "high", priority_score: 83, updated_at: now, trigger_signal_count: 2 },
  { run_id: "run-failed", package_id: "runtime-agent", run_type: "combined", status: "failed", risk_level: "p1", priority_score: 79, updated_at: now, trigger_signal_count: 3 },
  { run_id: "run-open-b", package_id: "research-agent", run_type: "combined", status: "evaluating", risk_level: "medium", priority_score: 61, updated_at: now, trigger_signal_count: 1 },
  { run_id: "run-completed", package_id: "attention-agent", run_type: "agent_policy", status: "completed", risk_level: "critical", priority_score: 40, updated_at: "2026-08-11T10:00:00Z", trigger_signal_count: 2 },
  { run_id: "run-rejected", package_id: "knowledge-agent", run_type: "knowledge_release", status: "rejected", risk_level: "low", priority_score: 35, updated_at: "2026-08-11T11:00:00Z", trigger_signal_count: 1 },
];
const terminalStatuses = ["completed", "blocked", "rejected", "failed", "superseded", "rolled_back"];

const fleet = ["attention-agent", "knowledge-agent", "runtime-agent", "research-agent"].map((packageID) => ({
  package_id: packageID,
  current: { package_id: packageID, version: "1.0.1", lifecycle_state: "published", published_at: now },
  history: [{ package_id: packageID, version: "1.0.1", lifecycle_state: "published", published_at: now }],
  open_runs: runs.filter((run) => run.package_id === packageID && !terminalStatuses.includes(run.status)),
}));

export function fixtureEvolutionOverview() {
  return {
    awaiting_approval: 1,
    blocked: 1,
    failed: 1,
    completed: 1,
    open_runs: runs.filter((run) => !terminalStatuses.includes(run.status)),
    agent_fleet: fleet,
  };
}

const mimeTypes = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".svg": "image/svg+xml",
};

function json(response, value, status = 200) {
  response.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" });
  response.end(JSON.stringify(value));
}

function wait() {
  return delayMS ? new Promise((resolve) => setTimeout(resolve, delayMS)) : Promise.resolve();
}

async function serveAPI(request, response, url) {
  if (url.pathname === "/browser/session") {
    json(response, { client_id: request.headers["x-kbase-browser-client-id"] || fixtureBrowserClientID, epoch: 1 });
    return true;
  }
  if (url.pathname === "/api/browser/session") {
    json(response, {
      client_id: request.headers["x-kbase-browser-client-id"] || fixtureBrowserClientID,
      epoch: 1,
      session: { session_id: "fixture-session" },
      csrf_token: "fixture-csrf",
      csrf_expires_at: "2099-01-01T00:00:00Z",
    });
    return true;
  }
  await wait();
  if (url.pathname === "/api/evolution/overview") {
    json(response, fixtureEvolutionOverview());
    return true;
  }
  if (url.pathname === "/api/evolution/runs") {
    const statuses = new Set(String(url.searchParams.get("status") || "").split(",").filter(Boolean));
    const risks = new Set(String(url.searchParams.get("risk") || "").split(",").filter(Boolean));
    const types = new Set(String(url.searchParams.get("type") || "").split(",").filter(Boolean));
    const filtered = runs.filter((run) => (
      (!statuses.size || statuses.has(run.status)) &&
      (!risks.size || risks.has(run.risk_level)) &&
      (!types.size || types.has(run.run_type))
    ));
    json(response, { runs: filtered, next_cursor: "" });
    return true;
  }
  const eventsMatch = url.pathname.match(/^\/api\/evolution\/runs\/([^/]+)\/events$/);
  if (eventsMatch) {
    const runID = decodeURIComponent(eventsMatch[1]);
    json(response, { events: [{ event_id: `event-${runID}`, to_status: runs.find((run) => run.run_id === runID)?.status || "detected", code: "fixture", created_at: now }] });
    return true;
  }
  const runMatch = url.pathname.match(/^\/api\/evolution\/runs\/([^/]+)$/);
  if (runMatch) {
    const run = runs.find((item) => item.run_id === decodeURIComponent(runMatch[1]));
    json(response, run ? { run } : { error: "not found" }, run ? 200 : 404);
    return true;
  }
  if (url.pathname === "/api/knowledge/releases") {
    json(response, { releases: [{ release_id: "release-fixture", book_id: "fixture-book" }], next_cursor: "" });
    return true;
  }
  return false;
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url || "/", `http://${host}:${port}`);
  if ((url.pathname.startsWith("/api/") || url.pathname.startsWith("/browser/")) && await serveAPI(request, response, url)) return;
  const requested = url.pathname === "/" || url.pathname === "/agent-packages" ? "index.html" : url.pathname.replace(/^\//, "");
  const filePath = path.resolve(root, requested);
  if (!filePath.startsWith(`${root}${path.sep}`) || !fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) {
    response.writeHead(404, { "Content-Type": "text/plain; charset=utf-8" });
    response.end("Not found");
    return;
  }
  response.writeHead(200, { "Content-Type": mimeTypes[path.extname(filePath)] || "application/octet-stream", "Cache-Control": "no-store" });
  fs.createReadStream(filePath).pipe(response);
});

if (process.argv[1] && path.resolve(process.argv[1]) === fixturePath) {
  server.listen(port, host, () => {
    process.stdout.write(`Agent evolution fixture: http://${host}:${port}/agent-packages\n`);
  });
}
