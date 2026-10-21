/**
 * Sidiora Launchpad AMM — Environment File Generator
 *
 * Generates .env files with deployed contract addresses, prefixed for
 * different project codebases (Next.js frontend, Vite frontend, backend).
 *
 * Usage:
 *   npx hardhat run scripts/env.js
 *
 * Output: deployments/env/
 *   ├── .env.nextjs        (NEXT_PUBLIC_ prefix)
 *   ├── .env.vite          (VITE_ prefix)
 *   ├── .env.backend       (SIDIORA_ prefix)
 *   └── .env.common        (no prefix, raw addresses)
 *
 * Requires: deployments/paxeer-addresses.json
 */

const fs = require("fs");
const path = require("path");

function main() {
  const NETWORK_TYPE = require("./lib/operations").networkName();
  const addrPath = path.join(__dirname, "..", "deployments", `${NETWORK_TYPE}-addresses.json`);
  if (!fs.existsSync(addrPath)) {
    throw new Error(`Addresses file not found: ${addrPath}\nRun deploy.js first (or set NETWORK_TYPE).`);
  }

  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  const meta = addresses._meta;

  console.log("\n📄 Generating environment files\n");

  // Map of logical name → address key
  const contractMap = {
    PROTOCOL_CONFIG: "ProtocolConfig_proxy",
    TREASURY: "Treasury_proxy",
    TIMELOCK: "Timelock",
    GOVERNANCE: "GovernanceModule_proxy",
    EVENT_EMITTER: "EventEmitter_proxy",
    POOL_REGISTRY: "PoolRegistry_proxy",
    FEE_ACCUMULATOR: "FeeAccumulator_proxy",
    POOL_BEACON: "PoolBeacon",
    NFT: "SidioraNFT_proxy",
    FACTORY: "SidioraFactory_proxy",
    OPTICAL_REGISTRY: "OpticalRegistry_proxy",
    ROUTER: "Router_proxy",
    QUOTER: "Quoter_proxy",
    FEES_ROUTER: "FeesRouter_proxy",
    // Optical presets
    ANTI_SNIPE_OPTICAL: "AntiSnipeOptical",
    MAX_WALLET_OPTICAL: "MaxWalletOptical",
    TAX_OPTICAL: "TaxOptical",
    COOLDOWN_OPTICAL: "CooldownOptical",
    BUYBACK_BURN_OPTICAL: "BuybackBurnOptical",
    // PECOR + Meta-AG — aggregator stack (proxy addresses are the user-facing ones)
    META_AG_ROUTER: "MetaAGRouter_proxy",
    META_AG_QUOTER: "MetaAGQuoter_proxy",
    PECOR_VAULT: "PECORVault_proxy",
    PECOR: "PECOR_proxy",
    PECOR_ORDERS: "PECOROrders_proxy",
    PRICE_ORACLE: "PriceOracle_proxy",
    ORACLE_HUB: "OracleHub_proxy",
    TRANSACTION_TRACKER: "TransactionTracker_proxy",
    // PECOR + Meta-AG — adapters (non-proxy)
    VAULT_ADAPTER: "VaultAdapter",
    SIDIORA_ADAPTER: "SidioraAdapter",
    PRICE_ORACLE_ADAPTER: "PriceOracleAdapter",
    SIDIORA_FEED_ADAPTER: "SidioraFeedAdapter",
  };

  // Network info
  const networkVars = {
    RPC_URL: meta.network === "125" ? "https://mainnet-beta.rpc.hyperpaxeer.com/rpc" : "http://127.0.0.1:8545",
    CHAIN_ID: meta.network,
    EXPLORER_URL: "https://paxscan.paxeer.app",
    USDL_ADDRESS: meta.usdl,
    SID_ADDRESS: meta.sid,
    WPAX_ADDRESS: addresses.WPAX,
  };

  function buildEnv(prefix, includeNetwork) {
    const lines = [];
    lines.push(`# Sidiora Launchpad AMM — Contract Addresses`);
    lines.push(`# Generated: ${new Date().toISOString()}`);
    lines.push(`# Network: Paxeer (Chain ID ${meta.network})`);
    lines.push(``);

    if (includeNetwork) {
      lines.push(`# ─── Network ───`);
      for (const [key, val] of Object.entries(networkVars)) {
        if (val) lines.push(`${prefix}${key}=${val}`);
      }
      lines.push(``);
    }

    lines.push(`# ─── Core Contracts ───`);
    for (const [logicalName, addrKey] of Object.entries(contractMap)) {
      const addr = addresses[addrKey];
      if (addr) {
        lines.push(`${prefix}${logicalName}_ADDRESS=${addr}`);
      }
    }

    lines.push(``);
    return lines.join("\n") + "\n";
  }

  const outDir = path.join(__dirname, "..", "deployments", "env");
  if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });

  // Next.js (.env.local format with NEXT_PUBLIC_ prefix)
  const nextjs = buildEnv("NEXT_PUBLIC_SIDIORA_", true);
  fs.writeFileSync(path.join(outDir, ".env.nextjs"), nextjs);
  console.log("  ✅ .env.nextjs");

  // Vite (VITE_ prefix)
  const vite = buildEnv("VITE_SIDIORA_", true);
  fs.writeFileSync(path.join(outDir, ".env.vite"), vite);
  console.log("  ✅ .env.vite");

  // Backend (SIDIORA_ prefix, includes network)
  const backend = buildEnv("SIDIORA_", true);
  fs.writeFileSync(path.join(outDir, ".env.backend"), backend);
  console.log("  ✅ .env.backend");

  // Common (no prefix, raw)
  const common = buildEnv("", true);
  fs.writeFileSync(path.join(outDir, ".env.common"), common);
  console.log("  ✅ .env.common");

  console.log(`\n  Output: ${outDir}\n`);
}

main();
