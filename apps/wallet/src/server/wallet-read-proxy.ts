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

export type WalletReadRoute =
  | 'pns-owned'
  | 'pns-domain'
  | 'pns-events'
  | 'pns-address'
  | 'pns-lookup'
  | 'points-balance'
  | 'fx-usd';

interface ReadRoute {
  origin: string;
  path: string;
  parameter?: 'name' | 'address';
  query: readonly string[];
}

const PNS_ORIGIN = 'https://paxeer-name-service-production.up.railway.app';

const ROUTES: Record<WalletReadRoute, ReadRoute> = {
  'pns-owned': {
    origin: PNS_ORIGIN,
    path: '/api/v1/addresses:lookup',
    query: ['address', 'owned_by', 'only_active', 'sort', 'order'],
  },
  'pns-domain': {
    origin: PNS_ORIGIN,
    path: '/api/v1/domains/{name}',
    parameter: 'name',
    query: [],
  },
  'pns-events': {
    origin: PNS_ORIGIN,
    path: '/api/v1/domains/{name}/events',
    parameter: 'name',
    query: ['order'],
  },
  'pns-address': {
    origin: PNS_ORIGIN,
    path: '/api/v1/addresses/{address}',
    parameter: 'address',
    query: [],
  },
  'pns-lookup': {
    origin: PNS_ORIGIN,
    path: '/api/v1/domains:lookup',
    query: ['name', 'only_active'],
  },
  'points-balance': {
    origin: 'https://sidiora-points-indexer-production.up.railway.app',
    path: '/points/balance/{address}',
    parameter: 'address',
    query: [],
  },
  'fx-usd': {
    origin: 'https://open.er-api.com',
    path: '/v6/latest/USD',
    query: [],
  },
};

export function walletReadRequest(
  request: NextRequest,
  route: WalletReadRoute,
  parameters: Readonly<Record<string, string>> = {},
  signal: AbortSignal = request.signal,
): Request {
  if (request.method !== 'GET') {
    throw new HttpBoundaryError(405, 'METHOD_NOT_ALLOWED', 'Method is not allowed');
  }
  if (!Object.hasOwn(ROUTES, route)) {
    throw new HttpBoundaryError(400, 'ROUTE_INVALID', 'Read route is invalid');
  }
  const definition = ROUTES[route];
  const keys = Object.keys(parameters);
  let path = definition.path;
  if (definition.parameter) {
    if (keys.length !== 1 || keys[0] !== definition.parameter) {
      throw new HttpBoundaryError(400, 'PATH_INVALID', 'Path parameters are invalid');
    }
    const segment = encodePathSegments([parameters[definition.parameter]], {
      maxSegments: 1,
      maxSegmentLength: 96,
    });
    path = path.replace(`{${definition.parameter}}`, segment);
  } else if (keys.length !== 0) {
    throw new HttpBoundaryError(400, 'PATH_INVALID', 'Path parameters are invalid');
  }
  validateQuery(request, {
    maxPairs: definition.query.length,
    maxKeyLength: 64,
    maxValueLength: 256,
  });
  const upstream = new URL(path, definition.origin);
  const seen = new Set<string>();
  for (const [key, value] of request.nextUrl.searchParams) {
    if (!definition.query.includes(key) || seen.has(key)) {
      throw new HttpBoundaryError(400, 'QUERY_INVALID', 'Query is invalid');
    }
    seen.add(key);
    upstream.searchParams.append(key, value);
  }
  return new Request(upstream, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    signal,
    cache: 'no-store',
    redirect: 'error',
    credentials: 'omit',
  });
}

export async function walletReadResponse(response: Response): Promise<NextResponse> {
  const { body } = await readBoundedUpstream(response, {
    maxBytes: 1_048_576,
    allowedContentTypes: new Set(['application/json']),
  });
  return new NextResponse(body, {
    status: response.status,
    headers: {
      'Content-Type': 'application/json; charset=utf-8',
      'Cache-Control': 'no-store',
    },
  });
}

export async function proxyWalletRead(
  request: NextRequest,
  route: WalletReadRoute,
  parameters: Readonly<Record<string, string>> = {},
): Promise<NextResponse> {
  const timed = timeoutSignal(request.signal, 10_000);
  try {
    const upstream = walletReadRequest(request, route, parameters, timed.signal);
    return await walletReadResponse(await fetch(upstream));
  } catch (error: unknown) {
    if (error instanceof HttpBoundaryError) return boundaryErrorResponse(error);
    if (timed.signal.aborted || (error instanceof Error && error.name === 'AbortError')) {
      return publicError(504, 'UPSTREAM_TIMEOUT', 'Upstream request timed out');
    }
    return publicError(502, 'UPSTREAM_UNAVAILABLE', 'Upstream request failed');
  } finally {
    timed.dispose();
  }
}
