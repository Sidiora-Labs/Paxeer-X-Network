/** Advanced-domain primitives. Inputs here must come from the serially admitted
 * chain125 deployment/receipt adapters; these models do not attest a deployment. */
export type Address = `0x${string}`;
export type OrderKind = 'limit-buy' | 'limit-sell' | 'stop-loss' | 'stop-limit-buy' | 'stop-limit-sell';
const UINT256 = (1n << 256n) - 1n;
export function address(value: string): Address {
  if (typeof value !== 'string' || !/^0x[\da-fA-F]{40}$/.test(value) || /^0x0{40}$/i.test(value)) throw new Error('Invalid or unavailable address');
  return value.toLowerCase() as Address;
}
export function uint(value: bigint, positive = false): bigint {
  if (typeof value !== 'bigint' || value < (positive ? 1n : 0n) || value > UINT256) throw new Error('Invalid uint256');
  return value;
}
export function exactUnits(value: string, decimals: number | undefined): bigint {
  if (!Number.isInteger(decimals) || decimals! < 0 || decimals! > 36) throw new Error('Verified decimals required');
  if (value.length > 116 || !/^(0|[1-9]\d*)(\.\d+)?$/.test(value)) throw new Error('Invalid decimal amount');
  const [whole, fraction = ''] = value.split('.');
  if (fraction.length > decimals!) throw new Error('Precision exceeds deployed scale');
  return uint(BigInt(whole) * 10n ** BigInt(decimals!) + BigInt(fraction.padEnd(decimals!, '0') || '0'), true);
}
export interface DeploymentProof {
  revision: string; chainId: number; address: Address; bytecode: string;
  abiCompatible: boolean; proxyCompatible: boolean; admitted: boolean;
}
export type CapabilityState = { state: 'blocked'; reason: string } | { state: 'supported'; revision: string; address: Address };
export function capability(proof: DeploymentProof | null): CapabilityState {
  if (!proof) return { state: 'blocked', reason: 'No accepted optional deployment manifest' };
  try { address(proof.address); } catch { return { state: 'blocked', reason: 'Invalid deployment address' }; }
  if (proof.chainId !== 125 || !proof.revision || !proof.admitted) return { state: 'blocked', reason: 'Deployment not admitted for chain125' };
  if (!/^0x(?:[\da-fA-F]{2})+$/.test(proof.bytecode)) return { state: 'blocked', reason: 'Missing contract bytecode (EOA)' };
  if (!proof.abiCompatible || !proof.proxyCompatible) return { state: 'blocked', reason: 'Unsupported ABI or proxy implementation' };
  return { state: 'supported', revision: proof.revision, address: address(proof.address) };
}
export interface OrderRules {
  supportedKinds: readonly OrderKind[]; priceScale: number; denomination: Address;
  priceFresh: boolean; maxLifetimeSeconds: bigint; gtcSentinel?: bigint;
  stopLimitRelation: 'buy-limit-gte-stop-sell-limit-lte-stop';
}
export interface OrderDraft {
  kind: OrderKind; owner: Address; token: Address; settlementAsset: Address;
  amount: string; amountDecimals?: number; triggerPrice?: string; limitPrice?: string; expiresAt: bigint;
}
export interface OrderIntent {
  type: OrderKind; chainId: 125; owner: Address; token: Address; settlementAsset: Address;
  amountRaw: bigint; triggerPriceRaw?: bigint; limitPriceRaw?: bigint; priceScale: number; expiresAt: bigint;
}
export function validateOrder(draft: OrderDraft, rules: OrderRules, nowSeconds: bigint): OrderIntent {
  if (!rules.supportedKinds.includes(draft.kind)) throw new Error('Unsupported order variant');
  if (!rules.priceFresh) throw new Error('Required price source stale');
  if (address(draft.settlementAsset) !== address(rules.denomination)) throw new Error('Wrong price denomination');
  if (address(draft.token) === address(draft.settlementAsset)) throw new Error('Identical order assets');
  uint(nowSeconds); uint(draft.expiresAt);
  if (draft.expiresAt !== rules.gtcSentinel && (draft.expiresAt <= nowSeconds || draft.expiresAt - nowSeconds > rules.maxLifetimeSeconds)) throw new Error('Expiry outside supported future interval');
  if (draft.expiresAt > (1n << 64n) - 1n) throw new Error('Expiry exceeds uint64');
  const stop = draft.kind.startsWith('stop');
  const limit = draft.kind !== 'stop-loss';
  if (stop !== (draft.triggerPrice !== undefined) || limit !== (draft.limitPrice !== undefined)) throw new Error('Incorrect price fields for variant');
  const triggerPriceRaw = stop ? exactUnits(draft.triggerPrice!, rules.priceScale) : undefined;
  const limitPriceRaw = limit ? exactUnits(draft.limitPrice!, rules.priceScale) : undefined;
  if (draft.kind.startsWith('stop-limit')) {
    if (rules.stopLimitRelation !== 'buy-limit-gte-stop-sell-limit-lte-stop') throw new Error('Unknown stop-limit semantics');
    if (draft.kind === 'stop-limit-buy' ? limitPriceRaw! < triggerPriceRaw! : limitPriceRaw! > triggerPriceRaw!) throw new Error('Invalid stop-limit relationship');
  }
  return { type: draft.kind, chainId: 125, owner: address(draft.owner), token: address(draft.token), settlementAsset: address(draft.settlementAsset), amountRaw: exactUnits(draft.amount, draft.amountDecimals), triggerPriceRaw, limitPriceRaw, priceScale: rules.priceScale, expiresAt: draft.expiresAt };
}
export type OrderState = 'active' | 'triggered' | 'filled' | 'cancelled' | 'expired' | 'failed_execution';
export interface OrderView {
  orderId: string; owner: Address; deploymentRevision: string; eventRevision: bigint; state: OrderState;
  filledRaw: bigint; executionTxHash?: string; refundRaw: bigint;
}
export interface OrderEvidence extends OrderView { emitter: Address; committed: boolean; transferVerified: boolean }
const terminal = (state: OrderState) => ['filled', 'cancelled', 'expired'].includes(state);
export function reconcileOrder(current: OrderView, next: OrderEvidence, expectedEmitter: Address, rules: { allowTriggeredToActive: boolean } = { allowTriggeredToActive: false }): OrderView {
  if (!next.committed || address(next.emitter) !== address(expectedEmitter) || next.orderId !== current.orderId || address(next.owner) !== address(current.owner) || next.deploymentRevision !== current.deploymentRevision) throw new Error('Untrusted order evidence');
  if (!['active', 'triggered', 'filled', 'cancelled', 'expired', 'failed_execution'].includes(next.state)) throw new Error('Unknown order state');
  uint(next.eventRevision); uint(next.filledRaw); uint(next.refundRaw);
  if (next.eventRevision < current.eventRevision) return current;
  if (next.eventRevision === current.eventRevision) {
    if (next.state !== current.state || next.filledRaw !== current.filledRaw || next.refundRaw !== current.refundRaw || next.executionTxHash?.toLowerCase() !== current.executionTxHash?.toLowerCase()) throw new Error('Conflicting payload for canonical order revision');
    return current;
  }
  if (current.state === 'triggered' && next.state === 'active' && !rules.allowTriggeredToActive) throw new Error('Unsupported triggered-to-active regression');
  if (terminal(current.state)) throw new Error('Conflicting terminal order revision');
  if (next.state === 'filled' && (!/^0x[\da-fA-F]{64}$/.test(next.executionTxHash ?? '') || !next.transferVerified || next.filledRaw === 0n || next.refundRaw !== 0n)) throw new Error('Fill settlement evidence missing');
  if (next.state !== 'filled' && next.filledRaw !== 0n) throw new Error('Unfilled order cannot claim output');
  if (next.refundRaw && (!['cancelled', 'expired'].includes(next.state) || !next.transferVerified)) throw new Error('Refund evidence missing');
  return { orderId: next.orderId, owner: address(next.owner), deploymentRevision: next.deploymentRevision, eventRevision: next.eventRevision, state: next.state, filledRaw: next.filledRaw, executionTxHash: next.executionTxHash, refundRaw: next.refundRaw };
}
export function eligibility(eligible: boolean | null, keeperCheckedAt: number, keeperHealthy: boolean, now: number) {
  return { eligibility: eligible === null ? 'unknown' : eligible ? 'eligible' : 'ineligible', executionHealth: keeperHealthy && now >= keeperCheckedAt && now - keeperCheckedAt <= 30_000 ? 'healthy' : 'degraded', lastCheckedAt: keeperCheckedAt } as const;
}
export interface CancelAttempt { orderId: string; owner: Address; preStateRevision: bigint; txHash: string; receiptStatus: 'unknown' | 'success' | 'reverted' }
export function canCancel(order: OrderView, owner: Address, pending?: CancelAttempt): boolean {
  return address(order.owner) === address(owner) && ['active', 'triggered'].includes(order.state) && !pending;
}
export function cancellationOutcome(attempt: CancelAttempt, order: OrderView) {
  if (attempt.orderId !== order.orderId || address(attempt.owner) !== address(order.owner)) throw new Error('Cancellation identity mismatch');
  if (attempt.receiptStatus === 'unknown') return { state: 'checking', refundRaw: 0n };
  if (order.eventRevision <= attempt.preStateRevision) return { state: attempt.receiptStatus === 'reverted' && ['active', 'triggered'].includes(order.state) ? 'cancel_failed' : 'checking', refundRaw: 0n };
  if (order.state === 'filled') return { state: 'filled_race', refundRaw: 0n };
  if (attempt.receiptStatus === 'success' && order.state === 'cancelled') return { state: 'cancelled', refundRaw: order.refundRaw };
  return { state: 'cancel_failed', refundRaw: 0n };
}
export interface Adapter { id: string; address: Address; active: boolean }
export interface RouteContext { revision: string; tokenIn: Address; tokenOut: Address; amountInRaw: bigint; recipient: Address }
export interface RouteQuote extends RouteContext { adapterId: string; adapterAddress: Address; outputRaw: bigint; outputBasis: 'net' | 'gross'; feeRaw: bigint; feeAsset: Address; expiresAt: number }
function registered(id: string, target: Address, adapters: readonly Adapter[]) {
  return /^0x[\da-fA-F]{64}$/.test(id) && adapters.some(a => a.active && a.id.toLowerCase() === id.toLowerCase() && address(a.address) === address(target));
}
/** Net quotes (including MetaAGBestQuote.amountOut) already include fees.
 * Gross quotes may subtract fees only in the exact output asset denomination. */
