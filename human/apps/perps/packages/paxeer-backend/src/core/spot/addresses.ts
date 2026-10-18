/* ══════════════════════════════════════════════════════════════════════
   Spot token + PECOR contract registry — Paxeer Mainnet Gen 3.

   Single source of truth for every on-chain address the spot UI uses
   to route trades through the PECOR engine. Addresses can be overridden
   via `NEXT_PUBLIC_SPOT_*` env vars for testnet / forked-network
   deployments without code changes; the defaults baked in below are
   the mainnet deployment Andrew confirmed on 2026-05-18.

   Native PAX is modelled as a first-class token with `address = 0x0`
   and `wrappedAddress = WPAX9`. The matching engine itself only speaks
   ERC-20 — every trade against the "PAX" market routes through
   WPAX9 under the hood (auto-wrap path, see `core/spot/native.ts`).
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";

/* ── helpers ────────────────────────────────────────────────────── */

const addr = (hex: string): Address => hex as Address;

/** Resolve a contract address from env or fall back to the baked-in default. */
function envAddress(envKey: string, fallback: Address): Address {
  const raw = (typeof process !== "undefined" ? process.env?.[envKey] : undefined) ?? "";
  if (/^0x[a-fA-F0-9]{40}$/.test(raw)) return raw as Address;
  return fallback;
}

/* ── PECOR engine contracts ─────────────────────────────────────── */

export const PECOR_CONTRACTS = {
  PECOR: envAddress(
    "NEXT_PUBLIC_SPOT_PECOR",
    addr("0x1AB090064857063bBB935cAe2b0FD2fE62F0d63B"),
  ),
  PECORQuoter: envAddress(
    "NEXT_PUBLIC_SPOT_PECOR_QUOTER",
    addr("0x63b53724EC271799a7e2c9702072F12080F09e13"),
  ),
  PECOROrders: envAddress(
    "NEXT_PUBLIC_SPOT_PECOR_ORDERS",
    addr("0xE89a3E5dffEfbB7f8C9E9f597bbfd4F4ADE77404"),
  ),
  PECORStopOrders: envAddress(
    "NEXT_PUBLIC_SPOT_PECOR_STOP_ORDERS",
    addr("0x49e2Fff129f9a351D94E3A25b2642Bfe37aCA912"),
  ),
  PECORVault: envAddress(
    "NEXT_PUBLIC_SPOT_PECOR_VAULT",
    addr("0xDe5A8fc4396aE392957b547154B29b000D906a87"),
  ),
  TransactionTracker: envAddress(
    "NEXT_PUBLIC_SPOT_TX_TRACKER",
    addr("0x7dACD66b913E427762c6FB35c57C5E52E860bc87"),
  ),
  PriceOracle: envAddress(
    "NEXT_PUBLIC_SPOT_PRICE_ORACLE",
    addr("0xe7B20ef0bE7322D4B8d7E054e810b2209032750B"),
  ),
} as const;

/* ── Token registry ─────────────────────────────────────────────── */

export interface SpotTokenMeta {
  /** On-chain ERC-20 address (or zero address for native PAX). */
  readonly address: Address;
  /**
   * For native tokens: the wrapped ERC-20 used for actual trading.
   * `undefined` for regular ERC-20s.
   */
  readonly wrappedAddress?: Address;
  /** Token decimals (uint8). */
  readonly decimals: number;
  /** Display symbol shown in the UI. */
  readonly symbol: string;
  /** Full token name shown in detail views. */
  readonly name: string;
  /** True for the three unified-USD stablecoins (USDL / USDC / USDT). */
  readonly isStablecoin: boolean;
  /** True for the native chain coin (PAX) — wrapping required before trade. */
  readonly isNative: boolean;
}

/** Sentinel address used everywhere a token slot represents native PAX. */
export const NATIVE_PAX_ADDRESS: Address =
  "0x0000000000000000000000000000000000000000";

