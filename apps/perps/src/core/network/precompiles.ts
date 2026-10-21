import type { Address } from "viem";

/** Resolve a precompile address from its override variable or fall back to the release default. */
function envAddress(envKey: string, raw: string | undefined, fallback: Address): Address {
  if (raw === undefined || raw === "") return fallback;
  if (!/^0x[a-fA-F0-9]{40}$/.test(raw)) {
    throw new Error(`${envKey} must be 0x followed by 40 hex digits, got "${raw}"`);
  }
  return raw as Address;
}

export const PRECOMPILES = {
  perps: envAddress(
    "NEXT_PUBLIC_PERPS_PRECOMPILE",
    process.env.NEXT_PUBLIC_PERPS_PRECOMPILE,
    "0x0000000000000000000000000000000000001015",
  ),
  launchpad: envAddress(
    "NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE",
    process.env.NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE,
    "0x0000000000000000000000000000000000001017",
  ),
  spot: {
    PECOR: envAddress(
      "NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE",
      process.env.NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE,
      "0x0000000000000000000000000000000000001021",
    ),
    PECOROrders: envAddress(
      "NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE",
      process.env.NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE,
      "0x0000000000000000000000000000000000001022",
    ),
    PECORStopOrders: envAddress(
      "NEXT_PUBLIC_PECOR_STOP_PRECOMPILE",
      process.env.NEXT_PUBLIC_PECOR_STOP_PRECOMPILE,
      "0x0000000000000000000000000000000000001023",
    ),
    PECORQuoter: envAddress(
      "NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE",
      process.env.NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE,
      "0x0000000000000000000000000000000000001024",
    ),
    PECORVault: envAddress(
      "NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE",
      process.env.NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE,
      "0x0000000000000000000000000000000000001025",
    ),
  },
} as const;
