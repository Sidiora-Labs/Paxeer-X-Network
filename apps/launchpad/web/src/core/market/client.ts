'use client';
import { createContext, useContext } from 'react';
import { SdkClient } from '@kindle/sdk';
import { dataApiUrl, gatewayUrl, metadataApiUrl, statsApiOrigin } from '@/core/sdk-config';
import { accountSession } from '@/widgets/profile/account-transport';
import { backendOrigins } from '@/core/backend-origins';

const production = process.env.NODE_ENV === 'production';
const originOf = (url: string) => new URL(url).origin;

/** NEXT_PUBLIC_STREAM_ORIGIN through the shared resolver; otherwise the ws(s) form of the client's api origin. */
function streamOrigin(apiOrigin: string): string {
  return process.env.NEXT_PUBLIC_STREAM_ORIGIN ? backendOrigins().stream : apiOrigin.replace(/^http/, 'ws');
}

let publicClient: SdkClient | undefined;
let userClient: SdkClient | undefined;
let statsClient: SdkClient | null | undefined;
let sessionClient: SdkClient | undefined;

/** Public (unauthenticated) client: api = core/api, identity = media/gateway, media = media/metadata. */
export function browserMarketClient(): SdkClient {
  if (publicClient) return publicClient;
  const apiOrigin = originOf(dataApiUrl('/'));
  const stream = streamOrigin(apiOrigin);
  publicClient = new SdkClient({ apiOrigin, identityOrigin: originOf(gatewayUrl('/')), mediaOrigin: originOf(metadataApiUrl('/')), streamOrigin: stream, approvedStreamOrigins: [stream], production });
  return publicClient;
}

/**
 * Public client for profiles: `/v2/profiles*` and `/v3/profiles/*` are generated
 * with service `media` and are served by media/gateway, which proxies them to
 * media/user over mTLS (media/user has no browser-reachable host in production).
 */
export function browserUserClient(): SdkClient {
  if (userClient) return userClient;
  const apiOrigin = originOf(dataApiUrl('/'));
  const stream = streamOrigin(apiOrigin);
  userClient = new SdkClient({ apiOrigin, identityOrigin: originOf(gatewayUrl('/')), mediaOrigin: originOf(gatewayUrl('/')), streamOrigin: stream, approvedStreamOrigins: [stream], production });
  return userClient;
}

/** Client addressing core/stats-workers directly (`/v1/pools/{p}/holders`); null when no stats host is configured. */
export function browserStatsClient(): SdkClient | null {
  if (statsClient !== undefined) return statsClient;
  const origin = statsApiOrigin();
  if (!origin) { statsClient = null; return statsClient; }
  const stream = streamOrigin(origin);
  statsClient = new SdkClient({ apiOrigin: origin, identityOrigin: originOf(gatewayUrl('/')), mediaOrigin: originOf(metadataApiUrl('/')), streamOrigin: stream, approvedStreamOrigins: [stream], production });
  return statsClient;
}

/** Bearer-bound client reading the live account session; requests fail with "Access token required" when signed out. */
export function browserSessionClient(): SdkClient {
  if (sessionClient) return sessionClient;
  const apiOrigin = originOf(dataApiUrl('/'));
  const stream = streamOrigin(apiOrigin);
  sessionClient = new SdkClient({ apiOrigin, identityOrigin: originOf(gatewayUrl('/')), mediaOrigin: originOf(gatewayUrl('/')), streamOrigin: stream, approvedStreamOrigins: [stream], production, session: { mode: 'bearer', token: () => accountSession()?.accessToken } });
  return sessionClient;
}

let gatewayClient: SdkClient | undefined;
let gatewaySessionClient: SdkClient | undefined;

/**
 * Public client whose api origin is media/gateway. Operations the gateway
 * serves itself (bounties: `/v2/bounties*`) are generated with service `api`,
 * so they need a client that addresses the gateway as its api origin.
 */
export function browserGatewayClient(): SdkClient {
  if (gatewayClient) return gatewayClient;
  const gateway = originOf(gatewayUrl('/'));
  const stream = streamOrigin(originOf(dataApiUrl('/')));
  gatewayClient = new SdkClient({ apiOrigin: gateway, identityOrigin: gateway, mediaOrigin: gateway, streamOrigin: stream, approvedStreamOrigins: [stream], production });
  return gatewayClient;
}

/** Bearer-bound gateway client (bounty create/submit/accept) reading the live account session. */
export function browserGatewaySessionClient(): SdkClient {
  if (gatewaySessionClient) return gatewaySessionClient;
  const gateway = originOf(gatewayUrl('/'));
  const stream = streamOrigin(originOf(dataApiUrl('/')));
  gatewaySessionClient = new SdkClient({ apiOrigin: gateway, identityOrigin: gateway, mediaOrigin: gateway, streamOrigin: stream, approvedStreamOrigins: [stream], production, session: { mode: 'bearer', token: () => accountSession()?.accessToken } });
  return gatewaySessionClient;
}

export const MarketClientContext = createContext<SdkClient | null>(null);
/** Overrides the gateway-addressed client (tests point it at a local server). */
export const GatewayClientContext = createContext<SdkClient | null>(null);

export function useMarketClient(): SdkClient {
  return useContext(MarketClientContext) ?? browserMarketClient();
}
