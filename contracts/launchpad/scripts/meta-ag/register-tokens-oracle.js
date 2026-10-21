/**
 * Sidiora Meta-AG + PECOR — PriceOracle Token Registration
 *
 * Registers the canonical Paxeer token universe with the live PriceOracle proxy.
 * Idempotent: any token already registered is skipped. Any token that fails the
 * registration is logged and the script keeps moving so partial progress is not
 * lost mid-batch.
 *
 * Reads the live oracle proxy from `deployments/paxeer-network-addresses.json`
 * (PriceOracle_proxy). Uses the deployer wallet — the deployer must still hold
 * DEFAULT_ADMIN_ROLE on PriceOracle (i.e. TRANSFER_ADMIN_TO_TIMELOCK has not yet
 * fired). On the current mainnet snapshot that is the case (admin still =
 * deployer; Timelock transfer is in `wiringPending`).
 *
 * Token universe (per AndrewsProfile.md / canonical Paxeer rails):
 *   stable:    USDC, USDT, USDL
 *   non-stable: UNI, WBNB, WETH, WSOL, WPAX9, SID
 *
 * Per-token oracle config (mirrors the bootstrap config used for USDL/WPAX in
 * `deploy-pecor-meta-ag.js`):
 *   heartbeatInterval     = 60 s
 *   deviationThresholdBps = 100 (1%)
 *   minPriceBound         = 1e16  ($0.01)
 *   maxPriceBound         = 1e24  ($1,000,000)
 *   maxStaleness          = 3600 s (1 hour)
 *
 * Override per-token via env:
 *   ORACLE_HEARTBEAT_<SYMBOL>      e.g. ORACLE_HEARTBEAT_WETH=120
 *   ORACLE_DEVIATION_BPS_<SYMBOL>  e.g. ORACLE_DEVIATION_BPS_USDC=50
 *   ORACLE_MIN_PRICE_<SYMBOL>      raw uint256 (18-dec)
 *   ORACLE_MAX_PRICE_<SYMBOL>      raw uint256 (18-dec)
 *   ORACLE_MAX_STALENESS_<SYMBOL>  seconds
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/register-tokens-oracle.js --network paxeer-network
 *
 *   # Dry run (no tx, just print state):
 *   DRY_RUN=true npx hardhat run scripts/meta-ag/register-tokens-oracle.js --network paxeer-network
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

// ─────────────────────────────────────────────────────────────────────────── //
// Constants                                                                   //
// ─────────────────────────────────────────────────────────────────────────── //

const ONE = 10n ** 18n;

/** @type {{ name: string, addr: string, stable: boolean }[]} */
const TOKENS = [
  { name: "UNI",   addr: "0x2235fB5dFe619d67FcA1F9a70BD2B6725b13aE50", stable: false },
  { name: "WBNB",  addr: "0x2cE6495AF2F6cF20ea1b4d637dC2E882a0276F36", stable: false },
  { name: "WETH",  addr: "0x5ba2f89F60f5805512A265bdFbB8984C85b4c9B7", stable: false },
  { name: "WSOL",  addr: "0x38416f047c53C6D295AfF15e2fD296B6C896E2d8", stable: false },
  { name: "USDC",  addr: "0xf8850b62AE017c55be7f571BBad840b4f3DA7D49", stable: true  },
  { name: "USDT",  addr: "0x5dfE06Ae465a39c442c45ed273c523BaC2d1f6a8", stable: true  },
  { name: "USDL",  addr: "0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37", stable: true  },
  { name: "WPAX9", addr: "0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de", stable: false },
  { name: "SID",   addr: "0x86949e4CdB89496490890B67C9cfF63eD8efB4b1", stable: false },
];

const DEFAULT_CONFIG = {
  heartbeatInterval:     60n,
  deviationThresholdBps: 100n,
  minPrice:              ONE / 100n,        // $0.01
  maxPrice:              ONE * 1_000_000n,  // $1,000,000
  maxStaleness:          3600n,
};

