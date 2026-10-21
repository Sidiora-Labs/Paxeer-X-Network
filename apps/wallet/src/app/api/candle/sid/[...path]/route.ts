import { NextRequest } from 'next/server';
import { proxyJsonGet } from '@/server/json-proxy';

const UPSTREAM = 'https://data-api.crossverse.app/api/sid';

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const resolvedParams = await params;
  return proxyJsonGet(request, {
    upstreamOrigin: UPSTREAM,
    path: resolvedParams.path,
    maxSegments: 2,
    cacheControl: 'public, s-maxage=2, stale-while-revalidate=5',
  });
}
