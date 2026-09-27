// @vitest-environment jsdom

import * as countersMock from 'mocks/address/counters';
import { beforeEach, describe, expect, it } from 'vitest';
import { renderHook, waitFor, wrapper } from 'vitest/lib';
import flushPromises from 'vitest/utils/flushPromises';

import useAddressCountersQuery from './useAddressCountersQuery';

const HASH = '0x1e7a5b0b0d3f4d5e6f708192a3b4c5d6e7f80912';

const requestedPaths = () => fetchMock.mock.calls.map((call) => new URL(String(call[0]), 'http://localhost').pathname);

describe('useAddressCountersQuery', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify(countersMock.forAddress), { headers: { 'Content-Type': 'application/json' } });
  });

  it('asks for the counters while the address itself is still on its way', async() => {
    renderHook(() => useAddressCountersQuery({ hash: HASH, isLoading: true }), { wrapper });
    await flushPromises();

    expect(requestedPaths()).toEqual([ `/api/v2/addresses/${ HASH }/counters` ]);
  });

  it('answers with the counters the backend returns', async() => {
    const { result } = renderHook(() => useAddressCountersQuery({ hash: HASH, isLoading: true }), { wrapper });

    await waitFor(() => {
      expect(result.current.data).toEqual(countersMock.forAddress);
    });

    expect(result.current.isDegradedData).toBe(false);
  });

  it('asks for nothing while the page has disabled its queries', async() => {
    renderHook(() => useAddressCountersQuery({ hash: HASH, isEnabled: false, isLoading: true }), { wrapper });
    await flushPromises();

    expect(requestedPaths()).toEqual([]);
  });

  it('asks for nothing without an address', async() => {
    renderHook(() => useAddressCountersQuery({ hash: '' }), { wrapper });
    await flushPromises();

    expect(requestedPaths()).toEqual([]);
  });
});
