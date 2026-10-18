/**
 * Exercises every {@link createAiDocsFacade} method without MCP (stdio).
 *
 * Prereq: `pnpm run build` in this package. Data: `bundled/` unless PAXEER_NETWORK_AI_DOCS_REPO_ROOT is set.
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { clearLoadBundleCache, clearQmdStoreCache, createAiDocsFacade, loadBundle } from "../dist/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

if (!process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT) {
  process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT = path.resolve(__dirname, "..", "bundled");
}

clearLoadBundleCache();
clearQmdStoreCache();
const bundle = loadBundle();
const facade = createAiDocsFacade(bundle);

function assertOk(label, result) {
  if (!result.ok) {
    console.error(`FAIL ${label}:`, JSON.stringify(result, null, 2));
    process.exit(1);
  }
}

assertOk("getComponent", facade.getComponent("@paxeer-network/ui:Avatar"));
assertOk("getComponentDoc", facade.getComponentDoc("@paxeer-network/ui:Avatar"));
assertOk("getType", facade.getType("TabTypes"));
assertOk("getPackageSurface", facade.getPackageSurface("@paxeer-network/ui"));
assertOk("getReleaseContext", facade.getReleaseContext());
assertOk("getWorkflow", facade.getWorkflow("wallet-connect"));
assertOk("getRecipe", facade.getRecipe("order-minimal"));
assertOk("getGuardrails", facade.getGuardrails());

const qmdDb = path.join(process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT, "qmd", "index.sqlite");
if (fs.existsSync(qmdDb)) {
  const search = await facade.searchDocs({ query: "wallet", k: 5 });
  assertOk("searchDocs", search);
  if (!search.data.narrativeHits?.length) {
    console.error("FAIL searchDocs: expected narrativeHits.length > 0");
    process.exit(1);
  }
  console.error("exercise-facade OK (all facade methods returned ok)", {
    root: process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT,
    searchHits: search.data.narrativeHits.length,
    fetchSmoke: process.env.PAXEER_NETWORK_SDK_FETCH_SMOKE === "1" ? "ran" : "skipped",
  });
} else {
  console.error("exercise-facade OK (searchDocs skipped — no qmd/index.sqlite)", {
    root: process.env.PAXEER_NETWORK_AI_DOCS_REPO_ROOT,
    fetchSmoke: process.env.PAXEER_NETWORK_SDK_FETCH_SMOKE === "1" ? "ran" : "skipped",
  });
}

if (process.env.PAXEER_NETWORK_SDK_FETCH_SMOKE === "1") {
  /* Local manifest gitSha may not exist on github.com/PaxeerNetworkNetwork/js-sdk; default ref for smoke. */
  if (!process.env.PAXEER_NETWORK_SDK_GITHUB_REF) {
    process.env.PAXEER_NETWORK_SDK_GITHUB_REF = "main";
  }
  const comp = facade.getComponent("@paxeer-network/ui:Avatar");
  assertOk("fetch smoke getComponent", comp);
  const relPath = comp.meta.citations[0]?.path;
  if (!relPath) {
    console.error("FAIL fetch smoke: no citation path from getComponent");
    process.exit(1);
  }
  const fetched = await facade.fetchSdkSource({ relPath, line: 1 });
  assertOk("fetchSdkSource", fetched);
  if (!fetched.data.text?.length) {
    console.error("FAIL fetchSdkSource: empty text");
    process.exit(1);
  }
  console.error("fetchSdkSource smoke OK", { relPath, ref: fetched.data.ref, bytes: fetched.data.text.length });
}
