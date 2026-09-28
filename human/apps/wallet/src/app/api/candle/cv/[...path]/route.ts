import { NextRequest } from 'next/server';
import { proxyJsonGet } from '@/server/json-proxy';

const UPSTREAM_BASE = 'https://data-api.crossverse.app/api';

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const resolvedParams = await params;
  return proxyJsonGet(request, {
    upstreamOrigin: UPSTREAM_BASE,
    path: resolvedParams.path,
    maxSegments: 3,
    cacheControl: 'public, s-maxage=2, stale-while-revalidate=5',
  });
}
