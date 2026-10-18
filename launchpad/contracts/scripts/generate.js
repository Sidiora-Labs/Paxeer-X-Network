const fs = require("fs");
const path = require("path");

/**
 * Generate Integration Kit — Sidiora Launchpad AMM
 *
 * Creates /integration-kit/ with:
 *   /env/       — .env templates for every major framework
 *   /abi/       — .json + .ts + .js ABI files for each contract needed by frontends/backends
 *
 * Usage:
 *   node scripts/generate.js
 */

// ============================================================
//                    CONFIG
// ============================================================

const OUTPUT_DIR = path.join(__dirname, "..", "integration-kit");
const ARTIFACTS_DIR = path.join(__dirname, "..", "artifacts", "contracts");
const DEPLOYMENT_PATH = path.join(__dirname, "..", "deployments", require("./lib/operations").networkName() + "-addresses.json");

// Load deployment (optional — falls back to placeholder addresses if missing)
let deployment = {};
if (fs.existsSync(DEPLOYMENT_PATH)) {
  deployment = JSON.parse(fs.readFileSync(DEPLOYMENT_PATH, "utf8"));
} else {
  console.warn(`  Deployment file not found at ${DEPLOYMENT_PATH} — env addresses will be placeholders`);
}

// Address book: { ContractName: "0x..." } — looked up case-insensitively
const CONTRACT_ADDRESSES = deployment.contracts || Object.fromEntries(Object.entries(deployment).filter(([k]) => !k.startsWith("_")).map(([k, v]) => [k.replace(/_proxy$/, ""), v]));

// Optional list of trading tokens (analogous to file-1's `collateral` list)
const TOKENS = deployment.tokens || [];

// RPC + chain config
const CHAIN_ID = Number(deployment._meta?.network || deployment._meta?.chainId);
if (!Number.isSafeInteger(CHAIN_ID)) throw new Error("Deployment chain ID required");
const CHAIN_NAME = "Paxeer Network";
const RPC_URL = "https://public-mainnet.rpcpaxeer.online/app";
const EXPLORER_URL = "https://paxscan.paxeer.app";
const NATIVE_SYMBOL = "PAX";

// ============================================================
//   ARTIFACT SEARCH PATHS — (from file 2)
// ============================================================

const SEARCH_DIRS = [
  // Core Sidiora launchpad
  "protocol",
  "data",
  "core",
  "periphery",
  "opticals",
  "opticals/presets",
  "opticals/interfaces",
  "base",
  "interfaces",
  "libraries",
  // PECOR + Meta-AG aggregator stack
  "meta-ag/engine",
  "meta-ag/vault",
  "meta-ag/router",
  "meta-ag/quoter",
  "meta-ag/oracle",
  "meta-ag/oracle/adapters",
  "meta-ag/adapters",
  "meta-ag/analytics",
  "meta-ag/interfaces",
];

// ============================================================
//   ABIS TO EXTRACT — grouped by use case
// ============================================================

