// ── Token & config sourced from @paxeer/pecor-sdk (PECOR V3 + V4 + Sidiora) ──

import { TOKENS, NATIVE_TOKEN, CONTRACTS } from '@/lib/swap/sdk';

// Re-export PECOR contract addresses for reference
export { CONTRACTS as SWAP_CONTRACTS };

// ── V4 PECORRouter adapter IDs (bytes32 keccak256 hashes) ────────────────────
export const VAULT_ADAPTER_ID = '0xab5023b25d7f5417700eec2d6efe1f686600abaf8b78c2dd253ef00eaa734f4a' as const;
export const SIDIORA_ADAPTER_ID = '0xd564e890383109bf777b017b704e5421847c7803ff18a2a9e21133702f2d2263' as const;

// ── Token registry ──────────────────────────────────────────────────────────

export interface SwapToken {
    symbol: string;
    name: string;
    address: string; // empty string = native PAX
    decimals: number;
    isNative?: boolean;
    isStablecoin?: boolean;
    isLaunchpad?: boolean;
    iconUrl?: string;
}

// ── HLPMMv2 Launchpad contract addresses ────────────────────────────────────
export const HLPMM_V2_ROUTER = '0xaedb6bB0451F9CA908f884345dEf5c538ca63022';
export const HLPMM_V2_QUOTER = '0x131928667BAB3081A3A47e429052617aF5530D87';
export const HLPMM_V2_FACTORY = '0x41897edE845Ec558E73dAb28Db55e0b16C85df89';

const PAX_ICON = 'https://raw.githubusercontent.com/Paxeer-Network/Paxeer-Network-Brand-Kit/refs/heads/main/PaxeerNetworkInvertedsymbol.png';

export const NATIVE_PAX: SwapToken = {
    symbol: 'PAX',
    name: 'Paxeer',
    address: '',
    decimals: 18,
    isNative: true,
    iconUrl: PAX_ICON,
};

export const SWAP_TOKENS: SwapToken[] = [
    NATIVE_PAX,
    {
        symbol: TOKENS.WPAX9.symbol,
        name: TOKENS.WPAX9.name,
        address: TOKENS.WPAX9.address.toLowerCase(),
        decimals: 18,
        iconUrl: PAX_ICON,
    },
    {
        symbol: TOKENS.USDC.symbol,
        name: TOKENS.USDC.name,
        address: TOKENS.USDC.address.toLowerCase(),
        decimals: 6,
        isStablecoin: true,
        iconUrl: 'https://img.logo.dev/crypto/usdc?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.USDT.symbol,
        name: TOKENS.USDT.name,
        address: TOKENS.USDT.address.toLowerCase(),
        decimals: 6,
        isStablecoin: true,
        iconUrl: 'https://img.logo.dev/crypto/usdt?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.USDL.symbol,
        name: TOKENS.USDL.name,
        address: TOKENS.USDL.address.toLowerCase(),
        decimals: 6,
        isStablecoin: true,
        iconUrl: 'https://raw.githubusercontent.com/Paxeer-Network/Paxeer-Token-Registry/refs/heads/main/USDL.jpg',
    },
    {
        symbol: TOKENS.USID.symbol,
        name: TOKENS.USID.name,
        address: TOKENS.USID.address.toLowerCase(),
        decimals: 18,
        isStablecoin: true,
        iconUrl: 'https://cdn.redixusercontent.ocfstudio.com/usid.svg',
    },
    {
        symbol: TOKENS.SID.symbol,
        name: TOKENS.SID.name,
        address: TOKENS.SID.address.toLowerCase(),
        decimals: 6,
        iconUrl: '/wallet/art2.png',
    },
    {
        symbol: TOKENS.WETH.symbol,
        name: TOKENS.WETH.name,
        address: TOKENS.WETH.address.toLowerCase(),
        decimals: 18,
        iconUrl: 'https://img.logo.dev/crypto/weth?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.WBNB.symbol,
        name: TOKENS.WBNB.name,
        address: TOKENS.WBNB.address.toLowerCase(),
        decimals: 18,
        iconUrl: 'https://img.logo.dev/crypto/wbnb?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.WUNI.symbol,
        name: TOKENS.WUNI.name,
        address: TOKENS.WUNI.address.toLowerCase(),
        decimals: 18,
        iconUrl: 'https://img.logo.dev/crypto/uni?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.WSOL.symbol,
        name: TOKENS.WSOL.name,
        address: TOKENS.WSOL.address.toLowerCase(),
        decimals: 9,
        iconUrl: 'https://img.logo.dev/crypto/sol?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.WDOGE.symbol,
        name: TOKENS.WDOGE.name,
        address: TOKENS.WDOGE.address.toLowerCase(),
        decimals: 8,
        iconUrl: 'https://img.logo.dev/crypto/doge?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
    {
        symbol: TOKENS.WBCH.symbol,
        name: TOKENS.WBCH.name,
        address: TOKENS.WBCH.address.toLowerCase(),
        decimals: 8,
        iconUrl: 'https://img.logo.dev/crypto/bch?token=pk_MvXTj0WhRQexPrFlRjv8lA&retina=true',
    },
];

// Default slippage bps (50 = 0.5%)
export const DEFAULT_SLIPPAGE_BPS = 150;

// ── Sidiora token detection ──────────────────────────────────────────────────
// All known PECOR V3 tokens — anything outside this set routes via Sidiora.
const PECOR_ADDRESSES = new Set(
    Object.values(TOKENS).map(t => t.address.toLowerCase()),
);

/**
 * Returns true if the token should be routed via Sidiora (launchpad protocol).
 * Any ERC-20 not in the known PECOR token set is a Sidiora launchpad token.
 */
export function isSidioraToken(token: SwapToken): boolean {
    if (token.isNative || !token.address) return false;
    return !PECOR_ADDRESSES.has(token.address.toLowerCase());
}

/**
 * Map a wallet SwapToken address to the SDK token address.
 * Native PAX → NATIVE_TOKEN.address (0x000...0); SDK uses checksummed addresses.
 */
export function toCentralSwapAddress(token: SwapToken): string {
    if (token.isNative || !token.address) return NATIVE_TOKEN.address;
    for (const t of Object.values(TOKENS)) {
        if (t.address.toLowerCase() === token.address.toLowerCase()) return t.address;
    }
    return token.address;
}
