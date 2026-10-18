import { afterEach, describe, expect, it, vi } from 'vitest';
import { NextRequest } from 'next/server';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
// Loaded before any test flips NODE_ENV, so React's JSX runtime matches the transform.
import { BackendConfigurationError } from '@/ui/shared/backend-configuration-error';
import { plainCopy } from '@/core/user-facing-error';

/**
 * The served CSP (src/middleware.ts) and every URL the browser clients can
 * produce (core/sdk-config.ts builders, the SDK stream origin, the identity
 * runtime's private origins, livestream, explorer, RPC, embedded wallet,
 * WalletConnect) come from one resolver, core/backend-origins.ts. For each
 * environment shape the connect-src the real middleware serves must EQUAL the
 * set of origins those real functions resolve: nothing missing, nothing extra.
 */
const NAMES = [
  'NEXT_PUBLIC_DATA_API_URL', 'NEXT_PUBLIC_GATEWAY_URL', 'NEXT_PUBLIC_METADATA_API_URL', 'NEXT_PUBLIC_PNL_API_URL', 'NEXT_PUBLIC_STATS_API_URL',
  'NEXT_PUBLIC_LIVESTREAM_API_URL', 'NEXT_PUBLIC_STREAM_ORIGIN', 'NEXT_PUBLIC_PRIVATE_API_ORIGIN', 'NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN',
  'NEXT_PUBLIC_RPC_URL', 'NEXT_PUBLIC_RPC_URL_FALLBACK', 'NEXT_PUBLIC_PAXEER_RPC_URL', 'NEXT_PUBLIC_SOCIAL_WS_PATH', 'NEXT_PUBLIC_SOCIAL_WRITE_PREFIX',
  'NEXT_PUBLIC_EXPLORER_API', 'NEXT_PUBLIC_PAXEER_WALLET_API', 'NEXT_PUBLIC_SUPABASE_URL', 'NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY',
  'NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID', 'NEXT_PUBLIC_WALLET_IFRAME_ORIGIN',
] as const;
type Name = (typeof NAMES)[number];
const saved = Object.fromEntries(NAMES.map(name => [name, process.env[name]]));
const savedNodeEnv = process.env.NODE_ENV;
const env = process.env as Record<string, string | undefined>;

/**
 * The Railway web service's backend variables (project sidiora-launch).
 * Production media/metadata is mTLS-only (METADATA_INGRESS=mtls), so the
 * metadata base URL is the gateway origin, which proxies the metadata routes.
 */
const RAILWAY: Partial<Record<Name, string>> = {
  NEXT_PUBLIC_DATA_API_URL: 'https://api-production-e4bd.up.railway.app',
  NEXT_PUBLIC_GATEWAY_URL: 'https://gateway-production-800c.up.railway.app',
  NEXT_PUBLIC_METADATA_API_URL: 'https://gateway-production-800c.up.railway.app',
  NEXT_PUBLIC_PNL_API_URL: 'https://pnl-production-72f3.up.railway.app',
};

function configure(values: Partial<Record<Name, string>>, nodeEnv = 'production') {
  for (const name of NAMES) delete env[name];
  Object.assign(env, values);
  env.NODE_ENV = nodeEnv;
  vi.resetModules();
}
afterEach(() => {
  for (const name of NAMES) { if (saved[name] === undefined) delete env[name]; else env[name] = saved[name]; }
  env.NODE_ENV = savedNodeEnv;
  vi.resetModules();
});

async function servedPolicy(): Promise<Response> {
  const { middleware } = await import('@/middleware');
  return middleware(new NextRequest('https://web-production-d956e.up.railway.app/explore'));
}

function directive(policy: string, name: string): string[] {
  const found = policy.split(';').map(part => part.trim()).find(part => part.startsWith(`${name} `));
  expect(found, name).toBeDefined();
  return found!.split(/\s+/).slice(1);
}

async function servedConnectSrc(): Promise<string[]> {
  const policy = (await servedPolicy()).headers.get('Content-Security-Policy') ?? '';
  return directive(policy, 'connect-src');
}

