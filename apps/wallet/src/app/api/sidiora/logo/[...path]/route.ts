import { NextRequest, NextResponse } from 'next/server';
import { sidioraLimiter } from '@/lib/rateLimit';
import {
    boundaryErrorResponse,
    encodePathSegments,
    HttpBoundaryError,
    readBoundedUpstream,
    timeoutSignal,
    trustedClientIdentity,
    validateQuery,
} from '@/server/http';

// Same-origin image proxy for Sidiora token logos.
// sidiora.fun does not send Access-Control-Allow-Origin headers on image responses,
// so direct browser fetches are blocked by ORB (ERR_BLOCKED_BY_ORB).
// Routing through this proxy makes all logo loads same-origin, eliminating the issue.
//
// URL: /api/sidiora/logo/{address}.png
//   → https://sidiora.fun/api/sdk/metadata/logo/{address}.png

const UPSTREAM_BASE = 'https://sidiora.fun/api/sdk/metadata/logo';

export async function GET(
    request: NextRequest,
    { params }: { params: Promise<{ path: string[] }> },
) {
    const timed = timeoutSignal(request.signal, 10_000);
    try {
        const rl = await sidioraLimiter.check(trustedClientIdentity(request));
        if (!rl.ok) {
            return new NextResponse(null, {
                status: 429,
                headers: {
                    'Retry-After': String(rl.retryAfter ?? 60),
                    'Cache-Control': 'no-store',
                },
            });
        }
        validateQuery(request, { maxPairs: 0 });
        const resolvedParams = await params;
        const filename = encodePathSegments(resolvedParams.path, {
            maxSegments: 2,
            maxSegmentLength: 96,
        });
        const upstream = `${UPSTREAM_BASE}/${filename}`;
        const res = await fetch(upstream, {
            method: 'GET',
            headers: { Accept: 'image/*' },
            signal: timed.signal,
            cache: 'no-store',
            redirect: 'error',
        });

        if (!res.ok) {
            return new NextResponse(null, { status: res.status });
        }

        const { contentType, body } = await readBoundedUpstream(res, {
            maxBytes: 1_048_576,
            allowedContentTypes: new Set([
                'image/gif',
                'image/jpeg',
                'image/png',
                'image/webp',
            ]),
        });

        return new NextResponse(body, {
            status: 200,
            headers: {
                'Content-Type': contentType,
                // Long-lived public cache — token logos are immutable after creation
                'Cache-Control': 'public, max-age=2592000, immutable',
            },
        });
    } catch (err: unknown) {
        if (err instanceof HttpBoundaryError) {
            return boundaryErrorResponse(err);
        }
        return new NextResponse(null, { status: 502 });
    } finally {
        timed.dispose();
    }
}
