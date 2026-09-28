import { NextRequest, NextResponse } from 'next/server';
import {
  getPushStats,
  sendToAll,
  sendToTag,
  sendToWallet,
  sendToWallets,
  type PushPayload,
} from '@/server/push-service';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
  readBoundedJson,
  requirePushAdmin,
} from '@/server/http';

function boundedString(
  value: unknown,
  name: string,
  maximum: number,
): string {
  if (
    typeof value !== 'string' ||
    value.length < 1 ||
    value.length > maximum
  ) {
    throw new HttpBoundaryError(400, 'PUSH_INVALID', `${name} is invalid`);
  }
  return value;
}

function sameOriginPath(value: unknown, name: string): string | undefined {
  if (value === undefined) return undefined;
  const path = boundedString(value, name, 512);
  if (!path.startsWith('/') || path.startsWith('//')) {
    throw new HttpBoundaryError(400, 'PUSH_INVALID', `${name} is invalid`);
  }
  return path;
}

function parseRequest(input: unknown): {
  payload: PushPayload;
  target:
    | { type: 'all' }
    | { type: 'tag'; tag: string }
    | { type: 'wallet'; address: string }
    | { type: 'wallets'; addresses: string[] };
} {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new HttpBoundaryError(400, 'PUSH_INVALID', 'Push request is invalid');
  }
  const body = input as Record<string, unknown>;
  if (
    Object.keys(body).some((key) => key !== 'payload' && key !== 'target') ||
    typeof body.payload !== 'object' ||
    body.payload === null ||
    Array.isArray(body.payload)
  ) {
    throw new HttpBoundaryError(400, 'PUSH_INVALID', 'Push request is invalid');
  }
  const source = body.payload as Record<string, unknown>;
  const payload: PushPayload = {
    title: boundedString(source.title, 'payload.title', 120),
    body: boundedString(source.body, 'payload.body', 500),
    url: sameOriginPath(source.url, 'payload.url') ?? '/',
    icon: sameOriginPath(source.icon, 'payload.icon'),
    image: sameOriginPath(source.image, 'payload.image'),
    tag:
      source.tag === undefined
        ? undefined
        : boundedString(source.tag, 'payload.tag', 128),
    requireInteraction:
      source.requireInteraction === undefined
        ? undefined
        : source.requireInteraction === true,
  };
  if (body.target === undefined) return { payload, target: { type: 'all' } };
  if (typeof body.target !== 'object' || body.target === null || Array.isArray(body.target)) {
    throw new HttpBoundaryError(400, 'PUSH_TARGET_INVALID', 'Push target is invalid');
  }
  const target = body.target as Record<string, unknown>;
  if (target.type === 'all') return { payload, target: { type: 'all' } };
  if (target.type === 'wallet') {
    const address = boundedString(target.address, 'target.address', 42);
    if (!/^0x[0-9a-fA-F]{40}$/.test(address)) {
      throw new HttpBoundaryError(400, 'PUSH_TARGET_INVALID', 'Push target is invalid');
    }
    return { payload, target: { type: 'wallet', address } };
  }
  if (target.type === 'wallets') {
    if (!Array.isArray(target.addresses) || target.addresses.length > 500) {
      throw new HttpBoundaryError(400, 'PUSH_TARGET_INVALID', 'Push target is invalid');
    }
    const addresses = target.addresses.map((address) => {
      const parsed = boundedString(address, 'target.addresses', 42);
      if (!/^0x[0-9a-fA-F]{40}$/.test(parsed)) {
        throw new HttpBoundaryError(400, 'PUSH_TARGET_INVALID', 'Push target is invalid');
      }
      return parsed;
    });
    return { payload, target: { type: 'wallets', addresses } };
  }
  if (target.type === 'tag') {
    return {
      payload,
      target: { type: 'tag', tag: boundedString(target.tag, 'target.tag', 64) },
    };
  }
  throw new HttpBoundaryError(400, 'PUSH_TARGET_INVALID', 'Push target is invalid');
}

export async function POST(request: NextRequest) {
  try {
    requirePushAdmin(request);
    const { payload, target } = parseRequest(
      await readBoundedJson(request, 65_536),
    );
    const result =
      target.type === 'wallet'
        ? await sendToWallet(target.address, payload)
        : target.type === 'wallets'
          ? await sendToWallets(target.addresses, payload)
          : target.type === 'tag'
            ? await sendToTag(target.tag, payload)
            : await sendToAll(payload);
    return NextResponse.json(
      { ok: true, result },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function GET(request: NextRequest) {
  try {
    requirePushAdmin(request);
    return NextResponse.json(await getPushStats(), {
      headers: { 'Cache-Control': 'no-store' },
    });
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}
