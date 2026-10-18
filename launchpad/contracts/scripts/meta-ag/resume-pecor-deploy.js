/**
 * Sidiora Meta-AG + PECOR — Resume / Repair Wiring Script
 *
 * Picks up where `deploy-pecor-meta-ag.js` left off when the RPC dropped mid-wiring.
 * Reads deployed addresses from `deployments/paxeer-addresses.json` and only sends
 * transactions for steps still pending. Every tx is wrapped in:
 *
 *   - Pre-check: skip if already done on-chain (where the call would revert on duplicate)
 *   - Retry loop: 3 attempts with exponential backoff on socket / transient errors
 *   - Persist: write updated wiringDone/wiringPending to JSON after each success
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/resume-pecor-deploy.js --network paxeer-network
 *
 * Honors the same env vars as deploy-pecor-meta-ag.js (RELAYER_ADDRESS, KEEPER_ADDRESS,
 * FEE_COLLECTOR, TRANSFER_ADMIN_TO_TIMELOCK, etc.). Anything unset falls back to the
 * values stored in `_meta_pecor` from the original (partial) deploy.
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

const COLORS = {
  reset: "\x1b[0m",
  green: "\x1b[32m",
  yellow: "\x1b[33m",
  red: "\x1b[31m",
  cyan: "\x1b[36m",
  gray: "\x1b[90m",
};
const c = (col, s) => `${COLORS[col]}${s}${COLORS.reset}`;
const step = (s) => console.log(`\n${c("yellow", `── ${s} ──`)}`);
const line = (label, val) => console.log(`  ${label.padEnd(28)} ${c("cyan", val)}`);
const ok = (msg) => console.log(`  ${c("green", "✓")} ${msg}`);
const skip = (msg) => console.log(`  ${c("gray", "○")} ${c("gray", msg)}`);
const warn = (msg) => console.log(`  ${c("red", "✗")} ${c("red", msg)}`);

const ADDR_FILE = path.join(__dirname, "..", "..", "deployments", require("../lib/operations").networkName() + "-addresses.json");

function loadAddresses() {
  return JSON.parse(fs.readFileSync(ADDR_FILE, "utf8"));
}

function saveAddresses(addresses) {
  require("../lib/operations").atomicWrite(ADDR_FILE, addresses);
}

function markDone(addresses, label) {
  const meta = addresses._meta_pecor;
  if (!meta) return;
  meta.wiringDone = meta.wiringDone || [];
  meta.wiringPending = meta.wiringPending || [];
  if (!meta.wiringDone.includes(label)) meta.wiringDone.push(label);
  meta.wiringPending = meta.wiringPending.filter((p) => p !== label);
  meta.wiringStatus = meta.wiringPending.length === 0 ? "complete" : "partial";
  saveAddresses(addresses);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function errorBlob(err) {
  return `${err?.shortMessage || ""} ${err?.reason || ""} ${err?.message || ""} ${JSON.stringify(err?.info || err?.error || {})}`.toLowerCase();
}

function isTransientError(err) {
  if (!err) return false;
  const code = err.code || err.error?.code;
  const blob = errorBlob(err);
  return (
    code === "UND_ERR_SOCKET" ||
    code === "ECONNRESET" ||
    code === "ETIMEDOUT" ||
    code === "ENETUNREACH" ||
    code === "EAI_AGAIN" ||
    blob.includes("socket") ||
    blob.includes("timeout") ||
    blob.includes("could not detect network") ||
    blob.includes("connection") ||
    blob.includes("fetch failed") ||
    blob.includes("other side closed")
  );
}

function isNonceTooLowError(err) {
  const blob = errorBlob(err);
  return (
    blob.includes("nonce too low") ||
    blob.includes("already known") ||
    blob.includes("already exists") ||
    blob.includes("known transaction") ||
    blob.includes("replacement transaction underpriced")
  );
}

function isInvalidSequenceError(err) {
  // Cosmos SDK strict sequence error: "invalid nonce; got X, expected Y"
  const blob = errorBlob(err);
  return blob.includes("invalid nonce") || blob.includes("invalid sequence");
}

async function withRetry(fn, label, attempts = 4, baseDelayMs = 2000) {
  let lastErr;
  for (let i = 0; i < attempts; i += 1) {
    try {
      return await fn();
    } catch (err) {
      lastErr = err;
      const transient = isTransientError(err);
      if (!transient || i === attempts - 1) throw err;
      const delay = baseDelayMs * 2 ** i;
      console.log(
        `    ${c("gray", `[retry ${i + 1}/${attempts - 1}] ${label}: ${err.code || err.shortMessage || err.message?.slice(0, 80)} — waiting ${delay}ms`)}`
      );
      await sleep(delay);
    }
  }
  throw lastErr;
}

// ─────────────────────────────────────────────────────────────────────────── //
// Sequential nonce manager — sourced from "latest" (confirmed) state.         //
// Avoids "pending" nonce desync when sockets drop mid-broadcast and a phantom //
// tx sits in one node's mempool but never reaches consensus.                  //
// ─────────────────────────────────────────────────────────────────────────── //
let _signer = null;
let _nonce = null;

async function initNonce(signer) {
  _signer = signer;
  _nonce = await withRetry(
    () => signer.provider.getTransactionCount(signer.address, "pending"),
    "fetch initial nonce"
  );
  console.log(`  ${c("gray", `Starting nonce (from chain "latest"): ${_nonce}`)}`);
}

async function resyncNonce(reason) {
  const fresh = await withRetry(
    () => _signer.provider.getTransactionCount(_signer.address, "pending"),
    "resync nonce"
  );
  console.log(`  ${c("gray", `[nonce resync — ${reason}] ${_nonce} → ${fresh}`)}`);
  _nonce = fresh;
}

/**
 * Send a tx with an explicit nonce override. buildFn(nonce) MUST return a
 * tx promise — callers add `{ nonce }` to their contract method overrides.
 */