// ─────────────────────────────────────────────────────────────────────────── //
// Logging                                                                     //
// ─────────────────────────────────────────────────────────────────────────── //

const COLORS = {
  reset:  "\x1b[0m",
  green:  "\x1b[32m",
  yellow: "\x1b[33m",
  red:    "\x1b[31m",
  cyan:   "\x1b[36m",
  gray:   "\x1b[90m",
};
const c = (col, s) => `${COLORS[col]}${s}${COLORS.reset}`;
const step = (s) => console.log(`\n${c("yellow", `── ${s} ──`)}`);
const line = (label, val) => console.log(`  ${label.padEnd(28)} ${c("cyan", val)}`);
const ok   = (msg) => console.log(`  ${c("green", "✓")} ${msg}`);
const skip = (msg) => console.log(`  ${c("gray", "○")} ${c("gray", msg)}`);
const warn = (msg) => console.log(`  ${c("red", "✗")} ${c("red", msg)}`);

// ─────────────────────────────────────────────────────────────────────────── //
// Retry / nonce helpers (mirrors resume-pecor-deploy.js)                       //
// ─────────────────────────────────────────────────────────────────────────── //

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
    blob.includes("known transaction") ||
    blob.includes("replacement transaction underpriced")
  );
}

function isInvalidSequenceError(err) {
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

let _signer = null;
let _nonce = null;
let _txOverrides = {};

async function initNonce(signer) {
  _signer = signer;
  _nonce = await withRetry(
    () => signer.provider.getTransactionCount(signer.address, "pending"),
    "fetch initial nonce"
  );
  console.log(`  ${c("gray", `Starting nonce (from chain "latest"): ${_nonce}`)}`);
}

/**
 * Detect whether the chain exposes EIP-1559 `baseFeePerGas`. If not (as is the
 * case for the hyperpaxeer.com /evm/ RPC endpoint, which returns `baseFeePerGas:
 * null`), force all transactions to legacy (type 0) with an explicit `gasPrice`
 * fetched from `eth_gasPrice`. Otherwise ethers v6's auto fee-population path
 * blows up with:
 *     Cannot read properties of null (reading 'baseFeePerGas')
 *
 * Overrides:
 *   GAS_PRICE=<wei>         explicit legacy gasPrice (pins type 0)
 *   FORCE_LEGACY_TX=true    always use type 0, fetch gasPrice from the node
 */
async function initTxOverrides(provider) {
  const forceLegacy = process.env.FORCE_LEGACY_TX === "true";
  const envGasPrice = process.env.GAS_PRICE;

  if (envGasPrice) {
    _txOverrides = { type: 0, gasPrice: BigInt(envGasPrice) };
    console.log(
      `  ${c("gray", `Tx mode: legacy (type 0) — GAS_PRICE override = ${_txOverrides.gasPrice}`)}`
    );
    return;
  }

  try {
    const block = await withRetry(
      () => provider.getBlock("latest"),
      "getBlock(latest) for gas mode detection"
    );
    const hasBaseFee = block && block.baseFeePerGas != null;

    if (!hasBaseFee || forceLegacy) {
      const gasPriceHex = await withRetry(
        () => provider.send("eth_gasPrice", []),
        "eth_gasPrice"
      );
      _txOverrides = { type: 0, gasPrice: BigInt(gasPriceHex) };
      const reason = forceLegacy
        ? "FORCE_LEGACY_TX=true"
        : "chain has no baseFeePerGas";
      console.log(
        `  ${c("gray", `Tx mode: legacy (type 0), gasPrice=${_txOverrides.gasPrice} (${reason})`)}`
      );
    } else {
      console.log(
        `  ${c("gray", `Tx mode: EIP-1559 (chain exposes baseFeePerGas=${block.baseFeePerGas})`)}`
      );
    }
  } catch (e) {
    console.log(
      `  ${c("gray", `Tx mode: default — gas detection failed: ${(e.message || "").slice(0, 100)}`)}`
    );
  }
}

/** Merge the detected tx overrides with a specific nonce for a single send. */
function txOpts(nonce) {
  return { nonce, ..._txOverrides };
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
 * Custom replacement for `tx.wait()`. Ethers v6's default wait() relies on
 * block subscriptions and a 4-second polling interval, which on this RPC
 * frequently never returns even after the tx confirms. We instead poll
 * `eth_getTransactionReceipt` directly at a tight cadence and bail out fast
 * on a hard timeout.
 *
 * Env overrides:
 *   TX_WAIT_TIMEOUT_MS  hard ceiling per tx (default 60s)
 *   TX_POLL_MS          poll interval (default 500ms)
 */
async function waitForTx(tx, label) {
  const timeoutMs = Number(process.env.TX_WAIT_TIMEOUT_MS || 60000);
  const pollMs = Number(process.env.TX_POLL_MS || 500);
  const provider = tx.provider || _signer.provider;
  const hash = tx.hash;
  const start = Date.now();

  while (Date.now() - start < timeoutMs) {
    let receipt = null;
    try {
      receipt = await provider.getTransactionReceipt(hash);
    } catch (err) {
      if (!isTransientError(err)) throw err;
    }

    if (receipt) {
      const status = typeof receipt.status === "bigint" ? Number(receipt.status) : receipt.status;
      if (status === 0) {
        throw new Error(`${label} reverted on-chain (hash=${hash})`);
      }
      return receipt;
    }

    await sleep(pollMs);
  }

  // Timeout — one last check before giving up.
  const finalReceipt = await provider.getTransactionReceipt(hash).catch(() => null);
  if (finalReceipt) {
    const status = typeof finalReceipt.status === "bigint" ? Number(finalReceipt.status) : finalReceipt.status;
    if (status === 0) {
      throw new Error(`${label} reverted on-chain (hash=${hash})`);
    }
    return finalReceipt;
  }

  const err = new Error(
    `${label} did not confirm within ${timeoutMs}ms (hash=${hash}). ` +
    `Tx may still land — re-run the script to pick up where it left off.`
  );
  err.timedOut = true;
  err.txHash = hash;
  throw err;
}

async function sendTx(buildFn, label) {
  // Never retry writes at a fresh nonce after an ambiguous broadcast/receipt.
  // Reads may retry independently; operator reconciliation is required for writes.
  const useNonce = _nonce;
  let tx;
  try { tx = await buildFn(useNonce); }
  catch (err) { throw new Error(`${label}: broadcast unresolved at nonce ${useNonce}; reconcile receipt and semantic postcondition before retrying`, { cause: err }); }
  await require("../lib/operations").checkpointTransaction(tx, label, "broadcast");
  let receipt;
  try { receipt = await waitForTx(tx, label); }
  catch (err) { throw new Error(`${label}: confirmation unresolved for ${tx.hash}; reconcile before retrying`, { cause: err }); }
  if (!receipt || Number(receipt.status) !== 1) throw new Error(`${label}: transaction did not succeed ${tx.hash}`);
  await require("../lib/operations").checkpointTransaction(tx, label, "confirmed", receipt);
  _nonce = useNonce + 1;
  ok(`${label} (nonce ${useNonce}, tx ${tx.hash})`);
  return tx;
}

// ─────────────────────────────────────────────────────────────────────────── //
// Address-file resolver                                                       //
// ─────────────────────────────────────────────────────────────────────────── //

function resolveAddrFile() {
  const file = require("../lib/operations").manifestPath();
  if (!fs.existsSync(file)) throw new Error(`Missing canonical deployment manifest ${file}`);
  return file;
}

// ─────────────────────────────────────────────────────────────────────────── //
// Per-token config resolver                                                    //
// ─────────────────────────────────────────────────────────────────────────── //

function resolveTokenConfig(symbol) {
  const env = process.env;
  const heartbeat   = env[`ORACLE_HEARTBEAT_${symbol}`];
  const deviation   = env[`ORACLE_DEVIATION_BPS_${symbol}`];
  const minPrice    = env[`ORACLE_MIN_PRICE_${symbol}`];
  const maxPrice    = env[`ORACLE_MAX_PRICE_${symbol}`];
  const staleness   = env[`ORACLE_MAX_STALENESS_${symbol}`];

  return {
    heartbeatInterval:     heartbeat ? BigInt(heartbeat) : DEFAULT_CONFIG.heartbeatInterval,
    deviationThresholdBps: deviation ? BigInt(deviation) : DEFAULT_CONFIG.deviationThresholdBps,
    minPrice:              minPrice  ? BigInt(minPrice)  : DEFAULT_CONFIG.minPrice,
    maxPrice:              maxPrice  ? BigInt(maxPrice)  : DEFAULT_CONFIG.maxPrice,
    maxStaleness:          staleness ? BigInt(staleness) : DEFAULT_CONFIG.maxStaleness,
  };
}

// ─────────────────────────────────────────────────────────────────────────── //
// Main                                                                        //
// ─────────────────────────────────────────────────────────────────────────── //

async function main() {
  // Drop ethers' internal block-poll interval from the 4s default to 500ms.
  // Speeds up any code path that still uses ethers' built-in polling.
  ethers.provider.pollingInterval = Number(process.env.PROVIDER_POLL_MS || 500);

  const [deployer] = await ethers.getSigners();
  const net = await ethers.provider.getNetwork();
  const chainId = Number(net.chainId);
  const DRY_RUN = process.env.DRY_RUN === "true";

  console.log(`\n${c("yellow", "╔══════════════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow",   "║ PriceOracle — Token Registration                         ║")}`);
  console.log(`${c("yellow",   "╚══════════════════════════════════════════════════════════╝")}`);

  line("Deployer", deployer.address);
  line("Balance", `${ethers.formatEther(await ethers.provider.getBalance(deployer.address))} PAX`);
  line("Network", `chainId ${chainId}`);
  line("Mode", DRY_RUN ? "DRY RUN (no tx)" : "EXECUTE");

  // ── Resolve addresses ─────────────────────────────────────────────────── //
  const addrFile = resolveAddrFile(chainId);
  line("Addresses file", addrFile);
  const addresses = JSON.parse(fs.readFileSync(addrFile, "utf8"));
  await require("../lib/operations").validateManifest(addresses, ethers.provider);
  const oracleAddr = addresses.PriceOracle_proxy;
  if (!oracleAddr) {
    throw new Error(`Missing PriceOracle_proxy in ${addrFile}`);
  }
  line("PriceOracle proxy", oracleAddr);

  // ── Attach to oracle ──────────────────────────────────────────────────── //
  const priceOracle = await ethers.getContractAt("PriceOracle", oracleAddr);

  // Sanity: deployer must hold DEFAULT_ADMIN_ROLE
  const DEFAULT_ADMIN_ROLE = "0x" + "00".repeat(32);
  const hasAdmin = await withRetry(
    () => priceOracle.hasRole(DEFAULT_ADMIN_ROLE, deployer.address),
    "hasRole(DEFAULT_ADMIN_ROLE, deployer)"
  );
  if (!hasAdmin) {
    warn(
      `Deployer ${deployer.address} does NOT hold DEFAULT_ADMIN_ROLE on PriceOracle. ` +
      `If admin was already transferred to Timelock, run this through governance instead.`
    );
    if (!DRY_RUN) {
      throw new Error("Deployer is not PriceOracle admin. Aborting.");
    }
  } else {
    ok("Deployer holds DEFAULT_ADMIN_ROLE on PriceOracle");
  }

  // ── Pre-flight: print current registration state ──────────────────────── //
  step("Pre-flight: current PriceOracle registration state");

  const preflight = [];
  for (const t of TOKENS) {
    const cfg = await withRetry(
      () => priceOracle.getTokenConfig(t.addr),
      `getTokenConfig(${t.name})`
    );
    preflight.push({ ...t, isRegistered: cfg.isRegistered });
    const tag = cfg.isRegistered ? c("gray", "registered") : c("yellow", "MISSING");
    console.log(`  ${t.name.padEnd(7)} ${t.addr}  ${tag}`);
  }

  const pending = preflight.filter((t) => !t.isRegistered);
  const already = preflight.filter((t) =>  t.isRegistered);

  console.log("");
  line("Already registered", `${already.length} / ${TOKENS.length}`);
  line("To register",        `${pending.length} / ${TOKENS.length}`);

  if (pending.length === 0) {
    ok("Nothing to do — all tokens already registered on PriceOracle.");
    return;
  }

  if (DRY_RUN) {
    console.log(c("gray", `\n  [DRY RUN] Would register: ${pending.map((p) => p.name).join(", ")}`));
    return;
  }

  // ── Init nonce manager + detect tx mode (EIP-1559 vs legacy) ──────────── //
  await initNonce(deployer);
  await initTxOverrides(ethers.provider);

  // ── Register each pending token ───────────────────────────────────────── //
  step(`Registering ${pending.length} token(s) on PriceOracle`);

  const failures = [];
  for (const t of pending) {
    const cfg = resolveTokenConfig(t.name);

    line(
      `${t.name} config`,
      `heartbeat=${cfg.heartbeatInterval}s deviation=${cfg.deviationThresholdBps}bps ` +
      `min=${cfg.minPrice} max=${cfg.maxPrice} staleness=${cfg.maxStaleness}s`
    );

    try {
      await sendTx(
        (nonce) =>
          priceOracle.registerToken(
            t.addr,
            cfg.heartbeatInterval,
            cfg.deviationThresholdBps,
            cfg.minPrice,
            cfg.maxPrice,
            cfg.maxStaleness,
            txOpts(nonce)
          ),
        `PriceOracle.registerToken(${t.name})`
      );
    } catch (err) {
      const blob = errorBlob(err);
      if (blob.includes("tokenalreadyregistered")) {
        skip(`PriceOracle.registerToken(${t.name}) — already registered (race)`);
      } else {
        warn(`PriceOracle.registerToken(${t.name}) failed: ${err.shortMessage || err.message}`);
        failures.push({ name: t.name, addr: t.addr, error: err.shortMessage || err.message });
      }
    }
  }

  // ── Post-flight verification ──────────────────────────────────────────── //
  step("Post-flight verification");

  let verifiedCount = 0;
  for (const t of TOKENS) {
    const cfg = await withRetry(
      () => priceOracle.getTokenConfig(t.addr),
      `verify getTokenConfig(${t.name})`
    );
    if (cfg.isRegistered) {
      ok(`${t.name.padEnd(7)} ${t.addr}  registered`);
      verifiedCount += 1;
    } else {
      warn(`${t.name.padEnd(7)} ${t.addr}  NOT REGISTERED`);
      failures.push({ name: t.name, addr: t.addr, error: "registration postcondition failed" });
    }
  }

  // ── Summary ───────────────────────────────────────────────────────────── //
  step("Summary");
  line("Verified registered", `${verifiedCount} / ${TOKENS.length}`);
  if (failures.length > 0) {
    console.log(c("red", `\n  Failures (${failures.length}):`));
    for (const f of failures) {
      console.log(`    - ${f.name} (${f.addr}): ${f.error}`);
    }
    process.exitCode = 1;
  } else {
    ok("All target tokens are registered on PriceOracle.");
  }
}

main()
  .then(() => process.exit(process.exitCode || 0))
  .catch((e) => {
    console.error(c("red", "\n❌ Registration script failed:"), e);
    process.exit(1);
  });
