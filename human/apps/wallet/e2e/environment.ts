import { IDENTITY_KEY } from '../src/wallet/test/gateway';

export const APP_PORT = 3099;
export const GATEWAY_PORT = 3098;
export const GATEWAY_URL = `http://127.0.0.1:${GATEWAY_PORT}`;

export const APP_ENV: Record<string, string> = {
    NEXT_PUBLIC_PAXEER_WALLET_API: GATEWAY_URL,
    NEXT_PUBLIC_PAXEER_RPC_URL: `${GATEWAY_URL}/rpc`,
    NEXT_PUBLIC_SUPABASE_URL: GATEWAY_URL,
    NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: IDENTITY_KEY,
    NEXT_PUBLIC_AUTH_REDIRECT_URL: `http://127.0.0.1:${APP_PORT}/auth/callback`,
};
