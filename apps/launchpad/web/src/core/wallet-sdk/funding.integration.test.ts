import { beforeAll, afterAll, expect, test } from 'vitest';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createWalletClient, custom, erc20Abi, verifyMessage, type Address, type EIP1193Provider, type Hex } from 'viem';
import { createSiweMessage } from 'viem/siwe';
import { getPublicClient, InjectedPaxeerWallet, paxeerWalletChain } from './client';
import { nativePAX, usdlAsset, assertAssetOnChain, formatAssetAmount } from './assets';
import { WalletSessionController } from './session';
import { WalletLogin, type AuthChallenge } from './auth';
import { WalletReconnect } from '../../widgets/wallet-recovery/reconnect';
import { FundingTracker, depositIntent } from '../../widgets/funding/funding';
import { WalletTransfer } from '../../widgets/funding/transfer';
const url = process.env.WALLET_TEST_RPC_URL;
if (!url || !['127.0.0.1', 'localhost'].includes(new URL(url).hostname) || new URL(url).port !== '22500') throw new Error('L25 requires owned loopback Anvil port22500');
const rpc = getPublicClient(url);
const provider = { request: rpc.request } as EIP1193Provider;
const external = new InjectedPaxeerWallet(provider);
const fixtureRoot = resolve(process.cwd(), '../../../evidence/production-system/L25/phase1');
const fixtureArtifacts = process.env.L25_FIXTURE_ARTIFACTS;
if (!fixtureArtifacts) throw new Error('Run the owned run-l25-check.py lifecycle runner to build fresh fixture artifacts');
const artifact = JSON.parse(readFileSync(resolve(fixtureArtifacts, 'FundingToken.sol/FundingToken.json'), 'utf8'));
const receipts: unknown[] = [];
let A: Address, B: Address, C: Address, token: Address, badToken: Address;
const sessions = new WalletSessionController(() => {});
beforeAll(async () => {
  expect(await rpc.getChainId()).toBe(125);
  [A, B, C] = (await external.connect()) as [Address, Address, Address];
  const deployer = createWalletClient({ chain: paxeerWalletChain, transport: custom(provider), account: A });
  for (const decimals of [6, 18]) {
    const hash = await deployer.deployContract({ abi: artifact.abi, bytecode: artifact.bytecode.object, args: [decimals] });
    const receipt = await rpc.waitForTransactionReceipt({ hash });
    expect(receipt.status).toBe('success');
    receipts.push({ kind: 'fixture_deployment', hash, address: receipt.contractAddress, decimals });
    if (decimals === 6) token = receipt.contractAddress!; else badToken = receipt.contractAddress!;
  }
}, 20000);
afterAll(() => { sessions.dispose(); writeFileSync(process.env.WALLET_TEST_RECEIPTS_PATH ?? resolve(fixtureRoot, 'native-receipts.json'), JSON.stringify({ observedAt: new Date().toISOString(), chainId: 125, rpc: url, fixture: 'private_anvil_not_mainnet', receipts }, (_, value) => typeof value === 'bigint' ? value.toString() : value, 2)); });

test('actual ERC20 code/decimals required; 1USDL deposit receipt + committed balance only', async () => {
  await expect(assertAssetOnChain(rpc, usdlAsset(badToken))).rejects.toThrow('decimals mismatch');
  await expect(assertAssetOnChain(rpc, usdlAsset('0x0000000000000000000000000000000000000001'))).rejects.toThrow('code unavailable');
  sessions.select('injected', 'injected', B, 125, ['injected']);
  const tracker = new FundingTracker(rpc, sessions, depositIntent(B, usdlAsset(token), '1000000'));
  const writer = createWalletClient({ chain: paxeerWalletChain, transport: custom(provider), account: A });
  const hash = await writer.writeContract({ address: token, abi: erc20Abi, functionName: 'transfer', args: [B, 1000000n] });
  const receipt = await rpc.waitForTransactionReceipt({ hash });
  const observed = await tracker.refresh(hash);
  expect(observed.state).toBe('balance_confirmed');
  expect(observed.detectedRaw).toBe('1000000');
  expect(formatAssetAmount(usdlAsset(token), observed.balanceRaw!)).toBe('1 USDL');
  receipts.push({ kind: 'USDL_deposit', receipt, observed });
  const unrelatedHash = await writer.writeContract({ address: token, abi: erc20Abi, functionName: 'transfer', args: [C, 1000000n] });
  await rpc.waitForTransactionReceipt({ hash: unrelatedHash });
  const unrelated = await tracker.refresh(unrelatedHash);
  expect(unrelated.state).toBe('unknown_external_source');
  expect(unrelated.balanceRaw).toBeUndefined();
  const missing = await tracker.refresh(`0x${'ff'.repeat(32)}`);
  expect(missing.state).toBe('checking_paused'); expect(missing.balanceRaw).toBeUndefined();
}, 20000);

