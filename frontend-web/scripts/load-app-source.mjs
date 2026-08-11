import fs from "node:fs";
import path from "node:path";

export function loadAppSource(root) {
  return [
    fs.readFileSync(path.join(root, "knowledge-deep-links.js"), "utf8"),
    fs.readFileSync(path.join(root, "app.js"), "utf8"),
  ].join("\n");
}
