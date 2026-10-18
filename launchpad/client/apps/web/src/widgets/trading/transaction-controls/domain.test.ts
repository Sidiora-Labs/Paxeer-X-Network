import { createRequire } from 'node:module';
import { PendingAttemptStore } from './recovery';
import { describe, it, expect } from 'vitest';
import { decodeFunctionData, formatUnits } from 'viem';
import { parseAmount, UINT256_MAX, contextKey, executableQuote, QuoteRequest, minimumOutput, tradeLimits, attemptState, approvalPlan, approvalMayContinue, CompletionLedger, serializePending, restorePending, replacementState, simulationAllowed, type TradeContext, type QuoteEnvelope, quoteFingerprint, approvalStepFingerprint, type PendingAttemptRecord, buildReviewedRouterTransaction, reviewedRouterAbi } from './domain';
const addr = '0x' + '11'.repeat(20), other = '0x' + '22'.repeat(20), tx = '0x' + '33'.repeat(32);
const c: TradeContext = { chainId: 125, manifestRevision: 'frozen', pool: addr, token: addr, router: addr, sender: addr, recipient: addr, inputRaw: 1000000n, isBuy: true, opticalRevision: '1', opticalData: '0x' };
const q: QuoteEnvelope = { quoteId: 'q1', context: c, blockHeight: 1n, quotedAt: 1000, expiresAt: 10000, outputRaw: 1000001n, feeComponents: [1n], impactBps: 100 };
const a: PendingAttemptRecord = { version: 1, intentId: 'i', chainId: 125, from: addr, txHash: tx, submittedAt: 1000, nonce: 1, action: 'buy', actionFingerprint: tx };
const receipt = { transactionHash: tx, status: 'success' as const, blockNumber: 2n };
describe('synthetic domain fixtures (not receipt evidence)', () => {
  it('round trips bounded uint256 at 0,6,18 decimals', () => { for (const d of [0, 6, 18]) for (const n of [0n, 1n, 1000001n, UINT256_MAX, ...Array.from({ length: 40 }, (_, i) => (UINT256_MAX / 41n) * BigInt(i))]) expect(parseAmount(formatUnits(n, d), d).parsedRaw).toBe(n); });
  it('rejects unsafe inputs without throwing', () => { for (const s of ['-1', '1e6', 'NaN', 'Infinity', ' ', '1.0000001', '9'.repeat(81), (UINT256_MAX + 1n).toString()]) expect(parseAmount(s, 6).errorCode).toBeTruthy(); expect(parseAmount('1.000001', 6).parsedRaw).toBe(1000001n); expect(parseAmount('', 6).parsedRaw).toBeUndefined(); expect(parseAmount('1', 6, 0n).errorCode).toBe('insufficient_balance'); });
  it('discards late quote and binds recipient/route/optical context', async () => { const r = new QuoteRequest(); let resolve!: (q: QuoteEnvelope) => void; const pending = r.request(c, () => new Promise(done => resolve = done), () => 2000); r.cancel(); resolve(q); expect(await pending).toBeUndefined(); for (const change of [{ inputRaw: 2n }, { recipient: other }, { sender: other }, { router: other }, { opticalRevision: '2' }]) expect(executableQuote(q, { ...c, ...change }, 2000)).toBe(false); expect(executableQuote(q, c, 10000)).toBe(false); });
  it('floors minimum and requires separate current-quote consents', () => { expect(minimumOutput(1000001n, 100)).toBe(990000n); for (const b of [0, 5001, 1.5]) expect(() => minimumOutput(1n, b)).toThrow(); expect(() => minimumOutput(1n, 100)).toThrow('zero_minimum'); expect(tradeLimits(q, c, 100, 2).deadlineUnix).toBeGreaterThan(0); expect(() => tradeLimits({ ...q, impactBps: 1001 }, c, 600, 2, 120, { quoteFingerprint: quoteFingerprint({ ...q, impactBps: 1001 }), slippageBps: 600, slippage: true, impact: false })).toThrow('impact'); });
  it('time cannot settle; mismatch rejected and revert terminal', () => { expect(attemptState(a, 61000)).toBe('delayed'); expect(attemptState(a, 3000)).toBe('checking'); expect(attemptState(a, 61000, { ...receipt, status: 'reverted' })).toBe('confirmed_reverted'); expect(() => attemptState(a, 61000, { ...receipt, transactionHash: '0x' + '44'.repeat(32) })).toThrow(); });
  it('exact approvals reset and bind step transaction and fresh allowance evidence', () => {
    const plan = approvalPlan(c, addr, 1n, true); expect(plan.steps).toEqual([0n, 1000000n]);
    const step = approvalStepFingerprint(plan, 0); const attempt = { ...a, action: 'approve', actionFingerprint: step };
    const allowance = { chainId: 125 as const, account: addr, token: addr, spender: addr, allowanceRaw: 0n, blockHeight: 2n, observedAt: 2000, approvalTxHash: tx, stepFingerprint: step };
    expect(approvalMayContinue(plan, c, attempt, undefined, allowance, 0, 2000)).toBe(false);
    expect(approvalMayContinue(plan, { ...c, sender: other }, attempt, receipt, allowance, 0, 2000)).toBe(false);
    expect(approvalMayContinue(plan, c, attempt, receipt, allowance, 0, 2000)).toBe(true);
    expect(approvalMayContinue(plan, c, a, receipt, allowance, 0, 2000)).toBe(false);
    for (const change of [{ blockHeight: 1n }, { observedAt: -10000 }, { spender: other }, { approvalTxHash: '0x' + '44'.repeat(32) }, { stepFingerprint: 'wrong' }]) expect(approvalMayContinue(plan, c, attempt, receipt, { ...allowance, ...change }, 0, 2000)).toBe(false);
    expect(approvalMayContinue(plan, c, attempt, receipt, allowance, 1, 2000)).toBe(false);
    expect(() => approvalPlan(c, other, 0n, false)).toThrow('spender');
  });
  it('immutable approval and quote snapshots cannot follow caller mutation', async () => {
    const mutable = { ...c }; const plan = approvalPlan(mutable, addr, 1n, true); mutable.recipient = other;
    expect(plan.context.recipient).toBe(addr); expect(Object.isFrozen(plan.context)).toBe(true); expect(Object.isFrozen(plan.steps)).toBe(true);
    const requested = { ...c }; let resolve!: (q: QuoteEnvelope) => void; const pending = new QuoteRequest().request(requested, () => new Promise(done => resolve = done), () => 2000);
    requested.inputRaw = 2n; resolve({ ...q, context: requested }); expect(await pending).toBeUndefined();
  });
  it('consent rejects changed same-ID quote and stale quote', () => {
    const risk = { ...q, impactBps: 1100 }; const consent = { quoteFingerprint: quoteFingerprint(risk), slippageBps: 600, slippage: true, impact: true };
    expect(tradeLimits(risk, c, 600, 2, 120, consent).minimumOutputRaw).toBe(940000n);
    for (const change of [{ outputRaw: 1000000n }, { feeComponents: [2n] }, { context: { ...c, recipient: other } }]) expect(() => tradeLimits({ ...risk, ...change }, ({ ...risk, ...change }).context, 600, 2, 120, consent)).toThrow();
    expect(() => tradeLimits(risk, c, 600, 11, 120, consent)).toThrow('stale');
  });
  it('completion is once and failed swaps never complete', () => { const ledger = new CompletionLedger(); expect(ledger.claim(a, { ...receipt, status: 'reverted' })).toBe(false); expect([1, 2, 3].map(() => ledger.claim(a, receipt))).toEqual([true, false, false]); expect(ledger.claim({ ...a, action: 'sell', actionFingerprint: 'mutated' }, receipt)).toBe(false); });
  it('persists whitelist only, scopes and retains 200/30days; corrupt isolated', () => { const input = { ...a, secret: 'never persist' }; const text = serializePending([input], addr, 2000); expect(text).not.toContain('secret'); expect(restorePending(text, other, 2000).records).toEqual([]); expect(restorePending('{', addr, 2000).corrupt).toBe(true); expect(restorePending(text, addr, 31 * 86400000).records).toEqual([]); expect(JSON.parse(serializePending(Array(205).fill(a), addr, 2000))).toHaveLength(200); });
  it('replacement and optical simulation require exact identity', () => { expect(replacementState(a, { ...a, txHash: '0x' + '44'.repeat(32) }, true)).toBe('replacement_validated'); expect(replacementState(a, { ...a, actionFingerprint: 'cancel' }, true)).toBe('action_changed'); expect(() => replacementState(a, { ...a, nonce: 2 }, true)).toThrow(); const simulation = { allowed: true, contextKey: contextKey(c), quoteFingerprint: quoteFingerprint(q), blockHeight: 1n, simulatedAt: 1000, expiresAt: 9000, revision: '1', restrictions: ['max-wallet'] }; expect(simulationAllowed(c, simulation, q, 2000)).toBe(true); for (const change of [{ contextKey: contextKey({ ...c, recipient: other }) }, { blockHeight: 0n }, { expiresAt: 1500 }, { quoteFingerprint: 'wrong' }, { simulatedAt: 3000 }]) expect(simulationAllowed(c, { ...simulation, ...change }, q, 2000)).toBe(false); });
});

