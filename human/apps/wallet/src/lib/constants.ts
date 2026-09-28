import { preferencesRepository } from '../platform/storage/repositories';

export const PAXEER_CONFIG = {
    rpcUrl: process.env.NEXT_PUBLIC_RPC_URL || 'https://api.hyperpax.xyz',
    chainId: Number(process.env.NEXT_PUBLIC_CHAIN_ID) || 125,
    explorerBase: process.env.NEXT_PUBLIC_EXPLORER_BASE || 'https://paxscan.io',
    // Same-origin proxy paths. The Next route handler at
    // `/api/wallet/[...path]` forwards to BLOCKSCOUT_UPSTREAM_BASE on the
    // server. Keeping these as relative paths means no upstream host ever
    // ships in the client bundle, and Capacitor + PWA share one origin.
    paxscanApi: '/api/wallet',
    // Cutover: the proxy upstream (BLOCKSCOUT_UPSTREAM_BASE) now points at the
    // first-party paxeer-indexer read API, which serves the Blockscout v2
    // contract at /api/v2 and the wallet BFF (portfolio/holdings/balance,
    // market, candles, ws/stream) at /api/v1. The deprecated Sidiora
    // portfolio/spot bases default to the same-origin proxy so every legacy
    // caller also lands on the indexer; env vars remain as the rollback lever.
    portfolioApiBase: process.env.NEXT_PUBLIC_PORTFOLIO_API_BASE || '/api/wallet',
    blockscoutApiBase: '/api/wallet',
    /** @deprecated Use blockscoutApiBase — kept for callers that hit /api/v2 directly */
    indexerApiBase: '/api/wallet/api/v2',
    spotApiBase: process.env.NEXT_PUBLIC_SPOT_API_URL || '/api/wallet',
    sessionTimeoutMs: 15 * 60 * 1000,
    encryptionTimeoutMs: 30 * 60 * 1000,
} as const;

/**
 * Return the active RPC URL: the user's customRpc from preferences when
 * set and valid, otherwise the environment-backed default.
 *
 * Client-only — reads from the preferencesRepository.  Server routes
 * should keep using PAXEER_CONFIG.rpcUrl directly because the server
 * has no access to browser localStorage.
 */
export function getActiveRpcUrl(): string {
    // Guard: this function is called from client code only.
    // On the server (SSR / API routes), fall through to the default.
    if (typeof window === 'undefined') return PAXEER_CONFIG.rpcUrl;
    try {
        const custom = preferencesRepository.read().customRpc;
        if (custom && typeof custom === 'string') {
            const parsed = new URL(custom);
            if (
                parsed.protocol === 'https:' &&
                !parsed.username &&
                !parsed.password
            ) {
                return parsed.toString();
            }
        }
    } catch {
        // Corrupt or missing preference — fall through.
    }
    return PAXEER_CONFIG.rpcUrl;
}

export const RPC_CHANGED_EVENT = 'paxport:rpc-changed';

export interface RpcValidationResult {
    url: string;
    chainId: number;
    latencyMs: number;
}

export async function validateRpcEndpoint(input: string): Promise<RpcValidationResult> {
    let url: URL;
    try {
        url = new URL(input.trim());
    } catch {
        throw new Error('Enter a valid RPC URL.');
    }
    if (url.protocol !== 'https:' || url.username || url.password) {
        throw new Error('RPC endpoints must use HTTPS and cannot contain credentials.');
    }
    const startedAt = performance.now();
    const response = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({
            jsonrpc: '2.0',
            id: 'paxport-rpc-check',
            method: 'eth_chainId',
            params: [],
        }),
        signal: AbortSignal.timeout(10_000),
        cache: 'no-store',
    });
    if (!response.ok) {
        throw new Error(`RPC returned HTTP ${response.status}.`);
    }
    const payload = await response.json() as {
        jsonrpc?: unknown;
        id?: unknown;
        result?: unknown;
        error?: { message?: unknown };
    };
    if (payload.error) {
        throw new Error(
            typeof payload.error.message === 'string'
                ? payload.error.message
                : 'RPC rejected the chain check.',
        );
    }
    if (
        payload.jsonrpc !== '2.0' ||
        payload.id !== 'paxport-rpc-check' ||
        typeof payload.result !== 'string' ||
        !/^0x[0-9a-f]+$/i.test(payload.result)
    ) {
        throw new Error('RPC returned an invalid eth_chainId response.');
    }
    const chainId = Number.parseInt(payload.result, 16);
    if (chainId !== PAXEER_CONFIG.chainId) {
        throw new Error(
            `RPC is connected to chain ${chainId}; PaxPort requires chain ${PAXEER_CONFIG.chainId}.`,
        );
    }
    return {
        url: url.toString(),
        chainId,
        latencyMs: Math.max(0, Math.round(performance.now() - startedAt)),
    };
}