const ABI_GROUPS = {
  // --- Protocol Layer ---
  Protocol: [
    "ProtocolConfig",
    "Treasury",
    "Timelock",
    "GovernanceModule",
  ],

  // --- Data Layer ---
  Data: [
    "EventEmitter",
    "PoolRegistry",
    "FeeAccumulator",
  ],

  // --- Core (Factory + Pool + LP/Position tokens) ---
  Core: [
    "SidioraFactory",
    "SidioraPool",
    "PoolBeacon",
    "SidioraERC20",
    "SidioraNFT",
  ],

  // --- Opticals (modular pool extensions) ---
  Opticals: [
    "OpticalRegistry",
    "AntiSnipeOptical",
    "MaxWalletOptical",
    "TaxOptical",
    "CooldownOptical",
    "BuybackBurnOptical",
  ],

  // --- Periphery (user entry points) ---
  Periphery: [
    "Router",
    "Quoter",
    "FeesRouter",
  ],

  // --- Meta-AG + PECOR (aggregator + autonomous capital) ---
  MetaAG: [
    "MetaAGRouter",
    "MetaAGQuoter",
    "PECORVault",
    "PECOR",
    "PECOROrders",
    "PriceOracle",
    "OracleHub",
    "TransactionTracker",
  ],

  // --- Adapters (protocol + oracle bridges) ---
  Adapters: [
    "VaultAdapter",
    "SidioraAdapter",
    "PriceOracleAdapter",
    "SidioraFeedAdapter",
  ],

  // --- Interfaces (typed integration for viem/wagmi/ethers) ---
  Interfaces: [
    "IRouter",
    "IQuoter",
    "IFeesRouter",
    "ISidioraPool",
    "ISidioraFactory",
    "ISidioraNFT",
    "IProtocolConfig",
    "ITreasury",
    "IFeeAccumulator",
    "IPoolRegistry",
    "IEventEmitter",
    "IMetaAGRouter",
    "IMetaAGQuoter",
    "IPECORVault",
    "IPECOR",
    "IPECOROrders",
    "IPriceOracle",
    "IOracleHub",
    "ITransactionTracker",
    "IProtocolAdapter",
    "IDataFeedAdapter",
  ],
};

// Flat list for iteration
const ALL_CONTRACTS = Object.values(ABI_GROUPS).flat();

// Contracts that get an address env var (skip per-pool contracts and pure interfaces)
const ADDRESSABLE_GROUPS = ["Protocol", "Data", "Core", "Opticals", "Periphery", "MetaAG", "Adapters"];
const ADDRESSABLE = ADDRESSABLE_GROUPS.flatMap(g => ABI_GROUPS[g])
  // SidioraPool and SidioraERC20 are deployed per-pool — no fixed address
  .filter(n => n !== "SidioraPool" && n !== "SidioraERC20");

// ============================================================
//                    HELPERS
// ============================================================

function ensureDir(dir) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
}

function findArtifact(contractName) {
  for (const dir of SEARCH_DIRS) {
    const artifactPath = path.join(
      ARTIFACTS_DIR,
      dir,
      `${contractName}.sol`,
      `${contractName}.json`
    );
    if (fs.existsSync(artifactPath)) return artifactPath;
  }
  return null;
}

function extractAbi(contractName) {
  const artifactPath = findArtifact(contractName);
  if (!artifactPath) {
    console.error(`  Missing artifact: ${contractName}`);
    return null;
  }
  const artifact = JSON.parse(fs.readFileSync(artifactPath, "utf8"));
  return artifact.abi;
}

// PECORVault -> PECOR_VAULT, MetaAGRouter -> META_AG_ROUTER, SidioraNFT -> SIDIORA_NFT
function camelToScreamingSnake(str) {
  return str
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1_$2")
    .toUpperCase();
}

// Case-insensitive lookup against the deployment.contracts map
function lookupAddress(name) {
  if (CONTRACT_ADDRESSES[name]) return CONTRACT_ADDRESSES[name];
  const lower = name.charAt(0).toLowerCase() + name.slice(1);
  if (CONTRACT_ADDRESSES[lower]) return CONTRACT_ADDRESSES[lower];
  for (const k of Object.keys(CONTRACT_ADDRESSES)) {
    if (k.toLowerCase() === name.toLowerCase()) return CONTRACT_ADDRESSES[k];
  }
  if (process.env.TEMPLATE_MODE === "true") return "0x0000000000000000000000000000000000000000";
  throw new Error(`Missing deployment address for ${name}; set TEMPLATE_MODE=true only for an explicit template`);
}

// ============================================================
//               GENERATE ABI FILES
// ============================================================

