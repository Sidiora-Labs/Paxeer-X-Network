export const ACCOUNT_ENV = {
    endpointUrl: 'NEXT_PUBLIC_PAXEER_RPC_URL',
    humanUrl: 'NEXT_PUBLIC_PAXEER_HUMAN_API',
    explorerUrl: 'NEXT_PUBLIC_PAXEER_EXPLORER_URL',
} as const;

export type AccountEnvName = (typeof ACCOUNT_ENV)[keyof typeof ACCOUNT_ENV];

export type AccountEnv = Partial<Record<AccountEnvName, string | undefined>>;

export interface AccountConfig {
    readonly endpointUrl: string;
    readonly humanUrl: string;
    readonly explorerUrl: string;
}

export class AccountConfigError extends Error {
    constructor(readonly variable: AccountEnvName, message: string) {
        super(message);
        this.name = 'AccountConfigError';
    }
}

function httpUrl(env: AccountEnv, name: AccountEnvName): string {
    const raw = env[name]?.trim();
    if (!raw) throw new AccountConfigError(name, `${name} is not set`);
    let parsed: URL;
    try {
        parsed = new URL(raw);
    } catch {
        throw new AccountConfigError(name, `${name} is not a valid URL`);
    }
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
        throw new AccountConfigError(name, `${name} must be an http or https URL`);
    }
    if (parsed.username || parsed.password) {
        throw new AccountConfigError(name, `${name} must not carry credentials`);
    }
    return raw.replace(/\/+$/, '');
}

export function readAccountConfig(env: AccountEnv): AccountConfig {
    return {
        endpointUrl: httpUrl(env, ACCOUNT_ENV.endpointUrl),
        humanUrl: httpUrl(env, ACCOUNT_ENV.humanUrl),
        explorerUrl: httpUrl(env, ACCOUNT_ENV.explorerUrl),
    };
}

export function processAccountEnv(): AccountEnv {
    return {
        NEXT_PUBLIC_PAXEER_RPC_URL: process.env.NEXT_PUBLIC_PAXEER_RPC_URL,
        NEXT_PUBLIC_PAXEER_HUMAN_API: process.env.NEXT_PUBLIC_PAXEER_HUMAN_API,
        NEXT_PUBLIC_PAXEER_EXPLORER_URL: process.env.NEXT_PUBLIC_PAXEER_EXPLORER_URL,
    };
}

export type AccountConfigResult =
    | { readonly ok: true; readonly config: AccountConfig }
    | { readonly ok: false; readonly error: AccountConfigError };

export function resolveAccountConfig(env: AccountEnv = processAccountEnv()): AccountConfigResult {
    try {
        return { ok: true, config: readAccountConfig(env) };
    } catch (error) {
        if (error instanceof AccountConfigError) return { ok: false, error };
        throw error;
    }
}
