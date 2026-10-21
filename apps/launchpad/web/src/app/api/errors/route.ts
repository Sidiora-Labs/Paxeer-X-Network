import { NextRequest, NextResponse } from 'next/server';
import * as Sentry from '@sentry/nextjs';

/**
 * Client error sink.
 *
 * `src/core/report-error.ts` falls back to `navigator.sendBeacon('/api/errors', ...)`
 * when no client-side Sentry DSN is configured (or the Sentry import fails).
 * Without this route those beacons 404 silently and all such telemetry is lost.
 *
 * This handler validates the beacon payload, forwards it to Sentry on the
 * server (so it is captured even when the browser SDK is absent), and always
 * responds 204 — error reporting must never surface its own error to the user.
 */

interface ErrorBeacon {
  message: string;
  stack?: string;
  context?: Record<string, unknown>;
  url?: string;
  timestamp?: number;
}

const MAX_BODY_BYTES = 64 * 1024; // hard cap; reject oversized payloads

function isErrorBeacon(value: unknown): value is ErrorBeacon {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  if (typeof v.message !== 'string' || v.message.length === 0) return false;
  if (v.stack !== undefined && typeof v.stack !== 'string') return false;
  if (v.url !== undefined && typeof v.url !== 'string') return false;
  if (v.timestamp !== undefined && typeof v.timestamp !== 'number') return false;
  if (
    v.context !== undefined &&
    (typeof v.context !== 'object' || v.context === null || Array.isArray(v.context))
  ) {
    return false;
  }
  return true;
}

export async function POST(request: NextRequest): Promise<NextResponse> {
  try {
    const raw = await request.text();
    if (raw.length > MAX_BODY_BYTES) {
      return new NextResponse(null, { status: 413 });
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return new NextResponse(null, { status: 400 });
    }

    if (!isErrorBeacon(parsed)) {
      return new NextResponse(null, { status: 400 });
    }

    const { message, stack, context, url, timestamp } = parsed;

    // Reconstruct a real Error so Sentry gets a stack-bearing exception.
    const error = new Error(message);
    if (stack) error.stack = stack;

    Sentry.captureException(error, {
      level: 'error',
      tags: { source: 'client-beacon' },
      extra: {
        ...(context ?? {}),
        clientUrl: url,
        clientTimestamp: timestamp,
        userAgent: request.headers.get('user-agent') ?? undefined,
      },
    });

    // Always log server-side too, so the signal survives even with no DSN.
    console.error('[client-error]', message, { url, context });

    return new NextResponse(null, { status: 204 });
  } catch (err) {
    // Never let the error sink itself throw a 500 back at the beacon.
    console.error('[api/errors] failed to process beacon:', err);
    return new NextResponse(null, { status: 204 });
  }
}