function generateAbis() {
  const abiDir = path.join(OUTPUT_DIR, "abi");
  ensureDir(abiDir);

  const indexExportsJs = [];
  const indexExportsTs = [];
  const generated = [];
  const skipped = [];

  for (const name of ALL_CONTRACTS) {
    const abi = extractAbi(name);
    if (!abi || abi.length === 0) {
      skipped.push(name);
      continue;
    }

    const jsonFile = path.join(abiDir, `${name}.json`);
    const jsFile = path.join(abiDir, `${name}.js`);
    const tsFile = path.join(abiDir, `${name}.ts`);

    // .json — raw ABI array
    require("./lib/operations").atomicWrite(jsonFile, abi);

    // .js — CommonJS export
    fs.writeFileSync(jsFile,
      `/** ${name} ABI — auto-generated by generate-integration-kit.js */\n` +
      `const ${name}ABI = ${JSON.stringify(abi, null, 2)};\n\n` +
      `module.exports = { ${name}ABI };\n`
    );

    // .ts — TypeScript const assertion for viem/wagmi
    fs.writeFileSync(tsFile,
      `/** ${name} ABI — auto-generated by generate-integration-kit.js */\n` +
      `export const ${name}ABI = ${JSON.stringify(abi, null, 2)} as const;\n`
    );

    indexExportsJs.push(`const { ${name}ABI } = require("./${name}");`);
    indexExportsTs.push(`export { ${name}ABI } from "./${name}";`);

    console.log(`  ${name}: ${abi.length} entries`);
    generated.push(name);
  }

  // index.js — barrel export (CommonJS)
  const jsBarrel =
    `/** Barrel export — all ABIs */\n` +
    indexExportsJs.join("\n") + "\n\n" +
    `module.exports = {\n` +
    generated.map(n => `  ${n}ABI,`).join("\n") + "\n};\n";
  fs.writeFileSync(path.join(abiDir, "index.js"), jsBarrel);

  // index.ts — barrel export (ESM/TypeScript)
  const tsBarrel =
    `/** Barrel export — all ABIs */\n` +
    indexExportsTs.join("\n") + "\n";
  fs.writeFileSync(path.join(abiDir, "index.ts"), tsBarrel);

  // index.json — name -> file map (handy for non-JS consumers)
  const indexJson = {};
  for (const name of generated) indexJson[name] = `./${name}.json`;
  fs.writeFileSync(path.join(abiDir, "index.json"), JSON.stringify(indexJson, null, 2));

  console.log(`\n  index.js + index.ts + index.json barrel exports written`);
  if (skipped.length) {
    console.log(`  ${skipped.length} skipped (artifact not found): ${skipped.join(", ")}`);
  }

  return { generated, skipped };
}

// ============================================================
//              GENERATE ENV FILES
// ============================================================

function buildEnvVars(prefix) {
  const lines = [
    `# Sidiora Launchpad AMM — Environment Variables`,
    `# Generated: ${new Date().toISOString()}`,
    `# Chain: ${CHAIN_NAME} (ID: ${CHAIN_ID})`,
    ``,
    `# ── Network ──`,
    `${prefix}RPC_URL=${RPC_URL}`,
    `${prefix}CHAIN_ID=${CHAIN_ID}`,
    `${prefix}CHAIN_NAME=${CHAIN_NAME}`,
    `${prefix}EXPLORER_URL=${EXPLORER_URL}`,
    `${prefix}NATIVE_SYMBOL=${NATIVE_SYMBOL}`,
    ``,
    `# ── Contract Addresses ──`,
  ];

  for (const groupName of ADDRESSABLE_GROUPS) {
    const names = ABI_GROUPS[groupName].filter(n => ADDRESSABLE.includes(n));
    if (names.length === 0) continue;

    lines.push(`# -- ${groupName} --`);
    for (const name of names) {
      const envKey = camelToScreamingSnake(name);
      const addr = lookupAddress(name);
      lines.push(`${prefix}${envKey}_ADDRESS=${addr}`);
    }
    lines.push(``);
  }

  // Optional token list (e.g. WPAX, USDC, common quote tokens)
  if (TOKENS.length > 0) {
    lines.push(`# ── Tokens ──`);
    for (const t of TOKENS) {
      lines.push(`${prefix}${t.symbol}_ADDRESS=${t.address}`);
    }
    lines.push(``);
  }

  lines.push(
    `# ── WalletConnect / Web3Modal (frontend only) ──`,
    `${prefix}WALLETCONNECT_PROJECT_ID=YOUR_PROJECT_ID_HERE`,
    ``,
    `# ── Backend / Keeper (server-side only — NEVER prefix with NEXT_PUBLIC_ etc.) ──`,
    `# DEPLOYER_PRIVATE_KEY=0x_YOUR_PRIVATE_KEY_HERE`,
    `# KEEPER_PRIVATE_KEY=0x_YOUR_PRIVATE_KEY_HERE`,
    `# ORACLE_PRIVATE_KEY=0x_YOUR_PRIVATE_KEY_HERE`,
  );

  return lines.join("\n") + "\n";
}