export function routeNetOutput(quote: RouteQuote): bigint {
  uint(quote.outputRaw, true); uint(quote.feeRaw); address(quote.feeAsset);
  if (quote.outputBasis === 'net') return quote.outputRaw;
  if (quote.outputBasis !== 'gross' || address(quote.feeAsset) !== address(quote.tokenOut)) throw new Error('Gross quote requires output-denominated fee');
  if (quote.feeRaw >= quote.outputRaw) throw new Error('Nonpositive net quote output');
  return quote.outputRaw - quote.feeRaw;
}
export function selectBestRoute(quotes: readonly RouteQuote[], context: RouteContext, adapters: readonly Adapter[], now: number): RouteQuote | null {
  if (quotes.length > 64 || adapters.length > 64) throw new Error('Route comparison bound exceeded');
  uint(context.amountInRaw, true); address(context.recipient);
  const valid = quotes.filter(q => q.revision === context.revision && address(q.tokenIn) === address(context.tokenIn) && address(q.tokenOut) === address(context.tokenOut) && address(q.recipient) === address(context.recipient) && q.amountInRaw === context.amountInRaw && Number.isFinite(q.expiresAt) && q.expiresAt > now && registered(q.adapterId, q.adapterAddress, adapters) && routeNetOutput(q) > 0n);
  return valid.reduce<RouteQuote | null>((best, q) => !best || routeNetOutput(q) > routeNetOutput(best) ? q : best, null);
}
export interface Hop { adapterId: string; adapterAddress: Address; tokenIn: Address; tokenOut: Address; minAmountOut: bigint; inputDecimals?: number; outputDecimals?: number }
export function validateCustomRoute(hops: readonly Hop[], adapters: readonly Adapter[], inputRaw: bigint, finalMinimumRaw: bigint, deadline: bigint, nowSeconds: bigint): string {
  if (hops.length < 1 || hops.length > 4) throw new Error('Route requires one through four hops');
  uint(inputRaw, true); uint(finalMinimumRaw, true); uint(deadline);
  if (deadline <= nowSeconds || deadline - nowSeconds > 1800n) throw new Error('Route deadline outside 30 minutes');
  const seen = new Set<string>();
  hops.forEach((hop, index) => {
    const from = address(hop.tokenIn), to = address(hop.tokenOut);
    if (!registered(hop.adapterId, hop.adapterAddress, adapters)) throw new Error('Unsupported adapter');
    if (index && address(hops[index - 1].tokenOut) !== from) throw new Error('Disconnected route');
    if (!index) seen.add(from);
    if (seen.has(to)) throw new Error('Cyclic route');
    seen.add(to); uint(hop.minAmountOut, true);
    for (const decimals of [hop.inputDecimals, hop.outputDecimals]) if (!Number.isInteger(decimals) || decimals! < 0 || decimals! > 36) throw new Error('Verified intermediate decimals required');
    if (index && hops[index - 1].outputDecimals !== hop.inputDecimals) throw new Error('Intermediate decimal mismatch');
  });
  if (hops[hops.length - 1].minAmountOut < finalMinimumRaw) throw new Error('Final hop minimum below aggregate minimum');
  // Canonical review identity; any hop/context edit invalidates prior review.
  return JSON.stringify({ hops, inputRaw, finalMinimumRaw, deadline }, (_, value) => typeof value === 'bigint' ? value.toString() : value);
}
export function positionView(ownedSharesRaw: bigint, redeemableRaw: bigint, ownershipVerified: boolean, redemptionSupported: boolean, paused: boolean) {
  uint(ownedSharesRaw); uint(redeemableRaw);
  if (!ownershipVerified || (!ownedSharesRaw && redeemableRaw)) throw new Error('Position ownership evidence missing');
  return { ownedSharesRaw, redeemableRaw: redemptionSupported && !paused ? redeemableRaw : 0n, canWithdraw: redemptionSupported && !paused && ownedSharesRaw > 0n && redeemableRaw > 0n, reason: !redemptionSupported ? 'Redemption unsupported' : paused ? 'Vault paused' : ownedSharesRaw > 0n && redeemableRaw === 0n ? 'Insufficient redeemable liquidity; position ownership retained' : null };
}
export function orderPageSize(value = 25): number {
  if (!Number.isInteger(value) || value < 1 || value > 100) throw new Error('Order page size must be 1..100');
  return value;
}
export interface AdvancedCapabilities {
  manifestRevision: string | null;
  orders: CapabilityState; vault: CapabilityState; routeAdapters: CapabilityState; liquidityActions: CapabilityState;
  executionHealth: ReturnType<typeof eligibility>['executionHealth'];
  supportedPriceSemantics: OrderRules | null;
}
export function advancedCapabilities(proofs: { orders: DeploymentProof | null; vault: DeploymentProof | null; routeAdapters: DeploymentProof | null; liquidityActions: DeploymentProof | null }, manifestRevision: string | null, execution: { healthy: boolean; checkedAt: number }, now: number, semantics: OrderRules | null): AdvancedCapabilities {
  const probe = (proof: DeploymentProof | null) => capability(proof && proof.revision === manifestRevision ? proof : null);
  const orders = probe(proofs.orders);
  const health = eligibility(null, execution.checkedAt, execution.healthy, now).executionHealth;
  return { manifestRevision, orders: orders.state === 'blocked' || (health === 'healthy' && semantics) ? orders : { state: 'blocked', reason: !semantics ? 'Unsupported deployed price semantics' : 'Execution service unavailable' }, vault: probe(proofs.vault), routeAdapters: probe(proofs.routeAdapters), liquidityActions: probe(proofs.liquidityActions), executionHealth: health, supportedPriceSemantics: orders.state === 'supported' ? semantics : null };
}
/** Portable durable attempt record for the L26 persistence adapter. This does
 * not issue a replacement transaction when a receipt is unresolved. */
