import { EmbeddedWallet } from './EmbeddedWallet';
import type { PaxeerEmbeddedConfig } from './sdk/types';

/**
 * Production defaults for the Paxeer Embedded Wallet — kept here so a
 * frontend dev only has to pass the Supabase publishable key (the only
 * value that varies per project) to get a working singleton.
 *
 * Override any of these via the `EmbeddedWalletOptions` argument or via
 * environment variables resolved by `resolveEmbeddedConfigFromEnv()`.
 */
export const DEFAULT_API_URL = 'https://connect.paxportwallet.com';
export const DEFAULT_SUPABASE_URL = 'https://supabase.paxeer.app';
export const DEFAULT_RPC_URL = 'https://public-mainnet.rpcpaxeer.online/evm';
export const DEFAULT_CHAIN_ID = 125;

export interface EmbeddedWalletOptions extends Partial<PaxeerEmbeddedConfig> {
    /** Required — Supabase publishable / anon key. */
    supabaseAnonKey: string;
    /** Optional override for the JSON-RPC URL used for read calls (chain 125). */
    rpcUrl?: string;
    /** Optional override for the chain ID. Defaults to 125 (Paxeer Mainnet). */
    chainId?: number;
}

/**
 * Build a fully-resolved config from explicit options + the production
 * defaults. Throws if `supabaseAnonKey` is missing — the SDK will reject
 * an empty key at runtime, so we surface the failure here with a clearer
 * error.
 */
export function buildEmbeddedConfig(opts: EmbeddedWalletOptions) {
    if (!opts.supabaseAnonKey) {
        throw new Error(
            '[paxeer/wallet] EmbeddedWallet requires `supabaseAnonKey`. ' +
            'Pass it as an argument or set NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY / ' +
            'VITE_SUPABASE_PUBLISHABLE_KEY in your environment.',
        );
    }
    return {
        apiUrl: opts.apiUrl ?? DEFAULT_API_URL,
        supabaseUrl: opts.supabaseUrl ?? DEFAULT_SUPABASE_URL,
        supabaseAnonKey: opts.supabaseAnonKey,
        fetch: opts.fetch,
        rpcUrl: opts.rpcUrl ?? DEFAULT_RPC_URL,
        chainId: opts.chainId ?? DEFAULT_CHAIN_ID,
    };
}

// ── Env resolution ───────────────────────────────────────────────────

interface EnvLike {
    [key: string]: string | undefined;
}

/**
 * Pull the embedded-wallet config from a process-style env object.
 *
 * Supports both Next.js (`NEXT_PUBLIC_…`) and Vite (`VITE_…`) prefixes so
 * the same library works in both bundlers. Returns null if the publishable
 * key is missing — callers should treat that as "embedded wallet feature
 * disabled" and fall back to the self-custody flow.
 *
 * Build-tool gotcha (the reason this function looks repetitive):
 * Next.js and Vite **only** statically replace bare `process.env.X` /
 * `import.meta.env.X` references that appear *literally* in the source.
 * If we read `env.NEXT_PUBLIC_…` off a parameter — even a parameter that
 * defaults to `process.env` — the bundler can't see the access and the
 * value never gets baked into the client chunk, so the embedded wallet
 * silently falls back to "unavailable" in production. To make sure
 * inlining actually happens we hardcode every literal lookup below.
 * Callers that want to inject a custom env (tests, server-side adapters)
 * can still pass an explicit `env` argument and we'll prefer that.
 */