test('reviewed native and ERC20 transfers produce authentic receipts; no consent means no dispatch', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  for (const asset of [nativePAX, usdlAsset(token)]) {
    const transfer = new WalletTransfer(rpc, external, sessions);
    const preview = await transfer.preview(`proof-${asset.kind}`, asset, B, '12345');
    expect(preview.gasEstimateRaw).not.toBe('0');
    expect(preview.recipient).toBe(B);
    const hash = await transfer.submit(preview, async () => true);
    const receipt = await rpc.waitForTransactionReceipt({ hash });
    const confirmed = await transfer.confirm();
    expect(confirmed.state).toBe('confirmed'); expect(confirmed.amountRaw).toBe('12345');
    receipts.push({ kind: `reviewed_${asset.kind}_transfer`, receipt, confirmed });
    await expect(transfer.submit(preview, async () => true)).rejects.toThrow('already');
  }
  const denied = new WalletTransfer(rpc, external, sessions);
  const preview = await denied.preview('denied', nativePAX, B, '1');
  const nonce = await rpc.getTransactionCount({ address: A });
  await expect(denied.submit(preview, async () => false)).rejects.toThrow('consent declined');
  expect(denied.hash).toBeUndefined();
  expect(await rpc.getTransactionCount({ address: A })).toBe(nonce);
  const changed = new WalletTransfer(rpc, external, sessions);
  const staleReview = await changed.preview('race', nativePAX, B, '1');
  await expect(changed.submit(staleReview, async () => { sessions.select('injected', 'injected', B, 125, ['injected']); return true; })).rejects.toThrow('Wallet changed');
  expect(changed.hash).toBeUndefined();
}, 20000);

function challenge(account: Address): AuthChallenge {
  const issuedAt = new Date().toISOString(), expiresAt = new Date(Date.now() + 299000).toISOString();
  const c: AuthChallenge = { challengeId: '00000000-0000-4000-8000-000000000001', wallet: account, chainId: '125', purpose: 'login', domain: 'sidiora.fun', uri: 'https://sidiora.fun', nonce: 'ab'.repeat(32), issuedAt, expiresAt, message: '' };
  c.message = createSiweMessage({ address: account, chainId: 125, domain: c.domain, uri: c.uri, version: '1', nonce: c.nonce, issuedAt: new Date(issuedAt), expirationTime: new Date(expiresAt), statement: 'Sign in to Sidiora.' }); return c;
}
test('real EIP191 login signing, rejected consent, and fresh-proof reconnect identity isolation', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const login = new WalletLogin(sessions, external); const c = challenge(A);
  await expect(login.sign({ ...c, domain: 'evil.example' }, 'https://sidiora.fun', 'Sign in to Sidiora.', async () => true)).rejects.toThrow('mismatch');
  await expect(login.sign(c, 'https://sidiora.fun', 'Sign in to Sidiora.', async () => false)).rejects.toThrow('consent declined');
  expect(login.state).toBe('anonymous');
  const proof = await login.sign(c, 'https://sidiora.fun', 'Sign in to Sidiora.', async review => review.fee === 'No transaction fee');
  await expect(login.sign(c, 'https://sidiora.fun', 'Sign in to Sidiora.', async () => true)).rejects.toThrow('already in progress');
  expect(proof.message).toBe(c.message); expect(await verifyMessage({ address: A, message: proof.message, signature: proof.signature })).toBe(true);
  await expect(login.verify(proof, async (_wire, signal) => {
    sessions.select('injected', 'injected', B, 125, ['injected']);
    expect(signal.aborted).toBe(true);
    return { privateSession: 'synthetic_backend_response_not_real_auth' };
  })).rejects.toThrow('Wallet changed');
  login.verificationFinished();
  const reconnect = new WalletReconnect(sessions);
  const restored = await reconnect.connect(external, 'injected', A, ['injected']);
  expect(restored.matched).toBe(true); expect(restored.privateAccess).toBe(false); expect(reconnect.state).toBe('awaiting_fresh_proof');
  const wrong = await reconnect.connect(external, 'injected', B, ['injected']);
  expect(wrong.matched).toBe(false); expect(wrong.privateAccess).toBe(false); expect(reconnect.state).toBe('distinct_identity_requires_own_proof');
});

