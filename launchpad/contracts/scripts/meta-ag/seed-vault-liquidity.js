/**
 * Sidiora Meta-AG + PECOR — Vault Liquidity Seeding
 *
 * Seeds reserves for the canonical Paxeer token universe into the live
 * PECORVault. For each token the script:
 *
 *   1. Ensures the token is registered with the vault
 *      (`PECORVault.registerToken(token, isStablecoin)`).
 *      Stable: USDC, USDT, USDL.  Non-stable: UNI, WBNB, WETH, WSOL, WPAX9, SID.
 *      Already-registered tokens are detected via `getTokenInfo` and skipped.
 *
 *   2. Pulls the configured seed amount from the deployer wallet:
 *      `IERC20.approve(vault, amount)` then `vault.deposit(token, amount)`.
 *      The deposit is recorded against the vault's `reserves[token]` ledger
 *      so adapters and PECOR see the liquidity.
 *
 * Idempotent: if a token already has reserves >= the configured floor, the
 * deposit is skipped. Re-running tops up only what's missing (when
 * `TOP_UP=true`) or skips entirely (default).
 *
 * Decimal-aware: amounts are specified in human-readable token units. The
 * script reads `IERC20.decimals()` once per token and scales accordingly.
 *
 * Default seed schedule (override per-token via env, see below):
 *   USDC   = 100,000   (stable, treasury-style float)
 *   USDT   = 100,000
 *   USDL   = 100,000
 *   WBNB   =     100
 *   WETH   =      30
 *   WSOL   =   1,000
 *   UNI    =   5,000
 *   WPAX9  = 100,000   (native rail)
 *   SID    = 1,000,000
 *
 * Env overrides (all optional):
 *   SEED_<SYMBOL>            human-readable amount, e.g. SEED_WETH=15
 *   SEED_RAW_<SYMBOL>        raw uint256 (already scaled), e.g. SEED_RAW_USDC=50000000000
 *   TOP_UP=true              top up to the configured floor instead of skipping
 *   ONLY=USDC,USDL           comma-separated list, only seed these tokens
 *   SKIP=SID                 comma-separated list, skip these tokens
 *   DRY_RUN=true             print plan, no tx
 *
 * Usage:
 *   npx hardhat run scripts/meta-ag/seed-vault-liquidity.js --network paxeer-network
 *   ONLY=USDC,USDL DRY_RUN=true npx hardhat run scripts/meta-ag/seed-vault-liquidity.js --network paxeer-network
 *
 * Requirements:
 *   - Deployer must hold DEFAULT_ADMIN_ROLE on PECORVault (for registerToken).
 *   - Deployer must hold sufficient ERC20 balance for each token being seeded.
 */

const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");

// ─────────────────────────────────────────────────────────────────────────── //
// Token universe + default seed amounts (human-readable units)                //
// ─────────────────────────────────────────────────────────────────────────── //

/**
 * @typedef {Object} SeedToken
 * @property {string}  name
 * @property {string}  addr
 * @property {boolean} stable
 * @property {string}  defaultSeed   Human-readable seed amount
 */

/** @type {SeedToken[]} */
const TOKENS = [
  { name: "UNI",   addr: "0x2235fB5dFe619d67FcA1F9a70BD2B6725b13aE50", stable: false, defaultSeed: "5000"    },
  { name: "WBNB",  addr: "0x2cE6495AF2F6cF20ea1b4d637dC2E882a0276F36", stable: false, defaultSeed: "100"     },
  { name: "WETH",  addr: "0x5ba2f89F60f5805512A265bdFbB8984C85b4c9B7", stable: false, defaultSeed: "30"      },
  { name: "WSOL",  addr: "0x38416f047c53C6D295AfF15e2fD296B6C896E2d8", stable: false, defaultSeed: "1000"    },
  { name: "USDC",  addr: "0xf8850b62AE017c55be7f571BBad840b4f3DA7D49", stable: true,  defaultSeed: "100000"  },
  { name: "USDT",  addr: "0x5dfE06Ae465a39c442c45ed273c523BaC2d1f6a8", stable: true,  defaultSeed: "100000"  },
  { name: "USDL",  addr: "0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37", stable: true,  defaultSeed: "100000"  },
  { name: "WPAX9", addr: "0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de", stable: false, defaultSeed: "100000"  },
  { name: "SID",   addr: "0x86949e4CdB89496490890B67C9cfF63eD8efB4b1", stable: false, defaultSeed: "1000000" },
];

