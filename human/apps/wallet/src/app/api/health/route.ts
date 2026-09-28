import { NextResponse } from 'next/server';
import { DEFAULT_API_URL, DEFAULT_SUPABASE_URL } from '@/lib/wallet/embedded/client';
import { PAXEER_CONFIG } from '@/lib/constants';
import { captureError, logEvent } from '@/lib/observability';
import { readBoundedUpstream } from '@/server/http';

export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

type CheckStatus = 'ok' | 'degraded';

interface HealthCheck {
  status: CheckStatus;
  latencyMs: number;
}

async function timedCheck(fn: (signal: AbortSignal) => Promise<void>): Promise<HealthCheck> {
  const startedAt = Date.now();
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5_000);
  try {
    await fn(controller.signal);
    return { status: 'ok', latencyMs: Date.now() - startedAt };
  } catch {
    return {
      status: 'degraded',
      latencyMs: Date.now() - startedAt,
    };
  } finally {
    clearTimeout(timer);
  }
}

async function checkRpc(signal: AbortSignal): Promise<void> {
  const res = await fetch(PAXEER_CONFIG.rpcUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'eth_chainId', params: [] }),
    signal,
    cache: 'no-store',
    redirect: 'error',
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const response = await readBoundedUpstream(res, {
    maxBytes: 16_384,
    allowedContentTypes: new Set(['application/json']),
  });
  const body: unknown = JSON.parse(Buffer.from(response.body).toString('utf-8'));
  if (
    typeof body !== 'object' ||
    body === null ||
    !('result' in body) ||
    typeof body.result !== 'string' ||
    !/^0x[0-9a-f]+$/i.test(body.result)
  ) {
    throw new Error('Chain response is invalid');
  }
}

async function checkHttp(url: string, signal: AbortSignal): Promise<void> {
  const res = await fetch(url, {
    method: 'GET',
    signal,
    cache: 'no-store',
    redirect: 'error',
  });
  if (!res.ok && res.status >= 500) throw new Error(`HTTP ${res.status}`);
}

export async function GET(request: Request) {
  const mode = new URL(request.url).searchParams.get('mode') ?? 'readiness';
  if (mode === 'liveness') {
    return NextResponse.json(
      { status: 'ok', check: 'liveness', timestamp: new Date().toISOString() },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  }
  if (mode !== 'readiness') {
    return NextResponse.json(
      {
        error: {
          code: 'HEALTH_MODE_INVALID',
          message: 'Health mode is invalid',
        },
      },
      { status: 400, headers: { 'Cache-Control': 'no-store' } },
    );
  }
  const checks = {
    rpc: await timedCheck((signal) => checkRpc(signal)),
    indexer: await timedCheck((signal) => checkHttp(`${PAXEER_CONFIG.portfolioApiBase}/health`, signal)),
    supabase: await timedCheck((signal) => checkHttp(`${DEFAULT_SUPABASE_URL}/auth/v1/health`, signal)),
    embeddedWallet: await timedCheck((signal) => checkHttp(`${DEFAULT_API_URL}/health`, signal)),
    push: await timedCheck(async () => {
      if (!process.env.NEXT_PUBLIC_VAPID_PUBLIC_KEY || !process.env.VAPID_PRIVATE_KEY) {
        throw new Error('VAPID keys are not configured');
      }
    }),
  };

  const degraded = Object.values(checks).some((check) => check.status !== 'ok');
  logEvent(degraded ? 'warn' : 'info', 'health_check', { degraded });
  if (degraded && process.env.HEALTH_CAPTURE_DEGRADED === 'true') {
    captureError(new Error('Health check degraded'), 'health_check_degraded', {
      checks: JSON.stringify(checks),
    });
  }

  return NextResponse.json(
    {
      status: degraded ? 'degraded' : 'ok',
      check: 'readiness',
      timestamp: new Date().toISOString(),
      checks,
    },
    {
      status: degraded ? 503 : 200,
      headers: { 'Cache-Control': 'no-store' },
    },
  );
}
