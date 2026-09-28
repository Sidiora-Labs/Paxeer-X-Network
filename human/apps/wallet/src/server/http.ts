import { createHash, timingSafeEqual } from 'node:crypto';
import { NextRequest, NextResponse } from 'next/server';

export class HttpBoundaryError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly publicMessage: string,
  ) {
    super(publicMessage);
    this.name = 'HttpBoundaryError';
  }
}

export function publicError(
  status: number,
  code: string,
  message: string,
): NextResponse {
  return NextResponse.json(
    { error: { code, message } },
    {
      status,
      headers: {
        'Cache-Control': 'no-store',
        'Content-Type': 'application/json; charset=utf-8',
      },
    },
  );
}

export function boundaryErrorResponse(error: unknown): NextResponse {
  if (error instanceof HttpBoundaryError) {
    return publicError(error.status, error.code, error.publicMessage);
  }
  return publicError(500, 'INTERNAL_ERROR', 'The request could not be completed');
}

export async function readBoundedJson(
  request: NextRequest,
  maxBytes: number,
): Promise<unknown> {
  const contentType = request.headers.get('content-type')?.split(';')[0]?.trim();
  if (contentType !== 'application/json') {
    throw new HttpBoundaryError(
      415,
      'CONTENT_TYPE_INVALID',
      'Content-Type must be application/json',
    );
  }
  const declared = request.headers.get('content-length');
  if (declared !== null) {
    const length = Number(declared);
    if (!Number.isSafeInteger(length) || length < 0 || length > maxBytes) {
      throw new HttpBoundaryError(
        413,
        'BODY_TOO_LARGE',
        'Request body exceeds the size limit',
      );
    }
  }
  const bytes = await readBoundedStream(request.body, maxBytes, {
    status: 413,
    code: 'BODY_TOO_LARGE',
    message: 'Request body exceeds the size limit',
  });
  try {
    const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    return JSON.parse(text) as unknown;
  } catch {
    throw new HttpBoundaryError(
      400,
      'JSON_INVALID',
      'Request body is not valid JSON',
    );
  }
}

function constantTimeEqual(left: string, right: string): boolean {
  const leftDigest = createHash('sha256').update(left).digest();
  const rightDigest = createHash('sha256').update(right).digest();
  return timingSafeEqual(leftDigest, rightDigest);
}

function bearerCredential(request: NextRequest): string | null {
  const authorization = request.headers.get('authorization');
  if (!authorization) return null;
  const match = /^Bearer ([A-Za-z0-9._~+/-]{16,512})$/.exec(authorization);
  return match?.[1] ?? null;
}

export function requirePushAdmin(request: NextRequest): void {
  const configured = process.env.PUSH_ADMIN_KEY;
  if (!configured || configured.length < 32) {
    throw new HttpBoundaryError(
      503,
      'ADMIN_UNAVAILABLE',
      'Administrative access is unavailable',
    );
  }
  const headerCredential = request.headers.get('x-push-admin-key');
  const bearer = bearerCredential(request);
  if (
    (headerCredential && bearer) ||
    (!headerCredential && !bearer) ||
    !constantTimeEqual(headerCredential ?? bearer ?? '', configured)
  ) {
    throw new HttpBoundaryError(
      401,
      'ADMIN_UNAUTHORIZED',
      'Administrative authorization failed',
    );
  }
}

export function trustedClientIdentity(request: NextRequest): string {
  const proxySecret = process.env.TRUSTED_PROXY_SECRET;
  const suppliedSecret = request.headers.get('x-paxport-proxy-secret');
  const clientId = request.headers.get('x-paxport-client-id');
  if (
    proxySecret &&
    proxySecret.length >= 32 &&
    suppliedSecret &&
    clientId &&
    /^[A-Za-z0-9._:-]{8,128}$/.test(clientId) &&
    constantTimeEqual(suppliedSecret, proxySecret)
  ) {
    return `proxy:${clientId}`;
  }
  return 'anonymous';
}

export function timeoutSignal(
  parent: AbortSignal,
  timeoutMs: number,
): { signal: AbortSignal; dispose: () => void } {
  const controller = new AbortController();
  const abort = () => controller.abort(parent.reason);
  if (parent.aborted) abort();
  else parent.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  return {
    signal: controller.signal,
    dispose: () => {
      clearTimeout(timer);
      parent.removeEventListener('abort', abort);
    },
  };
}