function generateEnvFiles() {
  const envDir = path.join(OUTPUT_DIR, "env");
  ensureDir(envDir);

  const frameworks = [
    {
      name: "nextjs",
      file: ".env.local.nextjs",
      prefix: "NEXT_PUBLIC_",
      desc: "Next.js (App Router / Pages Router)",
    },
    {
      name: "vite",
      file: ".env.vite",
      prefix: "VITE_",
      desc: "Vite (React, Vue, Svelte, Solid)",
    },
    {
      name: "create-react-app",
      file: ".env.cra",
      prefix: "REACT_APP_",
      desc: "Create React App",
    },
    {
      name: "nuxt",
      file: ".env.nuxt",
      prefix: "NUXT_PUBLIC_",
      desc: "Nuxt 3",
    },
    {
      name: "sveltekit",
      file: ".env.sveltekit",
      prefix: "PUBLIC_",
      desc: "SvelteKit",
    },
    {
      name: "remix",
      file: ".env.remix",
      prefix: "",
      desc: "Remix (no prefix — use loader to expose to client)",
    },
    {
      name: "node-backend",
      file: ".env.node",
      prefix: "",
      desc: "Node.js / Express / Fastify (backend — no prefix needed)",
    },
    {
      name: "python",
      file: ".env.python",
      prefix: "",
      desc: "Python (FastAPI / Flask / Django)",
    },
  ];

  for (const fw of frameworks) {
    const header =
      `# ═══════════════════════════════════════════════════\n` +
      `# ${fw.desc}\n` +
      `# Framework: ${fw.name}\n` +
      `# Prefix: ${fw.prefix || "(none)"}\n` +
      `# ═══════════════════════════════════════════════════\n\n`;

    const content = header + buildEnvVars(fw.prefix);
    const filePath = path.join(envDir, fw.file);
    fs.writeFileSync(filePath, content);
    console.log(`  ${fw.file} (${fw.desc})`);
  }

  // Generic .env template (no prefix)
  const genericHeader =
    `# ═══════════════════════════════════════════════════\n` +
    `# Generic .env template (copy and add your framework prefix)\n` +
    `#\n` +
    `# Prefixes by framework:\n` +
    `#   Next.js:    NEXT_PUBLIC_\n` +
    `#   Vite:       VITE_\n` +
    `#   CRA:        REACT_APP_\n` +
    `#   Nuxt 3:     NUXT_PUBLIC_\n` +
    `#   SvelteKit:  PUBLIC_\n` +
    `#   Remix:      (none — use loader)\n` +
    `#   Node/Py:    (none)\n` +
    `# ═══════════════════════════════════════════════════\n\n`;

  fs.writeFileSync(
    path.join(envDir, ".env.template"),
    genericHeader + buildEnvVars("")
  );
  console.log(`  .env.template (generic)`);
}

// ============================================================
//              GENERATE README
// ============================================================

