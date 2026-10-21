import { NextRequest, NextResponse } from 'next/server';
import { sdkLimiter } from '@/lib/rateLimit';
import {
    boundaryErrorResponse,
    encodePathSegments,
    HttpBoundaryError,
    publicError,
    readBoundedUpstream,
    timeoutSignal,
    trustedClientIdentity,
    validateQuery,
} from '@/server/http';

// Server-side proxy for the Sidiora SDK API (rankings, stats, metadata, candles).
// Upstream URL never ships in the client bundle.
const UPSTREAM = (process.env.SIDIORA_SDK_UPSTREAM || 'https://sidiora.fun/api/sdk').replace(/\/+$/, '');

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

export async function GET(
    request: NextRequest,
    { params }: { params: Promise<{ path: string[] }> },
) {
    const timed = timeoutSignal(request.signal, 15_000);

    try {
        const rl = await sdkLimiter.check(trustedClientIdentity(request));
        if (!rl.ok) {
            return publicError(429, 'RATE_LIMITED', 'Request limit exceeded');
        }
        const resolvedParams = await params;
        const segments = encodePathSegments(resolvedParams.path);
        validateQuery(request, { maxPairs: 24, maxValueLength: 256 });
        const upstream = `${UPSTREAM}/${segments}${request.nextUrl.search}`;
        const upstreamRes = await fetch(upstream, {
            method: 'GET',
            headers: { Accept: 'application/json' },
            signal: timed.signal,
            cache: 'no-store',
            redirect: 'error',
        });

        const { contentType, body } = await readBoundedUpstream(upstreamRes, {
            maxBytes: 1_048_576,
            allowedContentTypes: new Set(['application/json']),
        });

        return new NextResponse(body, {
            status: upstreamRes.status,
            headers: {
                'Content-Type': contentType,
                'Cache-Control': 'private, max-age=5, stale-while-revalidate=15',
            },
        });
    } catch (err: unknown) {
        if (err instanceof HttpBoundaryError) {
            return boundaryErrorResponse(err);
        }
        const aborted = err instanceof Error && err.name === 'AbortError';
        return publicError(
            aborted ? 504 : 502,
            aborted ? 'UPSTREAM_TIMEOUT' : 'UPSTREAM_UNAVAILABLE',
            aborted ? 'Upstream request timed out' : 'Upstream request failed',
        );
    } finally {
        timed.dispose();
    }
}