export function encodePathSegments(
  input: unknown,
  options: { maxSegments?: number; maxSegmentLength?: number } = {},
): string {
  const maxSegments = options.maxSegments ?? 12;
  const maxSegmentLength = options.maxSegmentLength ?? 128;
  if (!Array.isArray(input) || input.length === 0 || input.length > maxSegments) {
    throw new HttpBoundaryError(400, 'PATH_INVALID', 'Request path is invalid');
  }
  return input
    .map((segment) => {
      if (
        typeof segment !== 'string' ||
        segment.length < 1 ||
        segment.length > maxSegmentLength ||
        segment === '.' ||
        segment === '..' ||
        !/^[A-Za-z0-9._~:@+-]+$/.test(segment)
      ) {
        throw new HttpBoundaryError(
          400,
          'PATH_INVALID',
          'Request path is invalid',
        );
      }
      return encodeURIComponent(segment);
    })
    .join('/');
}

export function validateQuery(
  request: NextRequest,
  options: {
    maxPairs?: number;
    maxKeyLength?: number;
    maxValueLength?: number;
  } = {},
): void {
  const pairs = [...request.nextUrl.searchParams.entries()];
  if (pairs.length > (options.maxPairs ?? 32)) {
    throw new HttpBoundaryError(400, 'QUERY_INVALID', 'Query is too large');
  }
  for (const [key, value] of pairs) {
    if (
      key.length < 1 ||
      key.length > (options.maxKeyLength ?? 64) ||
      value.length > (options.maxValueLength ?? 512)
    ) {
      throw new HttpBoundaryError(400, 'QUERY_INVALID', 'Query is invalid');
    }
  }
}

export async function readBoundedUpstream(
  response: Response,
  options: {
    maxBytes: number;
    allowedContentTypes: ReadonlySet<string>;
  },
): Promise<{ body: ArrayBuffer; contentType: string }> {
  const contentType = response.headers
    .get('content-type')
    ?.split(';')[0]
    ?.trim()
    .toLowerCase();
  if (!contentType || !options.allowedContentTypes.has(contentType)) {
    throw new HttpBoundaryError(
      502,
      'UPSTREAM_TYPE_INVALID',
      'Upstream returned an unsupported response',
    );
  }
  const rawLength = response.headers.get('content-length');
  if (rawLength !== null) {
    const declared = Number(rawLength);
    if (
      !Number.isSafeInteger(declared) ||
      declared < 0 ||
      declared > options.maxBytes
    ) {
      throw new HttpBoundaryError(
        502,
        'UPSTREAM_TOO_LARGE',
        'Upstream response exceeds the size limit',
      );
    }
  }
  const bytes = await readBoundedStream(response.body, options.maxBytes, {
    status: 502,
    code: 'UPSTREAM_TOO_LARGE',
    message: 'Upstream response exceeds the size limit',
  });
  return {
    body: bytes.buffer.slice(
      bytes.byteOffset,
      bytes.byteOffset + bytes.byteLength,
    ) as ArrayBuffer,
    contentType,
  };
}

async function readBoundedStream(
  stream: ReadableStream<Uint8Array> | null,
  maxBytes: number,
  error: { status: number; code: string; message: string },
): Promise<Uint8Array> {
  if (!stream) return new Uint8Array();
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      total += next.value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        throw new HttpBoundaryError(error.status, error.code, error.message);
      }
      chunks.push(next.value);
    }
  } finally {
    reader.releaseLock();
  }
  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body;
}

export function limitReadableStream(
  source: ReadableStream<Uint8Array>,
  maxBytes: number,
): ReadableStream<Uint8Array> {
  const reader = source.getReader();
  let total = 0;
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      const next = await reader.read();
      if (next.done) {
        controller.close();
        reader.releaseLock();
        return;
      }
      total += next.value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        reader.releaseLock();
        controller.error(new Error('Response stream exceeded the size limit'));
        return;
      }
      controller.enqueue(next.value);
    },
    async cancel(reason) {
      await reader.cancel(reason);
      reader.releaseLock();
    },
  });
}
