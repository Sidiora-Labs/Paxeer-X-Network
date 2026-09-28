import { describe, expect, it } from 'vitest';
import { PAXEER_CHAIN_ID } from '@paxeer/wallet';
import { WALLET_ENV, WalletConfigError, authRedirectUrl, readWalletConfig, resolveWalletConfig, type WalletEnv } from './config';

const env: WalletEnv = {
    NEXT_PUBLIC_PAXEER_WALLET_API: 'http://127.0.0.1:8080/',
    NEXT_PUBLIC_PAXEER_RPC_URL: 'http://127.0.0.1:8545',
    NEXT_PUBLIC_SUPABASE_URL: 'http://127.0.0.1:54321',
    NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: 'synthetic-publishable-key',
};

describe('wallet config', () => {
    it('wallet_config_reads_every_variable_and_strips_trailing_slashes', () => {
        const config = readWalletConfig(env);
        expect(config).toEqual({
            gatewayUrl: 'http://127.0.0.1:8080',
            rpcUrl: 'http://127.0.0.1:8545',
            identityUrl: 'http://127.0.0.1:54321',
            identityKey: 'synthetic-publishable-key',
            authRedirectUrl: null,
            chainId: PAXEER_CHAIN_ID,
        });
        expect(authRedirectUrl(config, 'http://127.0.0.1:3000/')).toBe('http://127.0.0.1:3000/auth/callback');
        const redirected = readWalletConfig({ ...env, NEXT_PUBLIC_AUTH_REDIRECT_URL: 'http://127.0.0.1:3000/done' });
        expect(authRedirectUrl(redirected, 'http://127.0.0.1:3000')).toBe('http://127.0.0.1:3000/done');
    });

    it('wallet_config_refuses_missing_malformed_and_credentialled_values', () => {
        for (const name of Object.values(WALLET_ENV).filter((n) => n !== WALLET_ENV.authRedirectUrl)) {
            const missing = { ...env, [name]: undefined };
            const result = resolveWalletConfig(missing);
            expect(result.ok).toBe(false);
            if (!result.ok) expect(result.error.variable).toBe(name);
        }
        expect(() => readWalletConfig({ ...env, NEXT_PUBLIC_PAXEER_RPC_URL: 'ws://127.0.0.1:8546' })).toThrow(WalletConfigError);
        expect(() => readWalletConfig({ ...env, NEXT_PUBLIC_PAXEER_WALLET_API: 'not a url' })).toThrow(WalletConfigError);
        expect(() => readWalletConfig({ ...env, NEXT_PUBLIC_SUPABASE_URL: 'http://user:pass@127.0.0.1' })).toThrow(
            /must not carry credentials/,
        );
    });
});
