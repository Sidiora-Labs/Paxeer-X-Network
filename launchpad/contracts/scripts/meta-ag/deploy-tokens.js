/**
 * Sidiora Local Dev — Test Token Deployment
 *
 * Deploys the ERC20 tokens needed by the rest of the local-dev pipeline:
 *   - USDL   — 18 decimals, the stablecoin rail every Sidiora pool prices against
 *   - SID    — 6 decimals,  the governance token (proposal/quorum thresholds
 *                           assume 6-dec per scripts/deploy.js line ~105)
 *   - USDC   — 6 decimals,  reserve stablecoin for PECOR vault tests
 *   - USDT   — 6 decimals,  reserve stablecoin for PECOR vault tests
 *
 * Uses the already-in-repo MockStandardERC20 (mintable, standard ERC20).
 *
 * Flow:
 *   1. Run this script            → tokens deployed, addresses saved
 *   2. Export env vars it prints  → USDL_ADDRESS / SID_ADDRESS
 *   3. Run scripts/deploy.js      → Sidiora launchpad picks up the tokens
 *   4. Run scripts/meta-ag/deploy-pecor-meta-ag.js
 *   5. Run on-chain tests
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/deploy-tokens.js --network localhost
 *
 * Environment overrides (all optional):
 *   INITIAL_MINT       — human units minted to deployer (default 100_000_000)
 *   EXTRA_RECIPIENTS   — comma-separated 0x addresses that also receive the initial mint
 *   OUTPUT_FILE        — path to write addresses json (default: deployments/<network>-tokens.json)
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

const COLORS = {
  reset: "\x1b[0m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  cyan: "\x1b[36m",
  gray: "\x1b[90m",
};
const c = (col, s) => `${COLORS[col]}${s}${COLORS.reset}`;

async function main() {
  const [deployer] = await ethers.getSigners();
  const net = await ethers.provider.getNetwork();
  const chainId = Number(net.chainId);
  const NETWORK_TYPE =
    require("../lib/operations").networkName();

  const INITIAL_MINT_HUMAN = process.env.INITIAL_MINT || "100000000";
  const EXTRA_RECIPIENTS = (process.env.EXTRA_RECIPIENTS || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  console.log(`\n${c("yellow", "╔═══════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow", "║ Sidiora Local Dev — Test Token Deployment         ║")}`);
  console.log(`${c("yellow", "╚═══════════════════════════════════════════════════╝")}\n`);
  console.log(`  Deployer:        ${deployer.address}`);
  console.log(`  Network:         ${NETWORK_TYPE} (chainId ${chainId})`);
  console.log(`  Initial mint:    ${INITIAL_MINT_HUMAN} per token → deployer`);
  if (EXTRA_RECIPIENTS.length) {
    console.log(`  Extra recipients (same mint each):`);
    for (const r of EXTRA_RECIPIENTS) console.log(`    - ${r}`);
  }

  const SPECS = [
    { key: "USDL", name: "Sidiora USDL", symbol: "USDL", decimals: 6 },
    { key: "SID",  name: "Sidiora Governance", symbol: "SID",  decimals: 6 },
    { key: "USDC", name: "USD Coin",     symbol: "USDC", decimals: 6 },
    { key: "USDT", name: "Tether USD",   symbol: "USDT", decimals: 6 },
  ];

  const ERC20 = await ethers.getContractFactory("MockStandardERC20");
  const deployed = {};
  const recipients = [deployer.address, ...EXTRA_RECIPIENTS];

  for (const spec of SPECS) {
    console.log(`\n${c("cyan", `── ${spec.symbol} (${spec.decimals}-dec) ──`)}`);
    const t = await ERC20.deploy(spec.name, spec.symbol, spec.decimals);
    await t.waitForDeployment();
    const addr = await t.getAddress();
    await require("../lib/operations").checkpointDeployment(addr, "t");
    console.log(`  addr: ${c("cyan", addr)}`);
    deployed[spec.key] = {
      address: addr,
      name: spec.name,
      symbol: spec.symbol,
      decimals: spec.decimals,
    };

    const amount = ethers.parseUnits(INITIAL_MINT_HUMAN, spec.decimals);
    for (const to of recipients) {
      const tx = await t.mint(to, amount);
      await tx.wait();
      console.log(`  ${c("green", "✓")} mint ${INITIAL_MINT_HUMAN} ${spec.symbol} → ${to}`);
    }
  }

  // Persist addresses
  const outDir = path.join(__dirname, "..", "..", "deployments");
  if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });
  const outFile =
    process.env.OUTPUT_FILE ||
    path.join(outDir, `${NETWORK_TYPE}-tokens.json`);

  const payload = {
    _meta: {
      network: chainId.toString(),
      deployer: deployer.address,
      timestamp: new Date().toISOString(),
      initialMintHuman: INITIAL_MINT_HUMAN,
      recipients,
    },
    tokens: deployed,
  };
  require("../lib/operations").atomicWrite(outFile, payload);
  console.log(`\n${c("green", "✓")} Addresses saved to ${outFile}`);

  // Emit shell exports for the next stage
  console.log(`\n${c("yellow", "── Next step — export these, then run scripts/deploy.js ──")}`);
  console.log(`${c("gray", "# Run this in your shell (bash/zsh):")}\n`);
  console.log(`export USDL_ADDRESS=${deployed.USDL.address}`);
  console.log(`export SID_ADDRESS=${deployed.SID.address}`);
  console.log(`export NETWORK_TYPE=${NETWORK_TYPE}`);
  console.log(`\n${c("gray", "# Then:")}`);
  console.log(`${c("cyan", "npx hardhat run scripts/deploy.js --network " + NETWORK_TYPE)}`);
  console.log(`${c("cyan", "npx hardhat run scripts/meta-ag/deploy-pecor-meta-ag.js --network " + NETWORK_TYPE)}\n`);
}

main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error(c("reset", "\n❌ Token deployment failed:"), e);
    process.exit(1);
  });