export function resolveEmbeddedConfigFromEnv(
    env?: EnvLike,
): EmbeddedWalletOptions | null {
    // When an explicit env object is provided (tests, custom callers) we use
    // it. Otherwise fall through to bare `process.env.X` reads so Webpack's
    // DefinePlugin can substitute the values at build time.
    const get = (key: string, literal: string | undefined): string | undefined => {
        if (env) return env[key];
        return literal;
    };

    const supabaseAnonKey =
        get('NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY', process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY) ??
        get('VITE_SUPABASE_PUBLISHABLE_KEY', process.env.VITE_SUPABASE_PUBLISHABLE_KEY) ??
        get('PUBLIC_SUPABASE_PUBLISHABLE_KEY', process.env.PUBLIC_SUPABASE_PUBLISHABLE_KEY) ??
        '';
    if (!supabaseAnonKey) return null;

    return {
        apiUrl:
            get('NEXT_PUBLIC_PAXEER_WALLET_API', process.env.NEXT_PUBLIC_PAXEER_WALLET_API) ??
            get('VITE_PAXEER_WALLET_API', process.env.VITE_PAXEER_WALLET_API) ??
            get('PUBLIC_PAXEER_WALLET_API', process.env.PUBLIC_PAXEER_WALLET_API),
        supabaseUrl:
            get('NEXT_PUBLIC_SUPABASE_URL', process.env.NEXT_PUBLIC_SUPABASE_URL) ??
            get('VITE_SUPABASE_URL', process.env.VITE_SUPABASE_URL) ??
            get('PUBLIC_SUPABASE_URL', process.env.PUBLIC_SUPABASE_URL),
        supabaseAnonKey,
        rpcUrl:
            get('NEXT_PUBLIC_PAXEER_RPC_URL', process.env.NEXT_PUBLIC_PAXEER_RPC_URL) ??
            get('VITE_PAXEER_RPC_URL', process.env.VITE_PAXEER_RPC_URL) ??
            get('PUBLIC_PAXEER_RPC_URL', process.env.PUBLIC_PAXEER_RPC_URL),
    };
}

// ── Singleton ────────────────────────────────────────────────────────

let instance: EmbeddedWallet | null = null;
let lastConfigKey: string | null = null;

/**
 * Lazily construct (or reuse) the process-wide `EmbeddedWallet` singleton.
 *
 * SSR-safe: returns null on the server because Supabase auth depends on
 * `localStorage`. Always guard call sites with `typeof window` or wrap in
 * `useEffect`.
 *
 * Call signatures:
 *
 *   getEmbeddedWallet({ supabaseAnonKey: 'sb_...' })
 *     → explicit config, full control
 *
 *   getEmbeddedWallet()
 *     → reads from `process.env` using the prefix conventions documented
 *       in `resolveEmbeddedConfigFromEnv()`. Returns null if env is missing.
 *
 * The singleton is keyed on `(apiUrl, supabaseUrl, supabaseAnonKey)` so
 * passing different config rebuilds rather than silently reusing the old
 * instance.
 */
export function getEmbeddedWallet(opts?: EmbeddedWalletOptions): EmbeddedWallet | null {
    if (typeof window === 'undefined') return null;

    const resolved = opts ?? resolveEmbeddedConfigFromEnv();
    if (!resolved) return null;

    const config = buildEmbeddedConfig(resolved);
    const key = `${config.apiUrl}|${config.supabaseUrl}|${config.supabaseAnonKey}`;
    if (instance && lastConfigKey === key) return instance;

    instance = new EmbeddedWallet(config);
    lastConfigKey = key;
    return instance;
}

/**
 * Where Supabase OAuth providers redirect users back to after sign-in.
 *
 * Default: `${window.location.origin}/auth/callback`. Override with the
 * `NEXT_PUBLIC_AUTH_REDIRECT_URL` (or `VITE_…`) env var if your app
 * proxies auth from a different host.
 *
 * MUST only be called from a browser/event-handler context — Next.js
 * evaluates client modules during SSR and a module-level const would lock
 * in `''` for some build configurations.
 */
export function getAuthRedirectUrl(env: EnvLike = (typeof process !== 'undefined' ? (process as { env?: EnvLike }).env ?? {} : {})): string {
    const override =
        env.NEXT_PUBLIC_AUTH_REDIRECT_URL ??
        env.VITE_AUTH_REDIRECT_URL ??
        env.PUBLIC_AUTH_REDIRECT_URL;
    if (override) return override;
    if (typeof window === 'undefined') {
        throw new Error(
            '[paxeer/wallet] getAuthRedirectUrl() called during SSR — only call ' +
            'from event handlers or the auth callback page.',
        );
    }
    return `${window.location.origin}/auth/callback`;
}

/**
 * Reset the singleton. Mainly useful in tests; production code should not
 * call this.
 */
export function __resetEmbeddedWalletSingleton(): void {
    instance = null;
    lastConfigKey = null;
}
