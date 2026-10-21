import { describe, expect, test } from 'vitest';
import { validateAdapterData, decodeDeployedOrder, abiFields, abiUint, abiBoolean, abiHex, decodeLimitOrder, advancedCapabilities, encodeCancelAttempt, decodeCancelAttempt, capability, exactUnits, validateOrder, reconcileOrder, eligibility, canCancel, cancellationOutcome, routeNetOutput, selectBestRoute, validateCustomRoute, positionView, orderPageSize, type Address, type OrderView, type OrderEvidence, type OrderRules, type OrderDraft, type Hop } from './domain';
// Synthetic arithmetic/projection fixtures only; never deployment or receipt proof.
const A: Address = '0x1111111111111111111111111111111111111111';
const B: Address = '0x2222222222222222222222222222222222222222';
const C: Address = '0x3333333333333333333333333333333333333333';
const D: Address = '0x4444444444444444444444444444444444444444';
const ID = `0x${'ab'.repeat(32)}`;
const TX = `0x${'cd'.repeat(32)}`;
const rules: OrderRules = { supportedKinds: ['limit-buy', 'limit-sell', 'stop-loss', 'stop-limit-buy', 'stop-limit-sell'], priceScale: 18, denomination: B, priceFresh: true, maxLifetimeSeconds: 86400n, stopLimitRelation: 'buy-limit-gte-stop-sell-limit-lte-stop' };
const draft: OrderDraft = { kind: 'limit-buy', owner: A, token: C, settlementAsset: B, amount: '1.000001', amountDecimals: 6, limitPrice: '0.25', expiresAt: 200n };
const order: OrderView = { orderId: 'one', owner: A, deploymentRevision: 'r1', eventRevision: 1n, state: 'active', filledRaw: 0n, refundRaw: 0n };
const fill: OrderEvidence = { ...order, eventRevision: 2n, state: 'filled', filledRaw: 100n, executionTxHash: TX, emitter: D, committed: true, transferVerified: true };
const adapter = { id: ID, address: D, active: true };
const context = { revision: 'r1', tokenIn: A, tokenOut: C, amountInRaw: 100n, recipient: B };
const quote = { ...context, adapterId: ID, adapterAddress: D, outputRaw: 120n, outputBasis: 'gross' as const, feeRaw: 5n, feeAsset: C, expiresAt: 1000 };
const hop: Hop = { adapterId: ID, adapterAddress: D, tokenIn: A, tokenOut: B, minAmountOut: 100n, inputDecimals: 6, outputDecimals: 18 };
describe('capability truth', () => {
 test('absent and EOA are blocked', () => {
  expect(capability(null).state).toBe('blocked');
  expect(capability({ revision: 'r1', chainId: 125, address: A, bytecode: '0x', admitted: true, abiCompatible: true, proxyCompatible: true })).toEqual({ state: 'blocked', reason: 'Missing contract bytecode (EOA)' });
 });
 test('wrong chain, ABI and admission fail closed', () => {
  const proof = { revision: 'r1', chainId: 125, address: A, bytecode: '0x6000', admitted: true, abiCompatible: true, proxyCompatible: true };
  expect(capability(proof).state).toBe('supported');
  for (const change of [{ chainId: 1 }, { admitted: false }, { abiCompatible: false }, { proxyCompatible: false }]) expect(capability({ ...proof, ...change }).state).toBe('blocked');
 });
});
describe('exact order validation', () => {
 test('USDL6 and price18 exact without floating point', () => {
  const result = validateOrder(draft, rules, 100n);
  expect(result.amountRaw).toBe(1000001n); expect(result.limitPriceRaw).toBe(250000000000000000n);
 });
 test.each(['1e6', '-1', '0', '1.0000001', ' 1', '01', '1.'])('rejects malformed/overprecision amount %s', amount => expect(() => exactUnits(amount, 6)).toThrow());
 test('unknown decimals, overflow, stale price and unsupported variants block signing', () => {
  expect(() => exactUnits('1', undefined)).toThrow(); expect(() => exactUnits('9'.repeat(79), 0)).toThrow();
  expect(() => validateOrder(draft, { ...rules, priceFresh: false }, 100n)).toThrow();
  expect(() => validateOrder(draft, { ...rules, supportedKinds: [] }, 100n)).toThrow();
 });
 test('future expiry and explicit price fields', () => {
  for (const expiresAt of [100n, 10000000n, (1n << 64n)]) expect(() => validateOrder({ ...draft, expiresAt }, rules, 100n)).toThrow();
  expect(() => validateOrder({ ...draft, triggerPrice: '0.2' }, rules, 100n)).toThrow();
  expect(() => validateOrder({ ...draft, settlementAsset: A }, rules, 100n)).toThrow();
 });
 test('stop limit sides enforce deployed relationship', () => {
  expect(() => validateOrder({ ...draft, kind: 'stop-limit-buy', triggerPrice: '0.3' }, rules, 100n)).toThrow();
  expect(validateOrder({ ...draft, kind: 'stop-limit-sell', triggerPrice: '0.3' }, rules, 100n).triggerPriceRaw).toBe(300000000000000000n);
 });
});
describe('revisioned settlement and cancel races', () => {
 test('eligible/degraded does not fill, committed fill deduplicates', () => {
  expect(eligibility(true, 10, false, 20)).toMatchObject({ eligibility: 'eligible', executionHealth: 'degraded' });
  expect(order.filledRaw).toBe(0n);
  const filled = reconcileOrder(order, fill, D);
  expect(reconcileOrder(filled, fill, D)).toBe(filled);
 });
 test('forged emitter, owner, revision context and absent transfer cannot fill', () => {
  for (const change of [{ emitter: A }, { owner: B }, { committed: false }, { transferVerified: false }, { deploymentRevision: 'r2' }, { executionTxHash: undefined }]) expect(() => reconcileOrder(order, { ...fill, ...change }, D)).toThrow();
 });
 test('terminal conflicts and fabricated refunds rejected', () => {
  expect(() => reconcileOrder(fill, { ...fill, eventRevision: 3n, state: 'cancelled', filledRaw: 0n }, D)).toThrow();
  expect(() => reconcileOrder(order, { ...fill, refundRaw: 1n }, D)).toThrow();
 });
 test('owner and pending hash prevent duplicate cancel; fill wins without refund', () => {
  const attempt = { orderId: 'one', owner: A, preStateRevision: 1n, txHash: TX, receiptStatus: 'unknown' as const };
  expect(canCancel(order, A)).toBe(true); expect(canCancel(order, B)).toBe(false); expect(canCancel(order, A, attempt)).toBe(false);
  expect(cancellationOutcome(attempt, fill).state).toBe('checking');
  expect(cancellationOutcome({ ...attempt, receiptStatus: 'reverted' }, fill)).toEqual({ state: 'filled_race', refundRaw: 0n });
  const cancelled = reconcileOrder(order, { ...fill, state: 'cancelled', filledRaw: 0n, refundRaw: 123n }, D);
  expect(cancellationOutcome({ ...attempt, receiptStatus: 'success' }, cancelled)).toEqual({ state: 'cancelled', refundRaw: 123n });
 });
});
describe('registered routes and positions', () => {
 test('best net output excludes stale input and deactivated adapter', () => {
  const better = { ...quote, outputRaw: 125n, feeRaw: 20n };
  expect(selectBestRoute([quote, better], context, [adapter], 100)).toBe(quote);
  expect(selectBestRoute([quote], context, [{ ...adapter, active: false }], 100)).toBeNull();
  for (const change of [{ revision: 'r2' }, { recipient: A }, { amountInRaw: 101n }, { expiresAt: 100 }, { adapterAddress: C }]) expect(selectBestRoute([{ ...quote, ...change }], context, [adapter], 100)).toBeNull();
 });
 test('multihop continuity, cycles, decimals and minimums', () => {
  const second = { ...hop, tokenIn: B, tokenOut: C, inputDecimals: 18, outputDecimals: 6 };
  const review = validateCustomRoute([hop, second], [adapter], 100n, 90n, 200n, 100n);
  expect(validateCustomRoute([hop, { ...second, minAmountOut: 101n }], [adapter], 100n, 90n, 200n, 100n)).not.toBe(review);
  for (const broken of [{ ...second, tokenIn: D }, { ...second, tokenOut: A }, { ...second, inputDecimals: undefined }, { ...second, minAmountOut: 0n }]) expect(() => validateCustomRoute([hop, broken], [adapter], 100n, 90n, 200n, 100n)).toThrow();
  expect(() => validateCustomRoute(Array(5).fill(hop), [adapter], 100n, 90n, 200n, 100n)).toThrow();
  expect(() => validateCustomRoute([hop], [adapter], 100n, 101n, 200n, 100n)).toThrow();
 });
 test('zero shares never own reserves, unsupported or paused never withdraw', () => {
  expect(positionView(0n, 0n, true, false, false).canWithdraw).toBe(false);
  expect(() => positionView(0n, 999999n, true, true, false)).toThrow();
  expect(positionView(10n, 20n, true, true, true)).toMatchObject({ ownedSharesRaw: 10n, redeemableRaw: 0n, canWithdraw: false });
 });
 test('bounded order pagination', () => { expect(orderPageSize()).toBe(25); expect(() => orderPageSize(101)).toThrow(); });
});

