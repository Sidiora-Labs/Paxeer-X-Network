import { NextResponse } from 'next/server';
import type { NextRequest } from 'next/server';
import { browserConnectOrigins, browserImageOrigins, walletParentOrigins } from '@/core/backend-origins';

/**
 * Runtime CSP middleware.
 *
 * Content-Security-Policy is assembled here rather than in next.config.ts so
 * the frame-ancestors directive follows NEXT_PUBLIC_WALLET_IFRAME_ORIGIN at
 * request time. Every host list comes from core/backend-origins.ts, the same
 * resolver the SDK URL builders, stream clients, explorer client and embedded
 * wallet read, so connect-src is exactly the set of origins the bundle can
 * contact: no fallback host is listed that the bundle would not use, and no
 * host the bundle uses is missing.
 */
function buildCsp(): string {
  const connectSrc = browserConnectOrigins();
  // `https:` already admits every https image/media host; a plain-http backend (local stack) is listed explicitly.
  const plainHttpImageHosts = browserImageOrigins().filter(origin => origin.startsWith('http:'));
  const walletOrigins = walletParentOrigins();
  const walletList = walletOrigins.length > 0 ? ' ' + walletOrigins.join(' ') : '';

  const directives = [
    "default-src 'self'",
    "script-src 'self' 'unsafe-inline' blob:",
    "style-src 'self' 'unsafe-inline'",
    // Token logos and banners are on the metadata host; remote IPFS / avatar hosts are https.
    ['img-src', "'self'", 'data:', 'blob:', 'https:', ...plainHttpImageHosts].join(' '),
    "font-src 'self' data:",
    ['connect-src', "'self'", ...connectSrc].join(' '),
    "worker-src 'self' blob:",
    `child-src 'self' blob:${walletList}`,
    `frame-src 'self' blob:${walletList}`,
    // Livestream playback and uploaded media are served over https.
    ['media-src', "'self'", 'blob:', 'https:', ...plainHttpImageHosts].join(' '),
    "object-src 'none'",
    "base-uri 'self'",
    `frame-ancestors 'self'${walletList}`,
  ];

  return directives.join('; ');
}

export function middleware(request: NextRequest) {
  const response = NextResponse.next();
  const { pathname } = request.nextUrl;

  // Baseline security headers applied to EVERY route (incl. the charting
  // library branch below). HSTS forces TLS; nosniff blocks MIME-confusion
  // XSS; Referrer-Policy avoids leaking full URLs cross-origin.
  response.headers.set(
    'Strict-Transport-Security',
    'max-age=15552000; includeSubDomains; preload',
  );
  response.headers.set('X-Content-Type-Options', 'nosniff');
  response.headers.set('Referrer-Policy', 'strict-origin-when-cross-origin');

  // Charting library gets a permissive CSP (TradingView requirement)
  if (pathname.startsWith('/charting_library')) {
    response.headers.set(
      'Content-Security-Policy',
      "default-src 'self' 'unsafe-eval' 'unsafe-inline' blob: data:; worker-src 'self' blob:; child-src 'self' blob:",
    );
    return response;
  }

  const csp = buildCsp();
  response.headers.set('Content-Security-Policy', csp);

  // Only set X-Frame-Options if no wallet origin is configured
  // (CSP frame-ancestors supersedes X-Frame-Options per spec)
  if (walletParentOrigins().length === 0) {
    response.headers.set('X-Frame-Options', 'SAMEORIGIN');
  }

  response.headers.set('Permissions-Policy', 'clipboard-write=(self), clipboard-read=(self)');

  return response;
}

export const config = {
  matcher: [
    // Match all paths except static files and Next.js internals
    '/((?!_next/static|_next/image|favicon.ico|icon.svg).*)',
  ],
};
