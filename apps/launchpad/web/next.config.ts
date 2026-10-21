import path from 'path';
import type { NextConfig } from 'next';
import { withSentryConfig } from '@sentry/nextjs';
import bundleAnalyzer from '@next/bundle-analyzer';
import { backendConfigurationProblems, browserImageOrigins } from './src/core/backend-origins';

// Run `ANALYZE=true pnpm --filter @sidiora/web build` to emit treemap reports
// (.next/analyze/*.html) for per-route JS budgeting. No-op for normal builds.
const withBundleAnalyzer = bundleAnalyzer({ enabled: process.env.ANALYZE === 'true' });

// CSP headers are now set at RUNTIME via src/middleware.ts so that
// NEXT_PUBLIC_WALLET_IFRAME_ORIGIN is always read fresh (not baked at build time).

/**
 * Remote image hosts: the http(s) origins of core/backend-origins.ts
 * browserImageOrigins(), the same resolver the SDK URL builders and the CSP
 * use, so token logos, avatars and banners served by metadata/gateway are
 * optimisable exactly when the bundle requests them, with or without explicit
 * variables, and no host is listed that the bundle never uses.
 */
function backendImageHosts(): Array<{ protocol: 'https' | 'http'; hostname: string; port?: string }> {
  const problems = backendConfigurationProblems();
  if (problems.length && process.env.NODE_ENV === 'production') {
    // The deployed page renders the configuration error state; say why at build time too.
    console.error(`[sidiora] backend origins are not configured: ${problems.map(problem => problem.message).join('; ')}`);
  }
  const canonical = process.env.NEXT_PUBLIC_CANONICAL_ORIGIN;
  const origins = [...browserImageOrigins()];
  if (canonical) { try { origins.push(new URL(canonical).origin); } catch {} }
  const seen = new Set<string>();
  const out: Array<{ protocol: 'https' | 'http'; hostname: string; port?: string }> = [];
  for (const origin of origins) {
    if (seen.has(origin)) continue;
    seen.add(origin);
    const url = new URL(origin);
    out.push({ protocol: url.protocol === 'http:' ? 'http' : 'https', hostname: url.hostname, ...(url.port ? { port: url.port } : {}) });
  }
  return out;
}

/**
 * Same-origin backend paths. The wallet identity session requires the identity
 * origin to equal the page origin (core/wallet-sdk/auth.ts), and the network
 * authority / preflight / private snapshot reads are same-origin by contract,
 * so the web server proxies these paths to the private Railway services.
 *
 * Upstreams are server-side only (never NEXT_PUBLIC_) and are read when the
 * build writes routes-manifest.json, so they must be set as build args.
 * Next's external-rewrite proxy sets Host to the upstream, adds
 * X-Forwarded-Host = the public web host, and passes the edge's
 * X-Forwarded-Proto / X-Forwarded-For / Origin through unchanged.
 *
 * WebSocket upgrades are proxied by the same rewrite in the standalone server
 * (router-server upgradeHandler -> proxyRequest(ws: true)), but the public
 * market stream is opened directly on NEXT_PUBLIC_STREAM_ORIGIN; only the
 * private stream uses NEXT_PUBLIC_PRIVATE_STREAM_ORIGIN.
 *
 * Path ownership on the web origin follows the production topology
 * (deploy/production/topology/topology.py): `/v2/auth/*` and
 * `/v2/stream/tickets` -> media/gateway; `/v2/snapshots` -> core/api REST
 * (API_ROLE=rest); `/v2/stream` and `/v2/stream/sse` -> core/api sockets
 * (API_ROLE=sockets). The gateway's own `/v2/stream` (the social chat socket)
 * is never addressed through the web origin: the chat client opens it on the
 * absolute gateway host (NEXT_PUBLIC_GATEWAY_URL + NEXT_PUBLIC_SOCIAL_WS_PATH),
 * so `/v2/stream` here is unambiguously the market/private data stream.
 */
function upstream(value: string | undefined, fallback: string): string {
  return (value || fallback).replace(/\/+$/, '');
}

async function backendRewrites() {
  // A local `next dev` without upstreams keeps these paths unrouted (404)
  // instead of proxying to Railway-private hosts it cannot resolve.
  if (process.env.NODE_ENV !== 'production' && !process.env.WEB_GATEWAY_UPSTREAM && !process.env.WEB_API_UPSTREAM) return { beforeFiles: [], afterFiles: [], fallback: [] };
  const gateway = upstream(process.env.WEB_GATEWAY_UPSTREAM, 'http://gateway.railway.internal:8080');
  const api = upstream(process.env.WEB_API_UPSTREAM, 'http://api.railway.internal:8080');
  const sockets = upstream(process.env.WEB_SOCKETS_UPSTREAM, 'http://api-sockets.railway.internal:8080');
  const charts = upstream(process.env.WEB_CHARTS_UPSTREAM, 'http://charts.railway.internal:8080');
  return {
    beforeFiles: [
      // Identity (media/gateway): challenges, sessions, `sessions:refresh`, and stream tickets.
      { source: '/v2/auth/:path*', destination: `${gateway}/v2/auth/:path*` },
      { source: '/v2/stream/tickets', destination: `${gateway}/v2/stream/tickets` },
      // Control plane (media/gateway connected runtime, security/control): public GET
      // /v2/control/caps/{capability} read by security/control/cap-admission.ts on the
      // web origin before token creation, plus the authenticated POST operations.
      { source: '/v2/control/:path*', destination: `${gateway}/v2/control/:path*` },
      // core/api REST (API_ROLE=rest): runtime authority, preflight and topic snapshots.
      { source: '/v2/network/authority', destination: `${api}/v2/network/authority` },
      { source: '/v2/preflight/:path*', destination: `${api}/v2/preflight/:path*` },
      { source: '/v2/snapshots', destination: `${api}/v2/snapshots` },
      // core/api sockets (API_ROLE=sockets): the only role that registers GET /v2/stream and /v2/stream/sse.
      { source: '/v2/stream', destination: `${sockets}/v2/stream` },
      { source: '/v2/stream/sse', destination: `${sockets}/v2/stream/sse` },
      // core/trading-charts (no public charts domain): the versioned UDF documents
      // (RegisterProductionUDF). Live candles are core/api `/v2/stream` topics.
      { source: '/v2/udf/config', destination: `${charts}/v2/udf/config` },
      { source: '/v2/udf/symbols', destination: `${charts}/v2/udf/symbols` },
      { source: '/v2/udf/history', destination: `${charts}/v2/udf/history` },
      { source: '/v2/udf/time', destination: `${charts}/v2/udf/time` },
    ],
    afterFiles: [],
    fallback: [],
  };
}

