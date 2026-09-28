export const PWA_NETWORK_ENV = {
    gateway: 'NEXT_PUBLIC_PAXEER_WALLET_API',
    rpc: 'NEXT_PUBLIC_PAXEER_RPC_URL',
    identity: 'NEXT_PUBLIC_SUPABASE_URL',
    attestor: 'NEXT_PUBLIC_PAXEER_ATTESTOR_URL',
} as const;

export type PwaNetworkEnvName = (typeof PWA_NETWORK_ENV)[keyof typeof PWA_NETWORK_ENV];

export type PwaNetworkEnv = Partial<Record<PwaNetworkEnvName, string | undefined>>;

export class PwaConfigError extends Error {
    constructor(readonly variable: PwaNetworkEnvName, message: string) {
        super(message);
        this.name = 'PwaConfigError';
    }
}

export function normalizeBase(raw: string): string {
    const parsed = new URL(raw);
    const path = parsed.pathname.replace(/\/+$/, '');
    return `${parsed.origin}${path}`;
}

function base(env: PwaNetworkEnv, name: PwaNetworkEnvName): string | null {
    const raw = env[name]?.trim();
    if (!raw) return null;
    let parsed: URL;
    try {
        parsed = new URL(raw);
    } catch {
        throw new PwaConfigError(name, `${name} is not a valid URL`);
    }
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
        throw new PwaConfigError(name, `${name} must be an http or https URL`);
    }
    if (parsed.username || parsed.password) {
        throw new PwaConfigError(name, `${name} must not carry credentials`);
    }
    return normalizeBase(raw);
}

export function networkOnlyBases(env: PwaNetworkEnv): string[] {
    const bases = Object.values(PWA_NETWORK_ENV)
        .map((name) => base(env, name))
        .filter((value): value is string => value !== null);
    return [...new Set(bases)].sort();
}

export function pwaNetworkEnv(source: Record<string, string | undefined>): PwaNetworkEnv {
    return Object.fromEntries(Object.values(PWA_NETWORK_ENV).map((name) => [name, source[name]])) as PwaNetworkEnv;
}
