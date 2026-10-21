import { expect, test } from 'vitest';
import { createPublicClient, http, verifyMessage, type EIP1193Provider } from 'viem';
import { getInjectedProvider, InjectedPaxeerWallet, paxeerWalletChain, WrongPaxeerChainError } from './client';

// This is a real local RPC fixture, not a fabricated provider or signing result.
// Use a dedicated Anvil instance, never a shared integration fixture.
// Start it on chain 125 and set WALLET_TEST_ALLOW_CHAIN_MUTATION=1 for the chain guard scenario.
const fixtureUrl = process.env.WALLET_TEST_RPC_URL ?? 'http://127.0.0.1:18525';
const parsedUrl = new URL(fixtureUrl);
if (!['127.0.0.1', 'localhost', '[::1]'].includes(parsedUrl.hostname)) throw new Error('Wallet tests require a loopback-only EVM fixture');

const rpc = createPublicClient({ chain: paxeerWalletChain, transport: http(fixtureUrl, { retryCount: 0 }) });
const provider = { request: rpc.request } as EIP1193Provider;

test('external helper is SSR safe and native currency is PAX', () => {
  expect(getInjectedProvider()).toBeNull();
  expect(paxeerWalletChain.nativeCurrency).toEqual({ name: 'PAX', symbol: 'PAX', decimals: 18 });
});

test('real EIP-1193 account authorization, signature and transfer on isolated chain 125', async () => {
  expect(await rpc.getChainId()).toBe(125);
  const wallet = new InjectedPaxeerWallet(provider);
  const accounts = await wallet.connect();
  expect(accounts.length).toBeGreaterThanOrEqual(2);
  const from = accounts[0]!;
  const to = accounts[1]!;
  const signature = await wallet.signMessage(from, 'Sidiora local wallet integration proof; no login authorization');
  expect(await verifyMessage({ address: from, message: 'Sidiora local wallet integration proof; no login authorization', signature })).toBe(true);
  await expect(wallet.signMessage('0x0000000000000000000000000000000000000001', 'not authorized')).rejects.toThrow('no longer authorized');
  await expect(wallet.sendTransaction(from, { to, value: -1n })).rejects.toThrow('must not be negative');
  const before = await rpc.getBalance({ address: to });
  const hash = await wallet.sendTransaction(from, { to, value: 12345n });
  const receipt = await rpc.waitForTransactionReceipt({ hash });
  expect(receipt.status).toBe('success');
  expect(await rpc.getBalance({ address: to })).toBe(before + 12345n);
}, 20000);

test('wrong actual fixture chain is rejected before signing', async () => {
  // Mutate only a specifically authorized dedicated fixture, restoring on every outcome.
  if (process.env.WALLET_TEST_ALLOW_CHAIN_MUTATION !== '1') throw new Error('Wrong-chain test requires a dedicated Anvil fixture and WALLET_TEST_ALLOW_CHAIN_MUTATION=1');
  const response = await fetch(fixtureUrl, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'anvil_setChainId', params: [1] }) });
  const result: { error?: { message: string } } = await response.json();
  if (result.error) throw new Error(result.error.message);
  try {
    await expect(new InjectedPaxeerWallet(provider).assertChain()).rejects.toBeInstanceOf(WrongPaxeerChainError);
  } finally {
    const restored = await fetch(fixtureUrl, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'anvil_setChainId', params: [125] }) });
    const restoredResult: { error?: { message: string } } = await restored.json();
    if (restoredResult.error) throw new Error(restoredResult.error.message);
  }
});

test('account change before signing rejects the stale selection without dispatch', async () => {
  const accounts = await new InjectedPaxeerWallet(provider).getAccounts();
  let signingRequests = 0;
  const changedProvider: EIP1193Provider = {
    async request(args: { method: string; params?: unknown }) {
      if (args.method === 'eth_accounts') return [accounts[1]];
      if (args.method === 'personal_sign' || args.method === 'eth_sendTransaction') signingRequests++;
      const response = await fetch(fixtureUrl, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 5, ...args }) });
      const body: { result?: unknown; error?: { message: string } } = await response.json();
      if (body.error) throw new Error(body.error.message);
      return body.result;
    },
  } as EIP1193Provider;
  const wallet = new InjectedPaxeerWallet(changedProvider);
  await expect(wallet.signMessage(accounts[0]!, 'stale account')).rejects.toThrow('no longer authorized');
  await expect(wallet.sendTransaction(accounts[0]!, { to: accounts[1]!, value: 1n })).rejects.toThrow('no longer authorized');
  expect(signingRequests).toBe(0);
});

test('chain change during awaited account lookup blocks sign and send dispatch', async () => {
  if (process.env.WALLET_TEST_ALLOW_CHAIN_MUTATION !== '1') throw new Error('Dedicated chain mutation permission required');
  const accounts = await new InjectedPaxeerWallet(provider).getAccounts();
  let dispatched = 0;
  const requestRpc = async (method: string, params?: unknown) => {
    const response = await fetch(fixtureUrl, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 8, method, params }) });
    const body: { result?: unknown; error?: { message: string } } = await response.json();
    if (body.error) throw new Error(body.error.message);
    return body.result;
  };
  const racingProvider = {
    async request(args: { method: string; params?: unknown }) {
      const result = await requestRpc(args.method, args.params);
      if (args.method === 'eth_accounts') await requestRpc('anvil_setChainId', [1]);
      if (args.method === 'personal_sign' || args.method === 'eth_sendTransaction') dispatched++;
      return result;
    },
  } as EIP1193Provider;
  const wallet = new InjectedPaxeerWallet(racingProvider);
  try {
    await expect(wallet.signMessage(accounts[0]!, 'chain changed during account lookup')).rejects.toBeInstanceOf(WrongPaxeerChainError);
    await requestRpc('anvil_setChainId', [125]);
    await expect(wallet.sendTransaction(accounts[0]!, { to: accounts[1]!, value: 1n })).rejects.toBeInstanceOf(WrongPaxeerChainError);
    expect(dispatched).toBe(0);
  } finally {
    await requestRpc('anvil_setChainId', [125]);
  }
});
