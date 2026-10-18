import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { hash, runtime } from "./ui-presentation.mjs";
const root = fileURLToPath(new URL("../", import.meta.url));
const baseline = JSON.parse(await readFile(resolve(root, "docs/ui-source-baseline.json"), "utf8"));
const failures = [];
for (const entry of baseline.files) {
  try {
    const source = await readFile(resolve(root, entry.path), "utf8");
    if (hash(runtime(entry.path, source)) !== entry.runtimeSha256) failures.push(entry.path);
  } catch (error) { failures.push(`${entry.path}: ${error.message}`); }
}
if (failures.length) { console.error("Presentation differs from the reference-derived baseline:\n" + failures.join("\n")); process.exit(1); }
console.log(`Verified ${baseline.files.length} native presentation files against reference ${baseline.referenceCommit}; native brand artwork is independently owned.`);