test('deployment revision and keeper changes invalidate advanced capabilities', () => {
 const proof = { revision: 'r1', chainId: 125, address: A, bytecode: '0x6000', admitted: true, abiCompatible: true, proxyCompatible: true };
 const proofs = { orders: proof, vault: null, routeAdapters: null, liquidityActions: null };
 expect(advancedCapabilities(proofs, 'r1', { healthy: true, checkedAt: 100 }, 110, rules).orders.state).toBe('supported');
 expect(advancedCapabilities(proofs, 'r2', { healthy: true, checkedAt: 100 }, 110, rules).orders.state).toBe('blocked');
 expect(advancedCapabilities(proofs, 'r1', { healthy: true, checkedAt: 100 }, 31000, rules).orders.state).toBe('blocked');
});
test('durable cancellation roundtrip resumes same hash and rejects account contamination', () => {
 const attempt = { orderId: 'one', owner: A, preStateRevision: 1n, txHash: TX, receiptStatus: 'unknown' as const };
 const raw = encodeCancelAttempt(attempt);
 expect(decodeCancelAttempt(raw, A, 'one')).toEqual(attempt);
 expect(() => decodeCancelAttempt(raw, B, 'one')).toThrow();
 expect(() => decodeCancelAttempt(raw, A, 'two')).toThrow();
 expect(() => decodeCancelAttempt(raw.replace('125', '1'), A, 'one')).toThrow();
});

