// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from 'vitest/lib';

import useSocketBuffer, { SOCKET_FLUSH_INTERVAL_MS } from './useSocketBuffer';

const setDocumentVisibility = (state: 'visible' | 'hidden') => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
  document.dispatchEvent(new Event('visibilitychange'));
};

describe('useSocketBuffer', () => {
  afterEach(() => {
    vi.useRealTimers();
    setDocumentVisibility('visible');
  });

  it('flushes on a two second cadence', () => {
    expect(SOCKET_FLUSH_INTERVAL_MS).toBe(2000);
  });

  it('hands twenty messages pushed inside half a second over as one flush', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    const { result } = renderHook(() => useSocketBuffer<number>({ onFlush }));

    for (let index = 0; index < 20; index++) {
      result.current.push(index);
      act(() => {
        vi.advanceTimersByTime(25);
      });
    }

    expect(onFlush).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS);
    });

    expect(onFlush).toHaveBeenCalledTimes(1);
    expect(onFlush.mock.calls[0][0]).toHaveLength(20);
    expect(onFlush.mock.calls[0][0][19]).toBe(19);
  });

  it('flushes nothing while no message arrives', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    renderHook(() => useSocketBuffer<number>({ onFlush }));

    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS * 3);
    });

    expect(onFlush).not.toHaveBeenCalled();
  });

  it('holds the flush while the pointer rests on the list and releases it when it leaves', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    const { result } = renderHook(() => useSocketBuffer<number>({ onFlush }));

    act(() => {
      result.current.hoverProps.onMouseEnter();
    });
    expect(result.current.isHeld).toBe(true);

    result.current.push(1);
    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS * 2);
    });
    expect(onFlush).not.toHaveBeenCalled();

    act(() => {
      result.current.hoverProps.onMouseLeave();
    });
    expect(result.current.isHeld).toBe(false);

    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS);
    });
    expect(onFlush).toHaveBeenCalledTimes(1);
    expect(onFlush.mock.calls[0][0]).toEqual([ 1 ]);
  });

  it('holds the flush while the document is hidden and releases it when it comes back', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    const { result } = renderHook(() => useSocketBuffer<number>({ onFlush }));

    act(() => {
      setDocumentVisibility('hidden');
    });

    result.current.push(1);
    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS * 2);
    });
    expect(onFlush).not.toHaveBeenCalled();

    act(() => {
      setDocumentVisibility('visible');
    });
    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS);
    });
    expect(onFlush).toHaveBeenCalledTimes(1);
  });

  it('keeps only the newest messages the limit allows', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    const { result } = renderHook(() => useSocketBuffer<number>({ onFlush, limit: 3 }));

    for (let index = 0; index < 10; index++) {
      result.current.push(index);
    }

    act(() => {
      vi.advanceTimersByTime(SOCKET_FLUSH_INTERVAL_MS);
    });

    expect(onFlush).toHaveBeenCalledTimes(1);
    expect(onFlush.mock.calls[0][0]).toEqual([ 7, 8, 9 ]);
  });

  it('follows the cadence it is given', () => {
    vi.useFakeTimers();

    const onFlush = vi.fn();
    const { result } = renderHook(() => useSocketBuffer<number>({ onFlush, intervalMs: 500 }));

    result.current.push(1);
    act(() => {
      vi.advanceTimersByTime(499);
    });
    expect(onFlush).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(onFlush).toHaveBeenCalledTimes(1);
  });
});
