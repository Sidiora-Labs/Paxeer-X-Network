/* ══════════════════════════════════════════════════════════════════════
   Spot token + PECOR contract registry — Paxeer Mainnet Gen 3.

   Single source of truth for every on-chain address the spot UI uses
   to route trades through the PECOR engine. The engine addresses are
   the spot precompiles of the network address module.

   The venue registers three tokens: USDL (ERC-20), PAX (the uhpx bank
   coin) and SID (the SID bank coin). PAX and SID trade through their
   ERC-20 native pointers at 6 decimals; no wrap exists.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";

import { PRECOMPILES } from "@/core/network/precompiles";

/* ── helpers ────────────────────────────────────────────────────── */

const addr = (hex: string): Address => hex as Address;

/* ── PECOR engine contracts ─────────────────────────────────────── */

export const PECOR_CONTRACTS = PRECOMPILES.spot;

/* ── Token registry ─────────────────────────────────────────────── */

export interface SpotTokenMeta {
  /** On-chain ERC-20 address (or zero address for native PAX). */
  readonly address: Address;
  /** Token decimals (uint8). */
  readonly decimals: number;
  /** Display symbol shown in the UI. */
  readonly symbol: string;
  /** Full token name shown in detail views. */
  readonly name: string;
  /** True for the stable quote token (USDL). */
  readonly isStablecoin: boolean;
  /** True for the native chain coin (PAX), traded through the uhpx pointer. */
  readonly isNative: boolean;
}

/** Sentinel address used everywhere a token slot represents native PAX. */
export const NATIVE_PAX_ADDRESS: Address =
  "0x0000000000000000000000000000000000000000";

export const SPOT_TOKENS = {
  USDL: {
    address: addr("0x85FcD13735F4309833A503EE804ea32395851479"),
    decimals: 6,
    symbol: "USDL",
    name: "Liquidity USD",
    isStablecoin: true,
    isNative: false,
  },
  PAX: {
    address: addr("0x7D4B7B8CA7E1a24928Bb96D59249c7a5bd1DfBe6"),
    decimals: 6,
    symbol: "PAX",
    name: "Paxeer Network",
    isStablecoin: false,
    isNative: true,
  },
  SID: {
    address: addr("0x86949e4CdB89496490890B67C9cfF63eD8efB4b1"),
    decimals: 6,
    symbol: "SID",
    name: "Sidiora Markets",
    isStablecoin: false,
    isNative: false,
  },
} as const satisfies Record<string, SpotTokenMeta>;

/** Type-safe union of every symbol in `SPOT_TOKENS`. */
export type SpotTokenSymbol = keyof typeof SPOT_TOKENS;

/* ── Stablecoin set ─────────────────────────────────────────────── */

/** USDL is the only stable token of the venue. */
export const SPOT_STABLECOIN_LIST: readonly SpotTokenMeta[] = [SPOT_TOKENS.USDL];

/** O(1) membership check by lowercased address. */
export const SPOT_STABLECOIN_ADDRESSES: ReadonlySet<string> = new Set(
  SPOT_STABLECOIN_LIST.map((s) => s.address.toLowerCase()),
);

/* ── Lookups ────────────────────────────────────────────────────── */

/** Look up a token by its on-chain address. Case-insensitive. */
export function getTokenByAddress(
  addressLike: string,
): SpotTokenMeta | undefined {
  const lower = addressLike.toLowerCase();
  for (const meta of Object.values(SPOT_TOKENS) as readonly SpotTokenMeta[]) {
    if (meta.address.toLowerCase() === lower) return meta;
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

/** Quick boolean test by address. */
export function isStablecoinAddress(addressLike: string): boolean {
  return SPOT_STABLECOIN_ADDRESSES.has(addressLike.toLowerCase());
}
