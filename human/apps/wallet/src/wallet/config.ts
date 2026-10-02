import { PAXEER_CHAIN_ID } from '@paxeer/wallet';

export const WALLET_ENV = {
    gatewayUrl: 'NEXT_PUBLIC_PAXEER_WALLET_API',
    rpcUrl: 'NEXT_PUBLIC_PAXEER_RPC_URL',
    identityUrl: 'NEXT_PUBLIC_SUPABASE_URL',
    identityKey: 'NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY',
    authRedirectUrl: 'NEXT_PUBLIC_AUTH_REDIRECT_URL',
} as const;

export type WalletEnvName = (typeof WALLET_ENV)[keyof typeof WALLET_ENV];

export type WalletEnv = Partial<Record<WalletEnvName, string | undefined>>;

export interface WalletConfig {
    readonly gatewayUrl: string;
    readonly rpcUrl: string;
    readonly identityUrl: string;
    readonly identityKey: string;
    readonly authRedirectUrl: string | null;
    readonly chainId: number;
}

export class WalletConfigError extends Error {
    constructor(readonly variable: WalletEnvName, message: string) {
        super(message);
        this.name = 'WalletConfigError';
    }
}

function httpUrl(env: WalletEnv, name: WalletEnvName): string {
    const raw = env[name]?.trim();
    if (!raw) throw new WalletConfigError(name, `${name} is not set`);
    let parsed: URL;
    try {
        parsed = new URL(raw);
    } catch {
        throw new WalletConfigError(name, `${name} is not a valid URL`);
    }
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
        throw new WalletConfigError(name, `${name} must be an http or https URL`);
    }
    if (parsed.username || parsed.password) {
        throw new WalletConfigError(name, `${name} must not carry credentials`);
    }
    return raw.replace(/\/+$/, '');
}

export function readWalletConfig(env: WalletEnv): WalletConfig {
    const identityKey = env[WALLET_ENV.identityKey]?.trim();
    if (!identityKey) {
        throw new WalletConfigError(WALLET_ENV.identityKey, `${WALLET_ENV.identityKey} is not set`);
    }
    return {
        gatewayUrl: httpUrl(env, WALLET_ENV.gatewayUrl),
        rpcUrl: httpUrl(env, WALLET_ENV.rpcUrl),
        identityUrl: httpUrl(env, WALLET_ENV.identityUrl),
        identityKey,
        authRedirectUrl: env[WALLET_ENV.authRedirectUrl]?.trim()
            ? httpUrl(env, WALLET_ENV.authRedirectUrl)
            : null,
        chainId: PAXEER_CHAIN_ID,
    };
}

export function processWalletEnv(): WalletEnv {
    return {
        NEXT_PUBLIC_PAXEER_WALLET_API: process.env.NEXT_PUBLIC_PAXEER_WALLET_API,
        NEXT_PUBLIC_PAXEER_RPC_URL: process.env.NEXT_PUBLIC_PAXEER_RPC_URL,
        NEXT_PUBLIC_SUPABASE_URL: process.env.NEXT_PUBLIC_SUPABASE_URL,
        NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY,
        NEXT_PUBLIC_AUTH_REDIRECT_URL: process.env.NEXT_PUBLIC_AUTH_REDIRECT_URL,
    };
}

export type WalletConfigResult =
    | { readonly ok: true; readonly config: WalletConfig }
    | { readonly ok: false; readonly error: WalletConfigError };

export function resolveWalletConfig(env: WalletEnv = processWalletEnv()): WalletConfigResult {
    try {
        return { ok: true, config: readWalletConfig(env) };
    } catch (error) {
        if (error instanceof WalletConfigError) return { ok: false, error };
        throw error;
    }
}

export function authRedirectUrl(config: WalletConfig, origin: string): string {
    return config.authRedirectUrl ?? `${origin.replace(/\/+$/, '')}/wallet/auth/callback/`;
}