test('injected refusal and actual-signature account race discard login proof', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  let prompts = 0;
  const refusalProvider = { request: async (args: { method: string; params?: unknown }) => {
    if (args.method === 'personal_sign') { prompts++; throw Object.assign(new Error('User rejected signature'), { code: 4001 }); }
    return provider.request(args as never);
  } } as unknown as EIP1193Provider;
  // The refusal is an injected error scenario; successful signing is proven against Anvil above.
  const rejected = new WalletLogin(sessions, new InjectedPaxeerWallet(refusalProvider));
  await expect(rejected.sign(challenge(A), 'https://sidiora.fun', 'Sign in to Sidiora.', async () => true)).rejects.toThrow('rejected');
  expect(rejected.state).toBe('anonymous'); expect(prompts).toBe(1);
  const raceProvider = { request: async (args: { method: string; params?: unknown }) => {
    const result = await provider.request(args as never);
    if (args.method === 'personal_sign') sessions.select('injected', 'injected', B, 125, ['injected']);
    return result;
  } } as unknown as EIP1193Provider;
  const raced = new WalletLogin(sessions, new InjectedPaxeerWallet(raceProvider));
  await expect(raced.sign(challenge(A), 'https://sidiora.fun', 'Sign in to Sidiora.', async () => true)).rejects.toThrow('Wallet changed');
  expect(raced.state).toBe('anonymous');
});

test('actual mined revert remains reverted and retains hash; missing receipt never succeeds', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const transfer = new WalletTransfer(rpc, external, sessions);
  const preview = await transfer.preview('revert-proof', nativePAX, C, '1');
  const reject = JSON.parse(readFileSync(resolve(fixtureArtifacts, 'FundingToken.sol/RejectNative.json'), 'utf8'));
  async function setCode(code: Hex) {
    const response = await fetch(url!, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 9, method: 'anvil_setCode', params: [C, code] }) });
    const body = await response.json(); if (body.error) throw new Error(body.error.message);
  }
  // Change only our owned local fixture after gas preview to force authentic EVM failure.
  await setCode(reject.deployedBytecode.object);
  try {
    const hash = await transfer.submit(preview, async () => true);
    const receipt = await rpc.waitForTransactionReceipt({ hash });
    expect(receipt.status).toBe('reverted');
    const result = await transfer.confirm();
    expect(result.state).toBe('reverted'); expect(result.amountRaw).toBe('0'); expect(transfer.hash).toBe(hash);
    receipts.push({ kind: 'mined_revert', receipt, result });
  } finally { await setCode('0x'); }
});

test('actual pending transaction stays unknown, retains hash and confirms without resubmission', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const transfer = new WalletTransfer(rpc, external, sessions);
  const preview = await transfer.preview('pending-proof', nativePAX, B, '7');
  async function control(method: string, params: unknown[]) {
    const response = await fetch(url!, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 10, method, params }) });
    const body = await response.json(); if (body.error) throw new Error(body.error.message);
  }
  await control('evm_setAutomine', [false]);
  try {
    const hash = await transfer.submit(preview, async () => true);
    await expect(transfer.confirm()).rejects.toThrow();
    expect(transfer.state).toBe('unknown'); expect(transfer.hash).toBe(hash);
    await control('evm_mine', []);
    const confirmed = await transfer.confirm();
    expect(confirmed.state).toBe('confirmed'); expect(confirmed.amountRaw).toBe('7');
    receipts.push({ kind: 'pending_then_committed_no_resubmission', confirmed });
  } finally { await control('evm_setAutomine', [true]); }
}, 20000);

