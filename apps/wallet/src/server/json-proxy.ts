import { NextRequest, NextResponse } from 'next/server';
import {
  boundaryErrorResponse,
  encodePathSegments,
  HttpBoundaryError,
  publicError,
  readBoundedUpstream,
  timeoutSignal,
  validateQuery,
} from './http';

interface JsonProxyOptions {
  upstreamOrigin: string;
  path: unknown;
  maxSegments?: number;
  cacheControl: string;
  maxQueryPairs?: number;
  maxQueryValueLength?: number;
}

export async function proxyJsonGet(
  request: NextRequest,
  options: JsonProxyOptions,
): Promise<NextResponse> {
  const timed = timeoutSignal(request.signal, 10_000);
  try {
    const path = encodePathSegments(options.path, {
      maxSegments: options.maxSegments ?? 4,
      maxSegmentLength: 96,
    });
    validateQuery(request, {
      maxPairs: options.maxQueryPairs ?? 24,
      maxValueLength: options.maxQueryValueLength ?? 256,
    });
    const upstream = new URL(`${options.upstreamOrigin.replace(/\/+$/, '')}/${path}`);
    request.nextUrl.searchParams.forEach((value, key) => {
      upstream.searchParams.append(key, value);
    });
    const response = await fetch(upstream, {
      method: 'GET',
      headers: { Accept: 'application/json' },
      signal: timed.signal,
      cache: 'no-store',
      redirect: 'error',
    });
    const { body } = await readBoundedUpstream(response, {
      maxBytes: 1_048_576,
      allowedContentTypes: new Set(['application/json']),
    });
    return new NextResponse(body, {
      status: response.status,
      headers: {
        'Content-Type': 'application/json; charset=utf-8',
        'Cache-Control': options.cacheControl,
      },
    });
  } catch (error: unknown) {
    if (error instanceof HttpBoundaryError) {
      return boundaryErrorResponse(error);
    }
    if (error instanceof Error && error.name === 'AbortError') {
      return publicError(504, 'UPSTREAM_TIMEOUT', 'Upstream request timed out');
    }
    return publicError(502, 'UPSTREAM_UNAVAILABLE', 'Upstream request failed');
  } finally {
    timed.dispose();
  }
}
