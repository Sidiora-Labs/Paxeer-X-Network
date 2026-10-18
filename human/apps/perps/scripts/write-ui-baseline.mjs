import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { hash, runtime } from "./ui-presentation.mjs";
const root = fileURLToPath(new URL("../", import.meta.url));
const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" });
const paths = git("ls-files", "--", ":(glob)packages/*/src/**/*.css", ":(glob)packages/*/src/**/*.tsx", ":(glob)apps/web/**/*.css")
  .split("\n").filter(Boolean).sort();
const files = [];
for (const path of paths) files.push({ path, runtimeSha256: hash(runtime(path, await readFile(resolve(root, path), "utf8"))) });
const baseline = { referenceCommit: git("rev-parse", "HEAD").trim(), files };
await mkdir(resolve(root, "docs"), { recursive: true });
await writeFile(resolve(root, "docs/ui-source-baseline.json"), JSON.stringify(baseline, null, 2) + "\n");
console.log(`Wrote ${files.length} presentation files against reference ${baseline.referenceCommit} to docs/ui-source-baseline.json.`);
