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
  "renderDedaoLogin",
  "loadDedaoSession",
  "createDedaoLoginQRCode",
  "startDedaoLoginPolling",
  "stopDedaoLoginPolling",
  "routePathname === ROUTES.dedaoLogin",
  'window.addEventListener("beforeunload", stopDedaoLoginPolling)',
]) {
  assert.ok(js.includes(marker), `app.js should include login marker ${marker}`);
}

for (const className of [
  ".dedao-login",
  ".dedao-login__panel",
  ".dedao-login__qr-frame",
  ".dedao-login__status",
]) {
  assert.ok(css.includes(className), `styles.css should include ${className}`);
}

assert.ok(css.includes("@media (max-width: 760px)"), "login UI should have a responsive layout");
assert.ok(css.includes("@media (prefers-reduced-motion: reduce)"), "login UI should respect reduced motion");
assert.ok(html.includes("20260802-dedao-acquisition"), "login release should use a fresh browser cache version");

console.log("dedao ebook acquisition smoke passed");