// Minimal ERC20 ABI (we only use approve/balanceOf/decimals/symbol/allowance).
const ERC20_ABI = [
  "function balanceOf(address) view returns (uint256)",
  "function decimals() view returns (uint8)",
  "function symbol() view returns (string)",
  "function allowance(address owner, address spender) view returns (uint256)",
  "function approve(address spender, uint256 value) returns (bool)",
];

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
// Retry / nonce helpers (mirrors resume-pecor-deploy.js)                      //
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
// Filter resolution (ONLY / SKIP)                                             //
// ─────────────────────────────────────────────────────────────────────────── //

function applyFilters(tokens) {
  const onlyEnv = (process.env.ONLY || "").trim();
  const skipEnv = (process.env.SKIP || "").trim();

  let filtered = tokens;

  if (onlyEnv) {
    const allow = new Set(onlyEnv.split(",").map((s) => s.trim().toUpperCase()).filter(Boolean));
    filtered = filtered.filter((t) => allow.has(t.name.toUpperCase()));
  }

  if (skipEnv) {
    const deny = new Set(skipEnv.split(",").map((s) => s.trim().toUpperCase()).filter(Boolean));
    filtered = filtered.filter((t) => !deny.has(t.name.toUpperCase()));
  }

  return filtered;
}

// ─────────────────────────────────────────────────────────────────────────── //
// Per-token seed-amount resolver (decimal-aware)                              //
// ─────────────────────────────────────────────────────────────────────────── //

