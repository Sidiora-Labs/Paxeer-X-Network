import { backendOrigins } from './backend-origins';

/**
 * URL builders for the production backend. Each service has its own public
 * origin, resolved by core/backend-origins.ts (required in production builds:
 * an unset value is a configuration error, never a fallback host):
 *   NEXT_PUBLIC_DATA_API_URL     -> core/api        (REST snapshots, /v2/pools, rankings)
 *   NEXT_PUBLIC_GATEWAY_URL      -> media/gateway   (profiles, watchlist and account writes proxied to
 *                                                     media/user, approved media, social REST + chat socket)
 *   NEXT_PUBLIC_METADATA_API_URL -> media/metadata  (token metadata, logos, banners)
 *   NEXT_PUBLIC_PNL_API_URL      -> core/pnl-tracker(portfolio snapshots, history, shares)
 *   NEXT_PUBLIC_STATS_API_URL    -> core/stats-workers (optional)
 * Identity (/v2/auth/*), /v2/snapshots and /v2/stream are same-origin rewrites (next.config.ts).
 */

// Base URLs come from the one shared resolver (core/backend-origins.ts), whose
// browserConnectOrigins() is the CSP connect-src in src/middleware.ts. In a
// production build a missing or malformed required value throws
// BackendOriginError here instead of falling back to another host; the root
// layout renders that as the configuration error state before any client runs.
const HOSTS = {
  // core/api: REST snapshots, /v2/pools/*, /v2/rankings/*.
  get data() { return backendOrigins().data; },
  // media/gateway: profiles, account writes, social REST + chat socket, approved media.
  get gateway() { return backendOrigins().gateway; },
  // media/user has no browser-reachable host in production (mTLS-only listener):
  // profiles, preferences and the watchlist go through the gateway.
  // media/metadata: token metadata, logos and banners.
  get metadata() { return backendOrigins().metadata; },
  // core/pnl-tracker: portfolio snapshots, history, shares, referrals.
  get pnl() { return backendOrigins().pnl; },
  // core/stats-workers: holders. Optional: '' means no stats host is addressable.
  get stats() { return backendOrigins().stats ?? ''; },
};

function socialPrefix(value: string | undefined): string {
  if (!value) return '';
  const trimmed = value.replace(/\/+$/, '');
  return trimmed && !trimmed.startsWith('/') ? `/${trimmed}` : trimmed;
}

// Path prefixes / WS routes (host-relative; env-overridable).
const PATHS = {
  // Social REST + WS through the production gateway, which serves the social
  // routes at root (`/v2/chat/*`, `/v2/social/*`, `/v2/safety/*`,
  // `/v2/profiles/{w}/followers|follow-stats`, `/v2/me/following/{w}`) and the
  // chat socket at `/v2/stream` (media/gateway proxy/connected.go
  // SocialConnectedRoutes, proxy/connected_ws.go). Unset means root and
  // `/v2/stream`; NEXT_PUBLIC_SOCIAL_WRITE_PREFIX=/ also means "no prefix".
  socialWritePrefix: socialPrefix(process.env.NEXT_PUBLIC_SOCIAL_WRITE_PREFIX),
  socialWs: process.env.NEXT_PUBLIC_SOCIAL_WS_PATH || '/v2/stream',
} as const;

/** Join a base + path with exactly one slash between them. */
function join(base: string, path: string): string {
  if (!path) return base;
  const b = base.replace(/\/$/, '');
  const p = path.startsWith('/') ? path : `/${path}`;
  return `${b}${p}`;
}

/**
 * Build a REST URL on the DATA edge (core/api): `/v2/pools/*`, `/v2/rankings/*`,
 * snapshots and the other core/api REST routes. Chart UDF documents are served
 * by core/trading-charts (core/charts-udf.ts), not here.
 */
export function dataApiUrl(path: string): string {
  return join(HOSTS.data, path);
}

/** Build a REST URL on media/gateway: profiles, account writes, social REST, approved media. */
export function gatewayUrl(path: string): string {
  return join(HOSTS.gateway, path);
}

/** @deprecated Use `gatewayUrl`. Retained as an alias during migration. */
export const mediaApiUrl = gatewayUrl;

/** Build a REST URL on media/metadata : /metadata/*, /logo, /banner. */
export function metadataApiUrl(path: string): string {
  return join(HOSTS.metadata, path);
}

/**
 * Build a SOCIAL READ URL. media/social has no public host in production: it
 * listens on the private network behind the gateway, so reads go through the
 * gateway like every other social request (`/v2/social/users/{w}/*`,
 * `/v2/profiles/{w}/followers|follow-stats`). The browser is cross-site to the
 * gateway, so a session-bound read carries `Authorization: Bearer` with
 * `credentials: 'omit'`.
 */
export function socialReadUrl(path: string): string {
  return join(`${HOSTS.gateway}${PATHS.socialWritePrefix}`, path);
}

/**
 * Build a SOCIAL WRITE URL on the gateway, which authenticates the session and
 * signs the actor assertion media/social trusts. The web origin is cross-site
 * to the gateway (cookies are neither set nor sent), so writes carry
 * `Authorization: Bearer <accessToken>` with `credentials: 'omit'`; a bearer
 * request needs no CSRF token (gateway auth/identity_transport.go:70).
 */
export function socialWriteUrl(path: string): string {
  return join(`${HOSTS.gateway}${PATHS.socialWritePrefix}`, path);
}

/** Build a REST URL on core/pnl-tracker : portfolio, cards, referrals. */
export function pnlApiUrl(path: string): string {
  return join(HOSTS.pnl, path);
}

/** Origin of core/stats-workers when `NEXT_PUBLIC_STATS_API_URL` is configured, else null. */
export function statsApiOrigin(): string | null {
  if (!HOSTS.stats) return null;
  try { return new URL(HOSTS.stats).origin; } catch { return null; }
}

/** Build a REST URL on core/stats-workers, or null when no stats host is configured. */
export function statsApiUrl(path: string): string | null {
  return statsApiOrigin() ? join(HOSTS.stats, path) : null;
}

/** ws/wss scheme matching the current page (client) or the configured origin. */
function wsScheme(origin: string): string {
  if (origin.startsWith('https')) return 'wss';
  if (origin.startsWith('http')) return 'ws';
  if (typeof window !== 'undefined' && window.location.protocol === 'https:') return 'wss';
  return 'ws';
}

/** Absolute ws(s):// URL for a path on the given (possibly empty) origin. */
function wsUrl(origin: string, path: string): string {
  if (origin) {
    const scheme = wsScheme(origin);
    const host = origin.replace(/^https?:\/\//, '');
    return `${scheme}://${join(host, path)}`;
  }
  if (typeof window !== 'undefined') {
    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws';
    return `${scheme}://${join(window.location.host, path)}`;
  }
  // SSR has no live socket; return a syntactically-valid placeholder.
  return `ws://localhost${path.startsWith('/') ? path : `/${path}`}`;
}

/**
 * Social chat socket on the ABSOLUTE gateway origin (`wss://<gateway>/v2/stream`),
 * never same-origin: on the web origin `/v2/stream` belongs to core-api's
 * sockets. Protocol v2: subprotocols `sidiora.v2` + `ticket.<64 hex>` (the
 * ticket from POST /v2/stream/tickets) and `?topics=` naming the ticket's
 * topics (gateway proxy/connected_ws.go ServeHTTP).
 */
export function socialWsUrl(): string {
  return wsUrl(HOSTS.gateway, PATHS.socialWs);
}
