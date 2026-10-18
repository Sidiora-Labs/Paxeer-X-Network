/**
 * requestAnimationFrame batching utilities for high-frequency real-time data.
 *
 * Two primitives:
 *
 *  - `RafScheduler`     — coalesces many `schedule()` calls within a frame into
 *                         a single callback on the next animation frame. Use to
 *                         batch WS-driven React state updates (never setState per
 *                         tick). Falls back to a macrotask timer off the main
 *                         thread / in SSR where rAF is unavailable.
 *
 *  - `CoalescingBuffer` — a bounded, keyed buffer that keeps only the LATEST
 *                         value per key between flushes (stale-tick coalescing /
 *                         backpressure) and drops the oldest entry when full.
 *
 * Both are framework-agnostic and dependency-free so they can be unit-tested in
 * isolation and reused by the WS manager and React hooks alike.
 */

type Frame = (cb: () => void) => number;
type CancelFrame = (handle: number) => void;

const hasRaf =
  typeof globalThis !== 'undefined' &&
  typeof (globalThis as { requestAnimationFrame?: Frame }).requestAnimationFrame === 'function';

const scheduleFrame: Frame = hasRaf
  ? (globalThis as unknown as { requestAnimationFrame: Frame }).requestAnimationFrame.bind(globalThis)
  : (cb) => setTimeout(cb, 16) as unknown as number;

const cancelFrame: CancelFrame = hasRaf
  ? (globalThis as unknown as { cancelAnimationFrame: CancelFrame }).cancelAnimationFrame.bind(globalThis)
  : (handle) => clearTimeout(handle as unknown as ReturnType<typeof setTimeout>);

/**
 * Coalesces repeated `schedule()` calls into one callback per animation frame.
 * The most-recently-scheduled callback wins for the frame.
 */
export class RafScheduler {
  private handle: number | null = null;
  private pending: (() => void) | null = null;

  schedule(cb: () => void): void {
    this.pending = cb;
    if (this.handle !== null) return;
    this.handle = scheduleFrame(() => {
      this.handle = null;
      const fn = this.pending;
      this.pending = null;
      fn?.();
    });
  }

  /** Cancel any pending frame without running the callback. */
  cancel(): void {
    if (this.handle !== null) {
      cancelFrame(this.handle);
      this.handle = null;
    }
    this.pending = null;
  }
}

export interface CoalescingBufferOptions {
  /** Max distinct keys held between flushes. Oldest is dropped when exceeded. */
  maxSize?: number;
}

/**
 * A bounded, keyed buffer that retains only the latest value per key. Adding a
 * value for an existing key overwrites it in place (coalescing stale ticks).
 * When the number of distinct keys exceeds `maxSize`, the oldest inserted key
 * is evicted (backpressure — never grows unbounded).
 */
export class CoalescingBuffer<T> {
  private readonly map = new Map<string, T>();
  private readonly maxSize: number;

  constructor(options: CoalescingBufferOptions = {}) {
    const maxSize = options.maxSize ?? 5000;
    if (!Number.isSafeInteger(maxSize) || maxSize < 1 || maxSize > 100000) throw new RangeError('Invalid coalescing buffer bound');
    this.maxSize = maxSize;
  }

  /** Insert/overwrite the latest value for `key`. Returns dropped key, if any. */
  set(key: string, value: T): string | null {
    let dropped: string | null = null;
    if (!this.map.has(key) && this.map.size >= this.maxSize) {
      // Map preserves insertion order — the first key is the oldest.
      const oldest = this.map.keys().next().value;
      if (oldest !== undefined) {
        this.map.delete(oldest);
        dropped = oldest;
      }
    }
    // Delete-then-set so the key moves to the newest insertion slot.
    this.map.delete(key);
    this.map.set(key, value);
    return dropped;
  }

  get size(): number {
    return this.map.size;
  }

  isEmpty(): boolean {
    return this.map.size === 0;
  }

  /** Drain all buffered values (in insertion order) and clear the buffer. */
  drain(): T[] {
    const out = Array.from(this.map.values());
    this.map.clear();
    return out;
  }

  clear(): void {
    this.map.clear();
  }
}

/**
 * Exponential backoff with full jitter, capped.
 *   delay = random(0 .. min(capMs, baseMs * 2^attempt))
 * Using full jitter avoids reconnect thundering-herd across many clients.
 */
export function backoffWithJitter(
  attempt: number,
  baseMs = 1000,
  capMs = 30_000,
  rng: () => number = Math.random,
): number {
  const ceil = Math.min(capMs, baseMs * 2 ** Math.max(0, attempt));
  return Math.floor(rng() * ceil);
}
