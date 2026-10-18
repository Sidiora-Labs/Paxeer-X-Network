import { encodeFunctionData,getAddress,parseAbi,keccak256, stringToHex } from 'viem';
/** Phase-one trade invariants. No wallet writes or assumed settlement. */
export const UINT256_MAX = (1n << 256n) - 1n;
export type AmountInput = { displayText: string; decimals?: number; parsedRaw?: bigint; errorCode?: string };
export function parseAmount(displayText: string, decimals?: number, balance?: bigint): AmountInput {
  const result: AmountInput = { displayText, decimals };
  const fail = (errorCode: string) => ({ ...result, errorCode });
  if (displayText === '') return result;
  if (decimals === undefined) return fail('awaiting_asset_metadata');
  if (!Number.isInteger(decimals) || decimals < 0 || decimals > 77) return fail('invalid_decimals');
  if (displayText.length > 80) return fail('too_long');
  if (!/^\d+(?:\.\d+)?$/.test(displayText)) return fail('invalid_decimal');
  const [whole, fraction = ''] = displayText.split('.');
  if (fraction.length > decimals) return fail('excess_precision');
  const parsedRaw = BigInt(whole + fraction.padEnd(decimals, '0'));
  if (parsedRaw > UINT256_MAX) return fail('uint256_overflow');
  if (balance !== undefined && parsedRaw > balance) return fail('insufficient_balance');
  return { ...result, parsedRaw };
}
function requireThat(ok: unknown, message: string): asserts ok { if (!ok) throw new Error(message); }
function address(value: string) { requireThat(/^0x[0-9a-fA-F]{40}$/.test(value), 'invalid_address'); return value.toLowerCase(); }
function hash(value: string) { requireThat(/^0x[0-9a-fA-F]{64}$/.test(value), 'invalid_hash'); return value.toLowerCase(); }
export function raw(value: bigint) { requireThat(typeof value === 'bigint' && value >= 0n && value <= UINT256_MAX, 'invalid_uint256'); return value; }
export class EvidenceIntegrityError extends Error { constructor(reason: string) { super(reason); this.name = 'EvidenceIntegrityError'; } }
export interface TradeContext {
  chainId: 125; manifestRevision: string; pool: string; token: string; router: string;
  sender: string; recipient: string; inputRaw: bigint; isBuy: boolean;
  opticalRevision: string; opticalData: string;
}
export function contextKey(c: TradeContext): string {
  requireThat(typeof c.isBuy === 'boolean' && typeof c.opticalRevision === 'string', 'invalid_trade_context');
  requireThat(c.chainId === 125 && typeof c.manifestRevision === 'string' && c.manifestRevision.length > 0 && c.manifestRevision.length <= 128, 'invalid_deployment');
  requireThat(/^0x(?:[0-9a-fA-F]{2})*$/.test(c.opticalData) && c.opticalData.length <= 8194, 'invalid_optical_payload');
  requireThat(c.opticalRevision.length <= 128, 'invalid_optical_revision');
  return JSON.stringify([125, c.manifestRevision, address(c.pool), address(c.token), address(c.router), address(c.sender), address(c.recipient), raw(c.inputRaw).toString(), c.isBuy, c.opticalRevision, c.opticalData.toLowerCase()]);
}
function fingerprint(value: string): string { return keccak256(stringToHex(value)); }
function snapshot(context: TradeContext): Readonly<TradeContext> { contextKey(context); return Object.freeze({ ...context }); }
export interface QuoteEnvelope { quoteId: string; context: TradeContext; blockHeight: bigint; quotedAt: number; expiresAt: number; outputRaw: bigint; feeComponents: readonly bigint[]; impactBps: number }
export function executableQuote(q: QuoteEnvelope, current: TradeContext, now: number): boolean {
  try { return typeof q.quoteId === 'string' && q.quoteId.length > 0 && q.quoteId.length <= 256 && Number.isSafeInteger(now) && Number.isSafeInteger(q.quotedAt) && q.quotedAt >= 0 && Number.isSafeInteger(q.expiresAt) && Array.isArray(q.feeComponents) && q.feeComponents.length <= 32 && contextKey(q.context) === contextKey(current) && q.context.inputRaw > 0n && raw(q.outputRaw) > 0n && raw(q.blockHeight) >= 0n && q.quotedAt <= now && now < q.expiresAt && q.expiresAt - q.quotedAt <= 120000 && q.feeComponents.every(v => raw(v) >= 0n) && Number.isSafeInteger(q.impactBps) && q.impactBps >= 0; } catch { return false; }
}
/** Cancels previous reads; a late response cannot become the current quote. */
export class QuoteRequest {
  private epoch = 0; private controller?: AbortController;
  cancel() { this.epoch++; this.controller?.abort(); }
  async request(context: TradeContext, read: (signal: AbortSignal) => Promise<QuoteEnvelope>, now = Date.now): Promise<QuoteEnvelope | undefined> {
    const requested = snapshot(context); const requestedKey = contextKey(requested);
    this.cancel(); const epoch = this.epoch; this.controller = new AbortController();
    const q = await read(this.controller.signal);
    return epoch === this.epoch && contextKey(context) === requestedKey && executableQuote(q, requested, now()) ? Object.freeze({ ...q, context: snapshot(q.context), feeComponents: Object.freeze([...q.feeComponents]) }) : undefined;
  }
}
export function minimumOutput(output: bigint, bps: number): bigint {
  raw(output); requireThat(Number.isInteger(bps) && bps >= 1 && bps <= 5000, 'invalid_slippage');
  const minimum = output * BigInt(10000 - bps) / 10000n;
  requireThat(output === 0n || minimum > 0n, 'zero_minimum'); return minimum;
}
export function quoteFingerprint(q: QuoteEnvelope): string {
  return fingerprint(JSON.stringify([q.quoteId, contextKey(q.context), q.blockHeight.toString(), q.quotedAt, q.expiresAt, q.outputRaw.toString(), q.feeComponents.map(String), q.impactBps]));
}
export interface RiskConsent { quoteFingerprint: string; slippageBps: number; slippage: boolean; impact: boolean }
export function tradeLimits(q: QuoteEnvelope, current: TradeContext, bps = 100, nowUnix = Math.floor(Date.now()/1000), ttl = 120, consent?: RiskConsent) {
  requireThat(executableQuote(q, current, nowUnix * 1000), 'stale_or_mismatched_quote');
  requireThat(Number.isSafeInteger(nowUnix) && nowUnix >= 0, 'invalid_submission_time');
  requireThat(Number.isInteger(ttl) && ttl >= 30 && ttl <= 600, 'invalid_deadline');
  const binding = quoteFingerprint(q);
  requireThat((bps <= 500 && q.impactBps <= 1000) || (consent?.quoteFingerprint === binding && consent.slippageBps === bps), 'quote_consent_required');
  requireThat(bps <= 500 || consent?.slippage, 'slippage_consent_required');
  requireThat(q.impactBps <= 1000 || consent?.impact, 'impact_consent_required');
  return Object.freeze({ quoteId: q.quoteId, quoteFingerprint: binding, minimumOutputRaw: minimumOutput(q.outputRaw, bps), slippageBps: bps, deadlineUnix: nowUnix + ttl });
}
export interface TxAttempt { intentId: string; chainId: 125; from: string; txHash: string; submittedAt: number; nonce?: number; action: string; actionFingerprint: string }
export interface ReceiptEvidence { transactionHash: string; status: 'success' | 'reverted'; blockNumber: bigint }
export function attemptState(a: TxAttempt, now: number, receipt?: ReceiptEvidence) {
  requireThat(a.chainId === 125, 'invalid_chain');
  requireThat(Number.isSafeInteger(a.submittedAt) && a.submittedAt >= 0 && Number.isSafeInteger(now) && now >= a.submittedAt, 'invalid_attempt_time');
  if (receipt) { requireThat(receipt.status === 'success' || receipt.status === 'reverted', 'invalid_receipt_status'); requireThat(hash(receipt.transactionHash) === hash(a.txHash), 'receipt_hash_mismatch'); requireThat(raw(receipt.blockNumber) >= 0n, 'invalid_receipt_height'); return receipt.status === 'success' ? 'confirmed_success' : 'confirmed_reverted'; }
  return now - a.submittedAt >= 30000 ? 'delayed' : now - a.submittedAt >= 2000 ? 'checking' : 'submitted';
}
export interface ApprovalPlan { context: Readonly<TradeContext>; contextFingerprint: string; currentAllowanceRaw: bigint; targetAllowanceRaw: bigint; steps: readonly bigint[] }
export function approvalPlan(context: TradeContext, verifiedRouter: string, allowance: bigint, resetRequired: boolean, unlimited = false): ApprovalPlan {
  requireThat(address(context.router) === address(verifiedRouter), 'unverified_spender'); contextKey(context); raw(allowance);
  const target = unlimited ? UINT256_MAX : raw(context.inputRaw); requireThat(target > 0n, 'zero_approval');
  return Object.freeze({ context: snapshot(context), contextFingerprint: fingerprint(contextKey(context)), currentAllowanceRaw: allowance, targetAllowanceRaw: target, steps: Object.freeze(allowance >= target ? [] : resetRequired && allowance > 0n ? [0n, target] : [target]) });
}
export function approvalStepFingerprint(plan: ApprovalPlan, stepIndex: number): string {
  requireThat(Number.isInteger(stepIndex) && stepIndex >= 0 && stepIndex < plan.steps.length, 'invalid_approval_step');
  return fingerprint(JSON.stringify([plan.contextFingerprint, stepIndex, address(plan.context.token), address(plan.context.router), plan.steps[stepIndex].toString()]));
}
export interface AllowanceEvidence { chainId: 125; account: string; token: string; spender: string; allowanceRaw: bigint; blockHeight: bigint; observedAt: number; approvalTxHash: string; stepFingerprint: string }
export function approvalMayContinue(plan: ApprovalPlan, current: TradeContext, attempt: TxAttempt, receipt: ReceiptEvidence | undefined, allowance: AllowanceEvidence, stepIndex: number, now = Date.now()): boolean {
  try {
    const step = approvalStepFingerprint(plan, stepIndex);
    return fingerprint(contextKey(current)) === plan.contextFingerprint && fingerprint(contextKey(plan.context)) === plan.contextFingerprint &&
      address(attempt.from) === address(current.sender) && attempt.action === 'approve' && attempt.actionFingerprint === step &&
      !!receipt && attemptState(attempt, now, receipt) === 'confirmed_success' && allowance.chainId === 125 &&
      address(allowance.account) === address(current.sender) && address(allowance.token) === address(current.token) &&
      address(allowance.spender) === address(current.router) && allowance.stepFingerprint === step &&
      hash(allowance.approvalTxHash) === hash(attempt.txHash) && allowance.blockHeight >= receipt.blockNumber &&
      allowance.observedAt >= attempt.submittedAt && allowance.observedAt <= now && now - allowance.observedAt <= 10000 &&
      raw(allowance.allowanceRaw) === plan.steps[stepIndex];
  } catch { return false; }
}
export class CompletionLedger {
  private seen = new Set<string>();
  private transactions = new Set<string>();
  claim(a: TxAttempt, receipt: ReceiptEvidence): boolean {
    if (attemptState(a, Date.now(), receipt) !== 'confirmed_success') return false;
    const transaction = `${a.chainId}:${hash(a.txHash)}`;
    const key = `${transaction}:${a.action}`;
    if (this.seen.has(key) || this.transactions.has(transaction)) return false;
    requireThat(this.seen.size < 200, 'completion_ledger_full'); this.seen.add(key); this.transactions.add(transaction); return true;
  }
}
export interface PendingAttemptRecord extends TxAttempt { version: 1; replacementHash?: string }
export function serializePending(records: PendingAttemptRecord[], account: string, now: number): string {
  const owner = address(account);
  requireThat(Number.isSafeInteger(now) && now >= 0, 'invalid_storage_time');
  for (const record of records) {
    requireThat(record && record.version === 1 && typeof record.intentId === 'string' && record.intentId.length > 0 && record.intentId.length <= 128 && typeof record.action === 'string' && record.action.length > 0 && record.action.length <= 64 && typeof record.actionFingerprint === 'string' && record.actionFingerprint.length > 0 && record.actionFingerprint.length <= 256, 'invalid_pending_record');
    address(record.from); hash(record.txHash);
    requireThat(record.chainId === 125 && Number.isSafeInteger(record.submittedAt) && record.submittedAt >= 0 && record.submittedAt <= now, 'invalid_pending_record');
    requireThat(record.nonce === undefined || (Number.isSafeInteger(record.nonce) && record.nonce >= 0), 'invalid_pending_nonce');
    if (record.replacementHash !== undefined) hash(record.replacementHash);
  }
  return JSON.stringify(records.filter(a => address(a.from) === owner && a.chainId === 125 && Number.isSafeInteger(a.submittedAt) && a.submittedAt <= now && now-a.submittedAt <= 30*86400000).sort((a,b)=>b.submittedAt-a.submittedAt).slice(0,200).map(a => {
    hash(a.txHash); if (a.replacementHash) hash(a.replacementHash);
    requireThat(a.intentId.length <= 128 && a.action.length <= 64 && a.actionFingerprint.length <= 256, 'record_too_large');
    return { version: 1, intentId: a.intentId, chainId: 125, from: owner, txHash: a.txHash, submittedAt: a.submittedAt, nonce: a.nonce, action: a.action, actionFingerprint: a.actionFingerprint, replacementHash: a.replacementHash };
  }));
}
export function restorePending(text: string, account: string, now: number): { records: PendingAttemptRecord[]; corrupt: boolean } {
  try { requireThat(text.length <= 300000, 'storage_too_large'); const data = JSON.parse(text); requireThat(Array.isArray(data) && data.length <= 200, 'invalid_records'); requireThat(data.every(a => a?.version === 1), 'invalid_version'); return { records: JSON.parse(serializePending(data,account,now)), corrupt: false }; } catch { return { records: [], corrupt: true }; }
}
export function replacementState(original: TxAttempt, replacement: TxAttempt, verifiedSenderNonce: boolean) {
  requireThat(verifiedSenderNonce && original.chainId === replacement.chainId && address(original.from) === address(replacement.from) && original.nonce !== undefined && original.nonce === replacement.nonce, 'unverified_replacement');
  hash(replacement.txHash); return original.actionFingerprint === replacement.actionFingerprint ? 'replacement_validated' : 'action_changed';
}
export interface OpticalSimulation { contextKey: string; quoteFingerprint: string; blockHeight: bigint; simulatedAt: number; expiresAt: number; allowed: boolean; revision: string; reason?: string; restrictions: readonly string[] }
export function simulationAllowed(context: TradeContext, result: OpticalSimulation, quote: QuoteEnvelope, now = Date.now()): boolean {
  try { return executableQuote(quote, context, now) && result.allowed && result.contextKey === contextKey(context) && result.quoteFingerprint === quoteFingerprint(quote) && result.revision === context.opticalRevision && raw(result.blockHeight) >= quote.blockHeight && Number.isSafeInteger(result.simulatedAt) && Number.isSafeInteger(result.expiresAt) && result.simulatedAt >= quote.quotedAt && result.simulatedAt <= now && now < result.expiresAt && result.expiresAt <= quote.expiresAt && result.expiresAt - result.simulatedAt <= 10000; } catch { return false; }
}