it('actual browser storage persists scoped recovery records and isolates corruption', () => {
  const { JSDOM } = createRequire(import.meta.url)('jsdom') as { JSDOM: new (html: string, options: { url: string; }) => { window: { localStorage: Storage; close(): void; }; }; };
  const dom = new JSDOM('', { url: 'http://localhost:3050' });
  try {
    const store = new PendingAttemptStore(dom.window.localStorage, addr);
    store.save(a, 2000);
    const restored = new PendingAttemptStore(dom.window.localStorage, addr);
    expect(restored.load(2000).records).toEqual([a]);
    expect(new PendingAttemptStore(dom.window.localStorage, other).load(2000).records).toEqual([]);
    expect(() => store.save({ ...a, from: other }, 2000)).toThrow('scope');
    expect(() => store.save({ ...a, nonce: -1 }, 2000)).toThrow('invalid_pending');
    dom.window.localStorage.setItem(store.key, JSON.stringify([a, { version: 9 }]));
    expect(store.load(2000)).toEqual({ records: [a], corrupt: true });
    expect(() => store.save(a, 2000)).toThrow('corrupt');
    store.clear();
    for (let i = 0; i < 30; i++) store.save({ ...a, txHash: '0x' + i.toString(16).padStart(64, '0') }, 2000);
    expect(store.page(0, 2000).records).toHaveLength(25);
    expect(store.page(25, 2000).records).toHaveLength(5);
    expect(store.load(31 * 86400000).records).toHaveLength(0);
  } finally { dom.window.close(); }
});


