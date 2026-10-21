/**
 * The one resolver for every origin the browser may contact.
 *
 * sdk-config.ts (REST and WebSocket URL builders), core/market/client.ts and
 * app-foundation/search-client.ts (SDK stream origin), the identity runtime
 * (private API / stream), core/clients/livestream-api.ts, explorer-api.ts, the
 * embedded wallet client, the iframe wallet bridge, next.config.ts image hosts
 * and the CSP in src/middleware.ts all read from here. `browserConnectOrigins()`
 * is the CSP connect-src list and is built from exactly the values those
 * clients use, so an origin the bundle can call is always an origin the served
 * policy allows, and nothing else is listed.
 *
 * Required backend variables have no fallback host under any NODE_ENV: an
 * unset or malformed value is a configuration problem that the root layout
 * renders as a user-facing error state (ui/shared/backend-configuration-error.tsx)
 * and the server logs at startup (instrumentation.ts). A local checkout sets
 * them in client/apps/web/.env.local (gitignored).
 *
 * Edge-safe: no Node-only imports (src/middleware.ts runs on the edge runtime).
 * Every read is a literal `process.env.NEXT_PUBLIC_*` expression so Next inlines
 * the build-time value into the client bundle, the server and the middleware.
 */
import { getPaxeerRpcUrls } from './rpc';

/** Paxeer chain explorer API (Blockscout v2), used when NEXT_PUBLIC_EXPLORER_API is unset. */
export const PAXSCAN_EXPLORER_API = 'https://api.paxscan.io/api/v2';

/** PaxPort embedded wallet REST API, used when NEXT_PUBLIC_PAXEER_WALLET_API is unset. */
export const PAXPORT_WALLET_API = 'https://connect.paxportwallet.com';

export class BackendOriginError extends Error {
  readonly variable: string;
  constructor(name: string, value: string | undefined, reason: string) {
    super(value === undefined || value === ''
      ? `${name} is not set: ${reason}`
      : `${name}=${JSON.stringify(value)} is not a usable backend origin: ${reason}`);
    this.name = 'BackendOriginError';
    this.variable = name;
  }
}

function production(): boolean { return process.env.NODE_ENV === 'production'; }

/** Parses a configured base URL: http(s) only (wss/ws for stream origins), no credentials, query or fragment. Keeps a path prefix if present. */
function checked(name: string, value: string, schemes: readonly string[]): string {
  let url: URL;
  try { url = new URL(value); } catch { throw new BackendOriginError(name, value, 'not an absolute URL'); }
  if (!schemes.includes(url.protocol)) throw new BackendOriginError(name, value, `scheme must be one of ${schemes.join(', ')}`);
  if (production() && (url.protocol === 'http:' || url.protocol === 'ws:')) throw new BackendOriginError(name, value, 'production builds require https/wss');
  if (url.username || url.password || url.search || url.hash) throw new BackendOriginError(name, value, 'credentials, query and fragment are not allowed');
  return value.replace(/\/+$/, '');
}

const HTTP = ['https:', 'http:'] as const;
const WS = ['wss:', 'ws:'] as const;

function required(name: string, value: string | undefined): string {
  if (value) return checked(name, value, HTTP);
  throw new BackendOriginError(name, value, 'it is required (there is no fallback host)');
}
const optional = (name: string, value: string | undefined) => value ? checked(name, value, HTTP) : null;
const originOf = (url: string) => new URL(url).origin;
const wsFormOf = (origin: string) => origin.replace(/^http/, 'ws');

export interface BackendOrigins {
  /** core/api REST. */
  data: string;
  /** media/gateway: profiles, account writes, social REST and the chat socket. */
  gateway: string;
  metadata: string;
  pnl: string;
  /** core/stats-workers; null when no stats host is configured. */
  stats: string | null;
  /** media/livestream; null when not configured (calls answer as not served). */
  livestream: string | null;
  /** Public market stream origin (wss), defaulting to the ws form of the data host. */
  stream: string;
  /** Private portfolio REST/stream origins; null means the page origin (same-origin rewrites). */
  privateApi: string | null;
  privateStream: string | null;
}