test('bound provider authorization event permits reconnect; competing account, chain and disconnect reject', async () => {
  // This eventful EIP1193 adapter forwards actual Anvil authorization/RPC data.
  // It exercises event ordering, not physical extension/device qualification.
  for (const mode of ['authorized', 'competing_account', 'wrong_chain', 'disconnect', 'disconnect_then_authorized'] as const) {
    const localSessions = new WalletSessionController(() => {});
    const handlers = new Map<string, (...args: any[]) => void>();
    let replayCancelled = false;
    const eventful = {
      on(event: string, handler: (...args: any[]) => void) { handlers.set(event, handler); },
      removeListener(event: string) { handlers.delete(event); },
      async request(args: { method: string; params?: unknown }) {
        const result = await provider.request(args as never);
        if (args.method === 'eth_requestAccounts') {
          handlers.get('accountsChanged')?.(result);
          if (mode === 'competing_account') handlers.get('accountsChanged')?.([B]);
          if (mode === 'wrong_chain') handlers.get('chainChanged')?.('0x1');
          if (mode === 'disconnect' || (mode === 'disconnect_then_authorized' && !replayCancelled)) handlers.get('disconnect')?.({ code: 4900, message: 'Disconnected' });
          if (mode === 'disconnect_then_authorized' && !replayCancelled) handlers.get('accountsChanged')?.(result);
        }
        return result;
      },
    } as unknown as EIP1193Provider;
    localSessions.bind(eventful);
    const reconnect = new WalletReconnect(localSessions);
    try {
      if (mode === 'authorized') {
        const result = await reconnect.connect(new InjectedPaxeerWallet(eventful), 'injected', A, ['injected']);
        expect(result.matched).toBe(true); expect(result.privateAccess).toBe(false);
        expect(localSessions.current?.sessionEpoch).toBeGreaterThan(1);
      } else {
        await expect(reconnect.connect(new InjectedPaxeerWallet(eventful), 'injected', A, ['injected'])).rejects.toThrow('Wallet changed');
        expect(localSessions.current).toBeNull();
        if (mode === 'disconnect_then_authorized') {
          replayCancelled = true;
          const fresh = await reconnect.connect(new InjectedPaxeerWallet(eventful), 'injected', A, ['injected']);
          expect(fresh.matched).toBe(true); expect(fresh.privateAccess).toBe(false);
        }
      }
    } finally { localSessions.dispose(); }
  }
});

test('attested token mismatch blocks only USDL while the real native balance and transfer remain available', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const badIdentity = usdlAsset(token, `0x${'00'.repeat(32)}`);
  const tokenFunding = new FundingTracker(rpc, sessions, depositIntent(A, badIdentity));
  const rejected = await tokenFunding.refresh();
  expect(rejected.state).toBe('checking_paused');
  expect(rejected.reason).toContain('approved deployment');
  expect(rejected.balanceRaw).toBeUndefined();
  const nonce = await rpc.getTransactionCount({ address: A });
  await expect(new WalletTransfer(rpc, external, sessions).preview('wrong-runtime', badIdentity, B, '1')).rejects.toThrow('approved deployment');
  expect(await rpc.getTransactionCount({ address: A })).toBe(nonce);
  const native = await new FundingTracker(rpc, sessions, depositIntent(A, nativePAX)).refresh();
  expect(native.state).toBe('balance_confirmed');
  expect(BigInt(native.balanceRaw!)).toBeGreaterThan(0n);
  const valid = await new WalletTransfer(rpc, external, sessions).preview('native-unaffected', nativePAX, B, '1');
  expect(valid.assetRef.kind).toBe('native');
  expect(valid.gasEstimateRaw).not.toBe('0');
});

