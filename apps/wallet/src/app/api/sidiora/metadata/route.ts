import { NextRequest } from 'next/server';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
} from '@/server/http';
import { proxyJsonGet } from '@/server/json-proxy';

export async function GET(request: NextRequest) {
  try {
    const raw = request.nextUrl.searchParams.get('addresses');
    const addresses = raw?.split(',') ?? [];
    if (
      addresses.length < 1 ||
      addresses.length > 100 ||
      addresses.some((address) => !/^0x[0-9a-fA-F]{40}$/.test(address))
    ) {
      throw new HttpBoundaryError(
        400,
        'ADDRESS_INVALID',
        'One or more addresses are invalid',
      );
    }
    const normalizedRequestUrl = new URL(request.url);
    normalizedRequestUrl.searchParams.set(
      'addresses',
      addresses.map((address) => address.toLowerCase()).join(','),
    );
    const upstreamRequest = new NextRequest(normalizedRequestUrl, {
      method: 'GET',
      headers: request.headers,
      signal: request.signal,
    });
    return proxyJsonGet(upstreamRequest, {
      upstreamOrigin: 'https://sidiora.fun/api/sdk/metadata/metadata',
      path: ['batch'],
      maxSegments: 1,
      maxQueryPairs: 1,
      maxQueryValueLength: 4_299,
      cacheControl: 'public, s-maxage=86400, stale-while-revalidate=3600',
    });
  } catch (error: unknown) {
    return boundaryErrorResponse(error);
  }
}
