import { defineChain } from 'viem';
import { env } from './env.js';

/**
 * HyperPaxeer EVM chain definition for viem.
 * Chain ID 125, ~2s block times, native token PAX.
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
