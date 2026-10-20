/**
 * Paxeer Embedded Wallet — singleton client.
 *
 * The SDK creates a Supabase auth client internally; we need exactly one
 * instance per page so auth state stays in sync across components, hooks,
 * the wagmi connector, and the EIP-1193 bridge.
 *
 * SSR-safe: returns null on the server. Always guard call sites with a
 * typeof window check or wrap in useEffect.
 */

import { PaxeerWallet, type PaxeerWalletConfig } from "./sdk";
import { paxeerChain } from "@/core/chain";
import { getPaxeerRpcUrls } from "@/core/rpc";

const DEFAULT_API_URL = "https://connect.paxportwallet.com";
const DEFAULT_SUPABASE_URL = "https://zezsqawedbikldiedlse.supabase.co";

let instance: PaxeerWallet | null = null;
let lastConfigKey: string | null = null;

export interface ResolvedPaxeerConfig extends PaxeerWalletConfig {
  chainId: number;
  rpcUrl: string;
}

export function resolvePaxeerConfig(): ResolvedPaxeerConfig | null {
  if (typeof window === "undefined") return null;

  const apiUrl = process.env.NEXT_PUBLIC_PAXEER_WALLET_API || DEFAULT_API_URL;
  const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL || DEFAULT_SUPABASE_URL;
  const supabaseAnonKey = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY || "";
  // Route through getPaxeerRpcUrls so the embedded wallet's read RPC
  // honors the same env precedence + legacy-endpoint filtering as the
  // public client, instead of trusting a possibly-stale env var raw.
  const rpcUrl = getPaxeerRpcUrls()[0];
  if (rpcUrl === undefined) {
    throw new Error("[paxeer] getPaxeerRpcUrls() returned no RPC URL for the embedded wallet.");
  }

  // The Supabase publishable key MUST be set for the SDK to work; missing key
  // means the embedded wallet feature is disabled. Injected wallets still work.
  if (!supabaseAnonKey) return null;

  return { apiUrl, supabaseUrl, supabaseAnonKey, chainId: paxeerChain.id, rpcUrl };
}

export function getPaxeerWallet(): PaxeerWallet | null {
  const config = resolvePaxeerConfig();
  if (!config) return null;

  const key = `${config.apiUrl}|${config.supabaseUrl}|${config.supabaseAnonKey}`;
  if (instance && lastConfigKey === key) return instance;

  instance = new PaxeerWallet({
    apiUrl: config.apiUrl,
    supabaseUrl: config.supabaseUrl,
    supabaseAnonKey: config.supabaseAnonKey,
  });
  lastConfigKey = key;
  return instance;
}

/**
 * Where Supabase OAuth providers redirect users back to after login.
 * Always resolved against `window.location.origin` so it matches the URL the
 * user is actually on (localhost, preview, prod). Supports a hard override
 * via `NEXT_PUBLIC_AUTH_REDIRECT_URL`.
 *
 * MUST only be called from a browser/event-handler context — Next.js evaluates
 * client modules during SSR and a module-level const would lock in `""` for
 * some build configurations.
 */
export function getAuthRedirectUrl(): string {
  if (process.env.NEXT_PUBLIC_AUTH_REDIRECT_URL) {
    return process.env.NEXT_PUBLIC_AUTH_REDIRECT_URL;
  }
  if (typeof window === "undefined") {
    throw new Error(
      "[paxeer] getAuthRedirectUrl() called during SSR — only call from event handlers.",
    );
  }
  return `${window.location.origin}/auth/callback`;
}