const nextConfig: NextConfig = {
  rewrites: backendRewrites,
  // Builds can target a separate directory so `next build` never clobbers the running dev server's .next.
  distDir: process.env.NEXT_DIST_DIR ?? '.next',
  // Dev-only: lets the dev server be opened by this box's public IP without
  // Next blocking its own /_next assets as cross-origin.
  allowedDevOrigins: ['178.18.246.58', 'localhost', '127.0.0.1'],
  // `standalone` is for the Railway/Docker runtime (`node .next/standalone/
  // server.js`). On Vercel it is unnecessary and its file-tracing follows
  // pnpm's symlinked node_modules to absolute paths that break the deploy
  // (styled-jsx ENOENT), so disable it there (Vercel sets VERCEL=1 at build).
  output: process.env.VERCEL ? undefined : 'standalone',
  compress: true,

  // This app lives at apps/launchpad/web inside a pnpm workspace whose root is
  // apps/launchpad/. Next's NFT auto-detection picks the wrong tracing root and emits
  // symlinks that escape to an absolute /node_modules/.pnpm/... path, which
  // makes `vercel deploy --prebuilt` die with `ENOENT lstat .../styled-jsx`
  // (next.js#73648). Pinning the trace root to the workspace root keeps every
  // traced symlink relative and inside the uploaded artifact.
  //
  // `next dev --turbopack` resolves imports only inside that same root, and
  // Next forces turbopack.root to equal outputFileTracingRoot. The create flow
  // imports services/launchpad/security/control/cap-admission.ts, so in dev both
  // roots are the repository root; production builds (webpack) keep apps/launchpad/.
  outputFileTracingRoot: process.env.NODE_ENV === 'development' ? path.join(__dirname, '..', '..', '..') : path.join(__dirname, '..', '..'),
  turbopack: { root: process.env.NODE_ENV === 'development' ? path.join(__dirname, '..', '..', '..') : path.join(__dirname, '..', '..') },
  outputFileTracingIncludes: {
    '/*': [
      '../node_modules/.pnpm/sharp@*/node_modules/@img/sharp-libvips-*/versions.json',
      '../node_modules/.pnpm/sharp@*/node_modules/@img/sharp-libvips-*/package.json',
    ],
  },

  images: {
    remotePatterns: [
      ...backendImageHosts(),
      { protocol: 'https', hostname: 'api.dicebear.com' },
      { protocol: 'https', hostname: 'ipfs.io' },
      { protocol: 'https', hostname: 'gateway.pinata.cloud' },
      { protocol: 'https', hostname: 'cloudflare-ipfs.com' },
      { protocol: 'https', hostname: 'arweave.net' },
      { protocol: 'https', hostname: 'nftstorage.link' },
    ],
    // Remote SVG requires a validated raster derivative; local static icons bypass optimization.
    dangerouslyAllowSVG: false,
    maximumResponseBody: 8388608,
    contentDispositionType: 'attachment',
    contentSecurityPolicy: "default-src 'self'; script-src 'none'; style-src 'unsafe-inline'; sandbox;",
  },

  experimental: {
    serverActions: {},
    mdxRs: true,
    imgOptConcurrency: 2,
    imgOptMaxInputPixels: 16777216,
    imgOptTimeoutInSeconds: 7,
  },

  transpilePackages: [
    'candles-sdk',
    'indexer-sdk',
    'metadata-sdk',
    'ranking-algo-sdk',
    'stats-sdk',
    'users-sdk',
    'sidiora-launchpad-v3',
    'paxifyui',
    'sidiora-ui-tokens',
  ],
};

// Wrap with Sentry. Source-map upload only runs when SENTRY_AUTH_TOKEN is set
// (CI/release builds); locally it is a transparent no-op. `tunnelRoute` proxies
// Sentry ingestion through our own origin so ad-blockers don't drop events.
export default withSentryConfig(withBundleAnalyzer(nextConfig), {
  org: process.env.SENTRY_ORG,
  project: process.env.SENTRY_PROJECT,
  authToken: process.env.SENTRY_AUTH_TOKEN,
  silent: !process.env.CI,
  widenClientFileUpload: true,
  tunnelRoute: '/monitoring',
  disableLogger: true,
  automaticVercelMonitors: false,
  sourcemaps: {
    disable: !process.env.SENTRY_AUTH_TOKEN,
  },
});