export const SPOT_TOKENS = {
  PAX: {
    address: NATIVE_PAX_ADDRESS,
    wrappedAddress: addr("0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de"),
    decimals: 18,
    symbol: "PAX",
    name: "Paxeer Network",
    isStablecoin: false,
    isNative: true,
  },
  WPAX9: {
    address: addr("0xe5ccf339d1c89c7e6c6768b28507f78b861fc1de"),
    decimals: 18,
    symbol: "WPAX9",
    name: "Wrapped PAX",
    isStablecoin: false,
    isNative: false,
  },
  SID: {
    address: addr("0x86949e4CdB89496490890B67C9cfF63eD8efB4b1"),
    decimals: 6,
    symbol: "SID",
    name: "Sidiora Markets",
    isStablecoin: false,
    isNative: false,
  },
  WETH: {
    address: addr("0x5ba2f89F60f5805512A265bdFbB8984C85b4c9B7"),
    decimals: 18,
    symbol: "WETH",
    name: "Wrapped ETH",
    isStablecoin: false,
    isNative: false,
  },
  WBNB: {
    address: addr("0x2cE6495AF2F6cF20ea1b4d637dC2E882a0276F36"),
    decimals: 18,
    symbol: "WBNB",
    name: "Wrapped BNB",
    isStablecoin: false,
    isNative: false,
  },
  USDC: {
    address: addr("0xf8850b62AE017c55be7f571BBad840b4f3DA7D49"),
    decimals: 6,
    symbol: "USDC",
    name: "USD Coin",
    isStablecoin: true,
    isNative: false,
  },
  USDT: {
    address: addr("0x5dfE06Ae465a39c442c45ed273c523BaC2d1f6a8"),
    decimals: 6,
    symbol: "USDT",
    name: "Tether USD",
    isStablecoin: true,
    isNative: false,
  },
  USDL: {
    address: addr("0x7c69c84daAEe90B21eeCABDb8f0387897E9B7B37"),
    decimals: 6,
    symbol: "USDL",
    name: "Liquidity USD",
    isStablecoin: true,
    isNative: false,
  },
} as const satisfies Record<string, SpotTokenMeta>;

/** Type-safe union of every symbol in `SPOT_TOKENS`. */
export type SpotTokenSymbol = keyof typeof SPOT_TOKENS;

/* ── Stablecoin set (ordered for preference fallback) ───────────── */

/**
 * Stablecoins in preference order — USDL first (native liquidity USD),
 * then the canonical wrapped stables. The selector hook can override
 * this ordering at runtime based on user balance or vault depth, but
 * this is the deterministic tiebreaker.
 */
export const SPOT_STABLECOIN_LIST: readonly SpotTokenMeta[] = [
  SPOT_TOKENS.USDL,
  SPOT_TOKENS.USDC,
  SPOT_TOKENS.USDT,
];

/** O(1) membership check by lowercased address. */
export const SPOT_STABLECOIN_ADDRESSES: ReadonlySet<string> = new Set(
  SPOT_STABLECOIN_LIST.map((s) => s.address.toLowerCase()),
);

/* ── Lookups ────────────────────────────────────────────────────── */

/** Look up a token by its on-chain address (or wrapped address). Case-insensitive. */
export function getTokenByAddress(addressLike: string): SpotTokenMeta | undefined {
  const lower = addressLike.toLowerCase();
  for (const meta of Object.values(SPOT_TOKENS) as readonly SpotTokenMeta[]) {
    if (meta.address.toLowerCase() === lower) return meta;
    if (meta.wrappedAddress?.toLowerCase() === lower) return meta;
  }
  return undefined;
}

/** Look up a token by its display symbol. Case-insensitive. */
export function getTokenBySymbol(symbol: string): SpotTokenMeta | undefined {
  const upper = symbol.toUpperCase();
  for (const meta of Object.values(SPOT_TOKENS) as readonly SpotTokenMeta[]) {
    if (meta.symbol.toUpperCase() === upper) return meta;
  }
  return undefined;
}

/**
 * Resolve the on-chain ERC-20 address the matching engine should
 * actually receive for `meta`. For native PAX this is `WPAX9`; for
 * every other token it is the token's own address.
 */
export function getTradableAddress(meta: SpotTokenMeta): Address {
  return meta.isNative && meta.wrappedAddress ? meta.wrappedAddress : meta.address;
}

/** Quick boolean test by address. */
export function isStablecoinAddress(addressLike: string): boolean {
  return SPOT_STABLECOIN_ADDRESSES.has(addressLike.toLowerCase());
}
