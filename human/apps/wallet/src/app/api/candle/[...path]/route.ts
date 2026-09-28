import { NextRequest } from 'next/server';
import { proxyJsonGet } from '@/server/json-proxy';

const UPSTREAM = 'https://app.hyperpaxeer.com/api/candle';

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> },
) {
  const resolvedParams = await params;
  return proxyJsonGet(request, {
    upstreamOrigin: UPSTREAM,
    path: resolvedParams.path,
    cacheControl: 'public, s-maxage=2, stale-while-revalidate=5',
  });
}