export function announceRpcChanged(): void {
    if (typeof window !== 'undefined') {
        window.dispatchEvent(new Event(RPC_CHANGED_EVENT));
    }
}

export const PAX_ICON_URL = 'https://raw.githubusercontent.com/Paxeer-Network/Paxeer-Network-Brand-Kit/refs/heads/main/PaxeerNetworkInvertedsymbol.png';

export const TOKENS: Record<string, { symbol: string; name: string; decimals: number; native?: boolean; address?: string; isStablecoin?: boolean }> = {
  PAX:   { symbol: 'PAX',   name: 'Paxeer',        decimals: 18, native: true },
  USDC:  { symbol: 'USDC',  name: 'USD Coin',      decimals: 6,  address: process.env.NEXT_PUBLIC_USDC ?? '0x4b29871681c95DFB2c7824BC4b0326B80217bCe8', isStablecoin: true },
  USDT:  { symbol: 'USDT',  name: 'Tether USD',    decimals: 6,  address: process.env.NEXT_PUBLIC_USDT ?? '0xe76f24bcF307290e4e09Ee45021CeC998c3749ce', isStablecoin: true },
  USDL:  { symbol: 'USDL',  name: 'Liquidity USD', decimals: 6,  address: process.env.NEXT_PUBLIC_USDL ?? '0x85FcD13735F4309833A503EE804ea32395851479', isStablecoin: true },
  WPAX9: { symbol: 'WPAX9', name: 'Wrapped PAX',   decimals: 18, address: process.env.NEXT_PUBLIC_WPAX9 ?? '0xD152891923C7D6fE84d3DCF58621aB2be0eFCbc2' },
  SID:   { symbol: 'SID',   name: 'Sidiora',       decimals: 6,  address: process.env.NEXT_PUBLIC_SID ?? '0x21f7b20a555199fa73A238B1a91FD0f549068fEe' },
  MTX:   { symbol: 'MTX',   name: 'Matrix  Token', decimals: 6,  address: process.env.NEXT_PUBLIC_MTX ?? '0x471368EF4E11c6f8647e6743031Dfc346cB8A99c' },
  PAXIE: { symbol: 'PAXIE', name: 'Paxie',         decimals: 6,  address: process.env.NEXT_PUBLIC_PAXIE ?? '0x21AEd826Df2e4dd3dE3B29b7347a7aCF61F19b21' },
  FLIP:  { symbol: 'FLIP',  name: 'CoinFlip',      decimals: 6,  address: process.env.NEXT_PUBLIC_FLIP ?? '0xEB5272560d247Df28646abB17E9A42737eEa0092' },
  MISFITZ: { symbol: 'MISFITZ', name: 'Misfitz',   decimals: 6,  address: process.env.NEXT_PUBLIC_MISFITZ ?? '0xA87aF44F951598B7298668aC2A65D2943fA6B87B' },
  AGGIE: { symbol: 'AGGIE', name: 'Aggie Finance', decimals: 6,  address: process.env.NEXT_PUBLIC_AGGIE ?? '0x3583567817921070a5d1665da55e2B40881D5593' },
};