test('ABI tuple decoder rejects missing fields and unknown enums rather than zero prices', () => {
 const tuple = [1n, A, B, C, 1000001n, 250000000000000000n, 0, 0, 100n, 200n];
 expect(decodeLimitOrder(tuple)).toMatchObject({ amountRaw: 1000001n, type: 'limit-buy', state: 'active' });
 expect(() => decodeLimitOrder(tuple.slice(0, -1))).toThrow();
 expect(() => decodeLimitOrder({ id: 1n })).toThrow();
 const bad = [...tuple]; bad[7] = 99;
 expect(() => decodeLimitOrder(bad)).toThrow();
});

test('same revision requires identical canonical content; older updates remain ignored', () => {
 const filled = reconcileOrder(order, fill, D);
 for (const change of [{ state: 'cancelled' as const }, { filledRaw: 101n }, { refundRaw: 1n }, { executionTxHash: `0x${'ef'.repeat(32)}` }]) expect(() => reconcileOrder(filled, { ...fill, ...change }, D)).toThrow('Conflicting payload');
 expect(reconcileOrder(filled, fill, D)).toBe(filled);
 expect(reconcileOrder(filled, { ...fill, eventRevision: 1n, filledRaw: 200n }, D)).toBe(filled);
});
test('triggered regression requires explicit admitted transition rule', () => {
 const triggered = { ...order, state: 'triggered' as const };
 const active = { ...fill, state: 'active' as const, filledRaw: 0n, executionTxHash: undefined };
 expect(() => reconcileOrder(triggered, active, D)).toThrow('Unsupported triggered-to-active');
 expect(reconcileOrder(triggered, active, D, { allowTriggeredToActive: true }).state).toBe('active');
});
test('net output is never charged twice; gross fees require the output asset', () => {
 const net = { ...quote, outputBasis: 'net' as const, outputRaw: 116n, feeRaw: 1000n, feeAsset: A };
 expect(routeNetOutput(net)).toBe(116n);
 expect(routeNetOutput(quote)).toBe(115n);
 expect(selectBestRoute([quote, net], context, [adapter], 100)).toBe(net);
 expect(() => routeNetOutput({ ...quote, feeAsset: A })).toThrow('output-denominated fee');
 expect(() => selectBestRoute([{ ...quote, feeAsset: A }], context, [adapter], 100)).toThrow();
 expect(() => routeNetOutput({ ...quote, feeRaw: 120n })).toThrow();
 expect(() => routeNetOutput({ ...quote, outputBasis: undefined as never })).toThrow();
});


