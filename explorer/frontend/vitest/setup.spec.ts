// @vitest-environment jsdom

import type { FontFace } from 'use-font-face-observer';
import useFontFaceObserver from 'use-font-face-observer';

import { BODY_TYPEFACE, HEADING_TYPEFACE } from 'toolkit/theme/foundations/typography';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from 'vitest/lib';

const OBSERVED_FONTS = [
  { family: BODY_TYPEFACE, weight: '400' as FontFace['weight'] },
  { family: HEADING_TYPEFACE, weight: '400' as FontFace['weight'] },
];

const STYLE = `400 100px "${ BODY_TYPEFACE }"`;

describe('the font face set the vitest setup fills in', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('gives the jsdom document a font face set to read', () => {
    expect(document.fonts).toBeDefined();
    expect(document.fonts.status).toBe('loaded');
    expect(document.fonts.size).toBe(0);
  });

  it('resolves load with at least one font face', async() => {
    const loaded = await document.fonts.load(STYLE, 'BESbswy');

    expect(Array.isArray(loaded)).toBe(true);
    expect(loaded.length).toBeGreaterThanOrEqual(1);
  });

  it('answers check with true and resolves ready with the set itself', async() => {
    expect(document.fonts.check(STYLE)).toBe(true);
    await expect(document.fonts.ready).resolves.toBe(document.fonts);
  });

  it('holds no font face and yields nothing when iterated', () => {
    const seen: Array<unknown> = [];

    document.fonts.forEach((fontFace) => {
      seen.push(fontFace);
    });

    expect(seen).toHaveLength(0);
    expect(Array.from(document.fonts)).toHaveLength(0);
  });

  it('lets the font observer behind the dynamic hash shortener settle and leaves no timer behind', async() => {
    vi.useFakeTimers();

    const { result } = renderHook(() => useFontFaceObserver(OBSERVED_FONTS));

    await act(async() => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(result.current).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  });
});
