import * as Sentry from '@sentry/nextjs';
import type { NextRequest } from 'next/server';
import type {
  AppFailure,
  CorrelationId,
  FailureDomain,
  FailureKind,
  RetryPolicy,
} from '@/domains/shared';

type LogLevel = 'info' | 'warn' | 'error';
type TelemetryPrimitive = string | number | boolean | null;
type TelemetryValue =
  | TelemetryPrimitive
  | TelemetryValue[]
  | { [key: string]: TelemetryValue };
type LogFields = Record<string, unknown>;

export interface SafeException {
  name: string;
  message: string;
  stack?: string;
}

const SENSITIVE_KEY_PARTS = [
  'address',
  'approval',
  'auth',
  'cookie',
  'credential',
  'email',
  'endpoint',
  'key',
  'mnemonic',
  'password',
  'payload',
  'phone',
  'pin',
  'secret',
  'seed',
  'session',
  'signature',
  'token',
];

function isSensitiveKey(key: string): boolean {
  const normalized = key.toLowerCase().replace(/[^a-z0-9]/g, '');
  return SENSITIVE_KEY_PARTS.some((part) => normalized.includes(part));
}

function sanitizeString(input: string): string {
  let value = input.slice(0, 1_024);
  value = value.replace(/\b(?:Bearer|Basic)\s+[A-Za-z0-9._~+/-]+=*/gi, '[redacted]');
  value = value.replace(/\b0x[0-9a-f]{64}\b/gi, '[redacted-private-value]');
  value = value.replace(/\b0x([0-9a-f]{4})[0-9a-f]{32}([0-9a-f]{4})\b/gi, '0x$1…$2');
  value = value.replace(
    /\b(password|passphrase|mnemonic|private[_ -]?key|secret|token|signature)=([^&\s]+)/gi,
    '$1=[redacted]',
  );
  if (/^(?:[a-z]+\s+){11,23}[a-z]+$/i.test(value.trim())) {
    return '[redacted-recovery-phrase]';
  }
  try {
    const url = new URL(value);
    if (url.protocol === 'http:' || url.protocol === 'https:') {
      url.username = '';
      url.password = '';
      url.search = '';
      url.hash = '';
      value = url.toString();
    }
  } catch {
    // Non-URL strings continue through ordinary sanitization.
  }
  return value.slice(0, 512);
}

export function sanitizeTelemetry(
  input: unknown,
  depth = 0,
  seen = new WeakSet<object>(),
): TelemetryValue {
  if (depth > 6) return '[truncated]';
  if (input === null) return null;
  if (typeof input === 'string') return sanitizeString(input);
  if (typeof input === 'number') return Number.isFinite(input) ? input : 0;
  if (typeof input === 'boolean') return input;
  if (typeof input === 'bigint') return input.toString();
  if (typeof input === 'undefined') return '[undefined]';
  if (typeof input === 'function' || typeof input === 'symbol') {
    return '[unsupported]';
  }
  if (input instanceof Error) {
    const safe = safeException(input);
    return {
      name: safe.name,
      message: safe.message,
      ...(safe.stack ? { stack: safe.stack } : {}),
    };
  }
  if (typeof input !== 'object') return sanitizeString(String(input));
  if (seen.has(input)) return '[circular]';
  seen.add(input);
  if (Array.isArray(input)) {
    return input.slice(0, 50).map((value) =>
      sanitizeTelemetry(value, depth + 1, seen),
    );
  }
  return Object.fromEntries(
    Object.entries(input)
      .slice(0, 100)
      .map(([key, value]) => [
        key.slice(0, 80),
        isSensitiveKey(key)
          ? '[redacted]'
          : sanitizeTelemetry(value, depth + 1, seen),
      ]),
  );
}

export function safeException(error: unknown): SafeException {
  const source =
    error instanceof Error
      ? error
      : new Error(typeof error === 'string' ? error : 'Unknown failure');
  const message = sanitizeString(source.message || 'Unknown failure');
  const stack = source.stack
    ?.split('\n')
    .slice(0, 8)
    .map((line) =>
      sanitizeString(line).replace(
        /(?:file:\/\/)?(?:\/[A-Za-z0-9._-]+){2,}/g,
        '[path]',
      ),
    )
    .join('\n');
  return {
    name: sanitizeString(source.name || 'Error').slice(0, 80),
    message,
    stack,
  };
}

export function correlationId(): CorrelationId {
  return (globalThis.crypto?.randomUUID?.() ??
    `corr-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`) as CorrelationId;
}

export function toAppFailure(
  error: unknown,
  options: {
    domain?: FailureDomain;
    kind?: FailureKind;
    code?: string;
    severity?: AppFailure['severity'];
    retry?: RetryPolicy;
    publicMessageKey?: string;
    userAction?: string;
    correlation?: CorrelationId;
  } = {},
): AppFailure {
  const safe = safeException(error);
  const kind = options.kind ?? 'programmer';
  return {
    domain: options.domain ?? 'unknown',
    kind,
    code: options.code ?? 'UNEXPECTED_FAILURE',
    severity: options.severity ?? (kind === 'cancelled' ? 'info' : 'error'),
    retry:
      options.retry ??
      (kind === 'offline' || kind === 'timeout' || kind === 'unavailable'
        ? { kind: 'manual' }
        : { kind: 'never' }),
    publicMessageKey: options.publicMessageKey ?? 'errors.generic',
    correlationId: options.correlation ?? correlationId(),
    userAction: options.userAction,
    cause: safe,
  };
}

export function logEvent(
  level: LogLevel,
  event: string,
  fields: LogFields = {},
): void {
  const sanitized = sanitizeTelemetry(fields);
  const payload = {
    level,
    event: sanitizeString(event).slice(0, 100),
    timestamp: new Date().toISOString(),
    ...(typeof sanitized === 'object' &&
    sanitized !== null &&
    !Array.isArray(sanitized)
      ? sanitized
      : {}),
  };
  const line = JSON.stringify(payload);
  if (level === 'error') console.error(line);
  else if (level === 'warn') console.warn(line);
  else console.info(line);
}

export function captureError(
  error: unknown,
  context: string,
  fields: LogFields = {},
): AppFailure {
  const failure = toAppFailure(error, {
    code: context.toUpperCase().replace(/[^A-Z0-9]+/g, '_').slice(0, 80),
  });
  const exception = failure.cause as SafeException;
  logEvent('error', context, {
    ...fields,
    correlationId: failure.correlationId,
    exception,
  });
  const sanitizedError = new Error(exception.message);
  sanitizedError.name = exception.name;
  sanitizedError.stack = exception.stack;
  const extra = sanitizeTelemetry({
    ...fields,
    correlationId: failure.correlationId,
  });
  Sentry.captureException(sanitizedError, {
    tags: {
      context: sanitizeString(context).slice(0, 100),
      correlationId: failure.correlationId,
    },
    extra:
      typeof extra === 'object' && extra !== null && !Array.isArray(extra)
        ? extra
        : {},
  });
  return failure;
}

export function logApiRequest(
  req: NextRequest,
  route: string,
  status: number,
  startedAt: number,
  fields: LogFields = {},
): void {
  logEvent(
    status >= 500 ? 'error' : status >= 400 ? 'warn' : 'info',
    'api_request',
    {
      route,
      method: req.method,
      status,
      durationMs: Math.max(0, Date.now() - startedAt),
      ...fields,
    },
  );
}
