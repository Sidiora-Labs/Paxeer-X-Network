const withPWA = require('next-pwa')({
  dest: 'public',
  disable: process.env.NODE_ENV === 'development',
  register: true,
  skipWaiting: true,
  // Cache on demand, not 40 MB of charts, locales, models and source maps
  // on every visit. Trading requires a network; don't precache live pages.
  publicExcludes: ['!**/*'],
  buildExcludes: [/.*/],
  cacheStartUrl: false,
  runtimeCaching: [
    {
      urlPattern: /^https:\/\/.*\/static\/charting_library\/.*\.(?:js|css|woff2?)(?:\?.*)?$/i,
      handler: 'CacheFirst',
      options: {
        cacheName: 'chart-assets-on-demand',
        expiration: { maxEntries: 128, maxAgeSeconds: 7 * 24 * 60 * 60 },
      },
    },
    {
      urlPattern: /^https:\/\/.*\/_next\/static\/.*/i,
      handler: 'CacheFirst',
      options: {
        cacheName: 'static-assets',
        expiration: {
          maxEntries: 200,
          maxAgeSeconds: 365 * 24 * 60 * 60 // 1 year
        }
      }
    },
    {
      urlPattern: /^https:\/\/.*\/_next\/image\?.*/i,
      handler: 'CacheFirst',
      options: {
        cacheName: 'next-image',
        expiration: {
          maxEntries: 100,
          maxAgeSeconds: 24 * 60 * 60 // 24 hours
        }
      }
    },
    {
      urlPattern: /^https:\/\/.*\.(?:png|jpg|jpeg|svg|gif|webp)/i,
      handler: 'CacheFirst',
      options: {
        cacheName: 'static-images',
        expiration: {
          maxEntries: 100,
          maxAgeSeconds: 24 * 60 * 60 // 24 hours
        }
      }
    }
  ]
})

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Standalone output: produces .next/standalone with a minimal
  // node server + only the deps it traced, so the production
  // Docker image stays small (~180 MB vs ~1 GB for a full copy).
  // See Dockerfile for how the artifacts get assembled.
  output: 'standalone',
  outputFileTracingRoot: __dirname,
  webpack: config => {
    config.externals.push('pino-pretty', 'lokijs', 'encoding')
    return config
  },
  poweredByHeader: false,
  generateEtags: true,
  compress: true,
  images: {
    unoptimized: true, // No untrusted image decoding in the application server.
    remotePatterns: [
      {
        protocol: 'https',
        hostname: 'raw.githubusercontent.com',
        pathname: '/Paxeer-Network/**',
      },
    ],
    formats: ['image/webp'],
    deviceSizes: [640, 750, 828, 1080, 1200, 1920],
    imageSizes: [16, 24, 32, 48, 64, 96, 128],
  },
  async headers() {
    // F-15: baseline security headers.
    //
    // - HSTS: 1 year, includes subdomains. Safe to enable on prod
    //   because the app is served exclusively over HTTPS.
    // - Referrer-Policy: prevent leaking trade URLs (which can encode
    //   market + side) to third-party assets.
    // - X-Content-Type-Options: nosniff is a free win.
    // - Permissions-Policy: deny camera/microphone/geolocation outright.
    // - COOP: same-origin so cross-origin popups (extension wallets)
    //   can't reach into our window. We deliberately do NOT set
    //   `Cross-Origin-Embedder-Policy: require-corp` because the
    //   TradingView bundle uses cross-origin assets that don't ship
    //   CORP headers — turning COEP on would break the chart.
    const securityHeaders = [
      {
        key: 'Strict-Transport-Security',
        value: 'max-age=31536000; includeSubDomains',
      },
      {
        key: 'Referrer-Policy',
        value: 'strict-origin-when-cross-origin',
      },
      {
        key: 'X-Content-Type-Options',
        value: 'nosniff',
      },
      {
        key: 'X-Frame-Options',
        value: 'SAMEORIGIN',
      },
      {
        key: 'Permissions-Policy',
        value: 'camera=(), microphone=(), geolocation=(), payment=()',
      },
      {
        key: 'Cross-Origin-Opener-Policy',
        value: 'same-origin',
      },
    ];

    return [
      {
        source: '/_next/static/:path*',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=31536000, immutable',
          },
        ],
      },
      {
        source: '/static/:path*',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=31536000, immutable',
          },
        ],
      },
      {
        source: '/:path*',
        headers: securityHeaders,
      },
    ];
  },
};

// Sentry source-map upload is opt-in via the `@sentry/cli` postinstall
// script. We deliberately keep `silent: true` so dev builds don't spam
// warnings when no DSN/auth-token is configured. The plugin is a no-op
// at runtime when these envs are absent — local UI behaviour is
// identical to before.
const { withSentryConfig } = require('@sentry/nextjs');

const sentryWebpackPluginOptions = {
  silent: true,
  // No source-map upload by default; flip on when the org/project/auth
  // token are available. Without these, Sentry still receives errors —
  // only stack-frame filename remapping is missing.
  org: process.env.SENTRY_ORG,
  project: process.env.SENTRY_PROJECT,
  authToken: process.env.SENTRY_AUTH_TOKEN,
  // Don't upload sources unless explicitly enabled.
  widenClientFileUpload: false,
  hideSourceMaps: true,
  disableLogger: true,
};

const sentryOptions = {
  // Tunnel client requests through the Next app to bypass ad-blockers.
  // Disabled by default — set NEXT_PUBLIC_SENTRY_TUNNEL=1 to enable.
  tunnelRoute: process.env.NEXT_PUBLIC_SENTRY_TUNNEL === '1' ? '/monitoring' : undefined,
  automaticVercelMonitors: false,
};

module.exports = withSentryConfig(
  withPWA(nextConfig),
  sentryWebpackPluginOptions,
  sentryOptions,
);