/** Every origin the browser clients resolve for this environment, from the real client functions. */
async function clientOrigins(): Promise<string[]> {
  const config = await import('@/core/sdk-config');
  const origins = await import('@/core/backend-origins');
  const { getPaxeerRpcUrls } = await import('@/core/rpc');
  const { paxeerWalletChain } = await import('@/core/wallet-sdk/client');
  const resolved = origins.backendOrigins();
  const wallet = origins.embeddedWalletEndpoints();
  const urls = [
    config.dataApiUrl('/v2/pools'), config.gatewayUrl('/v3/profiles/x'), config.metadataApiUrl('/v2/tokens/x/metadata'), config.pnlApiUrl('/v3/me/portfolio'),
    config.socialReadUrl('/v2/social/users/x/followers'), config.socialWriteUrl('/v2/chat/read'), config.socialWsUrl(), config.statsApiOrigin(),
    // SDK stream origin (core/market/client.ts, search-client.ts) and the identity runtime's private origins (wallet-sdk/runtime.tsx).
    resolved.stream, resolved.privateApi, resolved.privateStream,
    origins.livestreamApiBase(), origins.explorerApiBase(), ...getPaxeerRpcUrls(), paxeerWalletChain.rpcUrls.default.http[0],
    // Embedded wallet REST + Supabase auth (https) and realtime (wss).
    wallet?.apiUrl, wallet?.supabaseUrl, wallet && wallet.supabaseUrl.replace(/^http/, 'ws'),
    ...(origins.walletConnectProjectId() ? origins.WALLETCONNECT_ORIGINS : []),
  ];
  return [...new Set(urls.filter((url): url is string => !!url).map(url => new URL(url).origin))].sort();
}

