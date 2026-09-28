import { createHash } from 'node:crypto';
import { AtomicJsonStore } from '@/server/atomic-json-store';

interface RateLimitEntry {
  count: number;
  resetAt: number;
}

interface RateLimitState {
  version: 1;
  buckets: Record<string, RateLimitEntry>;
}

export interface RateLimitResult {
  ok: boolean;
  retryAfter?: number;
  remaining: number;
}

interface RateLimiterOptions {
  limit?: number;
  windowMs?: number;
  namespace?: string;
}

function parseState(input: unknown): RateLimitState {
  if (
    typeof input !== 'object' ||
    input === null ||
    Array.isArray(input) ||
    (input as { version?: unknown }).version !== 1
  ) {
    throw new TypeError('Rate-limit store is invalid');
  }
  const rawBuckets = (input as { buckets?: unknown }).buckets;
  if (
    typeof rawBuckets !== 'object' ||
    rawBuckets === null ||
    Array.isArray(rawBuckets)
  ) {
    throw new TypeError('Rate-limit buckets are invalid');
  }
  const buckets: Record<string, RateLimitEntry> = {};
  for (const [key, value] of Object.entries(rawBuckets)) {
    if (
      !/^[a-f0-9]{64}$/.test(key) ||
      typeof value !== 'object' ||
      value === null ||
      !Number.isSafeInteger((value as RateLimitEntry).count) ||
      !Number.isSafeInteger((value as RateLimitEntry).resetAt) ||
      (value as RateLimitEntry).count < 0 ||
      (value as RateLimitEntry).resetAt < 0
    ) {
      throw new TypeError('Rate-limit entry is invalid');
    }
    buckets[key] = {
      count: (value as RateLimitEntry).count,
      resetAt: (value as RateLimitEntry).resetAt,
    };
  }
  return { version: 1, buckets };
}

const store = new AtomicJsonStore<RateLimitState>({
  filePath:
    process.env.RATE_LIMIT_STORE_PATH ??
    '/data/paxport/rate-limits.json',
  empty: () => ({ version: 1, buckets: {} }),
  parse: parseState,
  maxBytes: 2_097_152,
});

function bucketKey(namespace: string, identity: string): string {
  return createHash('sha256')
    .update(namespace)
    .update('\0')
    .update(identity)
    .digest('hex');
}

export function createRateLimiter({
  limit = 120,
  windowMs = 60_000,
  namespace = `limit-${limit}-${windowMs}`,
}: RateLimiterOptions = {}) {
  if (!Number.isSafeInteger(limit) || limit < 1) {
    throw new TypeError('Rate limit must be a positive integer');
  }
  if (!Number.isSafeInteger(windowMs) || windowMs < 1_000) {
    throw new TypeError('Rate-limit window must be at least one second');
  }

  return {
    async check(identity: string): Promise<RateLimitResult> {
      const now = Date.now();
      const key = bucketKey(namespace, identity);
      let result: RateLimitResult = { ok: false, remaining: 0 };
      await store.update((current) => {
        const buckets = Object.fromEntries(
          Object.entries(current.buckets)
            .filter(([, entry]) => entry.resetAt > now)
            .slice(-10_000),
        );
        const entry = buckets[key];
        if (!entry) {
          buckets[key] = { count: 1, resetAt: now + windowMs };
          result = { ok: true, remaining: limit - 1 };
        } else if (entry.count >= limit) {
          result = {
            ok: false,
            retryAfter: Math.max(1, Math.ceil((entry.resetAt - now) / 1000)),
            remaining: 0,
          };
        } else {
          buckets[key] = { ...entry, count: entry.count + 1 };
          result = { ok: true, remaining: limit - buckets[key].count };
        }
        return { version: 1, buckets };
      });
      return result;
    },
  };
}

export const sdkLimiter = createRateLimiter({
  limit: 120,
  windowMs: 60_000,
  namespace: 'sdk',
});

export const walletLimiter = createRateLimiter({
  limit: 600,
  windowMs: 60_000,
  namespace: 'wallet',
});

export const sidioraLimiter = createRateLimiter({
  limit: 200,
  windowMs: 60_000,
  namespace: 'sidiora',
});