export function encodeCancelAttempt(attempt: CancelAttempt): string {
  address(attempt.owner); uint(attempt.preStateRevision);
  if (!attempt.orderId || attempt.orderId.length > 128 || !/^0x[\da-fA-F]{64}$/.test(attempt.txHash) || !['unknown', 'success', 'reverted'].includes(attempt.receiptStatus)) throw new Error('Invalid cancellation recovery record');
  return JSON.stringify({ ...attempt, preStateRevision: attempt.preStateRevision.toString(), version: 1, chainId: 125 });
}
export function decodeCancelAttempt(raw: string, owner: Address, orderId: string): CancelAttempt {
  if (raw.length > 1024) throw new Error('Cancellation recovery record too large');
  const record = JSON.parse(raw);
  if (record.version !== 1 || record.chainId !== 125 || record.orderId !== orderId || address(record.owner) !== address(owner) || typeof record.preStateRevision !== 'string' || !/^(0|[1-9]\d{0,77})$/.test(record.preStateRevision)) throw new Error('Cancellation recovery context mismatch');
  const attempt: CancelAttempt = { orderId, owner: address(owner), preStateRevision: BigInt(record.preStateRevision), txHash: record.txHash, receiptStatus: record.receiptStatus };
  encodeCancelAttempt(attempt);
  return attempt;
}
/** Reporting contract only. Coordinator owns functional/resilience/capacity
 * qualification and verifies artifacts; a client record never grants admission. */