describe('backend origin resolver and CSP', () => {
  it('serves connect-src equal to the client origins for the Railway production environment', async () => {
    configure(RAILWAY);
    const origins = await clientOrigins();
    expect(origins).toEqual([
      'https://api-production-e4bd.up.railway.app', 'https://api.paxscan.io', 'https://api5.mainnet-beta.paxeer.network',
      'https://gateway-production-800c.up.railway.app', 'https://pnl-production-72f3.up.railway.app',
      'wss://api-production-e4bd.up.railway.app', 'wss://gateway-production-800c.up.railway.app',
    ]);
    const connect = await servedConnectSrc();
    expect(connect[0]).toBe("'self'");
    expect(connect.slice(1).sort()).toEqual(origins);
    expect(connect.join(' ')).not.toMatch(/kindlelaunch|metadata-production|wss:(\s|$)/);
    expect(connect.filter(origin => origin === 'https://gateway-production-800c.up.railway.app')).toHaveLength(1);
  });

  it('builds the metadata routes on the gateway origin without a second prefix', async () => {
    configure(RAILWAY);
    const { metadataApiUrl } = await import('@/core/sdk-config');
    const gateway = 'https://gateway-production-800c.up.railway.app';
    const token = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
    expect(metadataApiUrl('/v3/metadata-publication-authority')).toBe(`${gateway}/v3/metadata-publication-authority`);
    expect(metadataApiUrl(`/v3/tokens/${token}/metadata-publications`)).toBe(`${gateway}/v3/tokens/${token}/metadata-publications`);
    expect(metadataApiUrl(`/v2/tokens/${token}/assets/op`)).toBe(`${gateway}/v2/tokens/${token}/assets/op`);
    expect(metadataApiUrl('/metadata/batch?addresses=a')).toBe(`${gateway}/metadata/batch?addresses=a`);
    expect(metadataApiUrl(`/logo/${token}.png`)).toBe(`${gateway}/logo/${token}.png`);
    // The metadata action audience the browser signs (app/tokens/[tokenAddress]/metadata/page.tsx) is the gateway origin.
    expect(new URL(metadataApiUrl('/'), 'https://web-production-d956e.up.railway.app').origin).toBe(gateway);
    configure({ ...RAILWAY, NEXT_PUBLIC_METADATA_API_URL: `${gateway}/` });
    const trailing = await import('@/core/sdk-config');
    expect(trailing.metadataApiUrl('/v3/metadata-publication-authority')).toBe(`${gateway}/v3/metadata-publication-authority`);
  });

  it('follows every configured variable in both the clients and the CSP, exactly', async () => {
    configure({
      NEXT_PUBLIC_DATA_API_URL: 'https://data.example.test', NEXT_PUBLIC_GATEWAY_URL: 'https://gw.example.test/', NEXT_PUBLIC_METADATA_API_URL: 'https://meta.example.test',
      NEXT_PUBLIC_PNL_API_URL: 'https://pnl.example.test', NEXT_PUBLIC_STATS_API_URL: 'https://stats.example.test', NEXT_PUBLIC_LIVESTREAM_API_URL: 'https://live.example.test',
      NEXT_PUBLIC_STREAM_ORIGIN: 'wss://sockets.example.test', NEXT_PUBLIC_PRIVATE_API_ORIGIN: 'https://web.example.test', NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN: 'wss://web.example.test',
      NEXT_PUBLIC_RPC_URL: 'https://rpc.example.test', NEXT_PUBLIC_RPC_URL_FALLBACK: 'https://rpc2.example.test', NEXT_PUBLIC_SOCIAL_WS_PATH: '/v2/stream',
      NEXT_PUBLIC_EXPLORER_API: 'https://explorer.example.test/api/v2', NEXT_PUBLIC_PAXEER_WALLET_API: 'https://wallet.example.test',
      NEXT_PUBLIC_SUPABASE_URL: 'https://project.supabase.example.test', NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: 'sb_publishable_example',
      NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID: 'example-project',
    });
    const origins = await clientOrigins();
    expect(origins).toEqual(expect.arrayContaining([
      'https://data.example.test', 'wss://gw.example.test', 'https://stats.example.test', 'https://live.example.test', 'wss://sockets.example.test',
      'https://web.example.test', 'wss://web.example.test', 'https://rpc.example.test', 'https://rpc2.example.test', 'https://explorer.example.test',
      'https://wallet.example.test', 'https://project.supabase.example.test', 'wss://project.supabase.example.test', 'wss://relay.walletconnect.org',
    ]));
    expect(origins.join(' ')).not.toMatch(/railway\.app|kindlelaunch|paxscan|paxportwallet|paxeer\.network/);
    const connect = await servedConnectSrc();
    expect(connect.slice(1).sort()).toEqual(origins);
  });

  it('lists the default explorer, wallet API and RPC only because the clients use those defaults', async () => {
    configure({ ...RAILWAY, NEXT_PUBLIC_SUPABASE_URL: 'https://project.supabase.example.test', NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: 'sb_publishable_example' });
    const origins = await clientOrigins();
    expect(origins).toEqual(expect.arrayContaining(['https://api.paxscan.io', 'https://connect.paxportwallet.com', 'https://api5.mainnet-beta.paxeer.network']));
    expect((await servedConnectSrc()).slice(1).sort()).toEqual(origins);
    // Without the publishable key the embedded wallet is off: no wallet or Supabase host is contacted or allowed.
    configure({ ...RAILWAY, NEXT_PUBLIC_SUPABASE_URL: 'https://project.supabase.example.test' });
    const without = await clientOrigins();
    expect(without.join(' ')).not.toMatch(/paxportwallet|supabase/);
    expect((await servedConnectSrc()).slice(1).sort()).toEqual(without);
  });

  it('fails loudly in production when a required origin is unset: no fallback host, and the page renders the configuration error', async () => {
    configure({});
    const { backendConfigurationProblems } = await import('@/core/backend-origins');
    expect(backendConfigurationProblems().map(problem => problem.message)).toEqual([
      'NEXT_PUBLIC_DATA_API_URL is not set: it is required (there is no fallback host)',
      'NEXT_PUBLIC_GATEWAY_URL is not set: it is required (there is no fallback host)',
      'NEXT_PUBLIC_METADATA_API_URL is not set: it is required (there is no fallback host)',
      'NEXT_PUBLIC_PNL_API_URL is not set: it is required (there is no fallback host)',
    ]);
    const config = await import('@/core/sdk-config');
    expect(() => config.dataApiUrl('/v2/pools')).toThrow('NEXT_PUBLIC_DATA_API_URL is not set');
    expect(() => config.socialWsUrl()).toThrow('NEXT_PUBLIC_DATA_API_URL is not set');
    // The middleware still answers (the error page must load) and allows no backend host at all.
    const connect = await servedConnectSrc();
    expect(connect.sort()).toEqual(["'self'", 'https://api.paxscan.io', 'https://api5.mainnet-beta.paxeer.network']);

    const html = renderToStaticMarkup(createElement(BackendConfigurationError, { problems: backendConfigurationProblems() }));
    expect(html).toContain('role="alert"');
    expect(html).toContain(plainCopy.configuration);
    // Variable names are developer detail: never in a production page.
    expect(html).not.toMatch(/NEXT_PUBLIC_|data-detail/);
  });

  it('reports a malformed value the same way and never falls back', async () => {
    configure({ ...RAILWAY, NEXT_PUBLIC_DATA_API_URL: 'api-production-e4bd.up.railway.app' });
    const config = await import('@/core/sdk-config');
    expect(() => config.dataApiUrl('/v2/pools')).toThrow('NEXT_PUBLIC_DATA_API_URL="api-production-e4bd.up.railway.app" is not a usable backend origin: not an absolute URL');
    expect((await servedConnectSrc()).join(' ')).not.toMatch(/railway\.app/);
    configure({ ...RAILWAY, NEXT_PUBLIC_GATEWAY_URL: 'http://gateway-production-800c.up.railway.app' });
    await expect(import('@/core/backend-origins').then(module => module.backendOrigins())).rejects.toThrow('production builds require https/wss');
    configure({ ...RAILWAY, NEXT_PUBLIC_STREAM_ORIGIN: 'https://api-production-e4bd.up.railway.app' });
    await expect(import('@/core/backend-origins').then(module => module.backendOrigins())).rejects.toThrow('scheme must be one of wss:, ws:');
    configure({ ...RAILWAY, NEXT_PUBLIC_PNL_API_URL: 'https://user:pw@pnl.example.test' });
    const { backendConfigurationProblems } = await import('@/core/backend-origins');
    expect(backendConfigurationProblems().map(problem => problem.variable)).toEqual(['NEXT_PUBLIC_PNL_API_URL']);
  });

  it('has no fallback outside production builds either, and allows a plain-http local stack there', async () => {
    configure({}, 'development');
    const { backendConfigurationProblems } = await import('@/core/backend-origins');
    expect(backendConfigurationProblems().map(problem => problem.variable)).toEqual(['NEXT_PUBLIC_DATA_API_URL', 'NEXT_PUBLIC_GATEWAY_URL', 'NEXT_PUBLIC_METADATA_API_URL', 'NEXT_PUBLIC_PNL_API_URL']);
    expect((await servedConnectSrc()).join(' ')).not.toMatch(/railway\.app/);

    configure({ ...RAILWAY, NEXT_PUBLIC_DATA_API_URL: 'http://127.0.0.1:18080' }, 'development');
    const local = await clientOrigins();
    expect(local).toEqual(expect.arrayContaining(['http://127.0.0.1:18080', 'ws://127.0.0.1:18080']));
    const policy = (await servedPolicy()).headers.get('Content-Security-Policy') ?? '';
    expect(directive(policy, 'connect-src').slice(1).sort()).toEqual(local);
    expect(directive(policy, 'img-src')).toContain('http://127.0.0.1:18080');
  });

  it('frames and trusts exactly the configured wallet parents', async () => {
    configure(RAILWAY);
    let response = await servedPolicy();
    expect(directive(response.headers.get('Content-Security-Policy') ?? '', 'frame-ancestors')).toEqual(["'self'"]);
    expect(response.headers.get('X-Frame-Options')).toBe('SAMEORIGIN');
    configure({ ...RAILWAY, NEXT_PUBLIC_WALLET_IFRAME_ORIGIN: 'https://paxportwallet.com, https://staging.paxportwallet.com/' });
    const { walletParentOrigins } = await import('@/core/backend-origins');
    expect(walletParentOrigins()).toEqual(['https://paxportwallet.com', 'https://staging.paxportwallet.com']);
    response = await servedPolicy();
    const policy = response.headers.get('Content-Security-Policy') ?? '';
    expect(directive(policy, 'frame-ancestors')).toEqual(["'self'", ...walletParentOrigins()]);
    expect(directive(policy, 'frame-src')).toEqual(["'self'", 'blob:', ...walletParentOrigins()]);
    expect(response.headers.get('X-Frame-Options')).toBeNull();
  });
});
