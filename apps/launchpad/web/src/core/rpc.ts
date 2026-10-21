/**
 * Public Paxeer RPC endpoints the browser may read from.
 *
 * One authority for every client that needs an RPC URL (wagmi transports, the
 * wallet SDK public client, the embedded wallet's EIP-1193 bridge), so the
 * precedence is identical everywhere: NEXT_PUBLIC_RPC_URL, then
 * NEXT_PUBLIC_RPC_URL_FALLBACK, then NEXT_PUBLIC_PAXEER_RPC_URL. Endpoints
 * known to be offline are filtered out even when an environment still names
 * them, so a stale variable can never route reads to a dead host.
 */

export const DEFAULT_PAXEER_RPC_URL = 'https://api5.mainnet-beta.paxeer.network';

/** Hosts that no longer answer JSON-RPC for chain 125. */
const RETIRED_RPC_HOSTS = new Set(['public-mainnet.rpcpaxeer.online']);

function usable(value: string | undefined): string | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return null;
    if (RETIRED_RPC_HOSTS.has(url.hostname)) return null;
    return url.toString().replace(/\/$/, '');
  } catch {
    return null;
  }
}

export function getPaxeerRpcUrls(): string[] {
  const out: string[] = [];
  for (const candidate of [
    process.env.NEXT_PUBLIC_RPC_URL,
    process.env.NEXT_PUBLIC_RPC_URL_FALLBACK,
    process.env.NEXT_PUBLIC_PAXEER_RPC_URL,
  ]) {
    const url = usable(candidate);
    if (url && !out.includes(url)) out.push(url);
  }
  if (out.length === 0) out.push(DEFAULT_PAXEER_RPC_URL);
  return out;
}

/** The primary read RPC for this deployment. */
export const PAXEER_RPC_URL = getPaxeerRpcUrls()[0];