it('rejects adversarial bigint, timing and optical fields before execution', () => {
  for (const outputRaw of [1, '1000000', -1n, UINT256_MAX + 1n]) {
    expect(executableQuote({ ...q, outputRaw } as QuoteEnvelope, c, 2000)).toBe(false);
  }
  for (const change of [{ blockHeight: 1 }, { quotedAt: NaN }, { expiresAt: Infinity }, { feeComponents: [1] }, { impactBps: Number.MAX_SAFE_INTEGER + 1 }, { quoteId: '' }]) {
    expect(executableQuote({ ...q, ...change } as QuoteEnvelope, c, 2000)).toBe(false);
  }
  expect(() => contextKey({ ...c, inputRaw: 1 as unknown as bigint })).toThrow('invalid_uint256');
  expect(() => contextKey({ ...c, opticalData: '0x' + 'ff'.repeat(4097) })).toThrow('invalid_optical_payload');
  expect(() => contextKey({ ...c, opticalData: '<script>sign()</script>' })).toThrow('invalid_optical_payload');
  expect(() => tradeLimits(q, c, 100, NaN)).toThrow();
  expect(() => minimumOutput(1 as unknown as bigint, 100)).toThrow('invalid_uint256');
  expect(() => serializePending([{ ...a, nonce: -1 }], addr, 2000)).toThrow('invalid_pending_nonce');
  expect(restorePending(JSON.stringify([{ ...a, submittedAt: -1 }]), addr, 2000).corrupt).toBe(true);
  expect(restorePending(JSON.stringify([{ ...a, actionFingerprint: null }]), addr, 2000).corrupt).toBe(true);
});

it('isolates duplicate and malformed records in real browser storage', () => {
  const { JSDOM } = createRequire(import.meta.url)('jsdom');
  const dom = new JSDOM('', { url: 'https://recovery.local' });
  try {
    const store = new PendingAttemptStore(dom.window.localStorage, addr);
    dom.window.localStorage.setItem(store.key, JSON.stringify([a, a, { ...a, txHash: 'bad' }, { ...a, submittedAt: -1 }]));
    expect(store.load(2000)).toEqual({ records: [a], corrupt: true });
    expect(new PendingAttemptStore(dom.window.localStorage, other).load(2000).records).toEqual([]);
    const text = serializePending([{ ...a, authToken: 'private', signature: 'private', privateKey: 'private', recoverySecret: 'private' } as PendingAttemptRecord], addr, 2000);
    for (const secret of ['authToken', 'signature', 'privateKey', 'recoverySecret', 'private']) expect(text).not.toContain(secret);
  } finally { dom.window.close(); }
});


it('signs only the reviewed frozen router action and rejects unsupported execution contexts', () => {
  const authority = { chainId: 125 as const, manifestRevision: c.manifestRevision, router: addr, pool: addr, inputAsset: addr, recipientMode: 'same_sender' as const };
  const untrustedResponse = { ...q, calldata: '0xdeadbeef', target: other, signature: 'untrusted' };
  const reviewed = buildReviewedRouterTransaction(c, untrustedResponse, authority, 100, 2);
  expect(reviewed.to.toLowerCase()).toBe(addr);
  expect(reviewed.data).not.toBe(untrustedResponse.calldata);
  expect(decodeFunctionData({ abi: reviewedRouterAbi, data: reviewed.data })).toEqual({ functionName: 'buy', args: [addr, 1000000n, 990000n, 122n] });
  for (const context of [{ ...c, recipient: other }, { ...c, opticalData: '0x12' }]) {
    expect(() => buildReviewedRouterTransaction(context, { ...q, context }, authority, 100, 2)).toThrow();
  }
  expect(() => buildReviewedRouterTransaction(c, q, { ...authority, router: other }, 100, 2)).toThrow('execution_authority_mismatch');
});