export interface AdvancedQualification {
  stage: 'source_implemented' | 'functional_verified' | 'resilience_verified' | 'capacity_verified' | 'blocked';
  manifestHash: string | null;
  workloadMix: Readonly<Record<'pagination' | 'eligibility' | 'quotes' | 'cancels' | 'approvals' | 'accountSwitches', number>>;
  quoteRate: number; orderWriteRate: number; socketCount: number;
  latencyHistograms: Readonly<Record<string, readonly number[]>>;
  reconciliationErrors: number; evidenceArtifacts: readonly string[];
}
export const ADVANCED_WORKLOAD_LIMITS = Object.freeze({
  orderPageDefault: 25, orderPageMaximum: 100, capabilityHealthMaxAgeMs: 30_000,
  quoteFanoutMaximum: 8, quoteDeadlineMs: 750, hopsMaximum: 4,
  routeEditDebounceMs: 300, activeSimulationsPerForm: 1, subscriptionsPerSocketMaximum: 20,
});
export function abiFields(value: unknown, names: readonly string[]): unknown[] {
  if (!value || typeof value !== 'object') throw new Error('Malformed deployed ABI output');
  if (Array.isArray(value)) {
    if (value.length !== names.length || names.some((_, index) => !Object.hasOwn(value, index))) throw new Error('Incomplete deployed ABI tuple');
    return value;
  }
  const record = value as Record<string, unknown>;
  if (names.some(name => !Object.hasOwn(record, name) || record[name] === undefined)) throw new Error('Incomplete named deployed ABI output');
  return names.map(name => record[name]);
}
export function abiUint(value: unknown, positive = false): bigint {
  if (typeof value !== 'bigint') throw new Error('Malformed ABI uint256');
  return uint(value, positive);
}
export function abiBoolean(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new Error('Malformed ABI boolean');
  return value;
}
export function abiHex(value: unknown, bytes?: number): Address {
  if (typeof value !== 'string' || !/^0x(?:[\da-fA-F]{2})*$/.test(value) || bytes !== undefined && value.length !== 2 + bytes * 2) throw new Error('Malformed ABI bytes');
  return value as Address;
}
export function decodeDeployedOrder(value: unknown, family: 'limit' | 'stop') {
  const names = family === 'limit'
    ? ['id', 'user', 'stablecoin', 'token', 'amount', 'targetPrice', 'orderType', 'status', 'createdAt', 'expiresAt']
    : ['id', 'user', 'stablecoin', 'token', 'amount', 'stopPrice', 'limitPrice', 'orderType', 'status', 'createdAt', 'expiresAt'];
  const fields = abiFields(value, names);
  const record = Object.fromEntries(names.map((name, index) => [name, fields[index]]));
  const orderType = record.orderType, status = record.status;
  if (typeof orderType !== 'number' || !Number.isInteger(orderType) || !(family === 'limit' ? [0, 1, 2] : [3, 4]).includes(orderType)) throw new Error('Unsupported deployed order enum');
  if (typeof status !== 'number' || !Number.isInteger(status) || ![0, 1, 2, 3, 4].includes(status) || family === 'limit' && status === 1) throw new Error('Unsupported deployed order state');
  const user = address(record.user as string), stablecoin = address(record.stablecoin as string), token = address(record.token as string);
  if (stablecoin === token) throw new Error('Identical deployed order assets');
  const common = { id: abiUint(record.id, true), user, stablecoin, token, amount: abiUint(record.amount, true), orderType, status, createdAt: abiUint(record.createdAt), expiresAt: abiUint(record.expiresAt) };
  if (common.expiresAt !== 0n && common.expiresAt <= common.createdAt) throw new Error('Invalid deployed order expiry');
  return family === 'limit'
    ? { ...common, family: 'limit' as const, targetPrice: abiUint(record.targetPrice, true) }
    : { ...common, family: 'stop' as const, stopPrice: abiUint(record.stopPrice, true), limitPrice: abiUint(record.limitPrice, true) };
}
export function decodeLimitOrder(value: unknown) {
  const raw = decodeDeployedOrder(value, 'limit');
  if (raw.family !== 'limit') throw new Error('Wrong deployed order family');
  return { id: raw.id, owner: raw.user, settlementAsset: raw.stablecoin, token: raw.token, amountRaw: raw.amount, targetPriceRaw: raw.targetPrice, type: (['limit-buy', 'limit-sell', 'stop-loss'] as const)[raw.orderType], state: (['active', 'triggered', 'filled', 'cancelled', 'expired'] as const)[raw.status], createdAt: raw.createdAt, expiresAt: raw.expiresAt };
}