function generateReadme(generated) {
  const readme = `# Sidiora Launchpad AMM — Integration Kit

Auto-generated by \`scripts/generate-integration-kit.js\`.

## Structure

\`\`\`
integration-kit/
  env/                          # Environment variable templates
    .env.template               # Generic (no prefix)
    .env.local.nextjs           # Next.js (NEXT_PUBLIC_)
    .env.vite                   # Vite (VITE_)
    .env.cra                    # Create React App (REACT_APP_)
    .env.nuxt                   # Nuxt 3 (NUXT_PUBLIC_)
    .env.sveltekit              # SvelteKit (PUBLIC_)
    .env.remix                  # Remix (no prefix)
    .env.node                   # Node.js backend (no prefix)
    .env.python                 # Python backend (no prefix)
  abi/                          # Contract ABIs
    {Name}.json                 # Raw ABI array
    {Name}.js                   # CommonJS export
    {Name}.ts                   # TypeScript const assertion (viem/wagmi)
    index.js                    # Barrel export (CJS)
    index.ts                    # Barrel export (ESM)
    index.json                  # Name → file map
\`\`\`

## Network

- **Chain**: ${CHAIN_NAME} (ID: ${CHAIN_ID})
- **RPC**: ${RPC_URL}
- **Explorer**: ${EXPLORER_URL}
- **Native**: ${NATIVE_SYMBOL}

## Available ABIs

| ABI | Use Case |
|-----|----------|
${generated.map(n => `| \`${n}ABI\` | ${getUseCaseLabel(n)} |`).join("\n")}

## Quick Start (Next.js + viem)

\`\`\`ts
import { createPublicClient, http } from "viem";
import { RouterABI } from "./abi";

const paxeer = {
  id: ${CHAIN_ID},
  name: "${CHAIN_NAME}",
  nativeCurrency: { name: "PAX", symbol: "PAX", decimals: 18 },
  rpcUrls: { default: { http: ["${RPC_URL}"] } },
} as const;

const client = createPublicClient({ chain: paxeer, transport: http() });

const amountOut = await client.readContract({
  address: process.env.NEXT_PUBLIC_ROUTER_ADDRESS as \`0x\${string}\`,
  abi: RouterABI,
  functionName: "getAmountOut",
  args: [amountIn, path],
});
\`\`\`

## Quick Start (Node.js + ethers)

\`\`\`js
const { ethers } = require("ethers");
const { RouterABI } = require("./abi");

const provider = new ethers.JsonRpcProvider("${RPC_URL}");
const router = new ethers.Contract(process.env.ROUTER_ADDRESS, RouterABI, provider);

const reserves = await router.getReserves(tokenA, tokenB);
console.log(reserves);
\`\`\`

## Deployment File Format

Place at \`deployments/sidiora.json\`:

\`\`\`json
{
  "chainId": ${CHAIN_ID},
  "contracts": {
    "ProtocolConfig":   "0x...",
    "Treasury":         "0x...",
    "Timelock":         "0x...",
    "GovernanceModule": "0x...",
    "EventEmitter":     "0x...",
    "PoolRegistry":     "0x...",
    "FeeAccumulator":   "0x...",
    "SidioraFactory":   "0x...",
    "PoolBeacon":       "0x...",
    "SidioraNFT":       "0x...",
    "OpticalRegistry":  "0x...",
    "Router":           "0x...",
    "Quoter":           "0x...",
    "FeesRouter":       "0x...",
    "MetaAGRouter":     "0x...",
    "MetaAGQuoter":     "0x...",
    "PECORVault":       "0x...",
    "PECOR":            "0x...",
    "PECOROrders":      "0x...",
    "PriceOracle":      "0x...",
    "OracleHub":        "0x..."
  },
  "tokens": [
    { "symbol": "WPAX", "address": "0x..." },
    { "symbol": "USDC", "address": "0x..." }
  ]
}
\`\`\`

> **Note**: \`SidioraPool\` and \`SidioraERC20\` are deployed per-pool via the beacon
> proxy pattern, so they don't get a fixed address in env files. Look up live pool
> addresses via \`SidioraFactory.getPool(tokenA, tokenB)\` or \`PoolRegistry\`.
`;

  fs.writeFileSync(path.join(OUTPUT_DIR, "README.md"), readme);
  console.log(`  README.md`);
}

