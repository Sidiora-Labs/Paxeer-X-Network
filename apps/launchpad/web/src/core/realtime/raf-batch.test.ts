import { describe, it, expect } from 'vitest';
import { CoalescingBuffer, backoffWithJitter } from './raf-batch';

describe('CoalescingBuffer', () => {
  it('keeps only the latest value per key (coalesces stale ticks)', () => {
    const buf = new CoalescingBuffer<number>();
    expect(buf.set('a', 1)).toBeNull();
    expect(buf.set('a', 2)).toBeNull();
    expect(buf.set('a', 3)).toBeNull();
    expect(buf.size).toBe(1);
    expect(buf.drain()).toEqual([3]);
  });

  it('drains in insertion order and clears', () => {
    const buf = new CoalescingBuffer<string>();
    buf.set('a', 'x');
    buf.set('b', 'y');
    expect(buf.drain()).toEqual(['x', 'y']);
    expect(buf.isEmpty()).toBe(true);
    expect(buf.size).toBe(0);
  });

  it('evicts the oldest key when bounded capacity is exceeded (backpressure)', () => {
    const buf = new CoalescingBuffer<number>({ maxSize: 2 });
    expect(buf.set('a', 1)).toBeNull();
    expect(buf.set('b', 2)).toBeNull();
    // 'c' overflows -> oldest ('a') is dropped.
    expect(buf.set('c', 3)).toBe('a');
    expect(buf.size).toBe(2);
    expect(buf.drain()).toEqual([2, 3]);
  });

  it('re-setting an existing key refreshes its recency so it is not evicted next', () => {
    const buf = new CoalescingBuffer<number>({ maxSize: 2 });
    buf.set('a', 1);
    buf.set('b', 2);
    buf.set('a', 9); // 'a' becomes newest again
    // Now 'b' is the oldest; adding 'c' should evict 'b', not 'a'.
    expect(buf.set('c', 3)).toBe('b');
    expect(buf.drain()).toEqual([9, 3]);
  });
});

describe('backoffWithJitter', () => {
  it('returns 0 when rng yields 0', () => {
    expect(backoffWithJitter(0, 1000, 30_000, () => 0)).toBe(0);
    expect(backoffWithJitter(5, 1000, 30_000, () => 0)).toBe(0);
  });

  it('scales the ceiling exponentially with the attempt', () => {
    // ceil = base * 2^attempt; with rng=0.5 -> half the ceiling.
    expect(backoffWithJitter(0, 1000, 30_000, () => 0.5)).toBe(500);
    expect(backoffWithJitter(3, 1000, 30_000, () => 0.5)).toBe(4000); // 8000/2
  });

  it('caps the ceiling regardless of attempt', () => {
    // 1000 * 2^20 is far above the 30s cap -> ceiling clamps to 30_000.
    expect(backoffWithJitter(20, 1000, 30_000, () => 0.5)).toBe(15_000);
  });

  it('never returns a value at or above the (capped) ceiling', () => {
    for (let attempt = 0; attempt < 10; attempt++) {
      const ceil = Math.min(30_000, 1000 * 2 ** attempt);
      const v = backoffWithJitter(attempt, 1000, 30_000, () => 0.999999);
      expect(v).toBeLessThan(ceil);
      expect(v).toBeGreaterThanOrEqual(0);
    }
  });
});

describe('bounded admission', () => {
  it('rejects nonfinite and unbounded capacities', () => {
    for (const maxSize of [NaN, Infinity, -1, 0, 1.5, 100001]) {
      expect(() => new CoalescingBuffer({ maxSize })).toThrow(RangeError);
    }
  });
});