export function validateAdapterData(schema: 'vault-v2' | 'sidiora-v1', data: string, fields: readonly unknown[], tokenIn: Address, tokenOut: Address, usdl: Address) {
  abiHex(data); const input = address(tokenIn), output = address(tokenOut), settlement = address(usdl);
  if (input === output) throw new Error('Identical adapter assets');
  if (schema === 'vault-v2') {
    if (data.length !== 130 || fields.length !== 2 || address(fields[0] as string) !== input || address(fields[1] as string) !== output) throw new Error('Vault adapter quote data mismatch');
    if (data.toLowerCase() !== `0x${input.slice(2).padStart(64,'0')}${output.slice(2).padStart(64,'0')}`) throw new Error('Noncanonical vault adapter data');
    return { feeAsset: output, pools: [] as Address[] };
  }
  if (schema !== 'sidiora-v1' || data.length !== 194 || fields.length !== 3) throw new Error('Unknown or malformed admitted adapter data schema');
  const [mode, first, second] = fields;
  if (typeof mode !== 'number' || !Number.isInteger(mode) || mode < 0 || mode > 2 || typeof second !== 'string') throw new Error('Sidiora adapter quote data mismatch');
  const pool = address(first as string);
  if (mode === 2 ? address(second) === pool : !/^0x0{40}$/i.test(second)) throw new Error('Sidiora adapter pool data mismatch');
  if (!/^0x[\da-fA-F]{40}$/.test(second) || data.toLowerCase() !== `0x${mode.toString(16).padStart(64,'0')}${pool.slice(2).padStart(64,'0')}${second.toLowerCase().slice(2).padStart(64,'0')}`) throw new Error('Noncanonical Sidiora adapter data');
  if (mode !== (input === settlement ? 0 : output === settlement ? 1 : 2)) throw new Error('Sidiora adapter mode/assets mismatch');
  return { feeAsset: mode === 2 ? settlement : input, pools: mode === 2 ? [pool,address(second)] : [pool] };
}