export interface BackendConfiguration {
  /** Every resolved base URL; null when any variable is missing or unusable. */
  origins: BackendOrigins | null;
  /** One entry per missing or unusable variable, in a stable order. */
  problems: BackendOriginError[];
}

/** Resolves every backend base URL and collects every problem instead of stopping at the first. */
export function backendConfiguration(): BackendConfiguration {
  const problems: BackendOriginError[] = [];
  const attempt = <T>(resolve: () => T): T | undefined => {
    try { return resolve(); } catch (error) {
      if (error instanceof BackendOriginError) { problems.push(error); return undefined; }
      throw error;
    }
  };
  const data = attempt(() => required('NEXT_PUBLIC_DATA_API_URL', process.env.NEXT_PUBLIC_DATA_API_URL));
  const gateway = attempt(() => required('NEXT_PUBLIC_GATEWAY_URL', process.env.NEXT_PUBLIC_GATEWAY_URL));
  const metadata = attempt(() => required('NEXT_PUBLIC_METADATA_API_URL', process.env.NEXT_PUBLIC_METADATA_API_URL));
  const pnl = attempt(() => required('NEXT_PUBLIC_PNL_API_URL', process.env.NEXT_PUBLIC_PNL_API_URL));
  const stats = attempt(() => optional('NEXT_PUBLIC_STATS_API_URL', process.env.NEXT_PUBLIC_STATS_API_URL));
  const livestream = attempt(() => optional('NEXT_PUBLIC_LIVESTREAM_API_URL', process.env.NEXT_PUBLIC_LIVESTREAM_API_URL));
  const streamValue = process.env.NEXT_PUBLIC_STREAM_ORIGIN;
  const stream = streamValue ? attempt(() => originOf(checked('NEXT_PUBLIC_STREAM_ORIGIN', streamValue, WS))) : data && wsFormOf(originOf(data));
  const privateApiValue = process.env.NEXT_PUBLIC_PRIVATE_API_ORIGIN;
  const privateApi = privateApiValue ? attempt(() => originOf(checked('NEXT_PUBLIC_PRIVATE_API_ORIGIN', privateApiValue, HTTP))) : null;
  const privateStreamValue = process.env.NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN;
  const privateStream = privateStreamValue ? attempt(() => originOf(checked('NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN', privateStreamValue, WS))) : null;
  if (problems.length) return { origins: null, problems };
  return {
    origins: { data: data!, gateway: gateway!, metadata: metadata!, pnl: pnl!, stats: stats ?? null, livestream: livestream ?? null, stream: stream!, privateApi: privateApi ?? null, privateStream: privateStream ?? null },
    problems,
  };
}

/** Resolved backend base URLs. Throws the first BackendOriginError when the configuration is missing or unusable. */
export function backendOrigins(): BackendOrigins {
  const { origins, problems } = backendConfiguration();
  if (!origins) throw problems[0];
  return origins;
}

/** Every missing or unusable backend variable (empty when the deployment is configured). Never throws. */
export function backendConfigurationProblems(): BackendOriginError[] {
  return backendConfiguration().problems;
}

/** media/livestream base URL, or null when not configured or unusable (calls then answer as not served). */
export function livestreamApiBase(): string | null {
  try { return optional('NEXT_PUBLIC_LIVESTREAM_API_URL', process.env.NEXT_PUBLIC_LIVESTREAM_API_URL); } catch { return null; }
}

/** Explorer REST base (core/clients/explorer-api.ts): NEXT_PUBLIC_EXPLORER_API, else the Paxeer explorer. */
export function explorerApiBase(): string {
  const value = process.env.NEXT_PUBLIC_EXPLORER_API;
  return value ? checked('NEXT_PUBLIC_EXPLORER_API', value, HTTP) : PAXSCAN_EXPLORER_API;
}

export interface EmbeddedWalletEndpoints { apiUrl: string; supabaseUrl: string; supabaseAnonKey: string }

/**
 * Embedded wallet endpoints, or null when the feature is off. The SDK needs
 * both the Supabase project URL and its publishable key; without them the
 * sign-in dialog offers browser wallets only and no wallet host is contacted.
 */
