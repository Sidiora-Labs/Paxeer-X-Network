// @vitest-environment jsdom

import { useRouter } from 'next/router';

import { TX } from 'stubs/tx';
import { generateListStub } from 'stubs/utils';
import type { Mock } from 'vitest';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, wrapper } from 'vitest/lib';
import flushPromises from 'vitest/utils/flushPromises';

const { mockRouterPush, mockScrollToTop } = vi.hoisted(() => ({
  mockRouterPush: vi.fn(() => Promise.resolve(true)),
  mockScrollToTop: vi.fn(),
}));

vi.mock('next/router', () => ({
  useRouter: vi.fn(() => ({
    query: {},
    push: mockRouterPush,
  })),
}));
vi.mock('react-scroll', () => ({ animateScroll: { scrollToTop: mockScrollToTop } }));

import type { Params } from './useQueryWithPages';
import useQueryWithPages from './useQueryWithPages';

const mockUseRouter = useRouter as Mock<typeof useRouter>;

const router = {
  query: {},
  push: mockRouterPush,
  pathname: '/blocks' as const,
} as unknown as ReturnType<typeof useRouter>;

const HASH = '0x1e7a5b0b0d3f4d5e6f708192a3b4c5d6e7f80912a3b4c5d6e7f80912a3b4c5d6';

const responseInit = {
  headers: {
    'Content-Type': 'application/json',
  },
};

const responses = {
  page_1: {
    items: [ { hash: '11' }, { hash: '12' } ],
    next_page_params: {
      block_number: 11,
      index: 12,
      items_count: 13,
    },
  },
  page_2: {
    items: [ { hash: '21' }, { hash: '22' } ],
    next_page_params: null,
  },
};

const params: Params<'general:address_txs'> = {
  resourceName: 'general:address_txs',
  pathParams: { hash: HASH },
};

describe('useQueryWithPages placeholder data', () => {
  beforeEach(() => {
    fetchMock.resetMocks();
    mockRouterPush.mockClear();
    mockScrollToTop.mockClear();
    mockUseRouter.mockReturnValue(router);
  });

  it('holds the rows of the page it is leaving until the next page answers', async() => {
    let releasePage2: () => void = () => undefined;
    const page2 = new Promise<{ body: string; headers: Record<string, string> }>((resolve) => {
      releasePage2 = () => resolve({ body: JSON.stringify(responses.page_2), ...responseInit });
    });

    fetchMock.mockResponse(() => page2);
    fetchMock.once(JSON.stringify(responses.page_1), responseInit);

    const { result } = renderHook(() => useQueryWithPages(params), { wrapper });
    await waitForApiResponse();

    expect(result.current.data).toEqual(responses.page_1);
    expect(result.current.isPlaceholderData).toBe(false);

    await act(() => {
      result.current.pagination.onNextPageClick();
    });
    await flushPromises();

    expect(result.current.data).toEqual(responses.page_1);
    expect(result.current.isPlaceholderData).toBe(true);
    expect(result.current.pagination.isLoading).toBe(true);
    expect(result.current.pagination.page).toBe(2);

    releasePage2();
    await waitForApiResponse();

    expect(result.current.data).toEqual(responses.page_2);
    expect(result.current.isPlaceholderData).toBe(false);
    expect(result.current.pagination.isLoading).toBe(false);
  });

  it('keeps the stub the caller passes as the placeholder of the first page', async() => {
    const stub = generateListStub<'general:address_txs'>(TX, 2, { next_page_params: null });

    fetchMock.mockResponse(() => new Promise(() => undefined));

    const { result } = renderHook(() => useQueryWithPages({ ...params, options: { placeholderData: stub } }), { wrapper });
    await flushPromises();

    expect(result.current.data).toEqual(stub);
    expect(result.current.isPlaceholderData).toBe(true);
    expect(result.current.pagination.isLoading).toBe(true);
  });

  it('holds the previous rows when the caller computes its first-page placeholder', async() => {
    const stub = generateListStub<'general:address_txs'>(TX, 1, { next_page_params: null });
    const page2 = Promise.withResolvers<{ body: string; headers: Record<string, string> }>();

    fetchMock.mockResponse(() => page2.promise);
    fetchMock.once(JSON.stringify(responses.page_1), responseInit);

    const { result } = renderHook(() => useQueryWithPages({
      ...params,
      options: { placeholderData: () => stub },
    }), { wrapper });
    await waitForApiResponse();

    expect(result.current.data).toEqual(responses.page_1);
    expect(result.current.isPlaceholderData).toBe(false);

    await act(() => {
      result.current.pagination.onNextPageClick();
    });
    await flushPromises();

    expect(result.current.data).toEqual(responses.page_1);
    expect(result.current.isPlaceholderData).toBe(true);
    expect(result.current.pagination.page).toBe(2);
    expect(result.current.pagination.isLoading).toBe(true);

    page2.resolve({ body: JSON.stringify(responses.page_2), ...responseInit });
    await waitForApiResponse();

    expect(result.current.data).toEqual(responses.page_2);
    expect(result.current.isPlaceholderData).toBe(false);
    expect(result.current.pagination.isLoading).toBe(false);
  });

  it('leaves a placeholder the caller computes itself alone', async() => {
    const stub = generateListStub<'general:address_txs'>(TX, 1, { next_page_params: null });
    const placeholderData = vi.fn(() => stub);

    fetchMock.mockResponse(() => new Promise(() => undefined));

    const { result } = renderHook(() => useQueryWithPages({ ...params, options: { placeholderData } }), { wrapper });
    await flushPromises();

    expect(placeholderData).toHaveBeenCalled();
    expect(result.current.data).toEqual(stub);
  });
});

async function waitForApiResponse() {
  await flushPromises();
  await act(flushPromises);
}
