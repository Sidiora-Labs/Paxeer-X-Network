import { expect, test } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { defineChain } from 'viem';
import * as client from '../../core/clients/meta-ag';
import { CORE_DEPLOYMENT } from '../../core/network/deployment';
const chain = defineChain({ id: 125, name: 'Fixture boundary only', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:22800'] } } });
// Real wagmi mutation/query objects; no mocked hooks, wallet or RPC success.
test('all optional writes reject synchronously before mutation cache or RPC', () => {
 const queryClient = new QueryClient();
 const config = createConfig({ chains: [chain], transports: { 125: http() }, ssr: true });
 const writes: Array<(args: never) => unknown> = [];
 function Probe() {
  writes.push(client.useWriteMetaAGRouterSwapBestRoute().write as never);
  writes.push(client.useWriteMetaAGRouterSwapViaAdapter().write as never);
  writes.push(client.useWriteMetaAGRouterSwapMultiHop().write as never);
  writes.push(client.useWritePecorOrdersPlaceLimitBuy().write as never);
  writes.push(client.useWritePecorOrdersPlaceLimitSell().write as never);
  writes.push(client.useWritePecorOrdersPlaceStopLoss().write as never);
  writes.push(client.useWritePecorOrdersPlaceStopLimitBuy().write as never);
  writes.push(client.useWritePecorOrdersPlaceStopLimitSell().write as never);
  writes.push(client.useWritePecorOrdersCancelLimitOrder().write as never);
  writes.push(client.useWritePecorOrdersCancelStopLimitOrder().write as never);
  return null;
 }
 renderToStaticMarkup(<WagmiProvider config={config}><QueryClientProvider client={queryClient}><Probe /></QueryClientProvider></WagmiProvider>);
 expect(writes).toHaveLength(10);
 for (const write of writes) expect(() => write({} as never)).toThrow('no verified active deployment');
 expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
 queryClient.clear();
});
test('legacy optional constants are inert, USDL uses current manifest', () => {
 expect(client.META_AG_USDL_ADDRESS).toBe(CORE_DEPLOYMENT.usdl);
 expect(client.PECOR_ORDERS_ADDRESS).toBe('0x0000000000000000000000000000000000000000');
});
test('optional reads retain disabled query contracts on chain125', () => {
 const queryClient = new QueryClient();
 const config = createConfig({ chains: [chain], transports: { 125: http() }, ssr: true });
 const token = '0x1111111111111111111111111111111111111111';
 function Probe() {
  client.useReadMetaAGQuoterQuoteExactIn({ tokenIn: token, tokenOut: token, amountIn: 1n });
  client.useReadMetaAGQuoterGetTokenPrice({ token });
  client.useReadMetaAGQuoterGetLiquidityInfo({ token });
  client.useReadMetaAGRouterGetBestQuote({ tokenIn: token, tokenOut: token, amountIn: 1n });
  client.useReadMetaAGRouterGetAllQuotes({ tokenIn: token, tokenOut: token, amountIn: 1n });
  client.useReadMetaAGRouterGetAdapters();
  client.useReadMetaAGRouterIsAdapterActive({ adapterId: '0x00' });
  client.useReadPecorOrdersGetUserLimitOrders({ user: token });
  client.useReadPecorOrdersGetUserStopLimitOrders({ user: token });
  client.useReadPecorOrdersGetLimitOrder({ orderId: 1n });
  client.useReadPecorOrdersGetStopLimitOrder({ orderId: 1n });
  client.useReadPecorOrdersCanExecuteLimitOrder({ orderId: 1n });
  return null;
 }
 renderToStaticMarkup(<WagmiProvider config={config}><QueryClientProvider client={queryClient}><Probe /></QueryClientProvider></WagmiProvider>);
 const queries = queryClient.getQueryCache().getAll();
 expect(queries).toHaveLength(12);
 for (const query of queries) {
  expect((query.options as { enabled?: boolean }).enabled).toBe(false);
  expect(query.state.fetchStatus).toBe('idle');
  expect(query.queryKey[1]).toMatchObject({ chainId: 125 });
 }
 queryClient.clear();
});


test('legacy ABI normalizers reject absent fields and truthy coercions', () => {
 expect(client.normaliseQuote({})).toBeNull(); expect(client.normaliseBestQuote({ found: true })).toBeNull();
 expect(client.normaliseLimitOrder({ id: 1n })).toBeNull(); expect(client.normaliseStopLimitOrder({ id: 1n })).toBeNull();
 const quote = [1n,2n,3n,4n,5n,6n,0n,10n,true,100n,100n,100n,false,false];
 expect(client.normaliseQuote(quote)).toMatchObject({ amountIn: 1n, amountOut: 2n, sufficientLiquidity: true });
 for (const index of [8,12,13]) { const broken = [...quote]; broken[index] = 'false' as never; expect(client.normaliseQuote(broken)).toBeNull(); }
 for (const index of [0,3,4,10]) { const broken = [...quote]; broken[index] = undefined as never; expect(client.normaliseQuote(broken)).toBeNull(); }
 const entry = { adapterId: `0x${'aa'.repeat(32)}`, adapter: '0x1111111111111111111111111111111111111111', active: true, name: 'Real registry schema input' };
 expect(client.normaliseAdapters([entry])).toHaveLength(1);
 expect(client.normaliseAdapters([{ ...entry, active: 'false' }])).toEqual([]);
 expect(client.normaliseAdapters([entry,entry])).toEqual([]);
 expect(client.normaliseAdapters([{ ...entry, adapter: '0x0' }])).toEqual([]);
});

test('legacy status contract stays fail closed when current deployed terminal enums differ', () => {
 const tuple = [1n,'0x1111111111111111111111111111111111111111','0x2222222222222222222222222222222222222222','0x3333333333333333333333333333333333333333',100n,250n,0,0,100n,200n];
 expect(client.normaliseLimitOrder(tuple)?.status).toBe(client.OrderStatus.Active);
 for (const status of [1,2,3,4]) expect(client.normaliseLimitOrder([...tuple.slice(0,7),status,...tuple.slice(8)])).toBeNull();
});