export function embeddedWalletEndpoints(): EmbeddedWalletEndpoints | null {
  const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const supabaseAnonKey = process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY;
  if (!supabaseUrl || !supabaseAnonKey) return null;
  return {
    apiUrl: checked('NEXT_PUBLIC_PAXEER_WALLET_API', process.env.NEXT_PUBLIC_PAXEER_WALLET_API || PAXPORT_WALLET_API, HTTP),
    supabaseUrl: checked('NEXT_PUBLIC_SUPABASE_URL', supabaseUrl, HTTP),
    supabaseAnonKey,
  };
}

/**
 * Hosts the WalletConnect connector (wagmi `walletConnect`, @walletconnect/core
 * and its QR modal) contacts from the browser: the relay socket, the relay RPC,
 * telemetry, attestation and the wallet registry.
 */
export const WALLETCONNECT_ORIGINS = Object.freeze([
  'wss://relay.walletconnect.org', 'wss://relay.walletconnect.com',
  'https://rpc.walletconnect.org', 'https://rpc.walletconnect.com',
  'https://pulse.walletconnect.org', 'https://verify.walletconnect.org', 'https://verify.walletconnect.com',
  'https://api.web3modal.org',
]);

/** WalletConnect Cloud project id, or null when the WalletConnect connector is off. */
export function walletConnectProjectId(): string | null {
  return process.env.NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID || null;
}

/**
 * Wallet origins allowed to embed this app (CSP frame-ancestors / frame-src)
 * and trusted by the postMessage EIP-1193 bridge. Empty means the app is not
 * embeddable and the bridge is not installed.
 */
export function walletParentOrigins(): string[] {
  const out: string[] = [];
  for (const value of (process.env.NEXT_PUBLIC_WALLET_IFRAME_ORIGIN ?? '').split(',')) {
    const trimmed = value.trim();
    if (!trimmed) continue;
    try { const origin = new URL(trimmed).origin; if (origin !== 'null' && !out.includes(origin)) out.push(origin); } catch {}
  }
  return out;
}

/**
 * The CSP connect-src host list: every origin the browser can contact, exactly
 * when a client would use it. Backend REST hosts are listed in their http(s)
 * form; the hosts the bundle opens sockets on (market stream, gateway chat
 * socket, private stream, Supabase realtime) also in their ws(s) form.
 * Same-origin traffic (identity, snapshots, UDF charts, the private stream
 * rewrites) is covered by 'self'. When the backend configuration has a
 * problem no backend host is listed: the page renders the configuration
 * error and opens no backend connection.
 */
export function browserConnectOrigins(): string[] {
  const out = new Set<string>();
  const add = (url: string | null | undefined) => { if (url) out.add(originOf(url)); };
  const { origins } = backendConfiguration();
  if (origins) {
    for (const url of [origins.data, origins.gateway, origins.metadata, origins.pnl, origins.stats, origins.livestream, origins.privateApi]) add(url);
    add(origins.stream);
    add(wsFormOf(originOf(origins.gateway)));
    add(origins.privateStream);
  }
  for (const url of getPaxeerRpcUrls()) add(url);
  try { add(explorerApiBase()); } catch {}
  try {
    const wallet = embeddedWalletEndpoints();
    if (wallet) { add(wallet.apiUrl); add(wallet.supabaseUrl); add(wsFormOf(originOf(wallet.supabaseUrl))); }
  } catch {}
  if (walletConnectProjectId()) for (const origin of WALLETCONNECT_ORIGINS) out.add(origin);
  return [...out];
}

/**
 * Backend http(s) hosts the bundle loads images and media from (token logos,
 * banners and avatars on metadata/gateway, OG cards on pnl, stream thumbnails):
 * the CSP img-src/media-src extras and next.config.ts images.remotePatterns.
 */
export function browserImageOrigins(): string[] {
  const { origins } = backendConfiguration();
  if (!origins) return [];
  const out = new Set<string>();
  for (const url of [origins.data, origins.gateway, origins.metadata, origins.pnl, origins.stats, origins.livestream, origins.privateApi]) if (url) out.add(originOf(url));
  return [...out];
}
