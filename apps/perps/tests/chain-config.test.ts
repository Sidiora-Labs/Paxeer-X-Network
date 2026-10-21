// Chain config tests: chain 125, one explorer origin for the chain object and paxscan links, RPC env list.
// Run via `pnpm test` — uses node:test + tsx loader (no Jest/Vitest required).

import { describe, it } from "node:test";
import { strict as assert } from "node:assert";
import { spawnSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve(__dirname, "..");
const CHAIN_PATH = path.join(ROOT, "src/core/chain.ts");
const URLS_PATH = path.join(ROOT, "src/core/paxscan/urls.ts");

const ENV_KEYS = [
  "NEXT_PUBLIC_PAXSCAN_BASE_URL",
  "NEXT_PUBLIC_PAXEER_RPC_URLS",
  "NEXT_PUBLIC_PAXEER_RPC_URL",
] as const;

/** Load chain + paxscan urls in a fresh process with the given env; prints the values as JSON. */
function loadWith(overrides: Partial<Record<(typeof ENV_KEYS)[number], string>>) {
  const env: NodeJS.ProcessEnv = { ...process.env };
  for (const key of ENV_KEYS) delete env[key];
  Object.assign(env, overrides);
  const script = `
    const { paxeerChain } = require(${JSON.stringify(CHAIN_PATH)});
    const { paxscanTxUrl } = require(${JSON.stringify(URLS_PATH)});
    process.stdout.write(JSON.stringify({
      id: paxeerChain.id,
      explorer: paxeerChain.blockExplorers.default.url,
      rpc: paxeerChain.rpcUrls.default.http,
      tx: paxscanTxUrl("0xab"),
    }));`;
  return spawnSync(process.execPath, ["--import", "tsx", "-e", script], {
    cwd: ROOT,
    env,
    encoding: "utf8",
  });
}

describe("chain config", () => {
  it("defaultsToMainnetExplorer", () => {
    const run = loadWith({});
    assert.equal(run.status, 0, run.stderr);
    const loaded = JSON.parse(run.stdout);
    assert.equal(loaded.id, 125);
    assert.equal(loaded.explorer, "https://paxscan.io");
    assert.equal(loaded.tx, "https://paxscan.io/tx/0xab");
  });

  it("explorerOverrideFeedsChainAndLinks", () => {
    const run = loadWith({ NEXT_PUBLIC_PAXSCAN_BASE_URL: "https://explorer.example" });
    assert.equal(run.status, 0, run.stderr);
    const loaded = JSON.parse(run.stdout);
    assert.equal(loaded.id, 125);
    assert.equal(loaded.explorer, "https://explorer.example");
    assert.equal(loaded.tx, "https://explorer.example/tx/0xab");
  });

  it("invalidExplorerFailsNamingTheVariable", () => {
    for (const bad of ["paxscan.io", "ftp://paxscan.io", "https://paxscan.io/path"]) {
      const run = loadWith({ NEXT_PUBLIC_PAXSCAN_BASE_URL: bad });
      assert.notEqual(run.status, 0, bad);
      assert.match(run.stderr, /NEXT_PUBLIC_PAXSCAN_BASE_URL/, bad);
    }
  });

  it("rpcUrlsComeFromEnvList", () => {
    const run = loadWith({ NEXT_PUBLIC_PAXEER_RPC_URLS: "https://a.example,https://b.example" });
    assert.equal(run.status, 0, run.stderr);
    assert.deepEqual(JSON.parse(run.stdout).rpc, ["https://a.example", "https://b.example"]);
  });
});