export const reviewedRouterAbi=parseAbi(['function buy(address pool,uint256 usdlAmountIn,uint256 minTokensOut,uint256 deadline) returns(uint256)','function sell(address pool,uint256 tokenAmountIn,uint256 minUsdlOut,uint256 deadline) returns(uint256)']);
export interface RouterExecutionAuthority {
  chainId:125;manifestRevision:string;router:string;pool:string;inputAsset:string;recipientMode:'same_sender';
}
export function encodeReviewedRouterCalldata(context:TradeContext,authority:RouterExecutionAuthority,minimum:bigint,deadline:bigint){
  contextKey(context);
  requireThat(authority.chainId===125&&authority.manifestRevision===context.manifestRevision&&address(authority.router)===address(context.router)&&address(authority.pool)===address(context.pool)&&address(authority.inputAsset)===address(context.token),'execution_authority_mismatch');
  requireThat(authority.recipientMode==='same_sender'&&address(context.sender)===address(context.recipient),'recipient_route_unavailable');
  requireThat(context.opticalData==='0x','optical_payload_route_unavailable');
  requireThat(raw(context.inputRaw)>0n&&raw(minimum)>0n&&raw(deadline)>0n,'invalid_execution_amount_or_deadline');
  return encodeFunctionData({abi:reviewedRouterAbi,functionName:context.isBuy?'buy':'sell',args:[getAddress(context.pool),context.inputRaw,minimum,deadline]});
}
export function buildReviewedRouterTransaction(context:TradeContext,quote:QuoteEnvelope,authority:RouterExecutionAuthority,slippageBps=100,nowUnix=Math.floor(Date.now()/1000),ttl=120,consent?:RiskConsent){
  const limits=tradeLimits(quote,context,slippageBps,nowUnix,ttl,consent);
  const data=encodeReviewedRouterCalldata(context,authority,limits.minimumOutputRaw,BigInt(limits.deadlineUnix));
  return Object.freeze({chainId:125 as const,account:getAddress(context.sender),to:getAddress(context.router),data,value:0n,minimumOutputRaw:limits.minimumOutputRaw,deadlineUnix:limits.deadlineUnix,quoteFingerprint:limits.quoteFingerprint,actionFingerprint:fingerprint(JSON.stringify([125,address(context.router),data.toLowerCase(),'0']))});
}