describe('adversarial deployed ABI boundaries', () => {
 test('accepts exact named and tuple shapes without coercion', () => {
  const tuple = [1n, A, B, C, 100n, 250n, 0, 2, 100n, 200n];
  expect(decodeLimitOrder(tuple).state).toBe('filled');
  expect(decodeLimitOrder(Object.fromEntries(['id','user','stablecoin','token','amount','targetPrice','orderType','status','createdAt','expiresAt'].map((name, index) => [name, tuple[index]])))).toEqual(decodeLimitOrder(tuple));
  for (const status of [0,2,3,4]) expect(decodeLimitOrder([...tuple.slice(0,7),status,...tuple.slice(8)]).state).toBe(['active','triggered','filled','cancelled','expired'][status]);
  expect(() => decodeLimitOrder([...tuple.slice(0,7),1,...tuple.slice(8)])).toThrow('state');
 });
 test('rejects sparse, inherited, extra, coerced and overflow fields', () => {
  const tuple = [1n, A, B, C, 100n, 250n, 0, 0, 100n, 200n];
  for (const value of [undefined,null,{},Object.create({ id: 1n }),[...tuple,0n]]) expect(() => decodeLimitOrder(value)).toThrow();
  for (const value of [undefined,null,'250',250,-1n,1n << 256n,0n]) { const broken = [...tuple]; broken[5] = value as never; expect(() => decodeLimitOrder(broken)).toThrow(); }
  const sparse = [...tuple]; delete sparse[5]; expect(() => decodeLimitOrder(sparse)).toThrow();
  for (const value of [0,1,'false',null]) expect(() => abiBoolean(value)).toThrow();
  for (const value of ['1',1,true]) expect(() => abiUint(value)).toThrow();
  expect(() => abiHex('0xabc')).toThrow(); expect(() => abiHex('0xab',32)).toThrow();
  expect(() => abiFields(Object.create({ price: 1n }), ['price'])).toThrow();
 });
 test('stop family requires exact stop prices and actual contract enums', () => {
  const tuple = [1n,A,B,C,100n,200n,250n,3,1,100n,200n];
  expect(decodeDeployedOrder(tuple,'stop')).toMatchObject({ family: 'stop', orderType: 3, status: 1, stopPrice: 200n, limitPrice: 250n });
  for (const index of [5,6,7,8]) { const broken = [...tuple]; broken[index] = undefined as never; expect(() => decodeDeployedOrder(broken,'stop')).toThrow(); }
  const limitFamily = [...tuple]; limitFamily[7] = 0; expect(() => decodeDeployedOrder(limitFamily,'stop')).toThrow();
 });
});
test('expired refund is verified separately and rejected cancellation can recover without a new event', () => {
 const expired = reconcileOrder(order, { ...fill, state: 'expired', filledRaw: 0n, executionTxHash: undefined, refundRaw: 100n }, D);
 expect(expired).toMatchObject({ state: 'expired', refundRaw: 100n });
 expect(() => reconcileOrder(order, { ...fill, state: 'expired', filledRaw: 0n, refundRaw: 100n, transferVerified: false }, D)).toThrow();
 const attempt = { orderId: 'one', owner: A, preStateRevision: 1n, txHash: TX, receiptStatus: 'reverted' as const };
 expect(cancellationOutcome(attempt, order)).toEqual({ state: 'cancel_failed', refundRaw: 0n });
 expect(cancellationOutcome({ ...attempt, receiptStatus: 'unknown' }, order)).toEqual({ state: 'checking', refundRaw: 0n });
});
test('unavailable liquidity preserves verified shares without promising proceeds', () => {
 expect(positionView(100n,0n,true,true,false)).toEqual({ ownedSharesRaw: 100n, redeemableRaw: 0n, canWithdraw: false, reason: 'Insufficient redeemable liquidity; position ownership retained' });
 expect(positionView(100n,200n,true,true,true)).toMatchObject({ ownedSharesRaw: 100n, canWithdraw: false, reason: 'Vault paused' });
 expect(() => positionView(100n,200n,false,true,false)).toThrow();
});

test('adapter data rejects malicious mode, address, arbitrary fragments and asset mismatches', () => {
 const vaultData = `0x${A.slice(2).padStart(64,'0')}${C.slice(2).padStart(64,'0')}`, sidioraData = `0x${'0'.repeat(64)}${D.slice(2).padStart(64,'0')}${'0'.repeat(64)}`;
 expect(validateAdapterData('vault-v2',vaultData,[A,C],A,C,B)).toMatchObject({ feeAsset: C });
 expect(validateAdapterData('sidiora-v1',sidioraData,[0,D,'0x0000000000000000000000000000000000000000'],B,C,B)).toMatchObject({ feeAsset: B, pools: [D] });
 expect(() => validateAdapterData('vault-v2',vaultData,[A,B],A,C,B)).toThrow();
 expect(() => validateAdapterData('vault-v2',`${vaultData}00`,[A,C],A,C,B)).toThrow();
 for (const fields of [[99,D,A],[0,D,A],[2,D,D],[1,D,'0x0000000000000000000000000000000000000000'],[0,'0x0000000000000000000000000000000000000000','0x0000000000000000000000000000000000000000']]) expect(() => validateAdapterData('sidiora-v1',sidioraData,fields,B,C,B)).toThrow();
 expect(() => validateAdapterData('sidiora-v1','0x1234',[0,D,A],B,C,B)).toThrow();
});