function getUseCaseLabel(name) {
  const labels = {
    // Protocol
    ProtocolConfig: "Global protocol parameters & feature flags",
    Treasury: "Protocol fee collection & disbursement",
    Timelock: "Time-delayed governance actions",
    GovernanceModule: "On-chain governance (proposals, voting)",
    // Data
    EventEmitter: "Centralized event emission for indexers",
    PoolRegistry: "Pool discovery & metadata lookup",
    FeeAccumulator: "Per-pool fee tracking & distribution",
    // Core
    SidioraFactory: "Pool creation & deployment",
    SidioraPool: "Per-pool AMM logic (deployed via beacon proxy)",
    PoolBeacon: "Upgradeable pool implementation pointer",
    SidioraERC20: "Pool LP token (per-pool)",
    SidioraNFT: "Position NFT for liquidity providers",
    // Opticals
    OpticalRegistry: "Optical module registration & lookup",
    AntiSnipeOptical: "Anti-bot snipe protection at launch",
    MaxWalletOptical: "Per-wallet position cap",
    TaxOptical: "Buy/sell tax routing",
    CooldownOptical: "Trade cooldown enforcement",
    BuybackBurnOptical: "Automated buyback & burn from fees",
    // Periphery
    Router: "User entry point — swap, add/remove liquidity",
    Quoter: "Off-chain quote simulation (swap pricing)",
    FeesRouter: "Fee collection & routing entry point",
    // Meta-AG
    MetaAGRouter: "Multi-protocol aggregated swap routing",
    MetaAGQuoter: "Multi-protocol aggregated quoting",
    PECORVault: "PECOR managed vault (capital allocation)",
    PECOR: "PECOR engine (predictive execution & routing)",
    PECOROrders: "PECOR conditional/limit order management",
    PriceOracle: "Aggregated price oracle",
    OracleHub: "Oracle adapter registry & routing",
    TransactionTracker: "On-chain transaction analytics",
    // Adapters
    VaultAdapter: "PECOR vault → external protocol bridge",
    SidioraAdapter: "Meta-AG → Sidiora pool adapter",
    PriceOracleAdapter: "External price feed adapter",
    SidioraFeedAdapter: "Sidiora pool price feed adapter",
    // Interfaces
    IRouter: "Router typed interface",
    IQuoter: "Quoter typed interface",
    IFeesRouter: "FeesRouter typed interface",
    ISidioraPool: "Pool typed interface",
    ISidioraFactory: "Factory typed interface",
    ISidioraNFT: "Position NFT typed interface",
    IProtocolConfig: "ProtocolConfig typed interface",
    ITreasury: "Treasury typed interface",
    IFeeAccumulator: "FeeAccumulator typed interface",
    IPoolRegistry: "PoolRegistry typed interface",
    IEventEmitter: "EventEmitter typed interface",
    IMetaAGRouter: "Meta-AG router typed interface",
    IMetaAGQuoter: "Meta-AG quoter typed interface",
    IPECORVault: "PECORVault typed interface",
    IPECOR: "PECOR engine typed interface",
    IPECOROrders: "PECOROrders typed interface",
    IPriceOracle: "PriceOracle typed interface",
    IOracleHub: "OracleHub typed interface",
    ITransactionTracker: "TransactionTracker typed interface",
    IProtocolAdapter: "Protocol adapter typed interface",
    IDataFeedAdapter: "Data feed adapter typed interface",
  };
  return labels[name] || name;
}

// ============================================================
//                     MAIN
// ============================================================

console.log("╔═══════════════════════════════════════════════════════════╗");
console.log("║       SIDIORA — GENERATE INTEGRATION KIT                  ║");
console.log("╚═══════════════════════════════════════════════════════════╝\n");

ensureDir(OUTPUT_DIR);

console.log("━━━ ABIs ━━━\n");
const { generated, skipped } = generateAbis();

console.log("\n━━━ ENV Templates ━━━\n");
generateEnvFiles();

console.log("\n━━━ Docs ━━━\n");
generateReadme(generated);

console.log("\n━━━ Done ━━━\n");
console.log(`  Output:    ${OUTPUT_DIR}`);
console.log(`  ABIs:      ${generated.length} contracts (${skipped.length} skipped)`);
console.log(`  Envs:      9 framework templates`);
console.log("");