async function sendTx(buildFn, label) {
  // Never retry writes at a fresh nonce after an ambiguous broadcast/receipt.
  // Reads may retry independently; operator reconciliation is required for writes.
  const useNonce = _nonce;
  let tx;
  try { tx = await buildFn(useNonce); }
  catch (err) { throw new Error(`${label}: broadcast unresolved at nonce ${useNonce}; reconcile receipt and semantic postcondition before retrying`, { cause: err }); }
  await require("../lib/operations").checkpointTransaction(tx, label, "broadcast");
  let receipt;
  try { receipt = await tx.wait(); }
  catch (err) { throw new Error(`${label}: confirmation unresolved for ${tx.hash}; reconcile before retrying`, { cause: err }); }
  if (!receipt || Number(receipt.status) !== 1) throw new Error(`${label}: transaction did not succeed ${tx.hash}`);
  await require("../lib/operations").checkpointTransaction(tx, label, "confirmed", receipt);
  _nonce = useNonce + 1;
  ok(`${label} (nonce ${useNonce}, tx ${tx.hash})`);
  return tx;
}

async function main() {
  const [deployer] = await ethers.getSigners();
  const net = await ethers.provider.getNetwork();
  const chainId = Number(net.chainId);

  console.log(`\n${c("yellow", "╔══════════════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow", "║ Sidiora Meta-AG + PECOR — Resume Wiring                  ║")}`);
  console.log(`${c("yellow", "╚══════════════════════════════════════════════════════════╝")}`);

  const addresses = loadAddresses();
  await require("../lib/operations").validateManifest(addresses, ethers.provider);
  if (addresses._metaAgDeployment) {
    const previousResume = process.env.RESUME_DEPLOYMENT;
    process.env.RESUME_DEPLOYMENT = "true";
    try { return await require("./deploy-pecor-meta-ag").main(); }
    finally {
      if (previousResume === undefined) delete process.env.RESUME_DEPLOYMENT;
      else process.env.RESUME_DEPLOYMENT = previousResume;
    }
  }
  const meta = addresses._meta_pecor;
  if (!meta) {
    throw new Error("No `_meta_pecor` block found in paxeer-addresses.json. Run deploy-pecor-meta-ag.js first.");
  }

  const RELAYER = process.env.RELAYER_ADDRESS || meta.relayer;
  const KEEPER = process.env.KEEPER_ADDRESS || meta.keeper;
  const ADMIN = process.env.ADMIN_ADDRESS || meta.admin || addresses.Timelock;
  const FEE_COLLECTOR = process.env.FEE_COLLECTOR || meta.feeCollector;
  const TRANSFER_ADMIN = process.env.TRANSFER_ADMIN_TO_TIMELOCK === "true";

  line("Deployer", deployer.address);
  line("Balance", `${ethers.formatEther(await ethers.provider.getBalance(deployer.address))} PAX`);
  line("Network", `chainId ${chainId}`);
  line("Relayer", RELAYER);
  line("Keeper", KEEPER);
  line("Admin (Timelock)", ADMIN);
  line("Fee collector", FEE_COLLECTOR);
  line("Transfer admin", TRANSFER_ADMIN ? "yes (one-way)" : "no");

  // ────────────────────────────────────────────────────────────────────── //
  // Attach to deployed contracts                                            //
  // ────────────────────────────────────────────────────────────────────── //
  const required = [
    "PriceOracle_proxy",
    "OracleHub_proxy",
    "TransactionTracker_proxy",
    "PECORVault_proxy",
    "PECOR_proxy",
    "PECOROrders_proxy",
    "PriceOracleAdapter",
    "SidioraFeedAdapter",
    "VaultAdapter",
    "SidioraAdapter",
    "MetaAGRouter_proxy",
    "MetaAGQuoter_proxy",
    "WPAX",
  ];
  for (const k of required) {
    if (!addresses[k]) throw new Error(`Missing required address "${k}" in ${ADDR_FILE}`);
  }

  const USDL = addresses._meta?.usdl || addresses.tokens?.USDL;
  if (!USDL) throw new Error("Missing USDL address in paxeer-addresses.json");
  const WPAX = addresses.WPAX;

  step("Attaching to deployed contracts");
  const priceOracle = await ethers.getContractAt("PriceOracle", addresses.PriceOracle_proxy);
  const oracleHub = await ethers.getContractAt("OracleHub", addresses.OracleHub_proxy);
  const tracker = await ethers.getContractAt("TransactionTracker", addresses.TransactionTracker_proxy);
  const vault = await ethers.getContractAt("PECORVault", addresses.PECORVault_proxy);
  const pecor = await ethers.getContractAt("PECOR", addresses.PECOR_proxy);
  const pecorOrders = await ethers.getContractAt("PECOROrders", addresses.PECOROrders_proxy);
  const router = await ethers.getContractAt("MetaAGRouter", addresses.MetaAGRouter_proxy);
  const priceOracleAdapter = await ethers.getContractAt("PriceOracleAdapter", addresses.PriceOracleAdapter);
  const sidioraFeedAdapter = await ethers.getContractAt("SidioraFeedAdapter", addresses.SidioraFeedAdapter);
  const vaultAdapter = await ethers.getContractAt("VaultAdapter", addresses.VaultAdapter);
  const sidioraAdapter = await ethers.getContractAt("SidioraAdapter", addresses.SidioraAdapter);
  const quoter = await ethers.getContractAt("MetaAGQuoter", addresses.MetaAGQuoter_proxy);
  ok(`12 contracts attached`);

  // Initialize sequential nonce manager from confirmed state
  await initNonce(deployer);

  // ───────────────────────────────────────────────────────────────────── //
  // 1. Idempotent role wiring (overwrite-style — re-running is safe)        //
  // ───────────────────────────────────────────────────────────────────── //
  step("Role wiring (idempotent)");

  // PriceOracle relayer
  const relayerOk = await withRetry(() => priceOracle.authorizedRelayers(RELAYER), "check relayer");
  if (relayerOk) {
    skip(`PriceOracle.setRelayer(${RELAYER}) — already authorized`);
    markDone(addresses, "PriceOracle.setRelayer(RELAYER)");
  } else {
    await sendTx((nonce) => priceOracle.setRelayer(RELAYER, true, { nonce }), `PriceOracle.setRelayer(${RELAYER})`);
    markDone(addresses, "PriceOracle.setRelayer(RELAYER)");
  }

  // Vault operators
  const operatorTargets = [
    { name: "PECOR", addr: addresses.PECOR_proxy, label: "PECORVault.setOperator(PECOR)" },
    { name: "PECOROrders", addr: addresses.PECOROrders_proxy, label: "PECORVault.setOperator(PECOROrders)" },
    { name: "VaultAdapter", addr: addresses.VaultAdapter, label: "PECORVault.setOperator(VaultAdapter)" },
  ];
  for (const t of operatorTargets) {
    const isOp = await withRetry(() => vault.authorizedOperators(t.addr), `check operator ${t.name}`);
    if (isOp) {
      skip(`PECORVault.setOperator(${t.name}) — already authorized`);
      markDone(addresses, t.label);
    } else {
      await sendTx((nonce) => vault.setOperator(t.addr, true, { nonce }), `PECORVault.setOperator(${t.name})`);
      markDone(addresses, t.label);
    }
  }

  // Orders keeper
  const isKeeper = await withRetry(() => pecorOrders.keepers(KEEPER), "check keeper");
  if (isKeeper) {
    skip(`PECOROrders.setKeeper(${KEEPER}) — already authorized`);
    markDone(addresses, "PECOROrders.setKeeper(KEEPER)");
  } else {
    await sendTx((nonce) => pecorOrders.setKeeper(KEEPER, true, { nonce }), `PECOROrders.setKeeper(${KEEPER})`);
    markDone(addresses, "PECOROrders.setKeeper(KEEPER)");
  }

  // Tracker emitters
  const emitterTargets = [
    { name: "PECOR", addr: addresses.PECOR_proxy, label: "TransactionTracker.setAuthorizedEmitter(PECOR)" },
    { name: "PECOROrders", addr: addresses.PECOROrders_proxy, label: "TransactionTracker.setAuthorizedEmitter(PECOROrders)" },
    { name: "MetaAGRouter", addr: addresses.MetaAGRouter_proxy, label: "TransactionTracker.setAuthorizedEmitter(MetaAGRouter)" },
  ];
  for (const t of emitterTargets) {
    const isEmitter = await withRetry(() => tracker.authorizedEmitters(t.addr), `check emitter ${t.name}`);
    if (isEmitter) {
      skip(`TransactionTracker.setAuthorizedEmitter(${t.name}) — already authorized`);
      markDone(addresses, t.label);
    } else {
      await sendTx(
        (nonce) => tracker.setAuthorizedEmitter(t.addr, true, { nonce }),
        `TransactionTracker.setAuthorizedEmitter(${t.name})`
      );
      markDone(addresses, t.label);
    }
  }

  // ────────────────────────────────────────────────────────────────────── //
  // 2. Non-idempotent registrations (revert on duplicate — must pre-check)  //
  // ────────────────────────────────────────────────────────────────────── //
  step("OracleHub adapters (non-idempotent)");

  const hubAdapters = [
    {
      name: "PriceOracleAdapter",
      contract: priceOracleAdapter,
      addr: addresses.PriceOracleAdapter,
      priority: 10,
      label: "OracleHub.registerAdapter(PriceOracleAdapter, 10)",
    },
    {
      name: "SidioraFeedAdapter",
      contract: sidioraFeedAdapter,
      addr: addresses.SidioraFeedAdapter,
      priority: 20,
      label: "OracleHub.registerAdapter(SidioraFeedAdapter, 20)",
    },
  ];
  for (const a of hubAdapters) {
    const existing = (await oracleHub.getAdapters()).find(entry => entry.adapter.toLowerCase() === a.addr.toLowerCase());
    if (existing) {
      if (!existing.active || existing.priority !== BigInt(a.priority)) throw new Error(`${a.label}: registered adapter policy drift`);
      skip(`${a.label} — on-chain registration verified`);
    } else {
      await sendTx((nonce) => oracleHub.registerAdapter(a.addr, a.priority, { nonce }), a.label);
      const verified = (await oracleHub.getAdapters()).some(entry => entry.adapter.toLowerCase() === a.addr.toLowerCase() && entry.active && entry.priority === BigInt(a.priority));
      if (!verified) throw new Error(`${a.label}: registration postcondition failed`);
    }
    markDone(addresses, a.label);
  }

  step("MetaAGRouter adapters (non-idempotent)");

  const routerAdapters = [
    { name: "VaultAdapter", addr: addresses.VaultAdapter, label: "MetaAGRouter.registerAdapter(VaultAdapter)" },
    { name: "SidioraAdapter", addr: addresses.SidioraAdapter, label: "MetaAGRouter.registerAdapter(SidioraAdapter)" },
  ];
  for (const a of routerAdapters) {
    const existing = (await router.getAdapters()).find(entry => entry.adapter.toLowerCase() === a.addr.toLowerCase());
    if (existing) {
      if (!existing.active) throw new Error(`${a.label}: registered adapter disabled`);
      skip(`${a.label} — on-chain registration verified`);
    } else {
      await sendTx((nonce) => router.registerAdapter(a.addr, { nonce }), a.label);
      if (!(await router.getAdapters()).some(entry => entry.adapter.toLowerCase() === a.addr.toLowerCase() && entry.active)) throw new Error(`${a.label}: registration postcondition failed`);
    }
    markDone(addresses, a.label);
  }

  // ────────────────────────────────────────────────────────────────────── //
  // 3. Token registration (USDL + WPAX)                                    //
  // ────────────────────────────────────────────────────────────────────── //
  step("Token registry");
  const ONE = 10n ** 18n;
  const tokenConfig = [60n, 100n, ONE / 100n, ONE * 1_000_000n, 3600n];
  const bootstrapTokens = [
    { name: "USDL", addr: USDL, stable: true, vaultLabel: "PECORVault.registerToken(USDL)", oracleLabel: "PriceOracle.registerToken(USDL)" },
    { name: "WPAX", addr: WPAX, stable: false, vaultLabel: "PECORVault.registerToken(WPAX)", oracleLabel: "PriceOracle.registerToken(WPAX)" },
  ];

  for (const t of bootstrapTokens) {
    // Vault registration — getTokenInfo returns (isRegistered, isStable, decimals, reserves, deposited, withdrawn)
    const vInfo = await withRetry(() => vault.getTokenInfo(t.addr), `vault.getTokenInfo(${t.name})`);
    const vRegistered = vInfo[0];
    if (vRegistered) {
      skip(`${t.vaultLabel} — already registered`);
      markDone(addresses, t.vaultLabel);
    } else {
      await sendTx(
        (nonce) => vault.registerToken(t.addr, t.stable, { nonce }),
        `PECORVault.registerToken(${t.name}, stable=${t.stable})`
      );
      markDone(addresses, t.vaultLabel);
    }

    // PriceOracle registration — getTokenConfig returns the TokenConfig struct
    const oCfg = await withRetry(() => priceOracle.getTokenConfig(t.addr), `priceOracle.getTokenConfig(${t.name})`);
    if (oCfg.isRegistered) {
      skip(`${t.oracleLabel} — already registered`);
      markDone(addresses, t.oracleLabel);
    } else {
      await sendTx(
        (nonce) => priceOracle.registerToken(t.addr, ...tokenConfig, { nonce }),
        `PriceOracle.registerToken(${t.name})`
      );
      markDone(addresses, t.oracleLabel);
    }
  }

  // ────────────────────────────────────────────────────────────────────── //
  // 4. Admin transfer to Timelock (one-way; skipped unless explicitly true) //
  // ────────────────────────────────────────────────────────────────────── //
  if (!TRANSFER_ADMIN) {
    skip("Skipping admin transfer (TRANSFER_ADMIN_TO_TIMELOCK!=true)");
  } else if (ADMIN.toLowerCase() === deployer.address.toLowerCase()) {
    warn("ADMIN is deployer — refusing to transfer to self. Set ADMIN_ADDRESS to Timelock.");
  } else {
    step("Transferring DEFAULT_ADMIN_ROLE → Timelock (one-way)");
    const DEFAULT_ADMIN_ROLE = "0x" + "00".repeat(32);
    const adminables = [
      { name: "PriceOracle", c: priceOracle },
      { name: "OracleHub", c: oracleHub },
      { name: "TransactionTracker", c: tracker },
      { name: "PECORVault", c: vault },
      { name: "PECOR", c: pecor },
      { name: "PECOROrders", c: pecorOrders },
      { name: "MetaAGRouter", c: router },
      { name: "MetaAGQuoter", c: quoter },
      { name: "SidioraFeedAdapter", c: sidioraFeedAdapter },
      { name: "VaultAdapter", c: vaultAdapter },
      { name: "SidioraAdapter", c: sidioraAdapter },
    ];
    for (const { name, c: ct } of adminables) {
      // Grant first (idempotent if already granted; OZ-style _grantRole noops on duplicate)
      const adminHas = await withRetry(
        () => ct.hasRole(DEFAULT_ADMIN_ROLE, ADMIN),
        `hasRole(admin) on ${name}`
      );
      if (!adminHas) {
        await sendTx(
          (nonce) => ct.grantRole(DEFAULT_ADMIN_ROLE, ADMIN, { nonce }),
          `${name}.grantRole(DEFAULT_ADMIN_ROLE, Timelock)`
        );
      } else {
        skip(`${name}.grantRole(DEFAULT_ADMIN_ROLE, Timelock) — already granted`);
      }

      const deployerHas = await withRetry(
        () => ct.hasRole(DEFAULT_ADMIN_ROLE, deployer.address),
        `hasRole(deployer) on ${name}`
      );
      if (deployerHas) {
        await sendTx(
          (nonce) => ct.renounceRole(DEFAULT_ADMIN_ROLE, deployer.address, { nonce }),
          `${name}.renounceRole(DEFAULT_ADMIN_ROLE, deployer)`
        );
      } else {
        skip(`${name}.renounceRole — deployer already has no admin`);
      }
    }
    markDone(addresses, "TRANSFER_ADMIN_TO_TIMELOCK");
  }

  // ────────────────────────────────────────────────────────────────────── //
  // Final state                                                            //
  // ────────────────────────────────────────────────────────────────────── //
  step("Summary");
  const finalMeta = loadAddresses()._meta_pecor;
  console.log(`  Status:   ${c(finalMeta.wiringStatus === "complete" ? "green" : "yellow", finalMeta.wiringStatus)}`);
  console.log(`  Done:     ${finalMeta.wiringDone?.length || 0}`);
  console.log(`  Pending:  ${finalMeta.wiringPending?.length || 0}`);
  if (finalMeta.wiringPending?.length) {
    console.log(c("yellow", `\n  Still pending:`));
    for (const p of finalMeta.wiringPending) console.log(`    - ${p}`);
  } else {
    console.log(`\n  ${c("green", "✓ All wiring complete. Mainnet is wired.")}`);
  }
  console.log();
}

module.exports = { main };
if (require.main === module) main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error(c("red", "\n❌ Resume failed:"), e);
    process.exit(1);
  });
