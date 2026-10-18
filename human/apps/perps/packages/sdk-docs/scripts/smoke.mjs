/**
 * Minimal smoke: bundled/ exact + optional QMD narrative (run after `pnpm build` in this package).
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clearLoadBundleCache, clearQmdStoreCache, createAiDocsFacade, loadBundle } from "../dist/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const bundledRoot = path.resolve(__dirname, "..", "bundled");

process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT = bundledRoot;
clearLoadBundleCache();
clearQmdStoreCache();

const bundle = loadBundle();
const facade = createAiDocsFacade(bundle);

const comp = facade.getComponent("@paxeer-network/ui:Avatar");
if (!comp.ok) {
  console.error("FAIL getComponent:", comp);
  process.exit(1);
}

const qmdDb = path.join(bundledRoot, "qmd", "index.sqlite");
if (fs.existsSync(qmdDb)) {
  const search = await facade.searchDocs({ query: "wallet", k: 5 });
  if (!search.ok || !search.data.narrativeHits.length) {
    console.error("FAIL searchDocs:", search);
    process.exit(1);
  }
  console.log("smoke OK", {
    component: comp.data.name ?? comp.data.displayName,
    searchHits: search.data.narrativeHits.length,
  });
} else {
  console.log("smoke OK (searchDocs skipped — no qmd/index.sqlite; run ai-docs qmd:index then sync:bundle)", {
    component: comp.data.name ?? comp.data.displayName,
  });
}
