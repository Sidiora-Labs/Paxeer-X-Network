import fs from 'node:fs';
import { NextRequest, NextResponse } from 'next/server';
import { createRateLimiter } from '@/lib/rateLimit';
import {
  HttpBoundaryError,
  boundaryErrorResponse,
  encodePathSegments,
  readBoundedUpstream,
  validateQuery,
} from '@/server/http';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

const createSessionLimiter = createRateLimiter({
  limit: 20,
  windowMs: 60_000,
  namespace: 'browser-session-create',
});

function browserPlaneOrigin(): URL {
  const configured = process.env.BROWSER_PLANE_ORIGIN;
  if (!configured) {
    throw new HttpBoundaryError(
      503,
      'BROWSER_UNAVAILABLE',
      'The secure browser service is unavailable',
    );
  }
  let parsed: URL;
  try {
    parsed = new URL(configured);
  } catch {
    throw new HttpBoundaryError(
      503,
      'BROWSER_UNAVAILABLE',
      'The secure browser service is unavailable',
    );
  }
  if (
    !['http:', 'https:'].includes(parsed.protocol) ||
    parsed.username ||
    parsed.password ||
    parsed.pathname !== '/' ||
    parsed.search ||
    parsed.hash
  ) {
    throw new HttpBoundaryError(
      503,
      'BROWSER_UNAVAILABLE',
      'The secure browser service is unavailable',
    );
  }
  return parsed;
}

function internalKey(): string {
  const direct = process.env.BROWSER_PLANE_INTERNAL_KEY;
  const file = process.env.BROWSER_PLANE_INTERNAL_KEY_FILE;
  const value = direct ?? (file ? fs.readFileSync(file, 'utf8').trim() : '');
  if (value.length < 32) {
    throw new HttpBoundaryError(
      503,
      'BROWSER_UNAVAILABLE',
      'The secure browser service is unavailable',
    );
  }
  return value;
}

function requireSameOrigin(request: NextRequest): void {
  const origin = request.headers.get('origin');
  if (!origin) {
    throw new HttpBoundaryError(403, 'ORIGIN_REQUIRED', 'Request origin is required');
  }
  let parsed: URL;
  try {
    parsed = new URL(origin);
  } catch {
    throw new HttpBoundaryError(403, 'ORIGIN_INVALID', 'Request origin is invalid');
  }
  const requestHost = request.headers.get('host')?.trim().toLowerCase();
  const forwardedHost = request.headers
    .get('x-forwarded-host')
    ?.split(',', 1)[0]
    ?.trim()
    .toLowerCase();
  const forwardedProtocol = request.headers
    .get('x-forwarded-proto')
    ?.split(',', 1)[0]
    ?.trim()
    .toLowerCase();
  const hostMatches =
    Boolean(requestHost && parsed.host.toLowerCase() === requestHost) ||
    Boolean(forwardedHost && parsed.host.toLowerCase() === forwardedHost);
  const protocolMatches =
    parsed.protocol === request.nextUrl.protocol ||
    (forwardedProtocol !== undefined && parsed.protocol === `${forwardedProtocol}:`);
  if (!hostMatches || !protocolMatches) {
    throw new HttpBoundaryError(403, 'ORIGIN_INVALID', 'Request origin is invalid');
  }
}

function validateBrowserPath(path: string[]): string {
  const encoded = encodePathSegments(path, {
    maxSegments: 6,
    maxSegmentLength: 64,
  });
  if (
    encoded !== 'v1/sessions' &&
    !/^v1\/sessions\/[0-9a-f-]{36}(?:\/(?:state|events|frame|navigation|input|provider-event|tabs(?:\/[0-9a-f-]{36}(?:\/activate)?)?|rpc\/[0-9a-f-]{36}))?$/.test(
      encoded,
    )
  ) {
    throw new HttpBoundaryError(404, 'BROWSER_ROUTE_INVALID', 'Browser route not found');
  }
  return encoded;
}

async function requestBody(request: NextRequest): Promise<ArrayBuffer | undefined> {
  if (!['POST', 'PUT', 'PATCH'].includes(request.method)) return undefined;
  const declared = request.headers.get('content-length');
  if (declared !== null) {
    const length = Number(declared);
    if (!Number.isSafeInteger(length) || length < 0 || length > 131_072) {
      throw new HttpBoundaryError(
        413,
        'BROWSER_BODY_TOO_LARGE',
        'Browser request exceeds the size limit',
      );
    }
  }
  const body = await request.arrayBuffer();
  if (body.byteLength > 131_072) {
    throw new HttpBoundaryError(
      413,
      'BROWSER_BODY_TOO_LARGE',
      'Browser request exceeds the size limit',
    );
  }
  return body;
}

async function proxy(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
): Promise<Response> {
  validateQuery(request, { maxPairs: 4, maxKeyLength: 32, maxValueLength: 64 });
  if (request.method !== 'GET') requireSameOrigin(request);

  const path = validateBrowserPath((await context.params).path);
  if (request.method === 'POST' && path === 'v1/sessions') {
    const rate = await createSessionLimiter.check('global');
    if (!rate.ok) {
      return NextResponse.json(
        {
          error: {
            code: 'BROWSER_RATE_LIMITED',
            message: 'Too many browser sessions were requested',
          },
        },
        {
          status: 429,
          headers: {
            'Cache-Control': 'no-store',
            'Retry-After': String(rate.retryAfter ?? 60),
          },
        },
      );
    }
  }

  const upstreamUrl = new URL(path, browserPlaneOrigin());
  upstreamUrl.search = request.nextUrl.search;
  const authorization = request.headers.get('authorization');
  const contentType = request.headers.get('content-type');
  const upstream = await fetch(upstreamUrl, {
    method: request.method,
    headers: {
      'x-browser-plane-key': internalKey(),
      ...(authorization ? { Authorization: authorization } : {}),
      ...(contentType ? { 'Content-Type': contentType } : {}),
    },
    body: await requestBody(request),
    cache: 'no-store',
    redirect: 'error',
    signal: request.signal,
  });

  if (upstream.status === 204) {
    return new Response(null, {
      status: 204,
      headers: { 'Cache-Control': 'no-store' },
    });
  }

  const content = await readBoundedUpstream(upstream, {
    maxBytes: upstream.headers.get('content-type')?.startsWith('image/jpeg')
      ? 5 * 1_024 * 1_024
      : 512 * 1_024,
    allowedContentTypes: new Set(['application/json', 'image/jpeg']),
  });
  return new Response(content.body, {
    status: upstream.status,
    headers: {
      'Cache-Control': 'no-store',
      'Content-Type': content.contentType,
      ...(upstream.headers.get('x-frame-version')
        ? { 'X-Frame-Version': upstream.headers.get('x-frame-version')! }
        : {}),
    },
  });
}

export async function GET(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
) {
  try {
    return await proxy(request, context);
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function POST(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
) {
  try {
    return await proxy(request, context);
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function DELETE(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> },
) {
  try {
    return await proxy(request, context);
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}
