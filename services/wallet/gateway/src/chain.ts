import { defineChain } from 'viem';
import { env } from './env.js';

/** Spans several hundred blocks at the chain's sub-second block time; exists to outlast propagation lag. */
export const BROADCAST_GRACE_MS = 60_000;

/**
 * HyperPaxeer EVM chain definition for viem.
 * Chain ID 125, sub-second (~167 ms) block times, native token PAX.
 */
export const hyperPaxeer = defineChain({
  id: env.HYPERPAXEER_CHAIN_ID,
  name: 'HyperPaxeer',
  nativeCurrency: { name: 'Paxeer', symbol: 'PAX', decimals: 18 },
  rpcUrls: {
    default: { http: [env.HYPERPAXEER_RPC_URL] },
    public: { http: [env.HYPERPAXEER_RPC_URL] },
  },
  blockExplorers: env.HYPERPAXEER_EXPLORER_URL
    ? {
        default: {
          name: 'Paxeer Explorer',
          url: env.HYPERPAXEER_EXPLORER_URL,
        },
      }
    : undefined,
});
