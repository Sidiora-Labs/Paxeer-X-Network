import { NextRequest, NextResponse } from 'next/server';
import {
  parseStoredSubscription,
  removeSubscription,
  upsertSubscription,
} from '@/server/push-store';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
  readBoundedJson,
} from '@/server/http';

function record(input: unknown): Record<string, unknown> {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new HttpBoundaryError(
      400,
      'SUBSCRIPTION_INVALID',
      'Push subscription is invalid',
    );
  }
  return input as Record<string, unknown>;
}

export async function POST(request: NextRequest) {
  try {
    const body = record(await readBoundedJson(request, 8_192));
    const keys = record(body.keys);
    const subscription = parseStoredSubscription({
      endpoint: body.endpoint,
      keys: { p256dh: keys.p256dh, auth: keys.auth },
      walletAddress: body.walletAddress,
      userAgent: request.headers.get('user-agent') ?? undefined,
      createdAt: Date.now(),
      lastActiveAt: Date.now(),
    });
    await upsertSubscription(subscription);
    return NextResponse.json(
      { ok: true },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function DELETE(request: NextRequest) {
  try {
    const body = record(await readBoundedJson(request, 4_096));
    if (
      Object.keys(body).length !== 1 ||
      typeof body.endpoint !== 'string' ||
      body.endpoint.length < 12 ||
      body.endpoint.length > 2_048
    ) {
      throw new HttpBoundaryError(
        400,
        'SUBSCRIPTION_INVALID',
        'Push subscription is invalid',
      );
    }
    const endpoint = new URL(body.endpoint);
    if (endpoint.protocol !== 'https:') {
      throw new HttpBoundaryError(
        400,
        'SUBSCRIPTION_INVALID',
        'Push subscription is invalid',
      );
    }
    const removed = await removeSubscription(endpoint.toString());
    return NextResponse.json(
      { ok: true, removed },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}