test('delayed committed head and real RPC outage never credit funds; queued distinct hashes retain their identity', async () => {
  sessions.select('injected', 'injected', B, 125, ['injected']);
  const oldHead = await rpc.getBlock({ blockTag: 'latest' });
  let committed = { number: oldHead.number, hash: oldHead.hash };
  const tracker = new FundingTracker(rpc, sessions, depositIntent(B, usdlAsset(token)), async signal => { signal.throwIfAborted(); return committed; });
  const before = await tracker.refresh();
  const writer = createWalletClient({ chain: paxeerWalletChain, transport: custom(provider), account: A });
  const hash = await writer.writeContract({ address: token, abi: erc20Abi, functionName: 'transfer', args: [B, 1000000n] });
  const receipt = await rpc.waitForTransactionReceipt({ hash });
  const delayed = await tracker.refresh(hash);
  expect(delayed.state).toBe('checking_paused'); expect(delayed.hash).toBe(hash);
  expect(delayed.balanceRaw).toBeUndefined();
  const stillOld = await tracker.refresh();
  expect(stillOld.balanceRaw).toBe(before.balanceRaw);
  const latest = await rpc.getBlock({ blockTag: 'latest' });
  committed = { number: latest.number, hash: latest.hash };
  const first = tracker.refresh(hash);
  expect(tracker.refresh(hash)).toBe(first);
  const other = tracker.refresh(`0x${'ee'.repeat(32)}`);
  const verified = await first;
  expect(verified.hash).toBe(hash); expect(verified.state).toBe('balance_confirmed');
  expect(BigInt(verified.balanceRaw!)).toBe(BigInt(before.balanceRaw!) + 1000000n);
  const missing = await other;
  expect(missing.hash).toBe(`0x${'ee'.repeat(32)}`); expect(missing.state).toBe('checking_paused');
  const duplicate = await tracker.refresh(hash);
  expect(duplicate.balanceRaw).toBe(verified.balanceRaw);
  const offline = new FundingTracker(getPublicClient('http://127.0.0.1:0', sessions.capture().signal), sessions, depositIntent(B, nativePAX));
  const unavailable = await offline.refresh();
  expect(unavailable.state).toBe('checking_paused'); expect(unavailable.balanceRaw).toBeUndefined();
  receipts.push({ kind: 'delayed_committed_funding_no_duplicate_credit', receipt, verified });
}, 20000);

test('expired login, account change during consent and consumed proof cannot authorize another wallet', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const expired = challenge(A);
  await expect(new WalletLogin(sessions, external).sign(expired, 'https://sidiora.fun', 'Sign in to Sidiora.', async () => {
    sessions.select('injected', 'injected', B, 125, ['injected']);
    return true;
  })).rejects.toThrow('Wallet changed');
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const login = new WalletLogin(sessions, external);
  const c = challenge(A);
  await expect(login.sign({ ...c, expiresAt: new Date(0).toISOString() }, 'https://sidiora.fun', 'Sign in to Sidiora.', async () => true)).rejects.toThrow('Expired');
  expect(login.state).toBe('anonymous');
  const proof = await login.sign(c, 'https://sidiora.fun', 'Sign in to Sidiora.', async review => {
    expect(review.fee).toBe('No transaction fee'); return true;
  });
  expect(Object.isFrozen(proof)).toBe(true);
  expect(await verifyMessage({ address: A, message: proof.message, signature: proof.signature })).toBe(true);
  login.verificationFinished();
  await expect(login.verify(proof, async () => { throw new Error('A consumed proof must not reach a backend'); })).rejects.toThrow('stale or already consumed');
});

test('recipient normalization and duplicate confirmation preserve the reviewed transaction and never resubmit', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const transfer = new WalletTransfer(rpc, external, sessions);
  const review = await transfer.preview('normalized-recipient', nativePAX, ` ${B.toLowerCase()} `, '11');
  expect(review.recipient).toBe(B);
  expect(review.amountRaw).toBe('11');
  const hash = await transfer.submit(review, async exact => exact === review && exact.recipient === B);
  const receipt = await rpc.waitForTransactionReceipt({ hash });
  const nonce = await rpc.getTransactionCount({ address: A });
  sessions.disconnect();
  const first = transfer.confirm();
  expect(transfer.confirm()).toBe(first);
  const verified = await first;
  expect(verified.hash).toBe(hash); expect(verified.state).toBe('confirmed'); expect(verified.amountRaw).toBe('11');
  await expect(transfer.submit(review, async () => true)).rejects.toThrow('already');
  expect(await rpc.getTransactionCount({ address: A })).toBe(nonce);
  receipts.push({ kind: 'disconnect_preserves_real_transfer_hash', receipt, verified });
});

test('maximum native amount reserves the actual recipient estimate and never signs a negative amount', async () => {
  sessions.select('injected', 'injected', A, 125, ['injected']);
  const transfer = new WalletTransfer(rpc, external, sessions);
  const before = await rpc.getBalance({ address: A });
  const maximum = await transfer.maximum(B);
  const gas = await rpc.estimateGas({ account: A, to: B, value: maximum });
  const price = await rpc.getGasPrice();
  expect(maximum).toBeGreaterThan(0n);
  expect(maximum + gas * price).toBeLessThanOrEqual(before);
  expect(transfer.hash).toBeUndefined();
  await expect(transfer.maximum('0x0000000000000000000000000000000000000000')).rejects.toThrow('zero');
});
