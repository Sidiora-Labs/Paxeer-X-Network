/**
 * Exercise narrative / natural-language search via {@link createAiDocsFacade#searchDocs}.
 *
 * Prereq: `pnpm run build` in this package. Data: `bundled/` unless PAXEER_NETWORK_AI_DOCS_REPO_ROOT
 * points at a full `generated/` tree with `qmd/index.sqlite` (run ai-docs generate + qmd:index, then sync:bundle).
 *
 * Usage:
 *   pnpm run search-nl
 *   PAXEER_NETWORK_AI_DOCS_REPO_ROOT=/path/to/apps/ai-docs/generated node scripts/search-nl.mjs
 *   node scripts/search-nl.mjs --strict   # exit 1 if any case yields zero narrative hits
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clearLoadBundleCache, clearQmdStoreCache, createAiDocsFacade, loadBundle } from "../dist/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const strict = process.argv.includes("--strict");

if (!process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT) {
  process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT = path.resolve(__dirname, "..", "bundled");
}

/** Representative NL-style queries: single term, multi-token (AND→fallback), domain phrases. */
const CASES = [
  { label: "single-term", query: "wallet", k: 6 },
  { label: "multi-token", query: "plugin trading layout", k: 8 },
  { label: "phrase", query: "order entry", k: 6 },
  { label: "component-topic", query: "widget script ui", k: 8 },
  /** Path filter: only hits under packages whose path contains `markets`. */
  { label: "package-filter", query: "symbol", k: 6, packages: ["markets"] },
];

function preview(text, max = 120) {
  const t = (text ?? "").replace(/\s+/g, " ").trim();
  if (t.length <= max) return t;
  return `${t.slice(0, max - 1)}…`;
}

clearLoadBundleCache();
clearQmdStoreCache();

const root = process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT;
const bundle = loadBundle();
const facade = createAiDocsFacade(bundle);

const manifestPath = path.join(bundle.generatedRoot, "manifest.json");
const qmdRel =
  bundle.manifest.qmd && typeof bundle.manifest.qmd.indexPath === "string"
    ? bundle.manifest.qmd.indexPath
    : "qmd/index.sqlite";
const qmdDb = path.join(bundle.generatedRoot, qmdRel);

console.log("Natural-language search probe");
console.log("  PAXEER_NETWORK_AI_DOCS_REPO_ROOT:", root);
console.log("  manifest:", fs.existsSync(manifestPath) ? manifestPath : "(missing)");
console.log("  QMD DB:", fs.existsSync(qmdDb) ? qmdDb : "(missing — narrative hits will be empty)");
console.log("  strict:", strict);
console.log("");

if (!fs.existsSync(qmdDb)) {
  console.error(
    "No QMD index. Build one with: pnpm --filter @paxeer-network/ai-docs generate && pnpm --filter @paxeer-network/ai-docs qmd:index",
  );
  console.error("Then: pnpm --filter @paxeer-network/sdk-docs sync:bundle && pnpm --filter @paxeer-network/sdk-docs build");
  process.exit(strict ? 1 : 0);
}

let failures = 0;

for (const c of CASES) {
  const input = { query: c.query, k: c.k, ...(c.packages ? { packages: c.packages } : {}) };
  const started = performance.now();
  const res = await facade.searchDocs(input);
  const ms = Math.round(performance.now() - started);

  if (!res.ok) {
    console.log(`[${c.label}] FAIL`, res.error?.message ?? res);
    failures++;
    continue;
  }

  const hits = res.data.narrativeHits ?? [];
  console.log(`[${c.label}] query=${JSON.stringify(c.query)} k=${c.k}${c.packages ? ` packages=${JSON.stringify(c.packages)}` : ""} → ${hits.length} hits (${ms}ms)`);

  if (hits.length === 0) {
    failures++;
    console.log("  (no hits)");
  } else {
    const top = hits.slice(0, 3);
    for (let i = 0; i < top.length; i++) {
      const h = top[i];
      console.log(`  ${i + 1}. score=${h.score?.toFixed?.(4) ?? h.score} path=${h.path}`);
      console.log(`     ${preview(h.text ?? h.heading, 140)}`);
    }
  }
  console.log("");
}

if (strict && failures > 0) {
  console.error(`search-nl: strict mode — ${failures} case(s) had errors or zero hits`);
  process.exit(1);
}

console.error("search-nl OK", { cases: CASES.length, failures });