function resolveSeedAmount(symbol, defaultHuman, decimals) {
  const raw = process.env[`SEED_RAW_${symbol}`];
  if (raw) {
    const v = BigInt(raw);
    if (v < 0n) throw new Error(`SEED_RAW_${symbol} must be non-negative`);
    return v;
  }
  const human = process.env[`SEED_${symbol}`] || defaultHuman;
  if (human === "0" || human === "") return 0n;
  return ethers.parseUnits(human, decimals);
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
  const TOP_UP  = process.env.TOP_UP === "true";

  console.log(`\n${c("yellow", "╔══════════════════════════════════════════════════════════╗")}`);
  console.log(`${c("yellow",   "║ PECORVault — Liquidity Seeding                           ║")}`);
  console.log(`${c("yellow",   "╚══════════════════════════════════════════════════════════╝")}`);

  line("Deployer", deployer.address);
  line("Balance", `${ethers.formatEther(await ethers.provider.getBalance(deployer.address))} PAX`);
  line("Network", `chainId ${chainId}`);
  line("Mode", DRY_RUN ? "DRY RUN (no tx)" : "EXECUTE");
  line("Top-up", TOP_UP ? "yes (top up to floor)" : "no (skip if reserves >= floor)");

  // ── Resolve addresses ─────────────────────────────────────────────────── //
  const addrFile = resolveAddrFile(chainId);
  line("Addresses file", addrFile);
  const addresses = JSON.parse(fs.readFileSync(addrFile, "utf8"));
  await require("../lib/operations").validateManifest(addresses, ethers.provider);

  const vaultAddr = addresses.PECORVault_proxy;
  if (!vaultAddr) {
    throw new Error(`Missing PECORVault_proxy in ${addrFile}`);
  }
  line("PECORVault proxy", vaultAddr);

  const vault = await ethers.getContractAt("PECORVault", vaultAddr);

  // Sanity: deployer must hold DEFAULT_ADMIN_ROLE for registerToken.
  // (deposit() itself is permissionless, but registerToken requires admin.)
  const DEFAULT_ADMIN_ROLE = "0x" + "00".repeat(32);
  const hasAdmin = await withRetry(
    () => vault.hasRole(DEFAULT_ADMIN_ROLE, deployer.address),
    "hasRole(DEFAULT_ADMIN_ROLE, deployer) on Vault"
  );
  if (hasAdmin) {
    ok("Deployer holds DEFAULT_ADMIN_ROLE on PECORVault");
  } else {
    warn(
      `Deployer ${deployer.address} does NOT hold DEFAULT_ADMIN_ROLE on PECORVault. ` +
      `New token registrations will fail; only already-registered tokens can be seeded.`
    );
  }

  // ── Resolve token list ────────────────────────────────────────────────── //
  const targets = applyFilters(TOKENS);
  if (targets.length === 0) {
    warn("ONLY/SKIP filters left no tokens to seed. Aborting.");
    return;
  }

  step(`Token plan (${targets.length} target${targets.length === 1 ? "" : "s"})`);

  /**
   * @type {{
   *   token: SeedToken,
   *   erc20: import("ethers").Contract,
   *   decimals: number,
   *   symbol: string,
   *   isRegistered: boolean,
   *   reserves: bigint,
   *   balance: bigint,
   *   allowance: bigint,
   *   target: bigint,
   *   delta: bigint,
   * }[]}
   */
  const plan = [];

  for (const t of targets) {
    const erc20 = new ethers.Contract(t.addr, ERC20_ABI, deployer);

    const [decimalsRaw, balance, allowance, info, symbolRaw] = await Promise.all([
      withRetry(() => erc20.decimals(),                       `${t.name}.decimals()`),
      withRetry(() => erc20.balanceOf(deployer.address),      `${t.name}.balanceOf(deployer)`),
      withRetry(() => erc20.allowance(deployer.address, vaultAddr), `${t.name}.allowance(deployer, vault)`),
      withRetry(() => vault.getTokenInfo(t.addr),             `vault.getTokenInfo(${t.name})`),
      withRetry(() => erc20.symbol().catch(() => t.name),     `${t.name}.symbol()`),
    ]);
    const decimals = Number(decimalsRaw);
    const isRegistered = info[0];
    const reserves = info[3];

    const target = resolveSeedAmount(t.name, t.defaultSeed, decimals);
    const delta = TOP_UP
      ? (reserves >= target ? 0n : target - reserves)
      : (reserves >= target ? 0n : target);

    plan.push({
      token: t,
      erc20,
      decimals,
      symbol: symbolRaw,
      isRegistered,
      reserves,
      balance,
      allowance,
      target,
      delta,
    });
  }

  // ── Print plan table ──────────────────────────────────────────────────── //
  console.log("");
  console.log(`  ${c("gray", "symbol  decimals  registered  reserves           target            balance           plan")}`);
  for (const p of plan) {
    const reservesH  = ethers.formatUnits(p.reserves,  p.decimals);
    const targetH    = ethers.formatUnits(p.target,    p.decimals);
    const balanceH   = ethers.formatUnits(p.balance,   p.decimals);
    const planNote =
      p.delta === 0n
        ? c("gray",   "skip (reserves OK)")
        : p.balance < p.delta
          ? c("red",    `INSUFFICIENT BALANCE (need ${ethers.formatUnits(p.delta, p.decimals)})`)
          : c("green",  `deposit ${ethers.formatUnits(p.delta, p.decimals)}`);

    console.log(
      `  ${p.token.name.padEnd(7)} ` +
      `${String(p.decimals).padEnd(8)}  ` +
      `${(p.isRegistered ? "yes" : c("yellow", "NO ")).padEnd(10)}  ` +
      `${reservesH.padEnd(18)} ` +
      `${targetH.padEnd(17)} ` +
      `${balanceH.padEnd(17)} ` +
      `${planNote}`
    );
  }

  if (DRY_RUN) {
    console.log(c("gray", "\n  [DRY RUN] No transactions sent."));
    return;
  }

  // ── Init nonce manager + detect tx mode (EIP-1559 vs legacy) ──────────── //
  await initNonce(deployer);
  await initTxOverrides(ethers.provider);

  // ── Phase 1: register any missing tokens with the vault ───────────────── //
  step("Phase 1 — Vault token registration");

  const toRegister = plan.filter((p) => !p.isRegistered);
  if (toRegister.length === 0) {
    skip("All target tokens already registered with the vault");
  } else {
    for (const p of toRegister) {
      try {
        await sendTx(
          (nonce) => vault.registerToken(p.token.addr, p.token.stable, txOpts(nonce)),
          `PECORVault.registerToken(${p.token.name}, stable=${p.token.stable})`
        );
        p.isRegistered = true;
      } catch (err) {
        const blob = errorBlob(err);
        if (blob.includes("tokenalreadyregistered")) {
          skip(`PECORVault.registerToken(${p.token.name}) — already registered (race)`);
          p.isRegistered = true;
        } else {
          warn(`PECORVault.registerToken(${p.token.name}) failed: ${err.shortMessage || err.message}`);
        }
      }
    }
  }

  // ── Phase 2: approve + deposit per token ──────────────────────────────── //
  step("Phase 2 — Approve + deposit");

  const failures = [];
  for (const p of plan) {
    const t = p.token;

    if (!p.isRegistered) {
      warn(`${t.name} — not registered with vault, skipping deposit`);
      failures.push({ name: t.name, reason: "vault registration failed" });
      continue;
    }
    if (p.delta === 0n) {
      skip(`${t.name} — reserves already meet target (${ethers.formatUnits(p.reserves, p.decimals)} ≥ ${ethers.formatUnits(p.target, p.decimals)})`);
      continue;
    }
    if (p.balance < p.delta) {
      warn(
        `${t.name} — deployer balance ${ethers.formatUnits(p.balance, p.decimals)} < required ${ethers.formatUnits(p.delta, p.decimals)}, skipping`
      );
      failures.push({ name: t.name, reason: "insufficient deployer balance" });
      continue;
    }

    // Approve only if allowance is short of delta. A bumped approval is safer
    // on weird ERC20s than overwriting an existing allowance with a smaller
    // one (some tokens require the allowance to be 0 first).
    if (p.allowance < p.delta) {
      try {
        if (p.allowance > 0n) await sendTx((nonce) => p.erc20.approve(vaultAddr, 0, txOpts(nonce)), `${t.name}.approve reset`);
        await sendTx(
          (nonce) => p.erc20.approve(vaultAddr, p.delta, txOpts(nonce)),
          `${t.name}.approve(vault, ${ethers.formatUnits(p.delta, p.decimals)})`
        );
        if (await p.erc20.allowance(deployer.address, vaultAddr) < p.delta) throw new Error("Approval postcondition failed");
      } catch (err) {
        warn(`${t.name}.approve failed: ${err.shortMessage || err.message}`);
        failures.push({ name: t.name, reason: `approve failed: ${err.shortMessage || err.message}` });
        continue;
      }
    } else {
      skip(`${t.name}.approve — existing allowance ${ethers.formatUnits(p.allowance, p.decimals)} sufficient`);
    }

    // Deposit
    try {
      await sendTx(
        (nonce) => vault.deposit(t.addr, p.delta, txOpts(nonce)),
        `PECORVault.deposit(${t.name}, ${ethers.formatUnits(p.delta, p.decimals)})`
      );
    } catch (err) {
      warn(`PECORVault.deposit(${t.name}) failed: ${err.shortMessage || err.message}`);
      failures.push({ name: t.name, reason: `deposit failed: ${err.shortMessage || err.message}` });
    }
  }

  // ── Post-flight verification ──────────────────────────────────────────── //
  step("Post-flight verification");

  for (const p of plan) {
    const info = await withRetry(
      () => vault.getTokenInfo(p.token.addr),
      `verify getTokenInfo(${p.token.name})`
    );
    const isRegistered = info[0];
    const reserves = info[3];
    const status = isRegistered
      ? `reserves=${ethers.formatUnits(reserves, p.decimals)}`
      : c("red", "NOT REGISTERED");
    const ok_ = isRegistered && reserves >= p.target;
    if (!ok_) failures.push({ name: p.token.name, reason: `postcondition failed: registered=${isRegistered}, reserves=${reserves}, target=${p.target}` });
    if (ok_) {
      ok(`${p.token.name.padEnd(7)} ${status}`);
    } else if (isRegistered && reserves > 0n) {
      console.log(`  ${c("yellow", "~")} ${p.token.name.padEnd(7)} ${status} (target ${ethers.formatUnits(p.target, p.decimals)})`);
    } else {
      warn(`${p.token.name.padEnd(7)} ${status}`);
    }
  }

  // ── Summary ───────────────────────────────────────────────────────────── //
  step("Summary");
  if (failures.length > 0) {
    console.log(c("red", `\n  Issues (${failures.length}):`));
    for (const f of failures) {
      console.log(`    - ${f.name}: ${f.reason}`);
    }
    process.exitCode = 1;
  } else {
    ok("Vault liquidity seeding complete.");
  }
}

main()
  .then(() => process.exit(process.exitCode || 0))
  .catch((e) => {
    console.error(c("red", "\n❌ Vault seeding script failed:"), e);
    process.exit(1);
  });
