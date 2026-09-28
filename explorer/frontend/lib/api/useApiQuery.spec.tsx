// @vitest-environment jsdom

import type { QueryClient } from '@tanstack/react-query';
import { useQueryClient } from '@tanstack/react-query';
import React from 'react';

import { beforeEach, describe, expect, it } from 'vitest';
import { render, waitFor } from 'vitest/lib';
import flushPromises from 'vitest/utils/flushPromises';

import useApiQuery, { getResourceKey } from './useApiQuery';
import { STALE_TIME } from './useQueryClientConfig';

const HASH = '0x5d1eca7d7e1e1b4a3ba4f4a0f6ac5e0c0f1f1a3e3a4c5d6e7f8091a2b3c4d5e6';

const responseInit = {
  headers: {
    'Content-Type': 'application/json',
  },
};

const TxReader = () => {
  useApiQuery('general:tx', { pathParams: { hash: HASH } });

  return null;
};

const TxReaderWithOwnStaleTime = () => {
  useApiQuery('general:tx', { pathParams: { hash: HASH }, queryOptions: { staleTime: 0 } });

  return null;
};

const LogsReader = () => {
  useApiQuery('general:tx_logs', { pathParams: { hash: HASH } });

  return null;
};

// The second reader mounts only once the first one has its answer, which is where a resource with a
// stale time reads the cache and a resource without one asks again.
const TxReadTwice = () => {
  const first = useApiQuery('general:tx', { pathParams: { hash: HASH } });

  return first.isSuccess ? <TxReader/> : null;
};

const TxReadTwiceWithOwnStaleTime = () => {
  const first = useApiQuery('general:tx', { pathParams: { hash: HASH }, queryOptions: { staleTime: 0 } });

  return first.isSuccess ? <TxReaderWithOwnStaleTime/> : null;
};

const LogsReadTwice = () => {
  const first = useApiQuery('general:tx_logs', { pathParams: { hash: HASH } });

  return first.isSuccess ? <LogsReader/> : null;
};

const clientHolder: { current: QueryClient | undefined } = { current: undefined };

const ClientProbe = () => {
  clientHolder.current = useQueryClient();

  useApiQuery('general:tx', { pathParams: { hash: HASH } });

  return null;
};

describe('useApiQuery', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    fetchMock.mockResponse(JSON.stringify({ hash: HASH }), responseInit);
  });

  it('puts the stale time of the resource class on the query', async() => {
    render(<ClientProbe/>);
    await waitFor(() => expect(clientHolder.current).toBeDefined());

    const query = clientHolder.current?.getQueryCache()
      .find({ queryKey: getResourceKey('general:tx', { pathParams: { hash: HASH } }) });

    expect(query?.observers[0]?.options.staleTime).toBe(STALE_TIME.record);
  });

  it('lets the second reader of a written record use the answer the first one got', async() => {
    render(<TxReadTwice/>);

    await waitFor(() => expect(fetchMock.mock.calls).toHaveLength(1));
    await flushPromises();

    expect(fetchMock.mock.calls).toHaveLength(1);
  });

  it('asks again for a resource left outside the classes', async() => {
    render(<LogsReadTwice/>);

    await waitFor(() => expect(fetchMock.mock.calls).toHaveLength(2));
  });

  it('lets the caller keep its own stale time', async() => {
    render(<TxReadTwiceWithOwnStaleTime/>);

    await waitFor(() => expect(fetchMock.mock.calls).toHaveLength(2));
  });

  it('asks the endpoint the resource names', async() => {
    render(<TxReader/>);

    await waitFor(() => expect(fetchMock.mock.calls).toHaveLength(1));

    const url = new URL(String(fetchMock.mock.calls[0][0]), 'http://localhost');

    expect(url.pathname).toBe(`/api/v2/transactions/${ HASH }`);
  });
});
