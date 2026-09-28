import { NextRequest, NextResponse } from 'next/server';
import { notifyTransactionReceived } from '@/server/push-service';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
  readBoundedJson,
  requirePushAdmin,
} from '@/server/http';

function parseInput(input: unknown) {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new HttpBoundaryError(400, 'NOTIFICATION_INVALID', 'Notification is invalid');
  }
  const body = input as Record<string, unknown>;
  if (
    Object.keys(body).some(
      (key) =>
        !['walletAddress', 'txHash', 'fromAddress', 'value', 'symbol'].includes(key),
    ) ||
    typeof body.walletAddress !== 'string' ||
    !/^0x[0-9a-fA-F]{40}$/.test(body.walletAddress) ||
    typeof body.txHash !== 'string' ||
    !/^0x[0-9a-fA-F]{64}$/.test(body.txHash)
  ) {
    throw new HttpBoundaryError(400, 'NOTIFICATION_INVALID', 'Notification is invalid');
  }
  const optional = (value: unknown, maximum: number): string | undefined => {
    if (value === undefined) return undefined;
    if (typeof value !== 'string' || value.length > maximum) {
      throw new HttpBoundaryError(
        400,
        'NOTIFICATION_INVALID',
        'Notification is invalid',
      );
    }
    return value;
  };
  const fromAddress = optional(body.fromAddress, 42);
  if (fromAddress && !/^0x[0-9a-fA-F]{40}$/.test(fromAddress)) {
    throw new HttpBoundaryError(400, 'NOTIFICATION_INVALID', 'Notification is invalid');
  }
  return {
    walletAddress: body.walletAddress,
    txHash: body.txHash,
    fromAddress,
    value: optional(body.value, 80),
    symbol: optional(body.symbol, 20),
  };
}

export async function POST(request: NextRequest) {
  try {
    requirePushAdmin(request);
    const input = parseInput(await readBoundedJson(request, 8_192));
    const result = await notifyTransactionReceived(
      input.walletAddress,
      input.txHash,
      input.fromAddress,
      input.value,
      input.symbol,
    );
    return NextResponse.json(
      { ok: true, result },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}
