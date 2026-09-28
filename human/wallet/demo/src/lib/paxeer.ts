/**
 * Paxeer Wallet SDK singleton.
 *
 * Drop this file into any Paxeer Next.js app verbatim. The only thing that
 * varies per app is the env vars in `.env.local`.
 *
 * Why singleton: the SDK creates a Supabase client internally; we want exactly
 * one across the whole app so auth state stays in sync.
 */
import { PaxeerWallet } from '@paxeer/wallet';

function required(name: string, value: string | undefined): string {
  if (!value) {
    throw new Error(
      `[paxeer] Missing env var: ${name}. Copy .env.local.example to .env.local and fill it in.`,
    );
  }
  return value;
}

let instance: PaxeerWallet | null = null;

export function paxeerWallet(): PaxeerWallet {
  if (instance) return instance;
  instance = new PaxeerWallet({
    apiUrl: required('NEXT_PUBLIC_PAXEER_WALLET_API', process.env.NEXT_PUBLIC_PAXEER_WALLET_API),
    supabaseUrl: required('NEXT_PUBLIC_SUPABASE_URL', process.env.NEXT_PUBLIC_SUPABASE_URL),
    supabaseAnonKey: required(
      'NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY',
      process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY,
    ),
  });
  return instance;
}

/**
 * Where Supabase OAuth providers redirect users back to after login.
 *
 * IMPORTANT: this MUST be a function (not a const) because Next.js evaluates
 * client modules during SSR, where `window` is undefined. A module-level const
 * would resolve to `""` on the server pass and lock that empty value into the
 * client bundle for some build configurations — Supabase then silently falls
 * back to the project's "Site URL" when it receives an empty redirectTo.
 *
 * Always resolved against `window.location.origin` so it matches whatever URL
 * the user is actually on (localhost, preview deploys, prod). The optional
 * `NEXT_PUBLIC_AUTH_REDIRECT_URL` override is only useful if you want to force
 * a different host (rare).
 */
export function getAuthRedirectUrl(): string {
  if (process.env.NEXT_PUBLIC_AUTH_REDIRECT_URL) {
    return process.env.NEXT_PUBLIC_AUTH_REDIRECT_URL;
  }
  if (typeof window === 'undefined') {
    throw new Error('[paxeer] getAuthRedirectUrl() called during SSR — only call from event handlers.');
  }
  return `${window.location.origin}/auth/callback`;
}
