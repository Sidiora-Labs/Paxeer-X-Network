import { NextRequest, NextResponse } from 'next/server';
import { parseHttpsUrl } from '@/domains/shared';
import {
  ALLOWED_MEDIA_ORIGINS,
  isAllowedMediaContentType,
} from '@/lib/security/media-policy';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
  publicError,
  readBoundedUpstream,
  timeoutSignal,
} from '@/server/http';

const MAX_MEDIA_BYTES = 1_048_576;
export async function GET(request: NextRequest) {
  const parsed = parseHttpsUrl(request.nextUrl.searchParams.get('url'), {
    allowedOrigins: ALLOWED_MEDIA_ORIGINS,
    maxLength: 2048,
  });
  if (!parsed.ok) {
    return publicError(400, 'MEDIA_URL_INVALID', 'Media URL is not allowed');
  }

  const timeout = timeoutSignal(request.signal, 5_000);
  try {
    const upstream = await fetch(parsed.value, {
      method: 'GET',
      headers: { Accept: 'image/avif,image/webp,image/png,image/jpeg,image/gif' },
      cache: 'no-store',
      redirect: 'error',
      signal: timeout.signal,
    });
    if (!upstream.ok) {
      return publicError(502, 'MEDIA_UPSTREAM_FAILED', 'Media is unavailable');
    }
    const contentType = upstream.headers.get('content-type')?.split(';')[0]?.trim();
    if (!contentType || !isAllowedMediaContentType(contentType)) {
      return publicError(415, 'MEDIA_TYPE_REJECTED', 'Media type is not allowed');
    }
    const { body } = await readBoundedUpstream(upstream, {
      maxBytes: MAX_MEDIA_BYTES,
      allowedContentTypes: new Set([contentType]),
    });
    return new NextResponse(body, {
      status: 200,
      headers: {
        'Cache-Control': 'public, max-age=3600, stale-while-revalidate=86400',
        'Content-Length': String(body.byteLength),
        'Content-Type': contentType,
        'Cross-Origin-Resource-Policy': 'same-origin',
        'X-Content-Type-Options': 'nosniff',
      },
    });
  } catch (error: unknown) {
    if (error instanceof HttpBoundaryError) {
      return boundaryErrorResponse(error);
    }
    return publicError(502, 'MEDIA_UPSTREAM_FAILED', 'Media is unavailable');
  } finally {
    timeout.dispose();
  }
}
