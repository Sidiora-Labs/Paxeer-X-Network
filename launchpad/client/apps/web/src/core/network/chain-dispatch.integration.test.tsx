import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { EventEmitter } from 'node:events';
import { expect, test } from 'vitest';
import React, { act } from 'react';
import ts from 'typescript';
import { createPublicClient, encodeDeployData, http as viemHttp, type EIP1193Provider, type Address, type Abi, type Hex } from 'viem';
const url = process.env.DISPATCH_TEST_RPC_URL ?? 'http://127.0.0.1:18531';
if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(url).hostname)) throw new Error('Owned loopback EVM required');
const rpc = createPublicClient({ transport: viemHttp(url, { retryCount: 0 }) });
async function nodeRpc(method: string, params: unknown[] = []): Promise<unknown> {
 const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 2, method, params }) });
 const body = await response.json() as { result?: unknown; error?: { message: string } };
 if (body.error) throw new Error(body.error.message);
 return body.result;
}

const { JSDOM } = createRequire(import.meta.url)('jsdom') as { JSDOM: new (html: string, options: { url: string }) => { window: Window & typeof globalThis & { close(): void } } };
const dom = new JSDOM('<html><body><main id="root"></main></body></html>', { url: 'http://localhost:3050' });
const oldWindow = globalThis.window, oldDocument = globalThis.document;
Object.defineProperty(globalThis, 'window', { value: dom.window, configurable: true });
Object.defineProperty(globalThis, 'document', { value: dom.window.document, configurable: true });
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { createRoot } = await import('react-dom/client');
const { createConfig, http, WagmiProvider } = await import('wagmi');
const { connect, disconnect } = await import('wagmi/actions');
const { injected } = await import('wagmi/connectors');
const { useWriteErc20Approve } = await import('./contracts');
const { CORE_DEPLOYMENT } = await import('./deployment');
const { useWriteMetaAGRouterSwapBestRoute } = await import('../clients/meta-ag');
const chain = { id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: [url] } } } as const;
test('all19active raw contract dispatches pin chain125', () => {
 let count = 0;
 for (const path of ['contracts.ts', '../clients/meta-ag.ts', '../../widgets/rewards/rewards-view.tsx']) {
  const src = ts.createSourceFile(path, readFileSync(new URL(path, import.meta.url), 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const visit = (n: ts.Node) => {
   if (ts.isCallExpression(n) && /(?:^|\.)writeContract$/.test(n.expression.getText(src))) {
    count++; const arg = n.arguments[0];
    if (!arg || !ts.isObjectLiteralExpression(arg)) throw new Error('Concrete dispatch options required');
    const pin = arg.properties.find(p => ts.isPropertyAssignment(p) && p.name.getText(src) === 'chainId');
    expect(pin && ts.isPropertyAssignment(pin) ? pin.initializer.getText(src) : undefined, path).toBe('125');
   }
   ts.forEachChild(n, visit);
  }; visit(src);
 }
 expect(count).toBe(19);
});
test('actual approval hook rejects actual chain1 with zero transaction dispatch', async () => {
 expect(await rpc.getChainId()).toBe(1);
 const accounts = await nodeRpc('eth_accounts') as Address[];
 const events = new EventEmitter(); let sends = 0;
 // Transparent relay forwards every request to the actual owned EVM, including chain/account lookups.
 const provider = {
  on: events.on.bind(events), removeListener: events.removeListener.bind(events),
  async request(args: { method: string; params?: unknown }) {
   if (/^eth_send(?:Raw)?Transaction$/.test(args.method)) sends++;
   const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, ...args }) });
   const body = await response.json() as { result?: unknown; error?: { code: number; message: string } };
   if (body.error) throw Object.assign(new Error(body.error.message), { code: body.error.code });
   return body.result;
  },
 } as EIP1193Provider;
 const config = createConfig({ chains: [chain], connectors: [injected({ target: { id: 'owned-anvil', name: 'Owned Anvil', provider } })], multiInjectedProviderDiscovery: false, transports: { 125: http(url, { retryCount: 0 }) } });
 const nonce = await rpc.getTransactionCount({ address: accounts[0]!, blockTag: 'pending' });
 const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
 const root = createRoot(document.getElementById('root')!);
 let current: ReturnType<typeof useWriteErc20Approve> | undefined;
 let optional: ReturnType<typeof useWriteMetaAGRouterSwapBestRoute> | undefined;
 function Probe() { current = useWriteErc20Approve(); optional = useWriteMetaAGRouterSwapBestRoute(); return null; }
 async function waitForObservedMutation(predicate: () => boolean) {
  for (let i = 0; i < 100; i++) {
   if (predicate()) return;
   await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); });
  }
  throw new Error('Actual mutation observer did not reach the expected state');
 }
 async function resetApproval() {
  await act(async () => { current!.reset(); });
  // reset() notifies asynchronously; the preceding attempt's error must clear
  // before observing the next attempt, even under the full suite's scheduler.
  await waitForObservedMutation(() => current?.isIdle === true && current.error === null && current.data === undefined);
 }

 try {
  await act(async () => { root.render(<WagmiProvider config={config} reconnectOnMount={false}><QueryClientProvider client={query}><Probe /></QueryClientProvider></WagmiProvider>); });
  await act(async () => { await connect(config, { connector: config.connectors[0]! }); });
  await act(async () => { current!.write({ token: accounts[1]!, spender: CORE_DEPLOYMENT.router, amount: 1n }); });
  await waitForObservedMutation(() => current?.isError === true);
  expect(current?.error?.message).toMatch(/chain|network/i);
  expect(current?.data).toBeUndefined(); expect(sends).toBe(0);
  await resetApproval();
  await act(async () => { current!.write({ token: accounts[1]!, spender: accounts[2]!, amount: 1n }); });
  await waitForObservedMutation(() => current?.isError === true);
  expect(current?.error?.message).toMatch(/spender|deployment/i);
  await act(async () => { optional!.write({ tokenIn: accounts[1]!, tokenOut: accounts[2]!, amountIn: 1n, amountOutMin: 1n, deadline: 1n }); });
  await waitForObservedMutation(() => optional?.isError === true);
  expect(optional?.error?.message).toMatch(/unavailable|manifest/i);
  expect(optional?.data).toBeUndefined(); expect(sends).toBe(0);
  expect(await rpc.getTransactionCount({ address: accounts[0]!, blockTag: 'pending' })).toBe(nonce);
  // Positive control executes actual ERC20 approve bytecode and reads on-chain allowance.
  await nodeRpc('anvil_setChainId', [125]);
  await act(async () => { await disconnect(config); await connect(config, { connector: config.connectors[0]! }); });
  expect(await rpc.getChainId()).toBe(125);
  const artifactPath = process.env.DISPATCH_TEST_ERC20_ARTIFACT ?? '../../../../contracts/artifacts/contracts/test/MockERC20.sol/MockERC20.json';
  const artifact = JSON.parse(readFileSync(artifactPath, 'utf8')) as { abi: Abi; bytecode: Hex };
  const creation = encodeDeployData({ abi: artifact.abi, bytecode: artifact.bytecode, args: ['Owned fixture', 'FIX', 6] });
  const deploymentHash = await nodeRpc('eth_sendTransaction', [{ from: accounts[0]!, data: creation, gas: '0x4c4b40' }]) as Hex;
  const deploymentReceipt = await rpc.waitForTransactionReceipt({ hash: deploymentHash });
  expect(deploymentReceipt.status).toBe('success');
  const token = deploymentReceipt.contractAddress!;
  await resetApproval();
  await act(async () => { current!.write({ token, spender: CORE_DEPLOYMENT.router, amount: 12345n }); });
  await waitForObservedMutation(() => current?.isSuccess === true || current?.isError === true);
  expect(current?.error).toBeNull();
  expect(current?.data).toBeDefined();
  expect((await rpc.waitForTransactionReceipt({ hash: current!.data! })).status).toBe('success');
  expect(await rpc.readContract({ address: token, abi: artifact.abi, functionName: 'allowance', args: [accounts[0]!, CORE_DEPLOYMENT.router] })).toBe(12345n);
  expect(sends).toBe(1);

 } finally {
  await nodeRpc('anvil_setChainId', [1]);
  await act(async () => root.unmount()); query.clear(); await disconnect(config); dom.window.close();
  Object.defineProperty(globalThis, 'window', { value: oldWindow, configurable: true });
  Object.defineProperty(globalThis, 'document', { value: oldDocument, configurable: true });
 }
}, 15000);
